package handlers

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// allowedDrivers defines valid database drivers
var allowedDrivers = map[string]bool{
	"sqlite":   true,
	"postgres": true,
	"mysql":    true,
	"mariadb":  true, // alias → uses MySQL driver
	"mssql":    true,
	"redis":    true,
	"memcache": true,
	"kafka":    true,
	"s3_aws":   true,
	"s3_gcp":   true,
	"s3_oss":   true,
	"s3_obs":   true,
}

// encryptionKey for credential encryption (should be set from environment)
var encryptionKey []byte

func init() {
	// Default key - will be overridden by SetEncryptionKey if called
	key := "anveesa-nias-32-byte-secret-key!"
	encryptionKey = []byte(key)
}

// SetEncryptionKey sets the encryption key for credential storage
func SetEncryptionKey(key string) {
	if key == "" {
		return
	}
	// Ensure key is exactly 32 bytes for AES-256
	if len(key) > 32 {
		key = key[:32]
	} else if len(key) < 32 {
		key = key + strings.Repeat("0", 32-len(key))
	}
	encryptionKey = []byte(key)
}

// encryptCredential encrypts sensitive data using AES-GCM
func encryptCredential(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptCredential decrypts AES-GCM encrypted data
func decryptCredential(encrypted string) (string, error) {
	if encrypted == "" {
		return "", nil
	}
	// Handle legacy unencrypted data
	if !strings.HasPrefix(encrypted, "enc:") {
		return encrypted, nil
	}
	encrypted = strings.TrimPrefix(encrypted, "enc:")

	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

type Connection struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Driver       string `json:"driver"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Database     string `json:"database"`
	Username     string `json:"username"`
	Password     string `json:"password,omitempty"`
	SSL          bool   `json:"ssl"`
	Tags         string `json:"tags"`
	SSHHost      string `json:"ssh_host"`
	SSHPort      int    `json:"ssh_port"`
	SSHUser      string `json:"ssh_user"`
	SSHPassword  string `json:"ssh_password,omitempty"`
	SSHKey       string `json:"ssh_key,omitempty"`
	FolderID     *int64 `json:"folder_id"`
	Visibility   string `json:"visibility"`
	OwnerID      int64  `json:"owner_id"`
	Disconnected bool   `json:"disconnected"`
	CreatedAt    string `json:"created_at"`
}

type ConnectionInput struct {
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Database    string `json:"database"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	SSL         bool   `json:"ssl"`
	Tags        string `json:"tags"`
	SSHHost     string `json:"ssh_host"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user"`
	SSHPassword string `json:"ssh_password"`
	SSHKey      string `json:"ssh_key"`
	FolderID    *int64 `json:"folder_id"`
	Visibility  string `json:"visibility"`
}

// validateConnectionInput validates connection parameters
func validateConnectionInput(in *ConnectionInput) error {
	// Validate driver
	if !allowedDrivers[in.Driver] {
		return fmt.Errorf("invalid driver: must be sqlite, postgres, mysql, mariadb, mssql, redis, memcache, kafka, s3_aws, s3_gcp, s3_oss, or s3_obs")
	}

	// Validate port
	if in.Driver == "sqlite" {
		if strings.TrimSpace(in.Database) == "" {
			return fmt.Errorf("database file path is required")
		}
	} else if isObjectStorageDriver(in.Driver) {
		if strings.TrimSpace(in.Host) == "" {
			return fmt.Errorf("endpoint host is required")
		}
		if strings.TrimSpace(in.Database) == "" {
			return fmt.Errorf("bucket name is required")
		}
		if strings.TrimSpace(in.Username) == "" {
			return fmt.Errorf("access key is required")
		}
		if strings.TrimSpace(in.Password) == "" {
			return fmt.Errorf("secret key is required")
		}
	} else if in.Port < 1 || in.Port > 65535 {
		return fmt.Errorf("invalid port: must be 1-65535")
	}

	// Validate name length
	if len(in.Name) > 100 {
		return fmt.Errorf("name too long: maximum 100 characters")
	}

	// Validate host (no special characters that could be used for injection)
	if in.Host != "" && !isValidHostname(in.Host) {
		return fmt.Errorf("invalid host format")
	}

	// Validate visibility
	if in.Visibility != "" && in.Visibility != "private" && in.Visibility != "shared" {
		in.Visibility = "shared"
	}

	return nil
}

// isValidHostname checks for basic hostname validity
func isValidHostname(host string) bool {
	// Allow IP addresses and hostnames, reject obvious injection attempts
	if strings.ContainsAny(host, ";'\"\\`$(){}[]<>|&") {
		return false
	}
	// Max length check
	if len(host) > 253 {
		return false
	}
	return true
}

func buildDSN(in ConnectionInput) (string, error) {
	switch in.Driver {
	case "redis", "memcache", "kafka", "s3_aws", "s3_gcp", "s3_oss", "s3_obs":
		return "", fmt.Errorf("%s does not use SQL DSN", in.Driver)
	case "sqlite":
		dbPath := strings.TrimSpace(in.Database)
		if dbPath == "" {
			return "", fmt.Errorf("sqlite database file path is required")
		}
		return dbPath, nil
	case "postgres":
		sslMode := "disable"
		if in.SSL {
			sslMode = "require"
		}
		dbName := in.Database
		if dbName == "" {
			dbName = "postgres"
		}
		username := in.Username
		if username == "" {
			username = "postgres"
		}
		// URL format handles all special characters robustly
		return fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=%s",
			url.QueryEscape(username),
			url.QueryEscape(in.Password),
			in.Host,
			in.Port,
			url.PathEscape(dbName),
			sslMode,
		), nil
	case "mysql", "mariadb":
		// Use mysql.Config.FormatDSN() so that passwords with special characters
		// (!  $  *  @  /  #  etc.) are handled correctly — url.QueryEscape would
		// send the %-encoded string to the server and cause "Access denied".
		cfg := mysql.NewConfig()
		cfg.User = in.Username
		cfg.Passwd = in.Password
		cfg.Net = "tcp"
		cfg.Addr = fmt.Sprintf("%s:%d", in.Host, in.Port)
		cfg.DBName = in.Database
		cfg.ParseTime = true
		cfg.AllowNativePasswords = true
		cfg.Params = map[string]string{"charset": "utf8mb4"}
		cfg.Loc = time.UTC
		if in.SSL {
			cfg.TLSConfig = "true"
		}
		return cfg.FormatDSN(), nil
	case "mssql":
		// URL-encode credentials for SQL Server
		return fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			url.QueryEscape(in.Username),
			url.QueryEscape(in.Password),
			in.Host,
			in.Port,
			url.QueryEscape(in.Database),
		), nil
	}
	return "", fmt.Errorf("unsupported driver")
}

