package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/ssh"
)

// Docker hosts are reached over SSH: we open an SSH connection to the VM and
// dial its local Docker daemon socket (/var/run/docker.sock), then speak the
// Docker Engine REST API across it. This needs nothing exposed on the VM beyond
// the running daemon — no TCP port, no TLS certs, no docker CLI.

// ── Types ────────────────────────────────────────────────────────────────

type DockerHost struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SSHHost     string `json:"ssh_host"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user"`
	SSHPassword string `json:"ssh_password,omitempty"`
	SSHKey      string `json:"ssh_key,omitempty"`
	SocketPath  string `json:"socket_path"`
	OwnerID     int64  `json:"owner_id"`
	CreatedAt   string `json:"created_at"`
}

type DockerHostInput struct {
	Name        string `json:"name"`
	SSHHost     string `json:"ssh_host"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user"`
	SSHPassword string `json:"ssh_password"`
	SSHKey      string `json:"ssh_key"`
	SocketPath  string `json:"socket_path"`
}

// Output structs use lowercase json tags. Go's JSON decoder matches keys
// case-insensitively, so these also decode the Docker API's capitalized keys.

type dockerContainer struct {
	ID      string            `json:"id"`
	Names   []string          `json:"names"`
	Image   string            `json:"image"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Created int64             `json:"created"`
	Ports   []dockerPort      `json:"ports"`
	Labels  map[string]string `json:"labels"`
}

type dockerPort struct {
	IP          string `json:"ip"`
	PrivatePort int    `json:"privatePort"`
	PublicPort  int    `json:"publicPort"`
	Type        string `json:"type"`
}

type dockerImage struct {
	ID       string   `json:"id"`
	RepoTags []string `json:"repoTags"`
	Size     int64    `json:"size"`
	Created  int64    `json:"created"`
}

var dockerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
var dockerAllowedActions = map[string]bool{"start": true, "stop": true, "restart": true}

// ── Connection over SSH ───────────────────────────────────────────────────

type dockerConn struct {
	http   *http.Client // short-lived requests (30s timeout)
	stream *http.Client // long-lived requests (image pulls, follows) — no timeout
	ssh    *ssh.Client
}

func (d *dockerConn) Close() {
	if d.ssh != nil {
		d.ssh.Close()
	}
}

func dialDocker(h *DockerHost) (*dockerConn, error) {
	socket := strings.TrimSpace(h.SocketPath)
	if socket == "" {
		socket = "/var/run/docker.sock"
	}

	var dialCtx func(ctx context.Context, network, addr string) (net.Conn, error)
	var sshClient *ssh.Client

	if strings.TrimSpace(h.SSHHost) == "" {
		// Local mode — talk to the daemon socket on this machine directly.
		dialCtx = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}
	} else {
		var err error
		sshClient, err = sshClientForHost(h)
		if err != nil {
			return nil, err
		}
		dialCtx = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return sshClient.Dial("unix", socket)
		}
	}

	transport := &http.Transport{DialContext: dialCtx}
	return &dockerConn{
		http:   &http.Client{Timeout: 30 * time.Second, Transport: transport},
		stream: &http.Client{Transport: transport},
		ssh:    sshClient,
	}, nil
}

// sshClientForHost opens an SSH connection to a remote Docker host.
func sshClientForHost(h *DockerHost) (*ssh.Client, error) {
	authMethods := []ssh.AuthMethod{}
	if strings.TrimSpace(h.SSHKey) != "" {
		signer, err := ssh.ParsePrivateKey([]byte(h.SSHKey))
		if err != nil {
			return nil, fmt.Errorf("parse ssh key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if h.SSHPassword != "" {
		authMethods = append(authMethods, ssh.Password(h.SSHPassword))
	}
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no SSH credentials configured")
	}
	port := h.SSHPort
	if port == 0 {
		port = 22
	}
	cfg := &ssh.ClientConfig{
		User:            h.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", h.SSHHost, port), cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	return client, nil
}

// dialDockerSocket opens a single raw connection to the Docker socket. It is
// used for the hijacked exec/attach streams (interactive terminal) that the
// pooled http.Client can't expose. The returned cleanup closes any SSH client.
func dialDockerSocket(h *DockerHost) (net.Conn, func(), error) {
	socket := strings.TrimSpace(h.SocketPath)
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	if strings.TrimSpace(h.SSHHost) == "" {
		c, err := net.DialTimeout("unix", socket, 10*time.Second)
		return c, func() {}, err
	}
	sc, err := sshClientForHost(h)
	if err != nil {
		return nil, nil, err
	}
	c, err := sc.Dial("unix", socket)
	if err != nil {
		sc.Close()
		return nil, nil, err
	}
	return c, func() { sc.Close() }, nil
}

// do issues a Docker Engine API request. The host part of the URL is ignored —
// the custom transport always dials the unix socket (directly or over SSH).
func (d *dockerConn) do(method, path string, query url.Values) (*http.Response, error) {
	u := "http://docker" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		return nil, err
	}
	return d.http.Do(req)
}

// doBody issues a request with a JSON body. If stream is true it uses the
// no-timeout client (for long operations like image pulls).
func (d *dockerConn) doBody(method, path string, query url.Values, body interface{}, stream bool) (*http.Response, error) {
	u := "http://docker" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if stream {
		return d.stream.Do(req)
	}
	return d.http.Do(req)
}

// getJSON performs a GET and decodes a JSON body into out.
func (d *dockerConn) getJSON(path string, query url.Values, out interface{}) error {
	resp, err := d.do(http.MethodGet, path, query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("docker api %s: %s", path, dockerErrBody(resp))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func dockerErrBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return resp.Status
	}
	// Docker errors are usually {"message":"..."}
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &e) == nil && e.Message != "" {
		return e.Message
	}
	return msg
}

// ── Persistence ───────────────────────────────────────────────────────────

func loadDockerHost(id int64) (*DockerHost, error) {
	var h DockerHost
	var encPw, encKey string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(
		`SELECT id, name, ssh_host, ssh_port, ssh_user, COALESCE(ssh_password,''),
		        COALESCE(ssh_key,''), COALESCE(socket_path,'/var/run/docker.sock'),
		        COALESCE(owner_id,0), created_at
		 FROM docker_hosts WHERE id = ?`), id).
		Scan(&h.ID, &h.Name, &h.SSHHost, &h.SSHPort, &h.SSHUser, &encPw, &encKey,
			&h.SocketPath, &h.OwnerID, &h.CreatedAt)
	if err != nil {
		return nil, err
	}
	h.SSHPassword, _ = decryptCredential(encPw)
	h.SSHKey, _ = decryptCredential(encKey)
	return &h, nil
}

// connectHostByID loads a host and opens a Docker connection to it. The caller
// must Close() the returned connection.
func connectHostByID(id int64) (*dockerConn, error) {
	h, err := loadDockerHost(id)
	if err != nil {
		return nil, err
	}
	return dialDocker(h)
}

// dockerPathParts returns the path segments after /api/docker/hosts/.
func dockerPathParts(r *http.Request) []string {
	rest := strings.TrimPrefix(r.URL.Path, "/api/docker/hosts/")
	return strings.Split(strings.Trim(rest, "/"), "/")
}

func dockerHostIDFromPath(r *http.Request) (int64, error) {
	parts := dockerPathParts(r)
	if len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("missing host id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

// ── Host CRUD ─────────────────────────────────────────────────────────────

func ListDockerHosts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, ssh_host, ssh_port, ssh_user,
			        COALESCE(socket_path,'/var/run/docker.sock'), COALESCE(owner_id,0), created_at
			 FROM docker_hosts ORDER BY name ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		list := []DockerHost{}
		for rows.Next() {
			var h DockerHost
			if err := rows.Scan(&h.ID, &h.Name, &h.SSHHost, &h.SSHPort, &h.SSHUser,
				&h.SocketPath, &h.OwnerID, &h.CreatedAt); err != nil {
				continue
			}
			// Never expose stored credentials in list responses.
			list = append(list, h)
		}
		json.NewEncoder(w).Encode(list)
	}
}

func validateDockerHostInput(in *DockerHostInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	// Local mode — an empty SSH host connects to the daemon socket on this
	// machine, so no SSH credentials are needed.
	if strings.TrimSpace(in.SSHHost) == "" {
		return nil
	}
	if strings.TrimSpace(in.SSHUser) == "" {
		return fmt.Errorf("ssh_user is required")
	}
	if strings.TrimSpace(in.SSHPassword) == "" && strings.TrimSpace(in.SSHKey) == "" {
		return fmt.Errorf("an SSH password or private key is required")
	}
	return nil
}

func CreateDockerHost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in DockerHostInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if err := validateDockerHostInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if in.SSHPort == 0 {
			in.SSHPort = 22
		}
		if strings.TrimSpace(in.SocketPath) == "" {
			in.SocketPath = "/var/run/docker.sock"
		}
		encPw, err := encryptCredential(in.SSHPassword)
		if err != nil {
			http.Error(w, jsonError("encrypt password: "+err.Error()), http.StatusInternalServerError)
			return
		}
		encKey, err := encryptCredential(in.SSHKey)
		if err != nil {
			http.Error(w, jsonError("encrypt key: "+err.Error()), http.StatusInternalServerError)
			return
		}
		ownerID, _ := currentUserID(r)

		var id int64
		insert := `INSERT INTO docker_hosts (name, ssh_host, ssh_port, ssh_user, ssh_password, ssh_key, socket_path, owner_id)
			 VALUES (?,?,?,?,?,?,?,?)`
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			err = appdb.DB.QueryRow(appdb.ConvertQuery(insert+" RETURNING id"),
				in.Name, in.SSHHost, in.SSHPort, in.SSHUser, encPw, encKey, in.SocketPath, ownerID).Scan(&id)
		} else {
			var res interface {
				LastInsertId() (int64, error)
			}
			res, err = appdb.DB.Exec(appdb.ConvertQuery(insert),
				in.Name, in.SSHHost, in.SSHPort, in.SSHUser, encPw, encKey, in.SocketPath, ownerID)
			if err == nil {
				id, _ = res.LastInsertId()
			}
		}
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id})
	}
}

func UpdateDockerHost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var in DockerHostInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if err := validateDockerHostInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if in.SSHPort == 0 {
			in.SSHPort = 22
		}
		if strings.TrimSpace(in.SocketPath) == "" {
			in.SocketPath = "/var/run/docker.sock"
		}

		// Preserve existing secrets when the field is left blank on update.
		existing, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, `{"error":"host not found"}`, http.StatusNotFound)
			return
		}
		pwPlain := in.SSHPassword
		if strings.TrimSpace(pwPlain) == "" {
			pwPlain = existing.SSHPassword
		}
		keyPlain := in.SSHKey
		if strings.TrimSpace(keyPlain) == "" {
			keyPlain = existing.SSHKey
		}
		encPw, _ := encryptCredential(pwPlain)
		encKey, _ := encryptCredential(keyPlain)

		_, err = appdb.DB.Exec(appdb.ConvertQuery(
			`UPDATE docker_hosts SET name=?, ssh_host=?, ssh_port=?, ssh_user=?,
			        ssh_password=?, ssh_key=?, socket_path=? WHERE id=?`),
			in.Name, in.SSHHost, in.SSHPort, in.SSHUser, encPw, encKey, in.SocketPath, id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "ok": true})
	}
}

func DeleteDockerHost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		if _, err := appdb.DB.Exec(appdb.ConvertQuery(`DELETE FROM docker_hosts WHERE id=?`), id); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// dockerPing reports daemon reachability + version for a given connection.
func dockerPing(w http.ResponseWriter, d *dockerConn) {
	var ver struct {
		Version    string `json:"version"`
		APIVersion string `json:"apiVersion"`
		Os         string `json:"os"`
		Arch       string `json:"arch"`
	}
	if err := d.getJSON("/version", nil, &ver); err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"version":     ver.Version,
		"api_version": ver.APIVersion,
		"os":          ver.Os,
		"arch":        ver.Arch,
	})
}

// TestDockerHost tests arbitrary (unsaved) credentials before they are stored.
func TestDockerHost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in DockerHostInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if err := validateDockerHostInput(&in); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		h := &DockerHost{
			SSHHost: in.SSHHost, SSHPort: in.SSHPort, SSHUser: in.SSHUser,
			SSHPassword: in.SSHPassword, SSHKey: in.SSHKey, SocketPath: in.SocketPath,
		}
		d, err := dialDocker(h)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		dockerPing(w, d)
	}
}

// DockerPing tests connectivity for a saved host.
func DockerPing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		dockerPing(w, d)
	}
}

// ── Containers ────────────────────────────────────────────────────────────

func DockerContainers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		q := url.Values{}
		q.Set("all", "1") // include stopped containers
		var list []dockerContainer
		if err := d.getJSON("/containers/json", q, &list); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if list == nil {
			list = []dockerContainer{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

func DockerContainerAction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := dockerPathParts(r)
		// /{id}/containers/{cid}/{action}
		if len(parts) != 4 {
			http.NotFound(w, r)
			return
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		cid, action := parts[2], parts[3]
		if !dockerIDPattern.MatchString(cid) {
			http.Error(w, `{"error":"invalid container id"}`, http.StatusBadRequest)
			return
		}
		if !dockerAllowedActions[action] {
			http.Error(w, `{"error":"unsupported action"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		resp, err := d.do(http.MethodPost, "/containers/"+url.PathEscape(cid)+"/"+action, nil)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		// 204 = done, 304 = already in target state — both are success.
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "action": action})
	}
}

