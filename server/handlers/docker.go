package handlers

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
var dockerAllowedActions = map[string]bool{
	"start": true, "stop": true, "restart": true, "pause": true, "unpause": true,
}

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
		if tail != "all" {
			if _, err := strconv.Atoi(tail); err != nil {
				tail = "200"
			}
		}

		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		// The multiplexed-frame format depends on whether the container has a TTY.
		tty := dockerContainerHasTTY(d, cid)

		timestamps := "0"
		if r.URL.Query().Get("timestamps") == "1" {
			timestamps = "1"
		}
		q := url.Values{}
		q.Set("stdout", "1")
		q.Set("stderr", "1")
		q.Set("tail", tail)
		q.Set("timestamps", timestamps)
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
			Networks map[string]struct {
				RxBytes uint64 `json:"rx_bytes"`
				TxBytes uint64 `json:"tx_bytes"`
			} `json:"networks"`
			BlkioStats struct {
				IoServiceBytesRecursive []struct {
					Op    string `json:"op"`
					Value uint64 `json:"value"`
				} `json:"io_service_bytes_recursive"`
			} `json:"blkio_stats"`
			PidsStats struct {
				Current uint64 `json:"current"`
			} `json:"pids_stats"`
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
		var netRx, netTx uint64
		for _, n := range raw.Networks {
			netRx += n.RxBytes
			netTx += n.TxBytes
		}
		var blkRead, blkWrite uint64
		for _, b := range raw.BlkioStats.IoServiceBytesRecursive {
			switch strings.ToLower(b.Op) {
			case "read":
				blkRead += b.Value
			case "write":
				blkWrite += b.Value
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"cpu_percent": cpuPercent,
			"mem_usage":   raw.MemoryStats.Usage,
			"mem_limit":   raw.MemoryStats.Limit,
			"mem_percent": memPercent,
			"net_rx":      netRx,
			"net_tx":      netTx,
			"blk_read":    blkRead,
			"blk_write":   blkWrite,
			"pids":        raw.PidsStats.Current,
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
		q := url.Values{}
		q.Set("size", "1") // include SizeRw / SizeRootFs
		resp, err := d.do(http.MethodGet, "/containers/"+url.PathEscape(cid)+"/json", q)
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
		output, exitCode, err := execCommand(d, cid, []string{"/bin/sh", "-c", body.Cmd})
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"output": output, "exit_code": exitCode})
	}
}

// execCommand runs a command (argv form, no shell) inside a container and
// returns its combined output and exit code.
func execCommand(d *dockerConn, cid string, cmd []string) (string, int, error) {
	createBody := map[string]interface{}{
		"AttachStdout": true, "AttachStderr": true, "Tty": false, "Cmd": cmd,
	}
	var created struct {
		Id string `json:"id"`
	}
	cResp, err := d.doBody(http.MethodPost, "/containers/"+url.PathEscape(cid)+"/exec", nil, createBody, false)
	if err != nil {
		return "", -1, err
	}
	if cResp.StatusCode >= 400 {
		msg := dockerErrBody(cResp)
		cResp.Body.Close()
		return "", -1, fmt.Errorf("%s", msg)
	}
	json.NewDecoder(cResp.Body).Decode(&created)
	cResp.Body.Close()
	if created.Id == "" {
		return "", -1, fmt.Errorf("exec create returned no id")
	}
	startBody := map[string]interface{}{"Detach": false, "Tty": false}
	sResp, err := d.doBody(http.MethodPost, "/exec/"+url.PathEscape(created.Id)+"/start", nil, startBody, false)
	if err != nil {
		return "", -1, err
	}
	defer sResp.Body.Close()
	if sResp.StatusCode >= 400 {
		return "", -1, fmt.Errorf("%s", dockerErrBody(sResp))
	}
	output := demuxDockerLogs(io.LimitReader(sResp.Body, 1<<20))
	exitCode := 0
	var insp struct {
		ExitCode int `json:"exitCode"`
	}
	if d.getJSON("/exec/"+url.PathEscape(created.Id)+"/json", nil, &insp) == nil {
		exitCode = insp.ExitCode
	}
	return output, exitCode, nil
}

// ── Container file browser ────────────────────────────────────────────────

type dockerFileEntry struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
	Mode  string `json:"mode"`
}