// escapePostgresValue escapes values for PostgreSQL connection string
func escapePostgresValue(s string) string {
	// Escape single quotes and backslashes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	// If contains spaces, quote the whole thing
	if strings.ContainsAny(s, " \t\n") {
		return "'" + s + "'"
	}
	return s
}

func driverName(d string) string {
	switch d {
	case "postgres":
		return "postgres"
	case "mysql", "mariadb":
		return "mysql"
	case "mssql":
		return "sqlserver"
	case "sqlite":
		return "sqlite3"
	}
	return d
}

func ListConnections() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Get current user info from headers
		userIDStr := r.Header.Get("X-User-ID")
		userRole := r.Header.Get("X-User-Role")

		var userID int64
		if userIDStr != "" {
			userID, _ = strconv.ParseInt(userIDStr, 10, 64)
		}

		// Build query based on user permissions
		// Admin sees all, others see:
		// - Connections with visibility='shared'
		// - Connections with visibility='private' where they are the owner (created by)
		// - Connections in shared folders
		// - Connections in private folders they own
		var query string
		var args []interface{}

		if userRole == "admin" || !isAuthEnabled() {
			// Admin or no auth: see everything
			query = `SELECT c.id, c.name, c.driver, COALESCE(c.host,''), COALESCE(c.port,0), c.database,
			        COALESCE(c.username,''), c.ssl, COALESCE(c.tags,''),
			        COALESCE(c.ssh_host,''), COALESCE(c.ssh_port,22), COALESCE(c.ssh_user,''),
			        c.folder_id, COALESCE(c.visibility,'shared'), COALESCE(c.owner_id,0), COALESCE(c.disconnected,0), c.created_at
			 FROM connections c ORDER BY c.id`
		} else {
			// Regular user: filter by visibility, ownership, explicit permissions, and folder membership
			query = `SELECT DISTINCT c.id, c.name, c.driver, COALESCE(c.host,''), COALESCE(c.port,0), c.database,
			        COALESCE(c.username,''), c.ssl, COALESCE(c.tags,''),
			        COALESCE(c.ssh_host,''), COALESCE(c.ssh_port,22), COALESCE(c.ssh_user,''),
			        c.folder_id, COALESCE(c.visibility,'shared'), COALESCE(c.owner_id,0), COALESCE(c.disconnected,0), c.created_at
			 FROM connections c
			 LEFT JOIN connection_folders f ON c.folder_id = f.id
			 LEFT JOIN user_connections uc ON c.id = uc.conn_id AND uc.user_id = ?
			 LEFT JOIN folder_members fm ON f.id = fm.folder_id AND fm.user_id = ?
			 WHERE 
			   -- Connection is shared and not in a private folder
			   (c.visibility = 'shared' AND (f.id IS NULL OR f.visibility = 'shared'))
			   -- OR user owns the connection
			   OR c.owner_id = ?
			   -- OR user owns the folder containing the connection
			   OR f.owner_id = ?
			   -- OR connection has been explicitly assigned to user
			   OR uc.conn_id IS NOT NULL
			   -- OR user is a member of the folder (access group)
			   OR fm.folder_id IS NOT NULL
			 ORDER BY c.id`
			query = appdb.ConvertQuery(query)
			args = append(args, userID, userID, userID, userID)
		}

		rows, err := appdb.DB.Query(query, args...)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var conns []Connection
		for rows.Next() {
			var c Connection
			var ssl, disconnected int
			rows.Scan(&c.ID, &c.Name, &c.Driver, &c.Host, &c.Port, &c.Database,
				&c.Username, &ssl, &c.Tags,
				&c.SSHHost, &c.SSHPort, &c.SSHUser,
				&c.FolderID, &c.Visibility, &c.OwnerID, &disconnected, &c.CreatedAt)
			c.SSL = ssl == 1
			c.Disconnected = disconnected == 1
			conns = append(conns, c)
		}
		if conns == nil {
			conns = []Connection{}
		}
		json.NewEncoder(w).Encode(conns)
	}
}

func CreateConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in ConnectionInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		if in.Name == "" || in.Driver == "" {
			http.Error(w, `{"error":"name and driver are required"}`, http.StatusBadRequest)
			return
		}
		// Validate input
		if err := validateConnectionInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		if in.Visibility == "" {
			in.Visibility = "shared"
		}

		// Encrypt sensitive credentials
		encPassword, err := encryptCredential(in.Password)
		if err != nil {
			http.Error(w, `{"error":"encryption error"}`, http.StatusInternalServerError)
			return
		}
		encSSHPassword, err := encryptCredential(in.SSHPassword)
		if err != nil {
			http.Error(w, `{"error":"encryption error"}`, http.StatusInternalServerError)
			return
		}
		encSSHKey, err := encryptCredential(in.SSHKey)
		if err != nil {
			http.Error(w, `{"error":"encryption error"}`, http.StatusInternalServerError)
			return
		}

		ssl := 0
		if in.SSL {
			ssl = 1
		}

		// Get owner ID from request headers
		var ownerID int64
		if userIDStr := r.Header.Get("X-User-ID"); userIDStr != "" {
			ownerID, _ = strconv.ParseInt(userIDStr, 10, 64)
		}

		var id int64
		var c Connection
		var sslV int

		// PostgreSQL and MySQL support RETURNING clause
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			query := `INSERT INTO connections (name, driver, host, port, database, username, password, ssl, tags,
			  ssh_host, ssh_port, ssh_user, ssh_password, ssh_key, folder_id, visibility, owner_id)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`
			err := appdb.DB.QueryRow(query,
				in.Name, in.Driver, in.Host, in.Port, in.Database, in.Username, encPassword, ssl, in.Tags,
				in.SSHHost, in.SSHPort, in.SSHUser, encSSHPassword, encSSHKey, in.FolderID, in.Visibility, ownerID,
			).Scan(&id)
			if err != nil {
				http.Error(w, `{"error":"failed to save connection"}`, http.StatusInternalServerError)
				return
			}
		} else {
			res, err := appdb.DB.Exec(
				`INSERT INTO connections (name, driver, host, port, database, username, password, ssl, tags,
				  ssh_host, ssh_port, ssh_user, ssh_password, ssh_key, folder_id, visibility, owner_id)
				 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				in.Name, in.Driver, in.Host, in.Port, in.Database, in.Username, encPassword, ssl, in.Tags,
				in.SSHHost, in.SSHPort, in.SSHUser, encSSHPassword, encSSHKey, in.FolderID, in.Visibility, ownerID,
			)
			if err != nil {
				http.Error(w, `{"error":"failed to save connection"}`, http.StatusInternalServerError)
				return
			}
			id, _ = res.LastInsertId()
		}

		// Fetch the created connection
		var discV int
		appdb.DB.QueryRow(
			appdb.ConvertQuery(`SELECT id, name, driver, COALESCE(host,''), COALESCE(port,0), database,
			        COALESCE(username,''), ssl, COALESCE(tags,''),
			        COALESCE(ssh_host,''), COALESCE(ssh_port,22), COALESCE(ssh_user,''),
			        folder_id, COALESCE(visibility,'shared'), COALESCE(owner_id,0), COALESCE(disconnected,0), created_at
			 FROM connections WHERE id=?`), id,
		).Scan(&c.ID, &c.Name, &c.Driver, &c.Host, &c.Port, &c.Database,
			&c.Username, &sslV, &c.Tags,
			&c.SSHHost, &c.SSHPort, &c.SSHUser,
			&c.FolderID, &c.Visibility, &c.OwnerID, &discV, &c.CreatedAt)
		c.SSL = sslV == 1
		c.Disconnected = discV == 1

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(c)
	}
}

func GetConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		idStr := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		connID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}

		// Get current user info
		userIDStr := r.Header.Get("X-User-ID")
		userRole := r.Header.Get("X-User-Role")
		var userID int64
		if userIDStr != "" {
			userID, _ = strconv.ParseInt(userIDStr, 10, 64)
		}

		// Fetch connection
		var c Connection
		var ssl int
		var encPassword, encSSHPassword, encSSHKey string

		query := appdb.ConvertQuery(`SELECT id, name, driver, COALESCE(host,''), COALESCE(port,0), database,
			COALESCE(username,''), password, ssl, COALESCE(tags,''),
			COALESCE(ssh_host,''), COALESCE(ssh_port,22), COALESCE(ssh_user,''), ssh_password, ssh_key,
			folder_id, COALESCE(visibility,'shared'), COALESCE(owner_id,0), COALESCE(disconnected,0), created_at
			FROM connections WHERE id=?`)

		var disconnected int
		err = appdb.DB.QueryRow(query, connID).Scan(
			&c.ID, &c.Name, &c.Driver, &c.Host, &c.Port, &c.Database,
			&c.Username, &encPassword, &ssl, &c.Tags,
			&c.SSHHost, &c.SSHPort, &c.SSHUser, &encSSHPassword, &encSSHKey,
			&c.FolderID, &c.Visibility, &c.OwnerID, &disconnected, &c.CreatedAt)
		c.Disconnected = disconnected == 1

		if err != nil {
			http.Error(w, `{"error":"connection not found"}`, http.StatusNotFound)
			return
		}

		c.SSL = ssl == 1

		// Check permission
		if isAuthEnabled() && userRole != "admin" && c.OwnerID != userID && c.Visibility != "shared" {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		// Mask passwords (show bullets for security)
		if encPassword != "" {
			c.Password = "••••••••"
		}
		if encSSHPassword != "" {
			c.SSHPassword = "••••••••"
		}
		if encSSHKey != "" {
			c.SSHKey = "••••••••"
		}

		json.NewEncoder(w).Encode(c)
	}
}

func UpdateConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Get connection ID from URL
		idStr := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		connID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}

		// Check if connection exists and user has permission
		var existingOwnerID int64
		err = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT owner_id FROM connections WHERE id = ?`), connID).Scan(&existingOwnerID)
		if err != nil {
			http.Error(w, `{"error":"connection not found"}`, http.StatusNotFound)
			return
		}

		// Get current user info
		userIDStr := r.Header.Get("X-User-ID")
		userRole := r.Header.Get("X-User-Role")
		var userID int64
		if userIDStr != "" {
			userID, _ = strconv.ParseInt(userIDStr, 10, 64)
		}

		// Check permission: must be owner or admin
		if isAuthEnabled() && userRole != "admin" && existingOwnerID != userID {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		var in ConnectionInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}

		if in.Name == "" || in.Driver == "" {
			http.Error(w, `{"error":"name and driver are required"}`, http.StatusBadRequest)
			return
		}

		// Validate input
		if err := validateConnectionInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		if in.Visibility == "" {
			in.Visibility = "shared"
		}

		// Handle password: if empty or masked, keep existing
		var encPassword string
		if in.Password != "" && !strings.Contains(in.Password, "•") {
			encPassword, err = encryptCredential(in.Password)
			if err != nil {
				http.Error(w, `{"error":"encryption error"}`, http.StatusInternalServerError)
				return
			}
		} else {
			// Keep existing password
			appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT password FROM connections WHERE id = ?`), connID).Scan(&encPassword)
		}

		// Handle SSH credentials
		var encSSHPassword, encSSHKey string
		if in.SSHPassword != "" && !strings.Contains(in.SSHPassword, "•") {
			encSSHPassword, err = encryptCredential(in.SSHPassword)
			if err != nil {
				http.Error(w, `{"error":"encryption error"}`, http.StatusInternalServerError)
				return
			}
		} else {
			appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT ssh_password FROM connections WHERE id = ?`), connID).Scan(&encSSHPassword)
		}

		if in.SSHKey != "" && !strings.Contains(in.SSHKey, "•") {
			encSSHKey, err = encryptCredential(in.SSHKey)
			if err != nil {
				http.Error(w, `{"error":"encryption error"}`, http.StatusInternalServerError)
				return
			}
		} else {
			appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT ssh_key FROM connections WHERE id = ?`), connID).Scan(&encSSHKey)
		}

		ssl := 0
		if in.SSL {
			ssl = 1
		}

		// Update connection
		query := appdb.ConvertQuery(`UPDATE connections SET 
			name=?, driver=?, host=?, port=?, database=?, username=?, password=?, ssl=?, tags=?,
			ssh_host=?, ssh_port=?, ssh_user=?, ssh_password=?, ssh_key=?, folder_id=?, visibility=?
			WHERE id=?`)

		_, err = appdb.DB.Exec(query,
			in.Name, in.Driver, in.Host, in.Port, in.Database, in.Username, encPassword, ssl, in.Tags,
			in.SSHHost, in.SSHPort, in.SSHUser, encSSHPassword, encSSHKey, in.FolderID, in.Visibility, connID,
		)
		if err != nil {
			http.Error(w, `{"error":"failed to update connection"}`, http.StatusInternalServerError)
			return
		}

		// Evict from pool to force reconnect with new credentials
		EvictFromPool(connID)

		// Fetch updated connection
		var c Connection
		var sslV, discV2 int
		appdb.DB.QueryRow(
			appdb.ConvertQuery(`SELECT id, name, driver, COALESCE(host,''), COALESCE(port,0), database,
			        COALESCE(username,''), ssl, COALESCE(tags,''),
			        COALESCE(ssh_host,''), COALESCE(ssh_port,22), COALESCE(ssh_user,''),
			        folder_id, COALESCE(visibility,'shared'), COALESCE(owner_id,0), COALESCE(disconnected,0), created_at
			 FROM connections WHERE id=?`), connID,
		).Scan(&c.ID, &c.Name, &c.Driver, &c.Host, &c.Port, &c.Database,
			&c.Username, &sslV, &c.Tags,
			&c.SSHHost, &c.SSHPort, &c.SSHUser,
			&c.FolderID, &c.Visibility, &c.OwnerID, &discV2, &c.CreatedAt)
		c.SSL = sslV == 1
		c.Disconnected = discV2 == 1

		json.NewEncoder(w).Encode(c)
	}
}

func DeleteConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idStr := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		idStr = strings.Split(idStr, "/")[0]
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		// Check permission: admin or owner
		if !canModifyConnection(r, id) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		EvictFromPool(id)
		appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM connections WHERE id=?`), id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// UpdateConnectionFolder updates the folder and/or visibility of a connection