// DockerContainerLogs returns the tail of a container's logs as plain text.
func DockerContainerLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := dockerPathParts(r)
		// /{id}/containers/{cid}/logs
		if len(parts) != 4 {
			http.NotFound(w, r)
			return
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid host id"), http.StatusBadRequest)
			return
		}
		cid := parts[2]
		if !dockerIDPattern.MatchString(cid) {
			http.Error(w, jsonError("invalid container id"), http.StatusBadRequest)
			return
		}
		tail := r.URL.Query().Get("tail")
		if tail == "" {
			tail = "200"
		}
		if _, err := strconv.Atoi(tail); err != nil {
			tail = "200"
		}

		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		// The multiplexed-frame format depends on whether the container has a TTY.
		tty := dockerContainerHasTTY(d, cid)

		q := url.Values{}
		q.Set("stdout", "1")
		q.Set("stderr", "1")
		q.Set("tail", tail)
		q.Set("timestamps", "0")
		resp, err := d.do(http.MethodGet, "/containers/"+url.PathEscape(cid)+"/logs", q)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// Cap output to ~1MB to stay memory-safe.
		body := io.LimitReader(resp.Body, 1<<20)
		if tty {
			io.Copy(w, body)
			return
		}
		w.Write([]byte(demuxDockerLogs(body)))
	}
}

