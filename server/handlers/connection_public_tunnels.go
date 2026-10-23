package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	appdb "github.com/anveesa/nias/db"
	"golang.org/x/crypto/ssh"
)

// PublicTunnel is a raw TCP port-forward exposing a saved connection's native
// wire protocol on a public host:port, so it can be reached by a local DB
// client (psql, TablePlus, ...) instead of only through the in-app SQL editor.
//
// Unlike everything else under handlers/, the proxy relays opaque bytes once
// established, so Anveesa cannot see or audit individual queries sent over a
// tunnel the way it does for in-app SQL execution.
type PublicTunnel struct {
	ID           int64   `json:"id"`
	ConnectionID int64   `json:"connection_id"`
	Host         string  `json:"host"`
	Port         int     `json:"port"`
	Driver       string  `json:"driver"`
	Database     string  `json:"database"`
	Username     string  `json:"username"`
	Status       string  `json:"status"`
	CreatedBy    string  `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	RevokedAt    *string `json:"revoked_at,omitempty"`
}

// tunnelMaxConcurrentClients caps how many client sessions one tunnel will
// relay at once. Each accepted connection dials the target (and, for SSH
// connections, a fresh jump-host session), so an unbounded accept loop on a
// publicly reachable port is a cheap way to exhaust the server or the jump
// host's SSH connection limit.
const tunnelMaxConcurrentClients = 32

type activeTunnel struct {
	connID   int64
	port     int
	listener net.Listener

	mu      sync.Mutex
	clients map[net.Conn]struct{}
}

// trackClient registers an accepted client, reporting false when the tunnel is
// already at its concurrency cap.
func (t *activeTunnel) trackClient(c net.Conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.clients) >= tunnelMaxConcurrentClients {
		return false
	}
	t.clients[c] = struct{}{}
	return true
}

func (t *activeTunnel) untrackClient(c net.Conn) {
	t.mu.Lock()
	delete(t.clients, c)
	t.mu.Unlock()
}

// closeClients force-closes every in-flight client session. Without this,
// revoking a tunnel would only stop new connections while already-relayed
// sessions kept running to completion.
func (t *activeTunnel) closeClients() {
	t.mu.Lock()
	clients := make([]net.Conn, 0, len(t.clients))
	for c := range t.clients {
		clients = append(clients, c)
	}
	t.clients = make(map[net.Conn]struct{})
	t.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
}

var tunnelCfg struct {
	enabled    bool
	publicHost string
	bindAddr   string
	portStart  int
	portEnd    int
}

var tunnelRegistry struct {
	sync.RWMutex
	byConn map[int64]*activeTunnel
}

func init() {
	tunnelRegistry.byConn = make(map[int64]*activeTunnel)
}

// SetTunnelConfig configures the public tunnel feature. Called once at
// startup from main.go, mirroring SetEncryptionKey/SetJWTSecret.
func SetTunnelConfig(enabled bool, publicHost, bindAddr string, portStart, portEnd int) {
	tunnelCfg.enabled = enabled
	tunnelCfg.publicHost = publicHost
	tunnelCfg.bindAddr = bindAddr
	tunnelCfg.portStart = portStart
	tunnelCfg.portEnd = portEnd
}

func generateTunnelToken() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// loadConnectionForTunnel fetches the fields needed to dial a connection's
// target database, decrypting SSH credentials when present.
func loadConnectionForTunnel(connID int64) (Connection, error) {
	var c Connection
	var ssl int
	var encSSHPassword, encSSHKey string

	err := appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT id, name, driver, COALESCE(host,''), COALESCE(port,0), database,
		COALESCE(username,''), ssl, COALESCE(ssh_host,''), COALESCE(ssh_port,22), COALESCE(ssh_user,''), ssh_password, ssh_key
		FROM connections WHERE id=?`), connID).Scan(
		&c.ID, &c.Name, &c.Driver, &c.Host, &c.Port, &c.Database,
		&c.Username, &ssl, &c.SSHHost, &c.SSHPort, &c.SSHUser, &encSSHPassword, &encSSHKey)
	if err != nil {
		return c, err
	}
	c.SSL = ssl == 1

	if encSSHPassword != "" {
		if dec, err := decryptCredential(encSSHPassword); err == nil {
			c.SSHPassword = dec
		}
	}
	if encSSHKey != "" {
		if dec, err := decryptCredential(encSSHKey); err == nil {
			c.SSHKey = dec
		}
	}
	return c, nil
}