func parseLsOutput(out string) []dockerFileEntry {
	entries := []dockerFileEntry{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 || len(fields[0]) < 10 {
			continue
		}
		mode := fields[0]
		size, _ := strconv.ParseInt(fields[4], 10, 64)
		name := strings.Join(fields[8:], " ")
		if mode[0] == 'l' { // symlink "name -> target"
			if i := strings.Index(name, " -> "); i >= 0 {
				name = name[:i]
			}
		}
		if name == "." || name == ".." {
			continue
		}
		entries = append(entries, dockerFileEntry{Name: name, Size: size, IsDir: mode[0] == 'd', Mode: mode})
	}
	return entries
}

// DockerContainerLs lists a directory inside a running container (via a fixed
// `ls -la` exec — argv form, so the path can't inject a shell command).
func DockerContainerLs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, cid, err := dockerHostAndContainer(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		out, _, err := execCommand(d, cid, []string{"ls", "-la", "--", path})
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"path": path, "entries": parseLsOutput(out)})
	}
}

// DockerContainerDownload streams a single file out of a container.
func DockerContainerDownload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, cid, err := dockerHostAndContainer(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		path := r.URL.Query().Get("path")
		if strings.TrimSpace(path) == "" {
			http.Error(w, jsonError("path is required"), http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		q := url.Values{}
		q.Set("path", path)
		resp, err := d.do(http.MethodGet, "/containers/"+url.PathEscape(cid)+"/archive", q)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		tr := tar.NewReader(resp.Body)
		hdr, err := tr.Next()
		if err != nil {
			http.Error(w, jsonError("empty archive"), http.StatusBadGateway)
			return
		}
		if hdr.Typeflag != tar.TypeReg {
			http.Error(w, jsonError("path is not a regular file"), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(hdr.Name)+`"`)
		io.Copy(w, tr)
	}
}

// DockerContainerUpload copies an uploaded file into a container directory.
func DockerContainerUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, cid, err := dockerHostAndContainer(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			http.Error(w, jsonError("invalid upload"), http.StatusBadRequest)
			return
		}
		dest := r.FormValue("path")
		if strings.TrimSpace(dest) == "" {
			http.Error(w, jsonError("destination path is required"), http.StatusBadRequest)
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			http.Error(w, jsonError("file is required"), http.StatusBadRequest)
			return
		}
		defer file.Close()
		content, _ := io.ReadAll(io.LimitReader(file, 100<<20))

		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		tw.WriteHeader(&tar.Header{Name: filepath.Base(hdr.Filename), Mode: 0o644, Size: int64(len(content))})
		tw.Write(content)
		tw.Close()

		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		q := url.Values{}
		q.Set("path", dest)
		req, _ := http.NewRequest(http.MethodPut, "http://docker/containers/"+url.PathEscape(cid)+"/archive?"+q.Encode(), &buf)
		req.Header.Set("Content-Type", "application/x-tar")
		resp, err := d.http.Do(req)
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
			Image         string   `json:"image"`
			Name          string   `json:"name"`
			Env           []string `json:"env"`
			Cmd           string   `json:"cmd"`
			Network       string   `json:"network"`
			RestartPolicy string   `json:"restart_policy"`
			Memory        int64    `json:"memory"` // bytes
			Cpus          float64  `json:"cpus"`
			AutoPull      bool     `json:"auto_pull"`
			Ports         []struct {
				Host      string `json:"host"`
				Container string `json:"container"`
				Proto     string `json:"proto"`
			} `json:"ports"`
			Volumes []struct {
				Host      string `json:"host"`
				Container string `json:"container"`
				RO        bool   `json:"ro"`
			} `json:"volumes"`
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

		hostConfig := map[string]interface{}{"PortBindings": bindings}
		binds := []string{}
		for _, v := range req.Volumes {
			if strings.TrimSpace(v.Host) == "" || strings.TrimSpace(v.Container) == "" {
				continue
			}
			b := v.Host + ":" + v.Container
			if v.RO {
				b += ":ro"
			}
			binds = append(binds, b)
		}
		if len(binds) > 0 {
			hostConfig["Binds"] = binds
		}
		if rp := strings.TrimSpace(req.RestartPolicy); rp != "" && rp != "no" {
			hostConfig["RestartPolicy"] = map[string]interface{}{"Name": rp}
		}
		if req.Memory > 0 {
			hostConfig["Memory"] = req.Memory
		}
		if req.Cpus > 0 {
			hostConfig["NanoCpus"] = int64(req.Cpus * 1e9)
		}
		if nm := strings.TrimSpace(req.Network); nm != "" {
			hostConfig["NetworkMode"] = nm
		}

		createBody := map[string]interface{}{
			"Image":        req.Image,
			"Env":          req.Env,
			"ExposedPorts": exposed,
			"HostConfig":   hostConfig,
		}
		if cmd := strings.TrimSpace(req.Cmd); cmd != "" {
			createBody["Cmd"] = strings.Fields(cmd)
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
		// Auto-pull the image if it's missing, then retry once.
		if cResp.StatusCode == http.StatusNotFound && req.AutoPull {
			cResp.Body.Close()
			if perr := pullImageOn(d, req.Image, ""); perr != nil {
				http.Error(w, jsonError("pull failed: "+perr.Error()), http.StatusBadGateway)
				return
			}
			cResp, err = d.doBody(http.MethodPost, "/containers/create", q, createBody, false)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
		}
		if cResp.StatusCode >= 400 {
			msg := dockerErrBody(cResp)
			cResp.Body.Close()
			if cResp.StatusCode == http.StatusNotFound {
				msg = "image not found on host — enable auto-pull or pull it first: " + msg
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

// DockerContainerRename renames a container.
func DockerContainerRename() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, cid, err := dockerHostAndContainer(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		q := url.Values{}
		q.Set("name", strings.TrimSpace(body.Name))
		resp, err := d.do(http.MethodPost, "/containers/"+url.PathEscape(cid)+"/rename", q)
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

// ── Image lifecycle ───────────────────────────────────────────────────────

// splitImageTag splits "repo/name:tag" into name and tag (default "latest").
func splitImageTag(img string) (string, string) {
	name, tag := img, "latest"
	// A ':' after the last '/' is a tag separator (not a registry port).
	if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
		name, tag = img[:i], img[i+1:]
	}
	return name, tag
}

// registryAuthHeader builds the base64 X-Registry-Auth value Docker expects.
func registryAuthHeader(username, password, serverAddress string) string {
	if username == "" && password == "" {
		return ""
	}
	cfg := map[string]string{"username": username, "password": password}
	if serverAddress != "" {
		cfg["serveraddress"] = serverAddress
	}
	b, _ := json.Marshal(cfg)
	return base64.URLEncoding.EncodeToString(b)
}

// pullImageOn pulls an image on an existing connection, blocking until done.
// authHeader is an optional X-Registry-Auth value for private registries.
func pullImageOn(d *dockerConn, img, authHeader string) error {
	name, tag := splitImageTag(img)
	u := "http://docker/images/create?fromImage=" + url.QueryEscape(name) + "&tag=" + url.QueryEscape(tag)
	req, err := http.NewRequest(http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	if authHeader != "" {
		req.Header.Set("X-Registry-Auth", authHeader)
	}
	resp, err := d.stream.Do(req) // no timeout
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s", dockerErrBody(resp))
	}
	// Drain the progress stream; surface any error line it reports.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.Contains(line, []byte(`"errorDetail"`)) || bytes.Contains(line, []byte(`"error"`)) {
			var e struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(line, &e) == nil && e.Error != "" {
				return fmt.Errorf("%s", e.Error)
			}
		}
	}
	return nil
}

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
			Image    string `json:"image"`
			Username string `json:"username"`
			Password string `json:"password"`
			Registry string `json:"registry"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Image) == "" {
			http.Error(w, `{"error":"image is required"}`, http.StatusBadRequest)
			return
		}
		img := strings.TrimSpace(body.Image)
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		auth := registryAuthHeader(body.Username, body.Password, body.Registry)
		if err := pullImageOn(d, img, auth); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "image": img})
	}
}

// DockerImageBuild builds an image from a git remote URL or an inline
// Dockerfile, returning the build log.
func DockerImageBuild() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Tag        string `json:"tag"`
			GitURL     string `json:"git_url"`
			Dockerfile string `json:"dockerfile"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		tag := strings.TrimSpace(body.Tag)
		gitURL := strings.TrimSpace(body.GitURL)
		if tag == "" {
			http.Error(w, `{"error":"image tag is required"}`, http.StatusBadRequest)
			return
		}
		if gitURL == "" && strings.TrimSpace(body.Dockerfile) == "" {
			http.Error(w, `{"error":"provide a git URL or a Dockerfile"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()

		q := url.Values{}
		q.Set("t", tag)
		var reqBody io.Reader
		contentType := ""
		if gitURL != "" {
			q.Set("remote", gitURL)
		} else {
			// Build context = a tar containing just the Dockerfile.
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			content := []byte(body.Dockerfile)
			tw.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0o600, Size: int64(len(content))})
			tw.Write(content)
			tw.Close()
			reqBody = &buf
			contentType = "application/x-tar"
		}
		req, err := http.NewRequest(http.MethodPost, "http://docker/build?"+q.Encode(), reqBody)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := d.stream.Do(req) // builds can take a long time
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}

		var out strings.Builder
		var buildErr string
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			var line struct {
				Stream string `json:"stream"`
				Error  string `json:"error"`
			}
			if json.Unmarshal(scanner.Bytes(), &line) == nil {
				out.WriteString(line.Stream)
				if line.Error != "" {
					buildErr = line.Error
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     buildErr == "",
			"output": out.String(),
			"error":  buildErr,
		})
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
	Size       int64  `json:"size"`     // -1 = unknown
	RefCount   int64  `json:"refCount"` // containers using it; -1 = unknown
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
		// Merge size + refcount from the disk-usage report.
		usage := map[string]struct {
			size int64
			ref  int64
		}{}
		var df struct {
			Volumes []struct {
				Name      string `json:"Name"`
				UsageData struct {
					Size     int64 `json:"Size"`
					RefCount int64 `json:"RefCount"`
				} `json:"UsageData"`
			} `json:"Volumes"`
		}
		if d.getJSON("/system/df", nil, &df) == nil {
			for _, v := range df.Volumes {
				usage[v.Name] = struct {
					size int64
					ref  int64
				}{v.UsageData.Size, v.UsageData.RefCount}
			}
		}
		for i := range resp.Volumes {
			if u, ok := usage[resp.Volumes[i].Name]; ok {
				resp.Volumes[i].Size = u.size
				resp.Volumes[i].RefCount = u.ref
			} else {
				resp.Volumes[i].Size = -1
				resp.Volumes[i].RefCount = -1
			}
		}
		json.NewEncoder(w).Encode(resp.Volumes)
	}
}

func DockerVolumeCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Name   string `json:"name"`
			Driver string `json:"driver"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, `{"error":"volume name is required"}`, http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		create := map[string]interface{}{"Name": strings.TrimSpace(body.Name)}
		if strings.TrimSpace(body.Driver) != "" {
			create["Driver"] = body.Driver
		}
		resp, err := d.doBody(http.MethodPost, "/volumes/create", nil, create, false)
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

type dockerNetConn struct {
	Name string `json:"name"`
	IPv4 string `json:"ipv4"`
}

type dockerNetwork struct {
	Name       string          `json:"name"`
	ID         string          `json:"id"`
	Driver     string          `json:"driver"`
	Scope      string          `json:"scope"`
	Subnet     string          `json:"subnet"`
	Containers int             `json:"containers"`
	Connected  []dockerNetConn `json:"connected"`
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
			Containers map[string]struct {
				Name        string `json:"Name"`
				IPv4Address string `json:"IPv4Address"`
			} `json:"Containers"`
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
			connected := []dockerNetConn{}
			for _, c := range n.Containers {
				connected = append(connected, dockerNetConn{Name: c.Name, IPv4: c.IPv4Address})
			}
			out = append(out, dockerNetwork{
				Name: n.Name, ID: n.Id, Driver: n.Driver, Scope: n.Scope,
				Subnet: subnet, Containers: len(n.Containers), Connected: connected,
			})
		}
		json.NewEncoder(w).Encode(out)
	}
}

func DockerNetworkCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Name   string `json:"name"`
			Driver string `json:"driver"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, `{"error":"network name is required"}`, http.StatusBadRequest)
			return
		}
		driver := strings.TrimSpace(body.Driver)
		if driver == "" {
			driver = "bridge"
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		create := map[string]interface{}{"Name": strings.TrimSpace(body.Name), "Driver": driver}
		resp, err := d.doBody(http.MethodPost, "/networks/create", nil, create, false)
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

// ── Disk usage ────────────────────────────────────────────────────────────

type dfCategory struct {
	Count       int   `json:"count"`
	Size        int64 `json:"size"`
	Reclaimable int64 `json:"reclaimable"`
}

// DockerSystemDF reports disk usage (like `docker system df`): total and
// reclaimable bytes per resource type.
func DockerSystemDF() http.HandlerFunc {
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
		var raw struct {
			Images []struct {
				Size       int64 `json:"Size"`
				Containers int   `json:"Containers"`
			} `json:"Images"`
			Containers []struct {
				SizeRw int64  `json:"SizeRw"`
				State  string `json:"State"`
			} `json:"Containers"`
			Volumes []struct {
				UsageData struct {
					Size     int64 `json:"Size"`
					RefCount int64 `json:"RefCount"`
				} `json:"UsageData"`
			} `json:"Volumes"`
			BuildCache []struct {
				Size  int64 `json:"Size"`
				InUse bool  `json:"InUse"`
			} `json:"BuildCache"`
		}
		if err := d.getJSON("/system/df", nil, &raw); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var img, con, vol, cache dfCategory
		img.Count = len(raw.Images)
		for _, i := range raw.Images {
			img.Size += i.Size
			if i.Containers == 0 {
				img.Reclaimable += i.Size
			}
		}
		con.Count = len(raw.Containers)
		for _, c := range raw.Containers {
			con.Size += c.SizeRw
			if c.State != "running" {
				con.Reclaimable += c.SizeRw
			}
		}
		vol.Count = len(raw.Volumes)
		for _, v := range raw.Volumes {
			vol.Size += v.UsageData.Size
			if v.UsageData.RefCount == 0 {
				vol.Reclaimable += v.UsageData.Size
			}
		}
		cache.Count = len(raw.BuildCache)
		for _, b := range raw.BuildCache {
			cache.Size += b.Size
			if !b.InUse {
				cache.Reclaimable += b.Size
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"images":      img,
			"containers":  con,
			"volumes":     vol,
			"build_cache": cache,
		})
	}
}

// ── Compose stacks ────────────────────────────────────────────────────────

var composeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// runHostCommand runs a command on the Docker host — locally if the host has
// no SSH config, otherwise over SSH — optionally feeding stdin.
func runHostCommand(h *DockerHost, args []string, stdin string) (string, error) {
	if strings.TrimSpace(h.SSHHost) == "" {
		cmd := exec.Command(args[0], args[1:]...)
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	sc, err := sshClientForHost(h)
	if err != nil {
		return "", err
	}
	defer sc.Close()
	sess, err := sc.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	if stdin != "" {
		sess.Stdin = strings.NewReader(stdin)
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	out, err := sess.CombinedOutput(strings.Join(parts, " "))
	return string(out), err
}

// DockerComposeUp deploys a compose stack (runs `docker compose up -d` with the
// yaml piped via stdin).
func DockerComposeUp() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Name string `json:"name"`
			Yaml string `json:"yaml"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		name := strings.TrimSpace(body.Name)
		if !composeNamePattern.MatchString(name) {
			http.Error(w, `{"error":"invalid stack name — use lowercase letters, digits, - and _"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Yaml) == "" {
			http.Error(w, `{"error":"compose yaml is required"}`, http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusNotFound)
			return
		}
		out, runErr := runHostCommand(h, []string{"docker", "compose", "-p", name, "-f", "-", "up", "-d"}, body.Yaml)
		result := map[string]interface{}{"output": out, "ok": runErr == nil}
		if runErr != nil {
			result["error"] = runErr.Error() + " — is the docker compose plugin installed on the host?"
		}
		json.NewEncoder(w).Encode(result)
	}
}

// DockerComposeDown stops and removes a compose stack by project name.
func DockerComposeDown() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		name := strings.TrimSpace(body.Name)
		if !composeNamePattern.MatchString(name) {
			http.Error(w, `{"error":"invalid stack name"}`, http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusNotFound)
			return
		}
		out, runErr := runHostCommand(h, []string{"docker", "compose", "-p", name, "down", "--remove-orphans"}, "")
		result := map[string]interface{}{"output": out, "ok": runErr == nil}
		if runErr != nil {
			result["error"] = runErr.Error()
		}
		json.NewEncoder(w).Encode(result)
	}
}

// ── Image save / load ─────────────────────────────────────────────────────

// DockerImageSave streams an image out as a tar archive.
func DockerImageSave() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, jsonError("invalid host id"), http.StatusBadRequest)
			return
		}
		ref := strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("ref")), "sha256:")
		if !dockerIDPattern.MatchString(ref) {
			http.Error(w, jsonError("invalid image ref — use the image ID"), http.StatusBadRequest)
			return
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		req, _ := http.NewRequest(http.MethodGet, "http://docker/images/"+ref+"/get", nil)
		resp, err := d.stream.Do(req) // images can be large
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("Content-Disposition", `attachment; filename="image-`+ref[:min(12, len(ref))]+`.tar"`)
		io.Copy(w, resp.Body)
	}
}

// DockerImageLoad imports images from an uploaded tar archive.
func DockerImageLoad() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, jsonError("invalid upload"), http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, jsonError("file is required"), http.StatusBadRequest)
			return
		}
		defer file.Close()
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		req, _ := http.NewRequest(http.MethodPost, "http://docker/images/load", file)
		req.Header.Set("Content-Type", "application/x-tar")
		resp, err := d.stream.Do(req)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			http.Error(w, jsonError(dockerErrBody(resp)), http.StatusBadGateway)
			return
		}
		io.Copy(io.Discard, resp.Body)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// ── Events ────────────────────────────────────────────────────────────────

// DockerEvents returns daemon events in the [since, now] window (non-blocking).
func DockerEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := dockerHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		now := time.Now().Unix()
		since := r.URL.Query().Get("since")
		if since == "" {
			since = strconv.FormatInt(now-3600, 10)
		}
		d, err := connectHostByID(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer d.Close()
		q := url.Values{}
		q.Set("since", since)
		q.Set("until", strconv.FormatInt(now, 10)) // until set => returns and closes
		resp, err := d.do(http.MethodGet, "/events", q)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		type evt struct {
			Type   string `json:"type"`
			Action string `json:"action"`
			Name   string `json:"name"`
			Time   int64  `json:"time"`
		}
		events := []evt{}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			var ev struct {
				Type  string `json:"Type"`
				Action string `json:"Action"`
				Actor struct {
					Attributes map[string]string `json:"Attributes"`
				} `json:"Actor"`
				Time int64 `json:"time"`
			}
			if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.Type != "" {
				events = append(events, evt{Type: ev.Type, Action: ev.Action, Name: ev.Actor.Attributes["name"], Time: ev.Time})
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"events": events, "until": now})
	}
}

// ── Multi-host overview ───────────────────────────────────────────────────

type dockerHostSummary struct {
	HostID    int64  `json:"host_id"`
	Name      string `json:"name"`
	SSHHost   string `json:"ssh_host"`
	Reachable bool   `json:"reachable"`
	Running   int    `json:"running"`
	Total     int    `json:"total"`
	Images    int    `json:"images"`
	Version   string `json:"version"`
	Error     string `json:"error,omitempty"`
}

// DockerOverview aggregates container/image counts across every configured host.
func DockerOverview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, ssh_host FROM docker_hosts ORDER BY name ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		type hostRow struct {
			id      int64
			name    string
			sshHost string
		}
		var hostRows []hostRow
		for rows.Next() {
			var hr hostRow
			if err := rows.Scan(&hr.id, &hr.name, &hr.sshHost); err == nil {
				hostRows = append(hostRows, hr)
			}
		}
		rows.Close()

		summaries := make([]dockerHostSummary, len(hostRows))
		var wg sync.WaitGroup
		for i, hr := range hostRows {
			wg.Add(1)
			go func(i int, hr hostRow) {
				defer wg.Done()
				s := dockerHostSummary{HostID: hr.id, Name: hr.name, SSHHost: hr.sshHost}
				h, err := loadDockerHost(hr.id)
				if err != nil {
					s.Error = err.Error()
					summaries[i] = s
					return
				}
				d, err := dialDocker(h)
				if err != nil {
					s.Error = err.Error()
					summaries[i] = s
					return
				}
				defer d.Close()
				var ver struct {
					Version string `json:"version"`
				}
				if err := d.getJSON("/version", nil, &ver); err != nil {
					s.Error = err.Error()
					summaries[i] = s
					return
				}
				s.Reachable = true
				s.Version = ver.Version
				cq := url.Values{}
				cq.Set("all", "1")
				var conts []dockerContainer
				if d.getJSON("/containers/json", cq, &conts) == nil {
					s.Total = len(conts)
					for _, c := range conts {
						if c.State == "running" {
							s.Running++
						}
					}
				}
				var imgs []dockerImage
				if d.getJSON("/images/json", nil, &imgs) == nil {
					s.Images = len(imgs)
				}
				summaries[i] = s
			}(i, hr)
		}
		wg.Wait()
		json.NewEncoder(w).Encode(summaries)
	}
}
