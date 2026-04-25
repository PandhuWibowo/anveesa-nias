package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Server
	Port        string
	Host        string
	Environment string // "development" | "production"

	// TLS
	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string

	// Database
	DBDriver      string // "postgres" | "mysql"
	DBURL         string // PostgreSQL/MySQL: connection string
	DBSSLMode     string // PostgreSQL SSL mode: disable, require, verify-ca, verify-full
	DBSSLRootCert string // Path to SSL root certificate (for RDS)
	BackupEnabled bool
	BackupDir     string
	BackupHours   int // backup interval in hours

	// Authentication
	JWTSecret     string
	JWTExpiry     int // hours
	AuthEnabled   bool
	EncryptionKey string

	// CORS
	CORSOrigin string

	// Rate limiting
	RateLimitEnabled  bool
	RateLimitRequests int // requests per window
	RateLimitWindow   int // seconds

	// Optional Redis integration
	RedisURL      string
	RedisPassword string
	RedisDB       int
	RedisPrefix   string

	// Logging
	LogLevel  string // "debug" | "info" | "warn" | "error"
	LogFormat string // "text" | "json"
}

func Load() (*Config, error) {
	cfg := &Config{}

	// Environment detection
	cfg.Environment = getEnv("NIAS_ENV", "development")
	isProduction := cfg.Environment == "production"

	// Server
	cfg.Port = getEnv("PORT", "8080")
	cfg.Host = getEnv("HOST", "0.0.0.0")

	// TLS
	cfg.TLSEnabled = getEnvBool("TLS_ENABLED", false)
	cfg.TLSCertFile = getEnv("TLS_CERT_FILE", "")
	cfg.TLSKeyFile = getEnv("TLS_KEY_FILE", "")

	// Database - PostgreSQL/MySQL only
	cfg.DBDriver = strings.ToLower(getEnv("DB_DRIVER", "postgres"))
	cfg.DBURL = getEnv("DATABASE_URL", "")
	cfg.DBSSLMode = getEnv("DB_SSL_MODE", "disable")
	cfg.DBSSLRootCert = getEnv("DB_SSL_ROOT_CERT", "")
	cfg.BackupEnabled = getEnvBool("BACKUP_ENABLED", false)
	cfg.BackupDir = getEnv("BACKUP_DIR", "backups")
	cfg.BackupHours = getEnvInt("BACKUP_HOURS", 24)

	// Validate database config
	if cfg.DBDriver == "postgres" || cfg.DBDriver == "mysql" {
		// Add SSL mode to URL if not already present
		if cfg.DBDriver == "postgres" && cfg.DBSSLMode != "disable" && !strings.Contains(cfg.DBURL, "sslmode=") {
			separator := "?"
			if strings.Contains(cfg.DBURL, "?") {
				separator = "&"
			}
			cfg.DBURL = cfg.DBURL + separator + "sslmode=" + cfg.DBSSLMode
		}
	}

	// Warn about SSL in production
	if isProduction && (cfg.DBDriver == "postgres" || cfg.DBDriver == "mysql") {
		if cfg.DBSSLMode == "disable" || cfg.DBSSLMode == "" {
			log.Println("WARNING: Database SSL is disabled in production. Consider enabling SSL for RDS.")
		}
	}

	// Authentication
	cfg.AuthEnabled = getEnv("AUTH_ENABLED", "true") != "false"
	cfg.JWTExpiry = getEnvInt("JWT_EXPIRY_HOURS", 72)

	// JWT Secret - require strong secret in production
	cfg.JWTSecret = getEnv("JWT_SECRET", "")
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "anveesa-nias-dev-secret-change-in-production"
		if !isProduction {
			log.Println("WARNING: Using default JWT secret. Set JWT_SECRET in production!")
		}
	}

	// Encryption key for credentials
	cfg.EncryptionKey = getEnv("NIAS_ENCRYPTION_KEY", "")
	if cfg.EncryptionKey == "" {
		cfg.EncryptionKey = "anveesa-nias-32-byte-secret-key!"
		if !isProduction {
			log.Println("WARNING: Using default encryption key. Set NIAS_ENCRYPTION_KEY in production!")
		}
	}

	// CORS
	cfg.CORSOrigin = getEnv("CORS_ORIGIN", "http://localhost:5173")
	if isProduction && cfg.CORSOrigin == "*" {
		log.Println("WARNING: CORS_ORIGIN is set to '*' in production. Consider restricting to specific origins.")
	}

	// Rate limiting
	cfg.RateLimitEnabled = getEnvBool("RATE_LIMIT_ENABLED", isProduction)
	cfg.RateLimitRequests = getEnvInt("RATE_LIMIT_REQUESTS", 100)
	cfg.RateLimitWindow = getEnvInt("RATE_LIMIT_WINDOW", 60)
	if cfg.RateLimitRequests <= 0 {
		log.Printf("WARNING: Invalid RATE_LIMIT_REQUESTS=%d, using default 100", cfg.RateLimitRequests)
		cfg.RateLimitRequests = 100
	}
	if cfg.RateLimitWindow <= 0 {
		log.Printf("WARNING: Invalid RATE_LIMIT_WINDOW=%d, using default 60 seconds", cfg.RateLimitWindow)
		cfg.RateLimitWindow = 60
	}

	// Optional Redis
	cfg.RedisURL = strings.TrimSpace(getEnv("REDIS_URL", ""))
	cfg.RedisPassword = getEnv("REDIS_PASSWORD", "")
	cfg.RedisDB = getEnvInt("REDIS_DB", 0)
	cfg.RedisPrefix = getEnv("REDIS_PREFIX", "nias")
	if cfg.RedisDB < 0 {
		log.Printf("WARNING: Invalid REDIS_DB=%d, using default 0", cfg.RedisDB)
		cfg.RedisDB = 0
	}

	// Logging
	cfg.LogLevel = getEnv("LOG_LEVEL", "info")
	cfg.LogFormat = getEnv("LOG_FORMAT", "text")

	if cfg.BackupHours <= 0 {
		log.Printf("WARNING: Invalid BACKUP_HOURS=%d, using default 24 hours", cfg.BackupHours)
		cfg.BackupHours = 24
	}

	// Create backup directory if needed
	if cfg.BackupEnabled {
		if err := os.MkdirAll(cfg.BackupDir, 0750); err != nil {
			log.Printf("WARNING: Could not create backup directory %s: %v", cfg.BackupDir, err)
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate performs additional runtime validation
func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("PORT is required")
	}
	if c.DBDriver != "postgres" && c.DBDriver != "mysql" {
		return fmt.Errorf("unsupported DB_DRIVER: %s (must be 'postgres' or 'mysql')", c.DBDriver)
	}
	if (c.DBDriver == "postgres" || c.DBDriver == "mysql") && c.DBURL == "" {
		return fmt.Errorf("DATABASE_URL is required for %s", c.DBDriver)
	}
	if c.IsProduction() && len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be set and at least 32 characters in production")
	}
	if c.IsProduction() && c.EncryptionKey == "anveesa-nias-32-byte-secret-key!" {
		return fmt.Errorf("NIAS_ENCRYPTION_KEY must be set in production (32 bytes)")
	}
	if c.TLSEnabled {
		if c.TLSCertFile == "" || c.TLSKeyFile == "" {
			return fmt.Errorf("TLS_CERT_FILE and TLS_KEY_FILE must be set when TLS_ENABLED=true")
		}
		if _, err := os.Stat(c.TLSCertFile); err != nil {
			return fmt.Errorf("TLS certificate file not available: %w", err)
		}
		if _, err := os.Stat(c.TLSKeyFile); err != nil {
			return fmt.Errorf("TLS key file not available: %w", err)
		}
	}
	return nil
}