func dockerContainerHasTTY(d *dockerConn, cid string) bool {
	var insp struct {
		Config struct {
			Tty bool `json:"tty"`
		} `json:"config"`
	}
	if err := d.getJSON("/containers/"+url.PathEscape(cid)+"/json", nil, &insp); err != nil {
		return false
	}
	return insp.Config.Tty
}

// demuxDockerLogs strips Docker's 8-byte stream-multiplexing frame headers
// (stream type byte + 3 zero bytes + 4-byte big-endian payload size).
func demuxDockerLogs(r io.Reader) string {
	var out strings.Builder
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			break
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}
		payload := make([]byte, size)
		n, err := io.ReadFull(r, payload)
		out.Write(payload[:n])
		if err != nil {
			break
		}
	}
	return out.String()
}

// DockerContainerStats returns a single CPU/memory snapshot for a container.
func DockerContainerStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := dockerPathParts(r)
		// /{id}/containers/{cid}/stats
		if len(parts) != 4 {
			http.NotFound(w, r)
			return
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, jsonError("invalid host id"), http.StatusBadRequest)
			return
		}
		cid := parts[2]
		if !dockerIDPattern.MatchString(cid) {
			http.Error(w, jsonError("invalid container id"), http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		var raw struct {
			CPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemUsage uint64 `json:"system_cpu_usage"`
				OnlineCPUs  uint64 `json:"online_cpus"`
			} `json:"cpu_stats"`
			PreCPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemUsage uint64 `json:"system_cpu_usage"`
			} `json:"precpu_stats"`
			MemoryStats struct {
				Usage uint64 `json:"usage"`
				Limit uint64 `json:"limit"`
			} `json:"memory_stats"`
		}
		q := url.Values{}
		q.Set("stream", "false")
		if err := d.getJSON("/containers/"+url.PathEscape(cid)+"/stats", q, &raw); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		cpuPercent := 0.0
		cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
		sysDelta := float64(raw.CPUStats.SystemUsage) - float64(raw.PreCPUStats.SystemUsage)
		cpus := raw.CPUStats.OnlineCPUs
		if cpus == 0 {
			cpus = 1
		}
		if cpuDelta > 0 && sysDelta > 0 {
			cpuPercent = (cpuDelta / sysDelta) * float64(cpus) * 100.0
		}
		memPercent := 0.0
		if raw.MemoryStats.Limit > 0 {
			memPercent = float64(raw.MemoryStats.Usage) / float64(raw.MemoryStats.Limit) * 100.0
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"cpu_percent": cpuPercent,
			"mem_usage":   raw.MemoryStats.Usage,
			"mem_limit":   raw.MemoryStats.Limit,
			"mem_percent": memPercent,
		})
	}
}