// allocateTunnelPortLocked scans the configured range for a port not already
// held by the in-memory registry and confirms it's actually free to bind.
// Callers must hold tunnelRegistry's write lock.
func allocateTunnelPortLocked() (int, error) {
	used := make(map[int]bool, len(tunnelRegistry.byConn))
	for _, t := range tunnelRegistry.byConn {
		used[t.port] = true
	}

	for port := tunnelCfg.portStart; port <= tunnelCfg.portEnd; port++ {
		if used[port] {
			continue
		}
		l, err := net.Listen("tcp", net.JoinHostPort(tunnelCfg.bindAddr, strconv.Itoa(port)))
		if err != nil {
			continue
		}
		l.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port available in range %d-%d", tunnelCfg.portStart, tunnelCfg.portEnd)
}

// dialTunnelTarget opens a connection to the real database, either directly
// or (when the saved connection has SSH fields set) via an SSH client acting
// as a jump host — reusing the same ssh.Dial + Dial relay shape as the
// (otherwise dead) createSSHTunnel in pool.go.
func dialTunnelTarget(conn Connection) (net.Conn, *ssh.Client, error) {
	targetAddr := net.JoinHostPort(conn.Host, strconv.Itoa(conn.Port))

	if conn.SSHHost == "" {
		c, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
		return c, nil, err
	}

	authMethods := []ssh.AuthMethod{}
	if conn.SSHKey != "" {
		if signer, err := ssh.ParsePrivateKey([]byte(conn.SSHKey)); err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if conn.SSHPassword != "" {
		authMethods = append(authMethods, ssh.Password(conn.SSHPassword))
	}

	sshPort := conn.SSHPort
	if sshPort == 0 {
		sshPort = 22
	}
	sshClient, err := ssh.Dial("tcp", net.JoinHostPort(conn.SSHHost, strconv.Itoa(sshPort)), &ssh.ClientConfig{
		User:            conn.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         10 * time.Second,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("ssh dial: %w", err)
	}
	remote, err := sshClient.Dial("tcp", targetAddr)
	if err != nil {
		sshClient.Close()
		return nil, nil, err
	}
	return remote, sshClient, nil
}

// startTunnelListener binds a public listener for conn on port and relays
// every accepted connection to the real database, byte for byte.
func startTunnelListener(conn Connection, port int) (*activeTunnel, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(tunnelCfg.bindAddr, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}

	t := &activeTunnel{connID: conn.ID, port: port, listener: listener, clients: make(map[net.Conn]struct{})}

	go func() {
		for {
			local, err := listener.Accept()
			if err != nil {
				return
			}
			if !t.trackClient(local) {
				log.Printf("public tunnel: connection %d: refusing client, %d concurrent limit reached", conn.ID, tunnelMaxConcurrentClients)
				local.Close()
				continue
			}
			go func(local net.Conn) {
				defer t.untrackClient(local)
				defer local.Close()
				remote, sshClient, err := dialTunnelTarget(conn)
				if err != nil {
					log.Printf("public tunnel: connection %d: dial target failed: %v", conn.ID, err)
					return
				}
				defer remote.Close()
				if sshClient != nil {
					defer sshClient.Close()
				}
				done := make(chan struct{}, 2)
				go func() { io.Copy(remote, local); done <- struct{}{} }()
				go func() { io.Copy(local, remote); done <- struct{}{} }()
				<-done
			}(local)
		}
	}()

	return t, nil
}

// stopTunnel tears a tunnel down completely: stop accepting, then drop every
// client session already being relayed, so a revoked tunnel stops forwarding
// immediately rather than whenever its current clients happen to disconnect.
func stopTunnel(t *activeTunnel) {
	if t == nil {
		return
	}
	if t.listener != nil {
		t.listener.Close()
	}
	t.closeClients()
}

func scanPublicTunnelRow(row interface{ Scan(...any) error }) (PublicTunnel, error) {
	var t PublicTunnel
	var revokedAt sql.NullString
	err := row.Scan(&t.ID, &t.ConnectionID, &t.Port, &t.Status, &t.CreatedBy, &t.CreatedAt, &revokedAt)
	if revokedAt.Valid {
		t.RevokedAt = &revokedAt.String
	}
	return t, err
}

// GetPublicTunnel returns the active tunnel for a connection, if any.
func GetPublicTunnel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, ok := parseTunnelConnID(w, r)
		if !ok {
			return
		}

		row := appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT id, connection_id, port, status, created_by, created_at, revoked_at
			 FROM connection_public_tunnels WHERE connection_id=? AND status='active'`), connID)
		t, err := scanPublicTunnelRow(row)
		if err != nil {
			http.Error(w, `{"error":"no active tunnel"}`, http.StatusNotFound)
			return
		}

		conn, err := loadConnectionForTunnel(connID)
		if err != nil {
			http.Error(w, `{"error":"connection not found"}`, http.StatusNotFound)
			return
		}
		t.Host = tunnelCfg.publicHost
		t.Driver = conn.Driver
		t.Database = conn.Database
		t.Username = conn.Username
		json.NewEncoder(w).Encode(t)
	}
}

// CreatePublicTunnel opens (or returns the existing) public tunnel for a
// connection. Idempotent by design: a tunnel is a permanent link until
// explicitly revoked.
func CreatePublicTunnel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if !tunnelCfg.enabled {
			http.Error(w, `{"error":"public tunnels are disabled on this server"}`, http.StatusForbidden)
			return
		}

		connID, ok := parseTunnelConnID(w, r)
		if !ok {
			return
		}
		if !canModifyConnection(r, connID) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		conn, err := loadConnectionForTunnel(connID)
		if err != nil {
			http.Error(w, `{"error":"connection not found"}`, http.StatusNotFound)
			return
		}

		// The whole allocate -> bind -> insert -> register sequence runs under
		// a single write lock. Splitting it would let two concurrent requests
		// for the same connection both pass the "already exists" check, bind
		// two listeners, and leave the loser's listener bound and publicly
		// reachable with nothing in the registry to revoke it.
		tunnelRegistry.Lock()
		if existing, ok := tunnelRegistry.byConn[connID]; ok {
			tunnelRegistry.Unlock()
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(PublicTunnel{
				ConnectionID: connID, Host: tunnelCfg.publicHost, Port: existing.port,
				Driver: conn.Driver, Database: conn.Database, Username: conn.Username, Status: "active",
			})
			return
		}

		// A row can be left 'active' with no listener behind it (a port that
		// could not be rebound at startup). Clear it first so we don't stack a
		// second active row for the same connection.
		revokeTunnelRows(connID)

		port, err := allocateTunnelPortLocked()
		if err != nil {
			tunnelRegistry.Unlock()
			http.Error(w, jsonError(err.Error()), http.StatusInsufficientStorage)
			return
		}

		t, err := startTunnelListener(conn, port)
		if err != nil {
			tunnelRegistry.Unlock()
			http.Error(w, jsonError("failed to start tunnel: "+err.Error()), http.StatusBadGateway)
			return
		}

		token, err := generateTunnelToken()
		if err != nil {
			stopTunnel(t)
			tunnelRegistry.Unlock()
			http.Error(w, `{"error":"failed to generate tunnel token"}`, http.StatusInternalServerError)
			return
		}

		username := r.Header.Get("X-Username")
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		id, err := insertTunnelRow(connID, port, token, username, now)
		if err != nil {
			stopTunnel(t)
			tunnelRegistry.Unlock()
			http.Error(w, `{"error":"failed to persist tunnel"}`, http.StatusInternalServerError)
			return
		}

		tunnelRegistry.byConn[connID] = t
		tunnelRegistry.Unlock()

		go WriteFeatureAccessAudit(username, "connection_public_tunnel_create", conn.Name,
			fmt.Sprintf("host=%s port=%d", tunnelCfg.publicHost, port))

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(PublicTunnel{
			ID: id, ConnectionID: connID, Host: tunnelCfg.publicHost, Port: port,
			Driver: conn.Driver, Database: conn.Database, Username: conn.Username,
			Status: "active", CreatedBy: username, CreatedAt: now,
		})
	}
}

// revokeTunnelRows marks every active row for connID revoked. Kept in one
// place because it is the DB half of teardown, and callers must not forget it
// when they also stop the listener.
func revokeTunnelRows(connID int64) {
	appdb.DB.Exec(appdb.ConvertQuery(
		`UPDATE connection_public_tunnels SET status='revoked', revoked_at=? WHERE connection_id=? AND status='active'`),
		time.Now().UTC().Format("2006-01-02 15:04:05"), connID)
}

// insertTunnelRow persists a new active tunnel. PostgreSQL and MySQL need
// RETURNING to get the id back; LastInsertId returns 0 there.
func insertTunnelRow(connID int64, port int, token, username, now string) (int64, error) {
	if appdb.IsPostgreSQL() || appdb.IsMySQL() {
		var id int64
		err := appdb.DB.QueryRow(appdb.ConvertQuery(
			`INSERT INTO connection_public_tunnels (connection_id, port, token, status, created_by, created_at)
			 VALUES (?,?,?,'active',?,?) RETURNING id`), connID, port, token, username, now).Scan(&id)
		return id, err
	}
	res, err := appdb.DB.Exec(appdb.ConvertQuery(
		`INSERT INTO connection_public_tunnels (connection_id, port, token, status, created_by, created_at)
		 VALUES (?,?,?,'active',?,?)`), connID, port, token, username, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// StopConnectionTunnel tears down and revokes any tunnel for a connection that
// no longer exists. Called from DeleteConnection so deleting a connection can
// never leave a public forwarder pointing at it.
func StopConnectionTunnel(connID int64) {
	tunnelRegistry.Lock()
	t, ok := tunnelRegistry.byConn[connID]
	if ok {
		stopTunnel(t)
		delete(tunnelRegistry.byConn, connID)
	}
	tunnelRegistry.Unlock()
	revokeTunnelRows(connID)
}

// RefreshConnectionTunnel rebinds a live tunnel so it dials the connection's
// current host and credentials. A tunnel captures its target at bind time, so
// an edit that moves the database would otherwise leave the old target
// publicly reachable. If the rebind fails the tunnel is revoked outright
// rather than left in a half-working state.
func RefreshConnectionTunnel(connID int64) {
	tunnelRegistry.Lock()
	t, ok := tunnelRegistry.byConn[connID]
	if !ok {
		tunnelRegistry.Unlock()
		return
	}
	port := t.port
	stopTunnel(t)
	delete(tunnelRegistry.byConn, connID)
	tunnelRegistry.Unlock()

	conn, err := loadConnectionForTunnel(connID)
	if err != nil {
		revokeTunnelRows(connID)
		return
	}
	nt, err := startTunnelListener(conn, port)
	if err != nil {
		log.Printf("public tunnel: could not rebind connection %d on port %d after update: %v; revoking", connID, port, err)
		revokeTunnelRows(connID)
		return
	}

	tunnelRegistry.Lock()
	tunnelRegistry.byConn[connID] = nt
	tunnelRegistry.Unlock()
	log.Printf("public tunnel: rebound connection %d on %s:%d after update", connID, tunnelCfg.publicHost, port)
}

// RevokePublicTunnel tears down the listener and marks the tunnel revoked.
func RevokePublicTunnel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		connID, ok := parseTunnelConnID(w, r)
		if !ok {
			return
		}
		if !canModifyConnection(r, connID) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		tunnelRegistry.Lock()
		t, exists := tunnelRegistry.byConn[connID]
		if exists {
			stopTunnel(t)
			delete(tunnelRegistry.byConn, connID)
		}
		tunnelRegistry.Unlock()

		var connName string
		appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT name FROM connections WHERE id=?`), connID).Scan(&connName)

		revokeTunnelRows(connID)

		username := r.Header.Get("X-Username")
		go WriteFeatureAccessAudit(username, "connection_public_tunnel_revoke", connName, "")

		w.WriteHeader(http.StatusNoContent)
	}
}