func UpdateConnectionFolder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Extract connection ID from URL
		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.Split(path, "/")
		if len(parts) < 2 || parts[1] != "folder" {
			http.Error(w, `{"error":"invalid endpoint"}`, http.StatusBadRequest)
			return
		}

		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		// Check permission
		if !canModifyConnection(r, id) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		var payload struct {
			FolderID   *int64  `json:"folder_id"`
			Visibility *string `json:"visibility"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		// Update folder_id and/or visibility
		if payload.FolderID != nil {
			_, err = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET folder_id = ? WHERE id = ?`), payload.FolderID, id)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"failed to update folder: %v"}`, err), http.StatusInternalServerError)
				return
			}
		}

		if payload.Visibility != nil {
			_, err = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET visibility = ? WHERE id = ?`), *payload.Visibility, id)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"failed to update visibility: %v"}`, err), http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "connection updated"})
	}
}

// UpdateConnectionVisibility updates only the visibility of a connection
func UpdateConnectionVisibility() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Extract connection ID from URL
		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.Split(path, "/")
		if len(parts) < 2 || parts[1] != "visibility" {
			http.Error(w, `{"error":"invalid endpoint"}`, http.StatusBadRequest)
			return
		}

		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		// Check permission
		if !canModifyConnection(r, id) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		var payload struct {
			Visibility string `json:"visibility"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		_, err = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET visibility = ? WHERE id = ?`), payload.Visibility, id)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to update visibility: %v"}`, err), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "visibility updated"})
	}
}