// IsProduction returns true if running in production mode
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

// GenerateSecureKey generates a cryptographically secure random key
func GenerateSecureKey(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate secure key: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// PrintConfig outputs non-sensitive config values for debugging
func (c *Config) PrintConfig() {
	log.Printf("Configuration:")
	log.Printf("  Environment: %s", c.Environment)
	log.Printf("  Host: %s", c.Host)
	log.Printf("  Port: %s", c.Port)
	log.Printf("  TLS Enabled: %v", c.TLSEnabled)
	log.Printf("  Database Driver: %s", c.DBDriver)
	log.Printf("  Database URL: %s", maskDBURL(c.DBURL))
	if c.DBDriver == "postgres" || c.DBDriver == "mysql" {
		log.Printf("  SSL Mode: %s", c.DBSSLMode)
	}
	log.Printf("  Auth Enabled: %v", c.AuthEnabled)
	log.Printf("  Backup Enabled: %v", c.BackupEnabled)
	log.Printf("  Rate Limit: %v", c.RateLimitEnabled)
	log.Printf("  Redis Enabled: %v", c.RedisURL != "")
	log.Printf("  CORS Origin: %s", maskOrigin(c.CORSOrigin))
}

func maskOrigin(origin string) string {
	if origin == "*" {
		return "*"
	}
	if len(origin) > 30 {
		return origin[:30] + "..."
	}
	return origin
}

func maskDBURL(url string) string {
	// Mask password in postgres://user:password@host:port/db
	if strings.Contains(url, "://") && strings.Contains(url, "@") {
		parts := strings.SplitN(url, "@", 2)
		if len(parts) == 2 {
			userInfo := strings.SplitN(parts[0], "://", 2)
			if len(userInfo) == 2 && strings.Contains(userInfo[1], ":") {
				credentials := strings.SplitN(userInfo[1], ":", 2)
				return userInfo[0] + "://" + credentials[0] + ":****@" + parts[1]
			}
		}
	}
	return "****"
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return strings.ToLower(value) == "true" || value == "1"
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	if n, err := strconv.Atoi(value); err == nil {
		return n
	}
	return defaultValue
}
