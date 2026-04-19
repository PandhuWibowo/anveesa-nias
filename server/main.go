package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

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

	cfg := config.Load()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Print config in non-production
	if !cfg.IsProduction() {
		cfg.PrintConfig()
	}

	// Initialize database
	if err := appdb.Init(cfg); err != nil {
		log.Fatalf("Database init failed: %v", err)
	}
	switch cfg.DBDriver {
	case "sqlite":
		log.Printf("Database initialized: SQLite (%s)", cfg.DBPath)
	case "postgres":
		log.Printf("Database initialized: PostgreSQL")
	case "mysql":
		log.Printf("Database initialized: MySQL/MariaDB")
	}

	// Set JWT secret
	handlers.SetJWTSecret(cfg.JWTSecret)

	// Set encryption key for credentials
	handlers.SetEncryptionKey(cfg.EncryptionKey)

	// Start automatic backup if enabled
	if cfg.BackupEnabled {
		go startAutoBackup(cfg)
	}

	// Create router
	mux := http.NewServeMux()
	registerRoutes(mux, cfg)

	// Apply middleware stack
	var handler http.Handler = mux
	handler = mw.InjectUserContext(cfg.JWTSecret)(handler) // Extract JWT claims and set headers
	handler = mw.CORS(cfg.CORSOrigin)(handler)
	handler = mw.SecurityHeaders(handler)

	// Add rate limiting in production
	if cfg.RateLimitEnabled {
		rl := mw.NewRateLimiter(cfg.RateLimitRequests, time.Duration(cfg.RateLimitWindow)*time.Second)
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
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	sig := <-quit

	log.Printf("Received signal %v, shutting down...", sig)

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
	mux.HandleFunc("/api/auth/2fa/status", requireAny(handlers.PermSecuritySelf)(handlers.Get2FAStatus()))
	mux.HandleFunc("/api/auth/2fa/setup", requireAny(handlers.PermSecuritySelf)(handlers.Setup2FA()))
	mux.HandleFunc("/api/auth/2fa/enable", requireAny(handlers.PermSecuritySelf)(handlers.Enable2FA()))
	mux.HandleFunc("/api/auth/2fa/disable", requireAny(handlers.PermSecuritySelf)(handlers.Disable2FA()))
	mux.HandleFunc("/api/auth/2fa/verify", handlers.Verify2FA())

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

	// ── Schema search ─────────────────────────────────────────────
	mux.HandleFunc("/api/schema/search", handlers.SearchSchema())

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
			requireAny(handlers.PermAIManage)(handlers.GetAISettings())(w, r)
		case http.MethodPost:
			requireAny(handlers.PermAIManage)(handlers.SaveAISettings())(w, r)
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
func startAutoBackup(cfg *config.Config) {
	ticker := time.NewTicker(time.Duration(cfg.BackupHours) * time.Hour)
	defer ticker.Stop()

	// Run initial backup after 1 minute
	time.Sleep(time.Minute)
	runBackup(cfg)

	for range ticker.C {
		runBackup(cfg)
	}
}

func runBackup(cfg *config.Config) {
	timestamp := time.Now().Format("20060102_150405")
	backupFile := filepath.Join(cfg.BackupDir, fmt.Sprintf("nias_backup_%s.db", timestamp))

	src, err := os.Open(cfg.DBPath)
	if err != nil {
		log.Printf("Backup failed: cannot open source: %v", err)
		return
	}
	defer src.Close()

	dst, err := os.Create(backupFile)
	if err != nil {
		log.Printf("Backup failed: cannot create backup file: %v", err)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		log.Printf("Backup failed: copy error: %v", err)
		return
	}

	log.Printf("Backup completed: %s", backupFile)

	// Cleanup old backups (keep last 7)
	cleanupOldBackups(cfg.BackupDir, 7)
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