// canModifyConnection checks if the current user can modify a connection
func canModifyConnection(r *http.Request, connID int64) bool {
	// No auth or admin: allowed
	if !isAuthEnabled() {
		return true
	}
	userRole := r.Header.Get("X-User-Role")
	if userRole == "admin" {
		return true
	}

	// Check if user owns this connection
	userIDStr := r.Header.Get("X-User-ID")
	if userIDStr == "" {
		return false
	}
	userID, _ := strconv.ParseInt(userIDStr, 10, 64)

	var ownerID int64
	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT COALESCE(owner_id,0) FROM connections WHERE id=?`), connID).Scan(&ownerID)
	if err != nil {
		return false
	}
	return ownerID == userID || ownerID == 0 // Allow if owner matches or if owner_id is 0 (legacy)
}

func TestConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in ConnectionInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}

		// Validate input
		if err := validateConnectionInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		if in.Driver == "redis" {
			if err := testRedisInput(r.Context(), in); err != nil {
				http.Error(w, jsonError("connection failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"message": "Connection successful"})
			return
		}
		if in.Driver == "memcache" {
			if err := testMemcacheInput(r.Context(), in); err != nil {
				http.Error(w, jsonError("connection failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"message": "Connection successful"})
			return
		}
		if in.Driver == "kafka" {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			if _, err := readKafkaTopics(ctx, in); err != nil {
				http.Error(w, jsonError("connection failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"message": "Connection successful"})
			return
		}
		if isObjectStorageDriver(in.Driver) {
			if err := testObjectStorageInput(r.Context(), in); err != nil {
				http.Error(w, jsonError("connection failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"message": "Connection successful"})
			return
		}

		dsn, err := buildDSN(in)
		if err != nil {
			http.Error(w, `{"error":"invalid connection parameters"}`, http.StatusBadRequest)
			return
		}

		db, err := sql.Open(driverName(in.Driver), dsn)
		if err != nil {
			http.Error(w, jsonError("connection failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		defer db.Close()

		db.SetConnMaxLifetime(10 * time.Second)
		if err = db.Ping(); err != nil {
			http.Error(w, jsonError("connection failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "Connection successful"})
	}
}

// DisconnectConnection marks a connection as disconnected, evicts it from the
// pool, and blocks all future queries until ReconnectConnection is called.
func DisconnectConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.Split(path, "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}

		var exists int
		err = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT 1 FROM connections WHERE id=?`), connID).Scan(&exists)
		if err != nil {
			http.Error(w, `{"error":"connection not found"}`, http.StatusNotFound)
			return
		}

		if _, err = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET disconnected=1 WHERE id=?`), connID); err != nil {
			http.Error(w, `{"error":"failed to disconnect"}`, http.StatusInternalServerError)
			return
		}
		EvictFromPool(connID)

		json.NewEncoder(w).Encode(map[string]string{"message": "disconnected"})
	}
}

// ReconnectConnection clears the disconnected flag so the connection can be used again.
func ReconnectConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.Split(path, "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}

		var exists int
		err = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT 1 FROM connections WHERE id=?`), connID).Scan(&exists)
		if err != nil {
			http.Error(w, `{"error":"connection not found"}`, http.StatusNotFound)
			return
		}

		if _, err = appdb.DB.Exec(appdb.ConvertQuery(`UPDATE connections SET disconnected=0 WHERE id=?`), connID); err != nil {
			http.Error(w, `{"error":"failed to reconnect"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"message": "reconnected"})
	}
}

// openRemoteDB opens a connection to the stored remote database by ID.
func openRemoteDB(connID int64) (*sql.DB, string, error) {
	var in ConnectionInput
	var ssl, disconnected int
	var encPassword string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT driver, COALESCE(host,''), COALESCE(port,0), database, COALESCE(username,''), COALESCE(password,''), ssl, COALESCE(disconnected,0) FROM connections WHERE id=?`), connID,
	).Scan(&in.Driver, &in.Host, &in.Port, &in.Database, &in.Username, &encPassword, &ssl, &disconnected)
	if err != nil {
		return nil, "", fmt.Errorf("connection not found")
	}
	if disconnected == 1 {
		return nil, "", fmt.Errorf("connection is disconnected")
	}
	in.SSL = ssl == 1

	// Decrypt password
	password, err := decryptCredential(encPassword)
	if err != nil {
		return nil, "", fmt.Errorf("decryption error")
	}
	in.Password = password

	dsn, err := buildDSN(in)
	if err != nil {
		return nil, "", err
	}
	// Use the normalized Go driver name (e.g. "mariadb" → "mysql") so that
	// every handler's switch/case works without needing a separate "mariadb" branch.
	goDriver := driverName(in.Driver)
	db, err := sql.Open(goDriver, dsn)
	if err != nil {
		return nil, "", err
	}
	db.SetMaxOpenConns(5)
	return db, goDriver, nil
}

