package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// pingTimeout bounds the liveness check on a pooled connection so a stale or
// unreachable database can never block a request indefinitely.
const pingTimeout = 5 * time.Second

// pingDB pings db with a bounded timeout, returning an error instead of hanging
// forever when the remote database is slow or unreachable.
func pingDB(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	return db.PingContext(ctx)
}

type poolEntry struct {
	db        *sql.DB
	driver    string
	lastUsed  time.Time
	sshClient *ssh.Client
	listener  net.Listener
}

var dbPool struct {
	sync.RWMutex
	entries map[int64]*poolEntry
	// userEntries holds per-user native sessions, keyed by "connID:userID", so a
	// user connecting with their own DB login never shares a pool with the
	// connection's shared login or with another user.
	userEntries map[string]*poolEntry
}

func init() {
	dbPool.entries = make(map[int64]*poolEntry)
	dbPool.userEntries = make(map[string]*poolEntry)
}

func userPoolKey(userID, connID int64) string {
	return strconv.FormatInt(connID, 10) + ":" + strconv.FormatInt(userID, 10)
}

// GetDB returns a long-lived pooled connection for connID.
// Callers must NOT close the returned *sql.DB.
func GetDB(connID int64) (*sql.DB, string, error) {
	dbPool.RLock()
	entry, ok := dbPool.entries[connID]
	dbPool.RUnlock()

	if ok {
		if err := pingDB(entry.db); err == nil {
			// Update lastUsed under write lock to avoid the data race.
			dbPool.Lock()
			if e, still := dbPool.entries[connID]; still {
				e.lastUsed = time.Now()
			}
			dbPool.Unlock()
			return entry.db, entry.driver, nil
		}
		// Ping failed — evict under write lock. Re-check the entry is still
		// the same one we tested to avoid a TOCTOU double-close.
		dbPool.Lock()
		if current, still := dbPool.entries[connID]; still && current == entry {
			entry.db.Close()
			delete(dbPool.entries, connID)
		}
		dbPool.Unlock()
	}

	db, driver, err := openRemoteDB(connID)
	if err != nil {
		return nil, "", err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	dbPool.Lock()
	dbPool.entries[connID] = &poolEntry{db: db, driver: driver, lastUsed: time.Now()}
	dbPool.Unlock()

	return db, driver, nil
}

// EvictFromPool closes and removes the pooled DB for a connection.
func EvictFromPool(connID int64) {
	dbPool.Lock()
	defer dbPool.Unlock()
	if entry, ok := dbPool.entries[connID]; ok {
		entry.db.Close()
		if entry.listener != nil {
			entry.listener.Close()
		}
		if entry.sshClient != nil {
			entry.sshClient.Close()
		}
		delete(dbPool.entries, connID)
	}
	// Also drop any per-user sessions for this connection.
	prefix := strconv.FormatInt(connID, 10) + ":"
	for key, entry := range dbPool.userEntries {
		if strings.HasPrefix(key, prefix) {
			entry.db.Close()
			delete(dbPool.userEntries, key)
		}
	}
}

// EvictUserFromPool closes and removes a single user's native session for a
// connection — call this whenever that user's stored credentials change so a
// stale login can never be reused.
func EvictUserFromPool(userID, connID int64) {
	key := userPoolKey(userID, connID)
	dbPool.Lock()
	defer dbPool.Unlock()
	if entry, ok := dbPool.userEntries[key]; ok {
		entry.db.Close()
		delete(dbPool.userEntries, key)
	}
}

// GetDBForUser returns a pooled connection that authenticates to the target
// database as the given user's own DB login when they have stored per-user
// credentials. Behaviour:
//   - user has personal credentials    → pooled native session (DB enforces
//     natively as that login)
//   - no credentials, auth_mode shared → shared connection login (GetDB)
//   - no credentials, auth_mode per_user → error (personal login required)
//
// Callers must NOT close the returned *sql.DB.
func GetDBForUser(userID, connID int64) (*sql.DB, string, error) {
	if userID <= 0 {
		return GetDB(connID)
	}

	dbUser, encPass, hasCred := loadUserConnCredential(userID, connID)
	if !hasCred {
		if loadConnAuthMode(connID) == "per_user" {
			return nil, "", fmt.Errorf("this connection requires your own database login — ask an admin to set your credentials")
		}
		return GetDB(connID)
	}

	key := userPoolKey(userID, connID)

	dbPool.RLock()
	entry, ok := dbPool.userEntries[key]
	dbPool.RUnlock()
	if ok {
		if err := pingDB(entry.db); err == nil {
			dbPool.Lock()
			if e, still := dbPool.userEntries[key]; still {
				e.lastUsed = time.Now()
			}
			dbPool.Unlock()
			return entry.db, entry.driver, nil
		}
		dbPool.Lock()
		if current, still := dbPool.userEntries[key]; still && current == entry {
			entry.db.Close()
			delete(dbPool.userEntries, key)
		}
		dbPool.Unlock()
	}

	password, err := decryptCredential(encPass)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decrypt stored credentials")
	}
	db, driver, err := openRemoteDBWithCreds(connID, dbUser, password, true)
	if err != nil {
		return nil, "", err
	}
	// Keep per-user native pools small — many users can each hold a session.
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(15 * time.Minute)

	dbPool.Lock()
	dbPool.userEntries[key] = &poolEntry{db: db, driver: driver, lastUsed: time.Now()}
	dbPool.Unlock()

	return db, driver, nil
}

