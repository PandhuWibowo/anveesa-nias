package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/anveesa/nias/cache"
	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
	"github.com/anveesa/nias/handlers"
	mw "github.com/anveesa/nias/middleware"
	"github.com/joho/godotenv"
)

var (
	version   = "0.1.0"
	buildTime = "unknown"
	startTime time.Time
)

func main() {
	startTime = time.Now()

	// Load .env file from parent directory (if exists)
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("No .env file found in parent directory, using environment variables")
	} else {
		log.Println("✓ Loaded configuration from .env")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Printf("Configuration error: %v", err)
		return
	}

	// Print config in non-production
	if !cfg.IsProduction() {
		cfg.PrintConfig()
	}

	cache.Init(cfg)
	defer func() {
		if err := cache.Close(); err != nil {
			log.Printf("Cache close error: %v", err)
		}
	}()

	// Initialize database
	if err := appdb.Init(cfg); err != nil {
		log.Printf("Database init failed: %v", err)
		return
	}
	switch cfg.DBDriver {
	case "sqlite":
		log.Printf("Database initialized: SQLite")
	case "postgres":
		log.Printf("Database initialized: PostgreSQL")
	case "mysql":
		log.Printf("Database initialized: MySQL/MariaDB")
	}

	// Set JWT secret
	handlers.SetJWTSecret(cfg.JWTSecret)

	// Set encryption key for credentials
	handlers.SetEncryptionKey(cfg.EncryptionKey)

	// Seed global AI defaults from env/config (UI-saved values take priority at runtime)
	handlers.SetGlobalAIConfig(cfg.AIAPIKey, cfg.AIBaseURL, cfg.AIModel)

	// Start automatic backup if enabled
	backupCtx, backupCancel := context.WithCancel(context.Background())
	defer backupCancel()
	if cfg.BackupEnabled {
		go startAutoBackup(backupCtx, cfg)
	}
	handlers.StartNotificationWorker()

	// Create router
	mux := http.NewServeMux()
	registerRoutes(mux, cfg)

	// Apply middleware stack
	var handler http.Handler = mux
	handler = mw.EnforceMFASetup(handler)
	handler = mw.InjectUserContext(cfg.JWTSecret)(handler) // Extract JWT claims and set headers
	handler = mw.CORS(cfg.CORSOrigin)(handler)
	handler = mw.SecurityHeaders(handler)
	handler = mw.Recovery(handler)

	// Add rate limiting in production
	mw.ConfigureLoginRateLimiter(mw.NewRateLimiter(5, time.Minute, cache.Default(), "login"))
	if cfg.RateLimitEnabled {
		rl := mw.NewRateLimiter(cfg.RateLimitRequests, time.Duration(cfg.RateLimitWindow)*time.Second, cache.Default(), "http")
		handler = mw.RateLimit(rl)(handler)
	}

	// Configure server
	server := &http.Server{
		Addr:              cfg.Host + ":" + cfg.Port,
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	// Start server
	serverErr := make(chan error, 1)
	go func() {
		var err error
		if cfg.TLSEnabled {
			log.Printf("Anveesa Nias server listening on https://%s:%s", cfg.Host, cfg.Port)
			err = server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			log.Printf("Anveesa Nias server listening on http://%s:%s", cfg.Host, cfg.Port)
			if cfg.IsProduction() {
				log.Println("WARNING: Running without TLS in production!")
			}
			err = server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	var shutdownReason string
	select {
	case sig := <-quit:
		shutdownReason = "signal " + sig.String()
	case err := <-serverErr:
		log.Printf("Server error: %v", err)
		shutdownReason = "server error"
	}

	log.Printf("Shutting down due to %s...", shutdownReason)

	// Give active connections 60 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Stop accepting new requests and force-close if the grace window expires.
	shutdownErr := server.Shutdown(ctx)
	if shutdownErr != nil {
		log.Printf("Server shutdown error: %v", shutdownErr)
		if closeErr := server.Close(); closeErr != nil && closeErr != http.ErrServerClosed {
			log.Printf("Server force close error: %v", closeErr)
		}
	}

	// Stop background jobs
	handlers.StopScheduler()
	handlers.StopNotificationWorker()

	// Close database
	if err := appdb.Close(); err != nil {
		log.Printf("Database close error: %v", err)
	}

	if shutdownErr != nil {
		log.Println("Server stopped after forced close")
	} else {
		log.Println("Server stopped gracefully")
	}
}

func registerRoutes(mux *http.ServeMux, cfg *config.Config) {
	requireAny := mw.RequireAnyAppPermissionHeader

	// ── Health & Info ────────────────────────────────────────────
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler)
	mux.HandleFunc("/version", versionHandler)

	// ── Auth (with rate limiting for sensitive endpoints) ─────────
	mux.HandleFunc("/api/auth/setup", handlers.SetupHandler(cfg))
	mux.HandleFunc("/api/auth/login", mw.RateLimitLogin(handlers.LoginHandler(cfg)))
	mux.HandleFunc("/api/auth/register", mw.RateLimitLogin(requireAny(handlers.PermUsersManage)(handlers.RegisterHandler(cfg))))
	mux.HandleFunc("/api/auth/me", handlers.MeHandler())
	mux.HandleFunc("/api/auth/logout", handlers.LogoutHandler())
	mux.HandleFunc("/api/auth/password/change", requireAny(handlers.PermSecuritySelf)(handlers.ChangePasswordHandler()))
	mux.HandleFunc("/api/auth/sessions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermSecuritySelf)(handlers.ListSessionsHandler())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/auth/sessions/revoke-all", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			requireAny(handlers.PermSecuritySelf)(handlers.RevokeAllSessionsHandler())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/auth/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/revoke") && r.Method == http.MethodPost {
			requireAny(handlers.PermSecuritySelf)(handlers.RevokeSessionHandler())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/auth/activity", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAny(handlers.PermSecuritySelf)(handlers.LoginActivityHandler())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// ── 2FA ────────────────────────────────────────────────────────
	mux.HandleFunc("/api/auth/2fa/status", handlers.Get2FAStatus())
	mux.HandleFunc("/api/auth/2fa/setup", handlers.Setup2FA())
	mux.HandleFunc("/api/auth/2fa/enable", handlers.Enable2FA())
	mux.HandleFunc("/api/auth/2fa/disable", handlers.Disable2FA())
	mux.HandleFunc("/api/auth/2fa/verify", handlers.Verify2FA())
	mux.HandleFunc("/api/auth/mfa-policy", requireAny(handlers.PermUsersManage)(handlers.UpdateMFAPolicy()))

	// ── Connections (list + create) ───────────────────────────────
	mux.HandleFunc("/api/connections", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(
				handlers.PermConnectionsView,
				handlers.PermUsersManage,
				handlers.PermWorkflowsManage,
				handlers.PermSchemaDiffView,
				handlers.PermBackupsManage,
				handlers.PermSchedulesManage,
				handlers.PermHealthView,
				handlers.PermRowHistoryView,
			)(handlers.ListConnections())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermConnectionsCreate)(handlers.CreateConnection())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	mux.HandleFunc("/api/connections/test", handlers.TestConnection())

	// ── Per-connection routes ─────────────────────────────────────
	mux.HandleFunc("/api/connections/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.SplitN(path, "/", 5)

		// GET /api/connections/{id}
		if len(parts) == 1 && r.Method == http.MethodGet {
			requireAny(
				handlers.PermConnectionsView,
				handlers.PermUsersManage,
				handlers.PermWorkflowsManage,
				handlers.PermSchemaDiffView,
				handlers.PermBackupsManage,
				handlers.PermSchedulesManage,
				handlers.PermHealthView,
				handlers.PermRowHistoryView,
			)(handlers.GetConnection())(w, r)
			return
		}

		// PUT /api/connections/{id}
		if len(parts) == 1 && r.Method == http.MethodPut {
			requireAny(handlers.PermConnectionsEdit)(handlers.UpdateConnection())(w, r)
			return
		}

		// DELETE /api/connections/{id}
		if len(parts) == 1 && r.Method == http.MethodDelete {
			requireAny(handlers.PermConnectionsDelete)(handlers.DeleteConnection())(w, r)
			return
		}

		if len(parts) >= 2 {
			sub := parts[1]
			switch {
			case sub == "folder" && r.Method == http.MethodPatch:
				requireAny(handlers.PermConnectionsEdit)(handlers.UpdateConnectionFolder())(w, r)
			case sub == "visibility" && r.Method == http.MethodPatch:
				requireAny(handlers.PermConnectionsEdit)(handlers.UpdateConnectionVisibility())(w, r)
			case sub == "query" && r.Method == http.MethodPost:
				handlers.ExecuteQuery()(w, r)
			case sub == "explain" && r.Method == http.MethodPost:
				handlers.ExplainQuery()(w, r)
			case sub == "query" && len(parts) >= 3 && parts[2] == "stream" && r.Method == http.MethodPost:
				handlers.StreamQuery()(w, r)
			case sub == "profile" && r.Method == http.MethodPost:
				handlers.ProfileColumn()(w, r)
			case sub == "ping" && r.Method == http.MethodGet:
				handlers.PingConnection()(w, r)
			case sub == "disconnect" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit)(handlers.DisconnectConnection())(w, r)
			case sub == "reconnect" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit)(handlers.ReconnectConnection())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "ping" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.RedisPing())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "keys" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse, handlers.PermConnectionsView)(handlers.RedisKeys())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "key" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse, handlers.PermConnectionsView)(handlers.RedisKeyValue())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "key" && (r.Method == http.MethodPost || r.Method == http.MethodPut):
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.RedisWriteKey())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "key" && r.Method == http.MethodDelete:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.RedisDeleteKey())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "rename" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.RedisRenameKey())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "move" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.RedisMoveKey())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "command" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.RedisCommand())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "script" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.RedisGenerateScript())(w, r)
			case sub == "redis" && len(parts) >= 3 && parts[2] == "script" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.RedisExecuteScript())(w, r)
			case sub == "memcache" && len(parts) >= 3 && parts[2] == "ping" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.MemcachePing())(w, r)
			case sub == "memcache" && len(parts) >= 3 && parts[2] == "stats" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.MemcacheStats())(w, r)
			case sub == "memcache" && len(parts) >= 3 && parts[2] == "key" && (r.Method == http.MethodGet || r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete):
				requireAny(handlers.PermConnectionsView, handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.MemcacheKey())(w, r)
			case sub == "memcache" && len(parts) >= 3 && parts[2] == "flush" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.MemcacheFlush())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "queues" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.LaravelQueueQueues())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "jobs" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.LaravelQueueJobs())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "failed-jobs" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.LaravelQueueFailedJobs())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "horizon" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.LaravelQueueHorizon())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "ops-settings" && (r.Method == http.MethodGet || r.Method == http.MethodPut):
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.LaravelQueueOpsSettings())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "audit" && r.Method == http.MethodGet:
				requireAny(handlers.PermAuditView, handlers.PermConnectionsView)(handlers.LaravelQueueAudit())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "quarantine" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.LaravelQueueQuarantine())(w, r)
			case sub == "laravel-queue" && len(parts) >= 4 && parts[2] == "quarantine" && r.Method == http.MethodDelete:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.LaravelQueueQuarantineItem())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "alerts" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.LaravelQueueAlerts())(w, r)
			case sub == "laravel-queue" && len(parts) >= 3 && parts[2] == "agent" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.LaravelQueueAgentAction())(w, r)
			case sub == "laravel-queue" && len(parts) >= 4 && (parts[3] == "retry-failed" || parts[3] == "delete-failed") && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.LaravelQueueFailedJobAction())(w, r)
			case sub == "laravel-queue" && len(parts) >= 4 && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.LaravelQueueAction())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "topics" && r.Method == http.MethodGet:
				requireAny(handlers.PermKafkaView)(handlers.KafkaTopics())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "topics" && r.Method == http.MethodPost:
				requireAny(handlers.PermKafkaManage)(handlers.KafkaCreateTopic())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "topics" && r.Method == http.MethodDelete:
				requireAny(handlers.PermKafkaManage)(handlers.KafkaDeleteTopic())(w, r)
			case sub == "kafka" && len(parts) >= 4 && parts[2] == "topics" && parts[3] == "partitions" && r.Method == http.MethodPut:
				requireAny(handlers.PermKafkaManage)(handlers.KafkaUpdatePartitions())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "messages" && r.Method == http.MethodGet:
				requireAny(handlers.PermKafkaView)(handlers.KafkaMessages())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "produce" && r.Method == http.MethodPost:
				requireAny(handlers.PermKafkaProduce)(handlers.KafkaProduce())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "consume-test" && r.Method == http.MethodPost:
				requireAny(handlers.PermKafkaView)(handlers.KafkaConsumeTest())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "groups" && r.Method == http.MethodGet:
				requireAny(handlers.PermKafkaView)(handlers.KafkaGroups())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "groups-detail" && r.Method == http.MethodGet:
				requireAny(handlers.PermKafkaView)(handlers.KafkaGroupDetailHandler())(w, r)
			case sub == "kafka" && len(parts) >= 3 && parts[2] == "groups-health" && r.Method == http.MethodGet:
				requireAny(handlers.PermKafkaView)(handlers.KafkaGroupsHealth())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "ping" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoPing())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "dashboard" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoDashboard())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "health" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoHealth())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "databases" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoDatabases())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "collections" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoCollections())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "collections" && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete):
				requireAny(handlers.PermMongoAdmin)(handlers.MongoCollections())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "documents" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoDocuments())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "documents" && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete):
				requireAny(handlers.PermMongoWrite)(handlers.MongoDocuments())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "indexes" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoIndexes())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "indexes" && (r.Method == http.MethodPost || r.Method == http.MethodDelete):
				requireAny(handlers.PermMongoAdmin)(handlers.MongoIndexes())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "aggregate" && r.Method == http.MethodPost:
				requireAny(handlers.PermMongoView)(handlers.MongoAggregate())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "explain" && r.Method == http.MethodPost:
				requireAny(handlers.PermMongoView)(handlers.MongoExplain())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "schema" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoSchema())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "recommend-indexes" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoView)(handlers.MongoIndexRecommendations())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "queries" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
				requireAny(handlers.PermMongoView)(handlers.MongoSavedQueries())(w, r)
			case sub == "mongodb" && len(parts) >= 4 && parts[2] == "queries" && r.Method == http.MethodDelete:
				requireAny(handlers.PermMongoView)(handlers.MongoSavedQueryItem())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "import" && r.Method == http.MethodPost:
				requireAny(handlers.PermMongoImport)(handlers.MongoImport())(w, r)
			case sub == "mongodb" && len(parts) >= 3 && parts[2] == "export" && r.Method == http.MethodGet:
				requireAny(handlers.PermMongoExport)(handlers.MongoExport())(w, r)
			case sub == "cassandra" && len(parts) >= 3 && parts[2] == "ping" && r.Method == http.MethodGet:
				requireAny(handlers.PermCassandraView)(handlers.CassandraPing())(w, r)
			case sub == "cassandra" && len(parts) >= 3 && parts[2] == "dashboard" && r.Method == http.MethodGet:
				requireAny(handlers.PermCassandraView)(handlers.CassandraDashboard())(w, r)
			case sub == "cassandra" && len(parts) >= 3 && parts[2] == "keyspaces" && r.Method == http.MethodGet:
				requireAny(handlers.PermCassandraView)(handlers.CassandraKeyspaces())(w, r)
			case sub == "cassandra" && len(parts) >= 3 && parts[2] == "tables" && r.Method == http.MethodGet:
				requireAny(handlers.PermCassandraView)(handlers.CassandraTables())(w, r)
			case sub == "cassandra" && len(parts) >= 3 && parts[2] == "columns" && r.Method == http.MethodGet:
				requireAny(handlers.PermCassandraView)(handlers.CassandraColumns())(w, r)
			case sub == "cassandra" && len(parts) >= 3 && parts[2] == "rows" && r.Method == http.MethodGet:
				requireAny(handlers.PermCassandraView)(handlers.CassandraRows())(w, r)
			case sub == "cassandra" && len(parts) >= 3 && parts[2] == "query" && r.Method == http.MethodPost:
				requireAny(handlers.PermQueryExecute)(handlers.CassandraQuery())(w, r)
			case sub == "db-logs" && len(parts) >= 3 && parts[2] == "slow-queries" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse, handlers.PermConnectionsView)(handlers.DBSlowQueries())(w, r)
			case sub == "db-logs" && len(parts) >= 3 && parts[2] == "error-logs" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse, handlers.PermConnectionsView)(handlers.DBErrorLogs())(w, r)
			case sub == "cloud-config" && len(parts) >= 3 && parts[2] == "active" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit)(handlers.ActivateCloudConfig())(w, r)
			case sub == "cloud-config" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView)(handlers.GetCloudConfig())(w, r)
			case sub == "cloud-config" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit)(handlers.SaveCloudConfig())(w, r)
			case sub == "cloud-config" && r.Method == http.MethodDelete:
				requireAny(handlers.PermConnectionsEdit)(handlers.DeleteCloudConfig())(w, r)
			case sub == "cloud-logs" && len(parts) >= 3 && parts[2] == "error-logs" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse, handlers.PermConnectionsView)(handlers.CloudErrorLogs())(w, r)
			case sub == "cloud-logs" && len(parts) >= 3 && parts[2] == "slow-logs" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse, handlers.PermConnectionsView)(handlers.CloudSlowLogs())(w, r)
			case sub == "cloud-logs" && len(parts) >= 3 && parts[2] == "audit-logs" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse, handlers.PermConnectionsView)(handlers.CloudAuditLogs())(w, r)
			case sub == "cloud-logs" && len(parts) >= 3 && parts[2] == "audit-log-links" && r.Method == http.MethodPost:
				requireAny(handlers.PermSchemaBrowse, handlers.PermConnectionsView)(handlers.CloudAuditLogLinks())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "info" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchInfo())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "indices" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchIndices())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "query" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchQuery())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "document" && (r.Method == http.MethodGet || r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete):
				requireAny(handlers.PermConnectionsView, handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchDocument())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "index" && r.Method == http.MethodDelete:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchDeleteIndex())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "ilm-policies" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchListILMPolicies())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "ilm-policy" && r.Method == http.MethodPut:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchSaveILMPolicy())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "ilm-policy" && r.Method == http.MethodDelete:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchDeleteILMPolicy())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "templates" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchListTemplates())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "template" && r.Method == http.MethodPut:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchSaveTemplate())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "template" && r.Method == http.MethodDelete:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchDeleteTemplate())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "index-settings" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchGetIndexSettings())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "index-settings" && r.Method == http.MethodPut:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchUpdateIndexSettings())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "cluster-health" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchClusterHealth())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "nodes" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchNodes())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "shards" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchShards())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "mapping" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchIndexMapping())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "index-stats" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchIndexStats())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "list-indices" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchListIndices())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "aggregate" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchAggregate())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "fields" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchIndexFields())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watcher-stats" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchWatcherStats())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watches" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchListWatches())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watch" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchGetWatch())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watch" && r.Method == http.MethodPut:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchSaveWatch())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watch" && r.Method == http.MethodDelete:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchDeleteWatch())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watch-execute" && r.Method == http.MethodPost:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchExecuteWatch())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watch-activate" && r.Method == http.MethodPut:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchActivateWatch())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watch-deactivate" && r.Method == http.MethodPut:
				requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.SearchDeactivateWatch())(w, r)
			case sub == "search" && len(parts) >= 3 && parts[2] == "watch-history" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.SearchWatchHistory())(w, r)
			case sub == "backup" && r.Method == http.MethodGet:
				requireAny(handlers.PermBackupsManage)(handlers.GetBackup())(w, r)
			case sub == "restore" && r.Method == http.MethodPost:
				requireAny(handlers.PermBackupsManage)(handlers.RestoreBackup())(w, r)
			case sub == "transaction":
				action := ""
				if len(parts) >= 3 {
					action = parts[2]
				}
				switch action {
				case "begin":
					handlers.BeginTransaction()(w, r)
				case "commit":
					handlers.CommitTransaction()(w, r)
				case "rollback":
					handlers.RollbackTransaction()(w, r)
				case "status":
					handlers.TxStatus()(w, r)
				default:
					http.NotFound(w, r)
				}
			case sub == "databases" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse)(handlers.ListDatabases())(w, r)
			case sub == "schema" && r.Method == http.MethodGet && len(parts) == 2:
				requireAny(handlers.PermSchemaBrowse)(handlers.GetSchema())(w, r)
			case sub == "schema" && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tables"):
				handlers.CreateTable()(w, r)
			case sub == "schema" && r.Method == http.MethodPatch && !strings.Contains(parts[len(parts)-1], "/"):
				handlers.RenameTable()(w, r)
			case sub == "schema" && r.Method == http.MethodDelete && strings.Count(r.URL.Path, "/") == 6:
				handlers.DropTable()(w, r)
			case sub == "schema" && strings.HasSuffix(r.URL.Path, "/metadata") && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse)(handlers.ListSchemaMetadata())(w, r)
			case sub == "schema" && strings.HasSuffix(r.URL.Path, "/object-detail") && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse)(handlers.GetSchemaObjectDetail())(w, r)
			case sub == "schema" && strings.HasSuffix(r.URL.Path, "/columns") && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse)(handlers.GetTableColumns())(w, r)
			case sub == "schema" && strings.HasSuffix(r.URL.Path, "/columns") && r.Method == http.MethodPost:
				handlers.AddColumn()(w, r)
			case sub == "schema" && strings.Contains(r.URL.Path, "/columns/") && r.Method == http.MethodDelete:
				handlers.DropColumn()(w, r)
			case sub == "schema" && strings.HasSuffix(r.URL.Path, "/data"):
				requireAny(handlers.PermSchemaBrowse)(handlers.GetTableData())(w, r)
			case sub == "schema" && strings.HasSuffix(r.URL.Path, "/import") && r.Method == http.MethodPost:
				handlers.ImportRows()(w, r)
			case sub == "schema" && strings.HasSuffix(r.URL.Path, "/rows"):
				switch r.Method {
				case http.MethodPost:
					handlers.InsertRow()(w, r)
				case http.MethodPut:
					handlers.UpdateRow()(w, r)
				case http.MethodDelete:
					handlers.DeleteRow()(w, r)
				default:
					http.NotFound(w, r)
				}
			case sub == "er" && r.Method == http.MethodGet:
				requireAny(handlers.PermSchemaBrowse)(handlers.GetERDiagram())(w, r)
			case sub == "dashboard" && r.Method == http.MethodGet:
				requireAny(handlers.PermConnectionsView)(handlers.GetDashboard())(w, r)
			case sub == "history" && r.Method == http.MethodGet:
				handlers.GetHistory()(w, r)
			case sub == "history" && r.Method == http.MethodPost:
				handlers.SaveHistory()(w, r)
			case sub == "history" && r.Method == http.MethodDelete:
				handlers.ClearHistory()(w, r)
			case sub == "script" && r.Method == http.MethodPost:
				handlers.RunScript()(w, r)
			case sub == "folder" && r.Method == http.MethodPatch:
				requireAny(handlers.PermConnectionsEdit)(handlers.MoveConnectionToFolder())(w, r)
			case sub == "visibility" && r.Method == http.MethodPatch:
				requireAny(handlers.PermConnectionsEdit)(handlers.SetConnectionVisibility())(w, r)
			case sub == "row-history" && r.Method == http.MethodGet:
				requireAny(handlers.PermRowHistoryView)(handlers.ListRowHistory())(w, r)
			case sub == "row-history" && r.Method == http.MethodPost:
				requireAny(handlers.PermRowHistoryView)(handlers.UndoRowChange())(w, r)
			default:
				http.NotFound(w, r)
			}
			return
		}

		http.NotFound(w, r)
	})

	// ── Saved queries ─────────────────────────────────────────────
	mux.HandleFunc("/api/saved-queries", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermSavedQueriesManage)(handlers.ListSavedQueries())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermSavedQueriesManage)(handlers.CreateSavedQuery())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/saved-queries/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			requireAny(handlers.PermSavedQueriesManage)(handlers.UpdateSavedQuery())(w, r)
		case http.MethodDelete:
			requireAny(handlers.PermSavedQueriesManage)(handlers.DeleteSavedQuery())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	mux.HandleFunc("/api/analytics-dashboards", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermSavedQueriesManage)(handlers.ListAnalyticsDashboards())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermSavedQueriesManage)(handlers.CreateAnalyticsDashboard())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/analytics-dashboards/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAny(handlers.PermSavedQueriesManage)(handlers.ListAnalyticsDashboardUsers())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/analytics-dashboards/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			requireAny(handlers.PermSavedQueriesManage)(handlers.PreviewAnalyticsDashboardQuery())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/analytics-dashboards/blocks/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			requireAny(handlers.PermSavedQueriesManage)(handlers.UpdateAnalyticsDashboardBlock())(w, r)
		case http.MethodDelete:
			requireAny(handlers.PermSavedQueriesManage)(handlers.DeleteAnalyticsDashboardBlock())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/analytics-dashboards/shared/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handlers.RenderSharedAnalyticsDashboard()(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/analytics-dashboards/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/render") && r.Method == http.MethodGet:
			requireAny(handlers.PermSavedQueriesManage)(handlers.RenderAnalyticsDashboard())(w, r)
		case strings.HasSuffix(r.URL.Path, "/blocks") && r.Method == http.MethodPost:
			requireAny(handlers.PermSavedQueriesManage)(handlers.CreateAnalyticsDashboardBlock())(w, r)
		case r.Method == http.MethodGet:
			requireAny(handlers.PermSavedQueriesManage)(handlers.GetAnalyticsDashboard())(w, r)
		case r.Method == http.MethodPut:
			requireAny(handlers.PermSavedQueriesManage)(handlers.UpdateAnalyticsDashboard())(w, r)
		case r.Method == http.MethodDelete:
			requireAny(handlers.PermSavedQueriesManage)(handlers.DeleteAnalyticsDashboard())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── Admin: users ──────────────────────────────────────────────
	mux.HandleFunc("/api/admin/users", requireAny(handlers.PermUsersManage, handlers.PermWorkflowsManage)(handlers.ListUsers()))
	mux.HandleFunc("/api/admin/users/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if strings.HasSuffix(r.URL.Path, "/reset-password") {
				requireAny(handlers.PermUsersManage)(handlers.ResetPasswordHandler())(w, r)
				return
			}
			http.NotFound(w, r)
		case http.MethodPut:
			requireAny(handlers.PermUsersManage)(handlers.UpdateUser())(w, r)
		case http.MethodDelete:
			requireAny(handlers.PermUsersManage)(handlers.DeleteUser())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── User Connection Assignments ──────────────────────────────
	mux.HandleFunc("/api/users/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/users/")
		parts := strings.Split(path, "/")

		if len(parts) >= 2 && parts[1] == "connections" {
			switch r.Method {
			case http.MethodGet:
				requireAny(handlers.PermUsersManage)(handlers.GetUserConnections())(w, r)
			case http.MethodPost:
				requireAny(handlers.PermUsersManage)(handlers.SetUserConnections())(w, r)
			default:
				http.NotFound(w, r)
			}
			return
		}

		http.NotFound(w, r)
	})

	// ── Approval Workflows ────────────────────────────────────────
	mux.HandleFunc("/api/workflows", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermWorkflowsManage)(handlers.ListWorkflows())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermWorkflowsManage)(handlers.CreateWorkflow())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/workflows/applicable", requireAny(handlers.PermQueryExecute)(handlers.ListApplicableWorkflows()))
	mux.HandleFunc("/api/workflows/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/active") && r.Method == http.MethodPut:
			requireAny(handlers.PermWorkflowsManage)(handlers.ToggleWorkflowActive())(w, r)
		case r.Method == http.MethodGet:
			requireAny(handlers.PermWorkflowsManage)(handlers.GetWorkflow())(w, r)
		case r.Method == http.MethodPut:
			requireAny(handlers.PermWorkflowsManage)(handlers.UpdateWorkflow())(w, r)
		case r.Method == http.MethodDelete:
			requireAny(handlers.PermWorkflowsManage)(handlers.DeleteWorkflow())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── Query Approval Requests ───────────────────────────────────
	mux.HandleFunc("/api/approval-requests", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.ListApprovalRequests())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.CreateApprovalRequest())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/approval-requests/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/approval-progress") && r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.GetApprovalProgress())(w, r)
		case strings.HasSuffix(r.URL.Path, "/approve-step") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryApprove)(handlers.ApproveApprovalStep())(w, r)
		case strings.HasSuffix(r.URL.Path, "/execute") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.ExecuteApprovalRequest())(w, r)
		case r.Method == http.MethodPut:
			requireAny(handlers.PermQueryExecute)(handlers.UpdateApprovalRequest())(w, r)
		case r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.GetApprovalRequest())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── Change Sets ───────────────────────────────────────────────
	mux.HandleFunc("/api/change-sets", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.ListChangeSets())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.CreateChangeSet())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/change-sets/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/validate") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.ValidateChangeSet())(w, r)
		case strings.HasSuffix(r.URL.Path, "/submit") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.SubmitChangeSet())(w, r)
		case strings.HasSuffix(r.URL.Path, "/approval-progress") && r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.GetChangeSetApprovalProgress())(w, r)
		case strings.HasSuffix(r.URL.Path, "/approve-step") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryApprove)(handlers.ApproveChangeSetStep())(w, r)
		case strings.HasSuffix(r.URL.Path, "/execute") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.ExecuteChangeSet())(w, r)
		case r.Method == http.MethodPut:
			requireAny(handlers.PermQueryExecute)(handlers.UpdateChangeSet())(w, r)
		case r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.GetChangeSet())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/backup/to-bucket", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		requireAny(handlers.PermBackupsManage)(handlers.BackupToBucket())(w, r)
	})
	mux.HandleFunc("/api/backup/bucket-list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		requireAny(handlers.PermBackupsManage)(handlers.ListBucketBackups())(w, r)
	})
	mux.HandleFunc("/api/backup-download-requests", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove, handlers.PermBackupsManage)(handlers.ListBackupDownloadRequests())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermQueryExecute, handlers.PermBackupsManage)(handlers.CreateBackupDownloadRequestHandler())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/backup-download-requests/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/review") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryApprove)(handlers.ReviewBackupDownloadRequestHandler())(w, r)
		case strings.HasSuffix(r.URL.Path, "/download") && r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermBackupsManage)(handlers.DownloadApprovedBackupRequest())(w, r)
		case r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove, handlers.PermBackupsManage)(handlers.GetBackupDownloadRequest())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── Data Scripts ──────────────────────────────────────────────
	mux.HandleFunc("/api/data-scripts", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.ListDataScripts())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.CreateDataScript())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/data-scripts/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/versions") && r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.ListDataScriptVersions())(w, r)
		case strings.HasSuffix(r.URL.Path, "/versions") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.CreateDataScriptVersion())(w, r)
		case strings.HasSuffix(r.URL.Path, "/preview") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.PreviewDataScript())(w, r)
		case strings.HasSuffix(r.URL.Path, "/submit") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.SubmitDataScript())(w, r)
		case strings.HasSuffix(r.URL.Path, "/plans") && r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.ListDataScriptPlans())(w, r)
		case r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.GetDataScript())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/data-change-plans", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.ListAllDataChangePlans())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/data-change-plans/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.SubmitDataChangePlan())(w, r)
		case strings.HasSuffix(r.URL.Path, "/review") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryApprove)(handlers.ReviewDataChangePlan())(w, r)
		case strings.HasSuffix(r.URL.Path, "/execute") && r.Method == http.MethodPost:
			requireAny(handlers.PermQueryExecute)(handlers.ExecuteDataChangePlan())(w, r)
		case r.Method == http.MethodGet:
			requireAny(handlers.PermQueryExecute, handlers.PermQueryApprove)(handlers.GetDataChangePlan())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// Start background scheduler
	handlers.StartScheduler()

	// ── Schema diff ───────────────────────────────────────────────
	mux.HandleFunc("/api/diff", requireAny(handlers.PermSchemaDiffView)(handlers.GetSchemaDiff()))

	// ── Audit log ─────────────────────────────────────────────────
	mux.HandleFunc("/api/admin/audit", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermAuditView)(handlers.ListAuditLog())(w, r)
		case http.MethodDelete:
			requireAny(handlers.PermAuditView)(handlers.ClearAuditLog())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/admin/audit/stats", requireAny(handlers.PermAuditView)(handlers.GetAuditStats()))
	mux.HandleFunc("/api/query-performance/native", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAny(handlers.PermAuditView)(handlers.ListNativeQueryPerformance())(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/database-audit/native", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAny(handlers.PermAuditView)(handlers.ListNativeDatabaseAudit())(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/database-audit/history/native", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAny(handlers.PermAuditView)(handlers.ListNativeDatabaseAuditHistory())(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/audit/access", handlers.LogFeatureAccess())

	// ── Schedules ────────────────────────────────────────────────
	mux.HandleFunc("/api/schedules", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermSchedulesManage)(handlers.ListSchedules())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermSchedulesManage)(handlers.CreateSchedule())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/schedules/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/schedules/")
		parts := strings.Split(path, "/")
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodPut:
				requireAny(handlers.PermSchedulesManage)(handlers.UpdateSchedule())(w, r)
			case http.MethodDelete:
				requireAny(handlers.PermSchedulesManage)(handlers.DeleteSchedule())(w, r)
			default:
				http.NotFound(w, r)
			}
		} else if len(parts) == 2 && parts[1] == "run" {
			requireAny(handlers.PermSchedulesManage)(handlers.RunScheduleNow())(w, r)
		} else if len(parts) == 2 && parts[1] == "runs" {
			requireAny(handlers.PermSchedulesManage)(handlers.GetScheduleRuns())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// ── Data Pipelines ───────────────────────────────────────────
	mux.HandleFunc("/api/pipelines", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermPipelinesView)(handlers.ListPipelines())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermPipelinesManage)(handlers.CreatePipeline())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/pipelines/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/pipelines/")
		parts := strings.Split(path, "/")
		switch {
		case len(parts) == 1:
			switch r.Method {
			case http.MethodGet:
				requireAny(handlers.PermPipelinesView)(handlers.GetPipeline())(w, r)
			case http.MethodPut:
				requireAny(handlers.PermPipelinesManage)(handlers.UpdatePipeline())(w, r)
			case http.MethodDelete:
				requireAny(handlers.PermPipelinesManage)(handlers.DeletePipeline())(w, r)
			default:
				http.NotFound(w, r)
			}
		case len(parts) == 2 && parts[1] == "run" && r.Method == http.MethodPost:
			requireAny(handlers.PermPipelinesRun)(handlers.TriggerPipelineRun())(w, r)
		case len(parts) == 2 && parts[1] == "runs" && r.Method == http.MethodGet:
			requireAny(handlers.PermPipelinesView)(handlers.ListPipelineRuns())(w, r)
		case len(parts) == 3 && parts[1] == "runs" && r.Method == http.MethodGet:
			requireAny(handlers.PermPipelinesView)(handlers.GetPipelineRunStatus())(w, r)
		case len(parts) == 4 && parts[1] == "runs" && parts[3] == "logs" && r.Method == http.MethodGet:
			requireAny(handlers.PermPipelinesView)(handlers.GetRunLogs())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── Snippets ─────────────────────────────────────────────────
	mux.HandleFunc("/api/snippets", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermSnippetsManage)(handlers.ListSnippets())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermSnippetsManage)(handlers.CreateSnippet())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/snippets/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			requireAny(handlers.PermSnippetsManage)(handlers.UpdateSnippet())(w, r)
		case http.MethodDelete:
			requireAny(handlers.PermSnippetsManage)(handlers.DeleteSnippet())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── Health ping ───────────────────────────────────────────────
	mux.HandleFunc("/api/health", requireAny(handlers.PermHealthView)(handlers.PingAllConnections()))

	// ── Notifications ─────────────────────────────────────────────
	mux.HandleFunc("/api/notifications", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermNotificationsView)(handlers.ListNotifications())(w, r)
		case http.MethodPut:
			requireAny(handlers.PermNotificationsView)(handlers.MarkNotificationsRead())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/notifications/unread", requireAny(handlers.PermNotificationsView)(handlers.UnreadCount()))
	mux.HandleFunc("/api/notification-events", requireAny(handlers.PermNotificationsManage)(handlers.ListNotificationEvents()))
	mux.HandleFunc("/api/notification-targets", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermNotificationsManage)(handlers.ListNotificationTargets())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermNotificationsManage)(handlers.CreateNotificationTarget())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/notification-targets/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/test") && r.Method == http.MethodPost:
			requireAny(handlers.PermNotificationsManage)(handlers.TestNotificationTarget())(w, r)
		case r.Method == http.MethodPut:
			requireAny(handlers.PermNotificationsManage)(handlers.UpdateNotificationTarget())(w, r)
		case r.Method == http.MethodDelete:
			requireAny(handlers.PermNotificationsManage)(handlers.DeleteNotificationTarget())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/notification-rules", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermNotificationsManage)(handlers.ListNotificationRules())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermNotificationsManage)(handlers.CreateNotificationRule())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/notification-rules/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			requireAny(handlers.PermNotificationsManage)(handlers.UpdateNotificationRule())(w, r)
		case http.MethodDelete:
			requireAny(handlers.PermNotificationsManage)(handlers.DeleteNotificationRule())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/notification-deliveries", requireAny(handlers.PermNotificationsManage)(handlers.ListNotificationDeliveries()))

	// ── Connection folders ────────────────────────────────────────
	mux.HandleFunc("/api/folders", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermFoldersManage, handlers.PermWorkflowsManage)(handlers.ListFolders())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermFoldersManage)(handlers.CreateFolder())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/folders/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			requireAny(handlers.PermFoldersManage)(handlers.UpdateFolder())(w, r)
		case http.MethodDelete:
			requireAny(handlers.PermFoldersManage)(handlers.DeleteFolder())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── RBAC: Roles ───────────────────────────────────────────────
	mux.HandleFunc("/api/roles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermRolesManage, handlers.PermUsersManage, handlers.PermWorkflowsManage)(handlers.ListRoles())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermRolesManage)(handlers.CreateRole())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/roles/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermRolesManage, handlers.PermUsersManage, handlers.PermWorkflowsManage)(handlers.GetRole())(w, r)
		case http.MethodPut:
			requireAny(handlers.PermRolesManage)(handlers.UpdateRole())(w, r)
		case http.MethodDelete:
			requireAny(handlers.PermRolesManage)(handlers.DeleteRole())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// ── RBAC: Permissions ─────────────────────────────────────────
	mux.HandleFunc("/api/app-permissions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			requireAny(handlers.PermRolesManage)(handlers.ListAppPermissions())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/my-permissions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handlers.GetMyPermissions()(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// ── RBAC: Legacy permissions table ────────────────────────────
	mux.HandleFunc("/api/permissions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handlers.ListPermissions()(w, r)
		case http.MethodPost:
			handlers.UpsertPermission()(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/permissions/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			handlers.DeletePermission()(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// ── AI assistant ──────────────────────────────────────────────
	mux.HandleFunc("/api/ai/settings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermAIUse, handlers.PermAIManage)(handlers.GetAISettings())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermAIUse, handlers.PermAIManage)(handlers.SaveAISettings())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/ai/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			requireAny(handlers.PermAIUse)(handlers.AIChat())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/ai/analytics/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			requireAny(handlers.PermAIUse)(handlers.AIAnalyticsStream())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/ai/analytics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			requireAny(handlers.PermAIUse)(handlers.AIAnalytics())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/ai/reports", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermAIUse)(handlers.ListAIReports())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermAIUse)(handlers.SaveAIReport())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/ai/reports/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			requireAny(handlers.PermAIUse)(handlers.DeleteAIReport())(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// ── Search App Policies ───────────────────────────────────────
	mux.HandleFunc("/api/search-app-policies", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			requireAny(handlers.PermConnectionsView, handlers.PermSchemaBrowse)(handlers.ListSearchAppPolicies())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.CreateSearchAppPolicy())(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("/api/search-app-policies/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/run") && r.Method == http.MethodPost:
			requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.RunSearchAppPolicy())(w, r)
		case r.Method == http.MethodPut:
			requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.UpdateSearchAppPolicy())(w, r)
		case r.Method == http.MethodDelete:
			requireAny(handlers.PermConnectionsEdit, handlers.PermSchemaBrowse)(handlers.DeleteSearchAppPolicy())(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// Serve the built frontend when running the production image.
	registerStaticRoutes(mux)
}

// Health check handlers
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"uptime": time.Since(startTime).String(),
	})
}

func readyHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Check if database is accessible
	if err := appdb.Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not ready",
			"error":  "database unavailable",
		})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ready",
	})
}

func versionHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version":    version,
		"build_time": buildTime,
		"go_version": runtime.Version(),
	})
}

func registerStaticRoutes(mux *http.ServeMux) {
	staticDir := "/app/static"
	indexPath := filepath.Join(staticDir, "index.html")

	if _, err := os.Stat(indexPath); err != nil {
		log.Printf("Static UI not available at %s: %v", indexPath, err)
		return
	}

	fileServer := http.FileServer(http.Dir(staticDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if cleanPath == "." || cleanPath == "" {
			http.ServeFile(w, r, indexPath)
			return
		}

		candidate := filepath.Join(staticDir, cleanPath)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, indexPath)
	})
}

// Auto backup
func startAutoBackup(ctx context.Context, cfg *config.Config) {
	ticker := time.NewTicker(time.Duration(cfg.BackupHours) * time.Hour)
	defer ticker.Stop()

	select {
	case <-time.After(time.Minute):
		runBackup(cfg)
	case <-ctx.Done():
		return
	}

	for {
		select {
		case <-ticker.C:
			runBackup(cfg)
		case <-ctx.Done():
			return
		}
	}
}

func runBackup(cfg *config.Config) {
	log.Printf("Automatic file-based backups are disabled for %s; use external database backups instead", cfg.DBDriver)
}

func cleanupOldBackups(dir string, keep int) {
	files, err := filepath.Glob(filepath.Join(dir, "nias_backup_*.db"))
	if err != nil {
		return
	}
	if len(files) <= keep {
		return
	}

	// Sort by name (which includes timestamp)
	for i := 0; i < len(files)-keep; i++ {
		if err := os.Remove(files[i]); err != nil {
			log.Printf("Failed to remove old backup %s: %v", files[i], err)
		}
	}
}
