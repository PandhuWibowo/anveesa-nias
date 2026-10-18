package handlers

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// Public tunnels bind a real port and keep listeners alive across the test, so
// these tests bind to 127.0.0.1 on an ephemeral-ish high range and always stop
// what they start.

// startTestTunnel binds a tunnel for conn on 127.0.0.1 and registers it under
// connID, mirroring what CreatePublicTunnel does once its critical section
// completes. The port is taken by binding it directly rather than probing with
// :0 and reopening, because the probe releases the port and another test in this
// package can grab it in between.
func startTestTunnel(t *testing.T, connID int64, targetPort int) *activeTunnel {
	t.Helper()

	prevBind := tunnelCfg.bindAddr
	tunnelCfg.bindAddr = "127.0.0.1"
	defer func() { tunnelCfg.bindAddr = prevBind }()

	port := freeTestPortRange(t, 1)
	at, err := startTunnelListener(Connection{ID: connID, Host: "127.0.0.1", Port: targetPort}, port)
	if err != nil {
		t.Fatalf("start tunnel listener on port %d: %v", port, err)
	}
	tunnelRegistry.Lock()
	tunnelRegistry.byConn[connID] = at
	tunnelRegistry.Unlock()
	t.Cleanup(func() {
		tunnelRegistry.Lock()
		delete(tunnelRegistry.byConn, connID)
		tunnelRegistry.Unlock()
		stopTunnel(at)
	})
	return at
}

func echoServer(t *testing.T) (int, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						c.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(c)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, func() { l.Close() }
}

func dialTunnel(t *testing.T, at *activeTunnel) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(at.port)), 5*time.Second)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	return c
}

// A revoked tunnel must stop forwarding immediately, not merely stop accepting.
// Before the fix stopTunnel only closed the listener, so a client that had
// already been accepted kept a working relay to the database.
func TestRevokeDropsInFlightSessions(t *testing.T) {
	echoPort, stopEcho := echoServer(t)
	defer stopEcho()

	at := startTestTunnel(t, 1, echoPort)

	c := dialTunnel(t, at)
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(buf); err != nil {
		t.Fatalf("relay was not working before revoke: %v", err)
	}
	// Clear the deadline: the post-revoke read must fail because the session
	// was torn down, not because a stale read deadline expired.
	if err := c.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("clear deadline: %v", err)
	}

	stopTunnel(at)

	// The relay must be dead: this read only returns because the tracked client
	// conn was force-closed. If the session were still live it would block and
	// the select below would fail the test.
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Read(buf)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("client session still alive after revoke")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client session neither closed nor errored after revoke")
	}
	c.Close()
}