// RestoreActivePublicTunnels re-binds listeners for tunnels left active from
// a previous run, so a "permanent" link survives a server restart. Ports
// that can no longer be bound (e.g. already taken) are logged and left out
// of the in-memory registry rather than silently reassigned.
func RestoreActivePublicTunnels() {
	rows, err := appdb.DB.Query(appdb.ConvertQuery(
		`SELECT connection_id, port FROM connection_public_tunnels WHERE status='active'`))
	if err != nil {
		log.Printf("public tunnel: failed to load active tunnels: %v", err)
		return
	}
	defer rows.Close()

	type pending struct {
		connID int64
		port   int
	}
	var toRestore []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.connID, &p.port); err == nil {
			toRestore = append(toRestore, p)
		}
	}

	for _, p := range toRestore {
		conn, err := loadConnectionForTunnel(p.connID)
		if err != nil {
			log.Printf("public tunnel: connection %d no longer exists, leaving tunnel row as-is", p.connID)
			continue
		}
		t, err := startTunnelListener(conn, p.port)
		if err != nil {
			log.Printf("public tunnel: could not rebind connection %d on port %d: %v", p.connID, p.port, err)
			continue
		}
		tunnelRegistry.Lock()
		tunnelRegistry.byConn[p.connID] = t
		tunnelRegistry.Unlock()
		log.Printf("public tunnel: restored connection %d on %s:%d", p.connID, tunnelCfg.publicHost, p.port)
	}
}

func parseTunnelConnID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
	idStr := strings.SplitN(path, "/", 2)[0]
	connID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
		return 0, false
	}
	return connID, true
}
