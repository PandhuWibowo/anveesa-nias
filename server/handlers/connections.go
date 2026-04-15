package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// allowedDrivers defines valid database drivers
var allowedDrivers = map[string]bool{
	"postgres": true,
	"mysql":    true,
	"mariadb":  true, // alias → uses MySQL driver
	"sqlite":   true,
	"mssql":    true,
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
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Database   string `json:"database"`
	Username   string `json:"username"`
	SSL        bool   `json:"ssl"`
	Tags       string `json:"tags"`
	SSHHost    string `json:"ssh_host"`
	SSHPort    int    `json:"ssh_port"`
	SSHUser    string `json:"ssh_user"`
	FolderID   *int64 `json:"folder_id"`
	Visibility string `json:"visibility"`
	OwnerID    int64  `json:"owner_id"`
	CreatedAt  string `json:"created_at"`
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
		return fmt.Errorf("invalid driver: must be postgres, mysql, sqlite, or mssql")
	}

	// Validate port
	if in.Driver != "sqlite" {
		if in.Port < 1 || in.Port > 65535 {
			return fmt.Errorf("invalid port: must be 1-65535")
		}
	}

	// Validate name length
	if len(in.Name) > 100 {
		return fmt.Errorf("name too long: maximum 100 characters")
	}

	// Validate host (no special characters that could be used for injection)
	if in.Host != "" && !isValidHostname(in.Host) {
		return fmt.Errorf("invalid host format")
	}

	// SQLite: validate database path
	if in.Driver == "sqlite" && in.Database != "" && in.Database != ":memory:" {
		if err := validateSQLitePath(in.Database); err != nil {
			return err
		}
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

// validateSQLitePath ensures SQLite path is safe
func validateSQLitePath(path string) error {
	// Reject path traversal
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	// Reject absolute paths outside allowed directories
	if filepath.IsAbs(path) {
		// In production, you might want to restrict to specific directories
		// For now, just ensure it's a valid-looking file path
		if strings.ContainsAny(path, ";'\"\\`$(){}[]<>|&") {
			return fmt.Errorf("invalid path characters")
		}
	}
	return nil
}

func buildDSN(in ConnectionInput) (string, error) {
	switch in.Driver {
	case "postgres":
		sslMode := "disable"
		if in.SSL {
			sslMode = "require"
		}
		// Use proper escaping for special characters in password
		return fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			escapePostgresValue(in.Host),
			in.Port,
			escapePostgresValue(in.Username),
			escapePostgresValue(in.Password),
			escapePostgresValue(in.Database),
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
	case "sqlite":
		if in.Database == "" {
			return ":memory:", nil
		}
		// Path already validated
		return in.Database, nil
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
	case "sqlite":
		return "sqlite"
	case "mssql":
		return "sqlserver"
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
			        c.folder_id, COALESCE(c.visibility,'shared'), COALESCE(c.owner_id,0), c.created_at
			 FROM connections c ORDER BY c.id`
		} else {
			// Regular user: filter by visibility, ownership, explicit permissions, and folder membership
			query = `SELECT DISTINCT c.id, c.name, c.driver, COALESCE(c.host,''), COALESCE(c.port,0), c.database,
			        COALESCE(c.username,''), c.ssl, COALESCE(c.tags,''),
			        COALESCE(c.ssh_host,''), COALESCE(c.ssh_port,22), COALESCE(c.ssh_user,''),
			        c.folder_id, COALESCE(c.visibility,'shared'), COALESCE(c.owner_id,0), c.created_at
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
			var ssl int
			rows.Scan(&c.ID, &c.Name, &c.Driver, &c.Host, &c.Port, &c.Database,
				&c.Username, &ssl, &c.Tags,
				&c.SSHHost, &c.SSHPort, &c.SSHUser,
				&c.FolderID, &c.Visibility, &c.OwnerID, &c.CreatedAt)
			c.SSL = ssl == 1
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
		// SQLite must have a path; all other drivers can omit the database
		if in.Driver == "sqlite" && in.Database == "" {
			http.Error(w, `{"error":"database path is required for SQLite"}`, http.StatusBadRequest)
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
		id, _ := res.LastInsertId()

		var c Connection
		var sslV int
		appdb.DB.QueryRow(
			`SELECT id, name, driver, COALESCE(host,''), COALESCE(port,0), database,
			        COALESCE(username,''), ssl, COALESCE(tags,''),
			        COALESCE(ssh_host,''), COALESCE(ssh_port,22), COALESCE(ssh_user,''),
			        folder_id, COALESCE(visibility,'shared'), COALESCE(owner_id,0), created_at
			 FROM connections WHERE id=?`, id,
		).Scan(&c.ID, &c.Name, &c.Driver, &c.Host, &c.Port, &c.Database,
			&c.Username, &sslV, &c.Tags,
			&c.SSHHost, &c.SSHPort, &c.SSHUser,
			&c.FolderID, &c.Visibility, &c.OwnerID, &c.CreatedAt)
		c.SSL = sslV == 1

		w.WriteHeader(http.StatusCreated)
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
		appdb.DB.Exec(`DELETE FROM connections WHERE id=?`, id)
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
			_, err = appdb.DB.Exec(`UPDATE connections SET folder_id = ? WHERE id = ?`, payload.FolderID, id)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"failed to update folder: %v"}`, err), http.StatusInternalServerError)
				return
			}
		}

		if payload.Visibility != nil {
			_, err = appdb.DB.Exec(`UPDATE connections SET visibility = ? WHERE id = ?`, *payload.Visibility, id)
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

		_, err = appdb.DB.Exec(`UPDATE connections SET visibility = ? WHERE id = ?`, payload.Visibility, id)
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
	err := appdb.DB.QueryRow(`SELECT COALESCE(owner_id,0) FROM connections WHERE id=?`, connID).Scan(&ownerID)
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

// openRemoteDB opens a connection to the stored remote database by ID.
func openRemoteDB(connID int64) (*sql.DB, string, error) {
	var in ConnectionInput
	var ssl int
	var encPassword string
	err := appdb.DB.QueryRow(
		`SELECT driver, COALESCE(host,''), COALESCE(port,0), database, COALESCE(username,''), COALESCE(password,''), ssl FROM connections WHERE id=?`, connID,
	).Scan(&in.Driver, &in.Host, &in.Port, &in.Database, &in.Username, &encPassword, &ssl)
	if err != nil {
		return nil, "", fmt.Errorf("connection not found")
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