func testMemcacheInput(ctx context.Context, in ConnectionInput) error {
	host := strings.TrimSpace(in.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := in.Port
	if port == 0 {
		port = 11211
	}

	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return err
	}
	defer conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write([]byte("version\r\n")); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "VERSION") {
		return fmt.Errorf("unexpected memcache response: %s", strings.TrimSpace(line))
	}
	return nil
}

func isObjectStorageDriver(driver string) bool {
	switch driver {
	case "s3_aws", "s3_gcp", "s3_oss", "s3_obs":
		return true
	default:
		return false
	}
}

func testObjectStorageInput(ctx context.Context, in ConnectionInput) error {
	endpointHost := strings.TrimSpace(in.Host)
	endpointHost = strings.TrimPrefix(strings.TrimPrefix(endpointHost, "https://"), "http://")
	endpointHost = strings.TrimRight(endpointHost, "/")
	bucket := strings.Trim(strings.TrimSpace(in.Database), "/")
	if endpointHost == "" || bucket == "" {
		return fmt.Errorf("endpoint host and bucket are required")
	}
	scheme := "https"
	if !in.SSL {
		scheme = "http"
	}
	if in.Port > 0 && in.Port != 80 && in.Port != 443 && !strings.Contains(endpointHost, ":") {
		endpointHost = fmt.Sprintf("%s:%d", endpointHost, in.Port)
	}

	region := objectStorageRegion(in.Driver, endpointHost)
	path := "/" + url.PathEscape(bucket) + "/"
	requestURL := fmt.Sprintf("%s://%s%s?list-type=2&max-keys=0", scheme, endpointHost, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	signObjectStorageRequest(req, in.Username, in.Password, region, objectStorageService(in.Driver), "")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("object storage returned %s", resp.Status)
}

func objectStorageService(driver string) string {
	switch driver {
	case "s3_oss":
		return "oss"
	case "s3_obs":
		return "s3"
	default:
		return "s3"
	}
}

func objectStorageRegion(driver, host string) string {
	host = strings.ToLower(host)
	switch driver {
	case "s3_gcp":
		return "auto"
	case "s3_oss":
		if idx := strings.Index(host, "oss-"); idx >= 0 {
			rest := host[idx+4:]
			if dot := strings.Index(rest, "."); dot > 0 {
				return rest[:dot]
			}
		}
		return "cn-hangzhou"
	case "s3_obs":
		parts := strings.Split(host, ".")
		for i, part := range parts {
			if part == "obs" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
		return "ap-southeast-1"
	case "s3_aws":
		parts := strings.Split(host, ".")
		for i, part := range parts {
			if part == "s3" && i+1 < len(parts) && parts[i+1] != "amazonaws" {
				return parts[i+1]
			}
		}
		return "us-east-1"
	default:
		return "us-east-1"
	}
}

func signObjectStorageRequest(req *http.Request, accessKey, secretKey, region, service, payload string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHashBytes := sha256.Sum256([]byte(payload))
	payloadHash := hex.EncodeToString(payloadHashBytes[:])

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := req.URL.RawQuery
	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	canonicalRequestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(canonicalRequestHash[:])

	signingKey := objectStorageSigningKey(secretKey, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey,
		scope,
		signedHeaders,
		signature,
	))
}

func objectStorageSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
