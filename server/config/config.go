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
	DBPath        string
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

	// Logging
	LogLevel  string // "debug" | "info" | "warn" | "error"
	LogFormat string // "text" | "json"
}

func Load() *Config {
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

	// Database
	cfg.DBPath = getEnv("DB_PATH", "data.db")
	cfg.BackupEnabled = getEnvBool("BACKUP_ENABLED", isProduction)
	cfg.BackupDir = getEnv("BACKUP_DIR", "backups")
	cfg.BackupHours = getEnvInt("BACKUP_HOURS", 24)

	// Authentication
	cfg.AuthEnabled = getEnv("AUTH_ENABLED", "true") != "false"
	cfg.JWTExpiry = getEnvInt("JWT_EXPIRY_HOURS", 72)

	// JWT Secret - require strong secret in production
	cfg.JWTSecret = getEnv("JWT_SECRET", "")
	if cfg.JWTSecret == "" {
		if isProduction {
			log.Fatal("FATAL: JWT_SECRET must be set in production")
		}
		cfg.JWTSecret = "anveesa-nias-dev-secret-change-in-production"
		log.Println("WARNING: Using default JWT secret. Set JWT_SECRET in production!")
	} else if isProduction && len(cfg.JWTSecret) < 32 {
		log.Fatal("FATAL: JWT_SECRET must be at least 32 characters in production")
	}

	// Encryption key for credentials
	cfg.EncryptionKey = getEnv("NIAS_ENCRYPTION_KEY", "")
	if cfg.EncryptionKey == "" {
		if isProduction {
			log.Fatal("FATAL: NIAS_ENCRYPTION_KEY must be set in production (32 bytes)")
		}
		cfg.EncryptionKey = "anveesa-nias-32-byte-secret-key!"
		log.Println("WARNING: Using default encryption key. Set NIAS_ENCRYPTION_KEY in production!")
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

	// Logging
	cfg.LogLevel = getEnv("LOG_LEVEL", "info")
	cfg.LogFormat = getEnv("LOG_FORMAT", "text")

	// Validate TLS config
	if cfg.TLSEnabled {
		if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
			log.Fatal("FATAL: TLS_CERT_FILE and TLS_KEY_FILE must be set when TLS_ENABLED=true")
		}
		if _, err := os.Stat(cfg.TLSCertFile); os.IsNotExist(err) {
			log.Fatalf("FATAL: TLS certificate file not found: %s", cfg.TLSCertFile)
		}
		if _, err := os.Stat(cfg.TLSKeyFile); os.IsNotExist(err) {
			log.Fatalf("FATAL: TLS key file not found: %s", cfg.TLSKeyFile)
		}
	}

	// Create backup directory if needed
	if cfg.BackupEnabled {
		if err := os.MkdirAll(cfg.BackupDir, 0750); err != nil {
			log.Printf("WARNING: Could not create backup directory %s: %v", cfg.BackupDir, err)
		}
	}

	return cfg
}

// Validate performs additional runtime validation
func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("PORT is required")
	}
	if c.DBPath == "" {
		return fmt.Errorf("DB_PATH is required")
	}
	return nil
}

// IsProduction returns true if running in production mode
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

// GenerateSecureKey generates a cryptographically secure random key
func GenerateSecureKey(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}

// PrintConfig outputs non-sensitive config values for debugging
func (c *Config) PrintConfig() {
	log.Printf("Configuration:")
	log.Printf("  Environment: %s", c.Environment)
	log.Printf("  Host: %s", c.Host)
	log.Printf("  Port: %s", c.Port)
	log.Printf("  TLS Enabled: %v", c.TLSEnabled)
	log.Printf("  Database: %s", c.DBPath)
	log.Printf("  Auth Enabled: %v", c.AuthEnabled)
	log.Printf("  Backup Enabled: %v", c.BackupEnabled)
	log.Printf("  Rate Limit: %v", c.RateLimitEnabled)
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