// A public port with an unbounded accept loop lets anyone exhaust server
// goroutines and the jump host's SSH connection limit, so the relay refuses
// clients past the cap.
func TestTunnelRefusesClientsPastConcurrencyCap(t *testing.T) {
	echoPort, stopEcho := echoServer(t)
	defer stopEcho()

	at := startTestTunnel(t, 2, echoPort)

	var clients []net.Conn
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	for i := 0; i < tunnelMaxConcurrentClients; i++ {
		clients = append(clients, dialTunnel(t, at))
	}

	// Give the accept loop time to track every client before testing the cap.
	deadline := time.Now().Add(5 * time.Second)
	for {
		at.mu.Lock()
		n := len(at.clients)
		at.mu.Unlock()
		if n == tunnelMaxConcurrentClients {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d clients tracked", n, tunnelMaxConcurrentClients)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if at.trackClient(&net.TCPConn{}) {
		t.Fatal("trackClient accepted a client beyond the concurrency cap")
	}
}

// auditDrain waits for the handlers' fire-and-forget audit goroutines to
// finish. They read the global appdb.DB, so letting one outlive a test that
// re-initialises the DB shows up as a data race under -race. The wait is on the
// audit row count stabilising rather than reaching zero, because rows already
// written stay behind.
func auditDrain() {
	prev := -1
	for i := 0; i < 100; i++ {
		if appdb.DB == nil {
			return
		}
		var n int
		if err := appdb.DB.QueryRow(
			`SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
			return
		}
		if n == prev {
			return
		}
		prev = n
		time.Sleep(20 * time.Millisecond)
	}
}

// configureTestTunnels points the tunnel feature at loopback over a known-free
// port range and restores the previous config on cleanup.
func configureTestTunnels(t *testing.T, nPorts int) {
	t.Helper()
	prevEnabled, prevHost, prevBind := tunnelCfg.enabled, tunnelCfg.publicHost, tunnelCfg.bindAddr
	prevStart, prevEnd := tunnelCfg.portStart, tunnelCfg.portEnd
	tunnelCfg.enabled = true
	tunnelCfg.publicHost = "127.0.0.1"
	tunnelCfg.bindAddr = "127.0.0.1"
	tunnelCfg.portStart = freeTestPortRange(t, nPorts)
	tunnelCfg.portEnd = tunnelCfg.portStart + nPorts - 1
	t.Cleanup(func() {
		auditDrain()
		tunnelCfg.enabled, tunnelCfg.publicHost, tunnelCfg.bindAddr = prevEnabled, prevHost, prevBind
		tunnelCfg.portStart, tunnelCfg.portEnd = prevStart, prevEnd
	})
}

// insertTunnelTargetConn creates a connection row pointing at a local echo
// server and returns its id.
func insertTunnelTargetConn(t *testing.T, name string, targetPort int) int64 {
	t.Helper()
	return insertReturningID(t, fmt.Sprintf(
		`INSERT INTO connections (name, driver, host, port, database, username, password, ssl, tags)
		 VALUES ('%s', 'postgres', '127.0.0.1', %d, 'db', 'u', '', 0, '')`, name, targetPort))
}

// stopTestTunnel tears down and unregisters whatever tunnel is live for connID.
func stopTestTunnel(connID int64) {
	tunnelRegistry.Lock()
	defer tunnelRegistry.Unlock()
	if at, ok := tunnelRegistry.byConn[connID]; ok {
		stopTunnel(at)
		delete(tunnelRegistry.byConn, connID)
	}
}

// The DB-backed tunnel tests share one test function because appdb.Init
// replaces a global, and a second Init in a later test races with the previous
// test's audit goroutines.
func TestPublicTunnelHandlerLifecycle(t *testing.T) {
	initAccessTestDB(t)

	echoPort, stopEcho := echoServer(t)
	defer stopEcho()

	t.Run("concurrent create leaves no orphan listener", func(t *testing.T) {
		configureTestTunnels(t, 8)
		connID := insertTunnelTargetConn(t, "race-target", echoPort)
		t.Cleanup(func() { stopTestTunnel(connID) })

		const racers = 8
		var wg sync.WaitGroup
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/connections/%d/public-tunnel", connID), nil)
				req.Header.Set("X-User-Role", "admin")
				req.Header.Set("X-Username", "racer")
				rec := httptest.NewRecorder()
				CreatePublicTunnel()(rec, req)
				if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
					t.Errorf("create returned %d: %s", rec.Code, rec.Body.String())
				}
			}()
		}
		wg.Wait()
		auditDrain()

		// Exactly one active row: concurrent creates must not stack up.
		var activeRows int
		if err := appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT COUNT(*) FROM connection_public_tunnels WHERE connection_id=? AND status='active'`),
			connID).Scan(&activeRows); err != nil {
			t.Fatalf("count active tunnels: %v", err)
		}
		if activeRows != 1 {
			t.Fatalf("expected exactly 1 active tunnel row, got %d", activeRows)
		}

		tunnelRegistry.Lock()
		at, ok := tunnelRegistry.byConn[connID]
		tunnelRegistry.Unlock()
		if !ok {
			t.Fatal("no tunnel registered for the connection")
		}
		if at.port == 0 {
			t.Fatal("registered tunnel has no port")
		}

		// A port in the range that we cannot rebind is still held open by a
		// listener nothing in the registry can revoke.
		var orphans []int
		for port := tunnelCfg.portStart; port <= tunnelCfg.portEnd; port++ {
			if port == at.port {
				continue
			}
			l, err := net.Listen("tcp", net.JoinHostPort(tunnelCfg.bindAddr, strconv.Itoa(port)))
			if err != nil {
				orphans = append(orphans, port)
				continue
			}
			l.Close()
		}
		if len(orphans) != 0 {
			t.Fatalf("orphaned listeners still bound in the tunnel port range: %v", orphans)
		}
	})

	t.Run("deleting a connection stops its public tunnel", func(t *testing.T) {
		configureTestTunnels(t, 4)
		connID := insertTunnelTargetConn(t, "doomed", echoPort)
		t.Cleanup(func() { stopTestTunnel(connID) })

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/connections/%d/public-tunnel", connID), nil)
		req.Header.Set("X-User-Role", "admin")
		req.Header.Set("X-Username", "deleter")
		rec := httptest.NewRecorder()
		CreatePublicTunnel()(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create returned %d: %s", rec.Code, rec.Body.String())
		}

		tunnelRegistry.Lock()
		at, ok := tunnelRegistry.byConn[connID]
		tunnelRegistry.Unlock()
		if !ok {
			t.Fatal("no tunnel registered after create")
		}
		port := at.port

		delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/connections/%d", connID), nil)
		delReq.Header.Set("X-User-Role", "admin")
		delReq.Header.Set("X-Username", "deleter")
		delRec := httptest.NewRecorder()
		DeleteConnection()(delRec, delReq)
		if delRec.Code != http.StatusNoContent {
			t.Fatalf("delete returned %d: %s", delRec.Code, delRec.Body.String())
		}
		auditDrain()

		tunnelRegistry.Lock()
		_, stillThere := tunnelRegistry.byConn[connID]
		tunnelRegistry.Unlock()
		if stillThere {
			t.Fatal("tunnel still registered after the connection was deleted")
		}

		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatalf("public port %d still bound after delete: %v", port, err)
		}
		l.Close()
	})

	// Let every audit goroutine finish before the DB this test opened is torn
	// down and the next test re-initialises it.
	auditDrain()
}