// ── Images ────────────────────────────────────────────────────────────────

func DockerImages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		var list []dockerImage
		if err := d.getJSON("/images/json", nil, &list); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if list == nil {
			list = []dockerImage{}
		}
		json.NewEncoder(w).Encode(list)
	}
}

// ── Container details / lifecycle / exec ──────────────────────────────────

// dockerHostAndContainer parses /api/docker/hosts/{id}/containers/{cid}/...
func dockerHostAndContainer(r *http.Request) (int64, string, error) {
	parts := dockerPathParts(r)
	if len(parts) < 3 || parts[1] != "containers" {
		return 0, "", fmt.Errorf("invalid path")
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid host id")
	}
	cid := parts[2]
	if !dockerIDPattern.MatchString(cid) {
		return 0, "", fmt.Errorf("invalid container id")
	}
	return id, cid, nil
}

// DockerContainerInspect proxies the full inspect JSON for a container.
func DockerContainerInspect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, cid, err := dockerHostAndContainer(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		resp, err := d.do(http.MethodGet, "/containers/"+url.PathEscape(cid)+"/json", nil)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, io.LimitReader(resp.Body, 1<<20))
	}
}

// DockerContainerRemove force-removes a container (stops it first if running).
func DockerContainerRemove() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, cid, err := dockerHostAndContainer(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		q := url.Values{}
		q.Set("force", "true")
		resp, err := d.do(http.MethodDelete, "/containers/"+url.PathEscape(cid), q)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// DockerContainerExec runs a single command inside a running container and
// returns its combined output. This is gated by the separate docker.exec
// permission because it is effectively code execution inside the container.
func DockerContainerExec() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, cid, err := dockerHostAndContainer(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			Cmd string `json:"cmd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Cmd) == "" {
			http.Error(w, `{"error":"command is required"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		// Create the exec instance.
		createBody := map[string]interface{}{
			"AttachStdout": true,
			"AttachStderr": true,
			"Tty":          false,
			"Cmd":          []string{"/bin/sh", "-c", body.Cmd},
		}
		var created struct {
			Id string `json:"id"`
		}
		cResp, err := d.doBody(http.MethodPost, "/containers/"+url.PathEscape(cid)+"/exec", nil, createBody, false)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if cResp.StatusCode >= 400 {
			msg := dockerErrBody(cResp)
			cResp.Body.Close()
			http.Error(w, jsonError(msg), http.StatusBadGateway)
			return
		}
		json.NewDecoder(cResp.Body).Decode(&created)
		cResp.Body.Close()
		if created.Id == "" {
			http.Error(w, jsonError("exec create returned no id"), http.StatusBadGateway)
			return
		}

		// Start it and read the multiplexed output stream.
		startBody := map[string]interface{}{"Detach": false, "Tty": false}
		sResp, err := d.doBody(http.MethodPost, "/exec/"+url.PathEscape(created.Id)+"/start", nil, startBody, false)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer sResp.Body.Close()
		if sResp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(sResp)), http.StatusBadGateway)
			return
		}
		output := demuxDockerLogs(io.LimitReader(sResp.Body, 1<<20))

		exitCode := 0
		var insp struct {
			ExitCode int `json:"exitCode"`
		}
		if d.getJSON("/exec/"+url.PathEscape(created.Id)+"/json", nil, &insp) == nil {
			exitCode = insp.ExitCode
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"output": output, "exit_code": exitCode})
	}
}