// SSHConfig holds SSH tunnel configuration.
type SSHConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Key      string
}

// createSSHTunnel establishes an SSH tunnel and returns a local port.
func createSSHTunnel(cfg SSHConfig, dbHost string, dbPort int) (int, *ssh.Client, net.Listener, error) {
	authMethods := []ssh.AuthMethod{}
	if cfg.Key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.Key))
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         10 * time.Second,
	}

	sshPort := cfg.Port
	if sshPort == 0 {
		sshPort = 22
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", cfg.Host, sshPort), sshCfg)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("SSH dial: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		sshClient.Close()
		return 0, nil, nil, fmt.Errorf("local listen: %w", err)
	}

	localPort := listener.Addr().(*net.TCPAddr).Port
	remoteAddr := fmt.Sprintf("%s:%d", dbHost, dbPort)

	go func() {
		for {
			local, err := listener.Accept()
			if err != nil {
				return
			}
			go func(local net.Conn) {
				defer local.Close()
				remote, err := sshClient.Dial("tcp", remoteAddr)
				if err != nil {
					return
				}
				defer remote.Close()
				done := make(chan struct{}, 2)
				go func() { io.Copy(remote, local); done <- struct{}{} }()
				go func() { io.Copy(local, remote); done <- struct{}{} }()
				<-done
			}(local)
		}
	}()

	return localPort, sshClient, listener, nil
}

// GetDBWithSSH opens a DB through an SSH tunnel if configured.
func GetDBWithSSH(connID int64, sshCfg *SSHConfig, dbHost string, dbPort int) (*sql.DB, string, error) {
	if sshCfg == nil || sshCfg.Host == "" {
		return GetDB(connID)
	}

	dbPool.RLock()
	entry, ok := dbPool.entries[connID]
	dbPool.RUnlock()
	if ok {
		if err := pingDB(entry.db); err == nil {
			entry.lastUsed = time.Now()
			return entry.db, entry.driver, nil
		}
		EvictFromPool(connID)
	}

	localPort, sshClient, listener, err := createSSHTunnel(*sshCfg, dbHost, dbPort)
	if err != nil {
		return nil, "", err
	}

	// Patch the connection to use localhost:localPort
	// This is handled by the caller modifying the DSN before calling sql.Open
	_ = localPort
	_ = sshClient
	_ = listener
	_ = strconv.Itoa

	return nil, "", fmt.Errorf("SSH tunnel created on local port %d — override DSN to use it", localPort)
}