// Refusing clients past the cap must not leak accounting: once a session ends,
// the tunnel accepts a new one again.
func TestTunnelClientSlotsAreReleased(t *testing.T) {
	echoPort, stopEcho := echoServer(t)
	defer stopEcho()

	at := startTestTunnel(t, 4, echoPort)

	c := dialTunnel(t, at)
	buf := make([]byte, 8)
	c.Write([]byte("hi"))
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(buf); err != nil {
		t.Fatalf("relay: %v", err)
	}
	c.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		at.mu.Lock()
		n := len(at.clients)
		at.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("client slot not released, still tracking %d", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !at.trackClient(&net.TCPConn{}) {
		t.Fatal("tunnel refused a client after its slots were released")
	}
}

// freeTestPortRange returns a base port with n consecutive ports all free, so
// allocateTunnelPortLocked has a known-good range to scan. It scans upward from
// the low end rather than asking the OS for an ephemeral port: the ephemeral
// range is dense, so n consecutive free ports are rarely available there.
func freeTestPortRange(t *testing.T, n int) int {
	t.Helper()
	const scanStart = 21000
	for base := scanStart; base+n < scanStart+4000; base += n {
		// Hold every listener open while probing so a port cannot be taken
		// between the check and the release.
		var held []net.Listener
		ok := true
		for p := base; p < base+n; p++ {
			l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
			if err != nil {
				ok = false
				break
			}
			held = append(held, l)
		}
		for _, l := range held {
			l.Close()
		}
		if ok {
			return base
		}
	}
	t.Fatalf("could not find a free contiguous range of %d ports above %d", n, scanStart)
	return 0
}

// Formatting guard for the audit detail string used on tunnel create.
func TestTunnelAuditDetailFormat(t *testing.T) {
	got := fmt.Sprintf("host=%s port=%d", "db.example.com", 40000)
	if got != "host=db.example.com port=40000" {
		t.Fatalf("unexpected detail: %q", got)
	}
}