// DockerContainerRun creates and starts a container from an image.
func DockerContainerRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var req struct {
			Image string   `json:"image"`
			Name  string   `json:"name"`
			Env   []string `json:"env"`
			Ports []struct {
				Host      string `json:"host"`
				Container string `json:"container"`
				Proto     string `json:"proto"`
			} `json:"ports"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Image) == "" {
			http.Error(w, `{"error":"image is required"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		exposed := map[string]struct{}{}
		bindings := map[string][]map[string]string{}
		for _, p := range req.Ports {
			if strings.TrimSpace(p.Container) == "" {
				continue
			}
			proto := p.Proto
			if proto == "" {
				proto = "tcp"
			}
			key := p.Container + "/" + proto
			exposed[key] = struct{}{}
			if strings.TrimSpace(p.Host) != "" {
				bindings[key] = []map[string]string{{"HostPort": p.Host}}
			}
		}
		createBody := map[string]interface{}{
			"Image":        req.Image,
			"Env":          req.Env,
			"ExposedPorts": exposed,
			"HostConfig":   map[string]interface{}{"PortBindings": bindings},
		}
		q := url.Values{}
		if strings.TrimSpace(req.Name) != "" {
			q.Set("name", req.Name)
		}
		var created struct {
			Id string `json:"id"`
		}
		cResp, err := d.doBody(http.MethodPost, "/containers/create", q, createBody, false)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if cResp.StatusCode >= 400 {
			msg := dockerErrBody(cResp)
			cResp.Body.Close()
			if cResp.StatusCode == http.StatusNotFound {
				msg = "image not found on host — pull it first: " + msg
			}
			http.Error(w, jsonError(msg), http.StatusBadGateway)
			return
		}
		json.NewDecoder(cResp.Body).Decode(&created)
		cResp.Body.Close()
		if created.Id == "" {
			http.Error(w, jsonError("create returned no id"), http.StatusBadGateway)
			return
		}

		sResp, err := d.do(http.MethodPost, "/containers/"+url.PathEscape(created.Id)+"/start", nil)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer sResp.Body.Close()
		if sResp.StatusCode >= 400 && sResp.StatusCode != http.StatusNotModified {
			http.Error(w, jsonError(dockerErrBody(sResp)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": created.Id})
	}
}

// ── Image lifecycle ───────────────────────────────────────────────────────

// DockerImagePull pulls an image, blocking until the pull completes.
func DockerImagePull() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Image string `json:"image"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Image) == "" {
			http.Error(w, `{"error":"image is required"}`, http.StatusBadRequest)
			return
		}
		img := strings.TrimSpace(body.Image)
		name, tag := img, "latest"
		// A ':' after the last '/' is a tag separator (not a registry port).
		if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
			name, tag = img[:i], img[i+1:]
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		q := url.Values{}
		q.Set("fromImage", name)
		q.Set("tag", tag)
		resp, err := d.doBody(http.MethodPost, "/images/create", q, nil, true) // no timeout
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		// Drain the progress stream; surface any error line it reports.
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		var pullErr string
		for scanner.Scan() {
			line := scanner.Bytes()
			if bytes.Contains(line, []byte(`"errorDetail"`)) || bytes.Contains(line, []byte(`"error"`)) {
				var e struct {
					Error string `json:"error"`
				}
				if json.Unmarshal(line, &e) == nil && e.Error != "" {
					pullErr = e.Error
				}
			}
		}
		if pullErr != "" {
			http.Error(w, jsonError(pullErr), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "image": img})
	}
}

// DockerImageRemove removes an image by its ID (frontend sends the image ID to
// avoid path-encoding issues with repo/tag slashes).
func DockerImageRemove() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Ref   string `json:"ref"`
			Force bool   `json:"force"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		ref := strings.TrimPrefix(strings.TrimSpace(body.Ref), "sha256:")
		if !dockerIDPattern.MatchString(ref) {
			http.Error(w, `{"error":"invalid image ref — send the image ID"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		q := url.Values{}
		if body.Force {
			q.Set("force", "true")
		}
		resp, err := d.do(http.MethodDelete, "/images/"+ref, q)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// dockerPrune proxies a Docker prune response ({...Deleted, SpaceReclaimed}).
func dockerPrune(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "application/json")
	id, err := dockerHostIDFromPath(r)
	if err != nil {
		http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
		return
	}
	d, err := connectHostByID(id)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return
	}
	defer d.Close()
	resp, err := d.doBody(http.MethodPost, path, nil, nil, false)
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
		return
	}
	io.Copy(w, io.LimitReader(resp.Body, 1<<20))
}

// DockerContainersPrune removes all stopped containers.
func DockerContainersPrune() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dockerPrune(w, r, "/containers/prune")
	}
}

// DockerImagesPrune removes dangling images.
func DockerImagesPrune() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dockerPrune(w, r, "/images/prune")
	}
}

// ── Interactive terminal (WebSocket) ──────────────────────────────────────

// dockerWSUserID validates the JWT passed as a query param (browsers can't set
// the Authorization header on a WebSocket) and returns the user id.
func dockerWSUserID(r *http.Request) (int64, error) {
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		return 0, fmt.Errorf("missing token")
	}
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(_ *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil {
		return 0, err
	}
	if claims.UserID == 0 {
		return 0, fmt.Errorf("invalid token")
	}
	return claims.UserID, nil
}

// DockerContainerTerminal bridges a browser WebSocket to an interactive
// `docker exec` (TTY) session inside a running container. Gated by docker.exec.
func DockerContainerTerminal() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth is enforced here (not via middleware) because the WS upgrade
		// request can't carry the Authorization header.
		if len(jwtSecret) > 0 {
			userID, err := dockerWSUserID(r)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !appdb.HasUserAppPermission(userID, PermDockerExec) {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
		}
		id, cid, err := dockerHostAndContainer(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusNotFound)
			return
		}

		// HTTP client for exec create + resize.
		d, err := dialDocker(h)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		shell := r.URL.Query().Get("shell")
		if shell == "" {
			shell = "/bin/sh"
		}
		createBody := map[string]interface{}{
			"AttachStdin":  true,
			"AttachStdout": true,
			"AttachStderr": true,
			"Tty":          true,
			"Cmd":          []string{shell},
		}
		var created struct {
			Id string `json:"id"`
		}
		cResp, err := d.doBody(http.MethodPost, "/containers/"+url.PathEscape(cid)+"/exec", nil, createBody, false)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if cResp.StatusCode >= 400 {
			msg := dockerErrBody(cResp)
			cResp.Body.Close()
			http.Error(w, jsonError(msg), http.StatusBadGateway)
			return
		}
		json.NewDecoder(cResp.Body).Decode(&created)
		cResp.Body.Close()
		if created.Id == "" {
			http.Error(w, jsonError("exec create returned no id"), http.StatusBadGateway)
			return
		}

		// Raw connection for the hijacked exec/start stream.
		rawConn, closeSocket, err := dialDockerSocket(h)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer closeSocket()
		defer rawConn.Close()

		startBody := `{"Detach":false,"Tty":true}`
		fmt.Fprintf(rawConn,
			"POST /exec/%s/start HTTP/1.1\r\nHost: docker\r\nContent-Type: application/json\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: %d\r\n\r\n%s",
			created.Id, len(startBody), startBody)
		br := bufio.NewReader(rawConn)
		if _, err := br.ReadString('\n'); err != nil { // status line
			http.Error(w, jsonError("exec start failed"), http.StatusBadGateway)
			return
		}
		for { // skip headers up to the blank line
			line, err := br.ReadString('\n')
			if err != nil {
				http.Error(w, jsonError("exec start failed"), http.StatusBadGateway)
				return
			}
			if line == "\r\n" || line == "\n" {
				break
			}
		}

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer ws.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Container stdout/stderr → browser.
		go func() {
			buf := make([]byte, 8192)
			for {
				n, rerr := br.Read(buf)
				if n > 0 {
					if werr := ws.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
						cancel()
						return
					}
				}
				if rerr != nil {
					cancel()
					return
				}
			}
		}()

		// Text frames are control messages (resize); binary frames are keystrokes.
		for {
			typ, data, rerr := ws.Read(ctx)
			if rerr != nil {
				break
			}
			if typ == websocket.MessageText {
				var ctl struct {
					Type string `json:"type"`
					Cols int    `json:"cols"`
					Rows int    `json:"rows"`
				}
				if json.Unmarshal(data, &ctl) == nil && ctl.Type == "resize" {
					q := url.Values{}
					q.Set("h", strconv.Itoa(ctl.Rows))
					q.Set("w", strconv.Itoa(ctl.Cols))
					if rs, e := d.doBody(http.MethodPost, "/exec/"+url.PathEscape(created.Id)+"/resize", q, nil, false); e == nil {
						rs.Body.Close()
					}
				}
				continue
			}
			if _, werr := rawConn.Write(data); werr != nil {
				break
			}
		}
		cancel()
	}
}

// ── Volumes ───────────────────────────────────────────────────────────────

type dockerVolume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	CreatedAt  string `json:"createdAt"`
	Scope      string `json:"scope"`
}

func DockerVolumes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		var resp struct {
			Volumes []dockerVolume `json:"volumes"`
		}
		if err := d.getJSON("/volumes", nil, &resp); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if resp.Volumes == nil {
			resp.Volumes = []dockerVolume{}
		}
		json.NewEncoder(w).Encode(resp.Volumes)
	}
}

func DockerVolumeRemove() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Name  string `json:"name"`
			Force bool   `json:"force"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.Name) == "" {
			http.Error(w, `{"error":"volume name required"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		q := url.Values{}
		if body.Force {
			q.Set("force", "true")
		}
		resp, err := d.do(http.MethodDelete, "/volumes/"+url.PathEscape(body.Name), q)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// DockerVolumesPrune removes unused volumes.
func DockerVolumesPrune() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dockerPrune(w, r, "/volumes/prune")
	}
}

// ── Networks ──────────────────────────────────────────────────────────────

type dockerNetwork struct {
	Name       string `json:"name"`
	ID         string `json:"id"`
	Driver     string `json:"driver"`
	Scope      string `json:"scope"`
	Subnet     string `json:"subnet"`
	Containers int    `json:"containers"`
}

func DockerNetworks() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		var raw []struct {
			Name   string `json:"Name"`
			Id     string `json:"Id"`
			Driver string `json:"Driver"`
			Scope  string `json:"Scope"`
			IPAM   struct {
				Config []struct {
					Subnet string `json:"Subnet"`
				} `json:"Config"`
			} `json:"IPAM"`
			Containers map[string]interface{} `json:"Containers"`
		}
		if err := d.getJSON("/networks", nil, &raw); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		out := []dockerNetwork{}
		for _, n := range raw {
			subnet := ""
			if len(n.IPAM.Config) > 0 {
				subnet = n.IPAM.Config[0].Subnet
			}
			out = append(out, dockerNetwork{
				Name: n.Name, ID: n.Id, Driver: n.Driver, Scope: n.Scope,
				Subnet: subnet, Containers: len(n.Containers),
			})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func DockerNetworkRemove() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		nid := strings.TrimSpace(body.ID)
		if !dockerIDPattern.MatchString(nid) {
			http.Error(w, `{"error":"invalid network id"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		resp, err := d.do(http.MethodDelete, "/networks/"+nid, nil)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// DockerNetworksPrune removes unused networks.
func DockerNetworksPrune() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dockerPrune(w, r, "/networks/prune")
	}
}
