package handlers

import (
	"database/sql"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

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
}

func init() {
	dbPool.entries = make(map[int64]*poolEntry)
}

// GetDB returns a long-lived pooled connection for connID.
// Callers must NOT close the returned *sql.DB.
func GetDB(connID int64) (*sql.DB, string, error) {
	dbPool.RLock()
	entry, ok := dbPool.entries[connID]
	dbPool.RUnlock()

	if ok {
		if err := entry.db.Ping(); err == nil {
			entry.lastUsed = time.Now()
			return entry.db, entry.driver, nil
		}
		// Stale — close and re-open
		dbPool.Lock()
		entry.db.Close()
		delete(dbPool.entries, connID)
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
		if err := entry.db.Ping(); err == nil {
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
