package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/pkg/sftp"
)

// Nginx management rides on the same SSH host records as Docker and SFTP
// (the docker_hosts table). It reads/edits config, toggles sites, tests/reloads
// nginx, and tails logs — all over SSH. Paths and the binary (nginx vs
// openresty) are auto-detected per host and can be overridden by the caller,
// so non-Debian layouts (conf.d/*.conf, openresty, custom prefixes) work too.
//
// When the SSH user isn't root (the common case — e.g. reading 640 root:adm
// logs or writing /etc/nginx), the caller can request `sudo`. Privileged
// commands then run via `sudo -S`, feeding the host's stored SSH password as
// the sudo password (they're usually the same); with key auth it uses `sudo -n`
// (NOPASSWD). Local hosts (no SSH) are not supported.

const (
	defaultNginxConfigRoot = "/etc/nginx"
	defaultNginxLogRoot    = "/var/log/nginx"
	nginxMaxFile           = 4 << 20 // 4 MiB cap for config read/write
)

// nginxNamePattern guards site/file names used to build paths for mv/ln/rm.
var nginxNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type nginxFileEntry struct {
	Path string `json:"path"` // relative to root
	Size int64  `json:"size"`
}

type nginxSite struct {
	Name       string `json:"name"`       // actual on-disk filename — used for toggle
	Enabled    bool   `json:"enabled"`    // currently active
	State      string `json:"state"`      // "enabled" | "disabled" | "inactive"
	Toggleable bool   `json:"toggleable"` // can be flipped automatically
}

type nginxInfo struct {
	Bin         string `json:"bin"` // "nginx" or "openresty"
	Version     string `json:"version"`
	ConfigRoot  string `json:"config_root"`
	LogRoot     string `json:"log_root"`
	Active      string `json:"active"`
	SitesLayout string `json:"sites_layout"` // "symlink" | "confd" | ""
}

func nginxPathParts(r *http.Request) []string {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/nginx/hosts/"), "/")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}

func nginxHostID(r *http.Request) (int64, error) {
	parts := nginxPathParts(r)
	if len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("missing host id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

func nginxRunHost(r *http.Request) (*DockerHost, error) {
	id, err := nginxHostID(r)
	if err != nil {
		return nil, err
	}
	return loadDockerHost(id)
}

// cleanRoot validates and normalizes a caller-supplied root path. Empty falls
// back to def; non-absolute is rejected.
func cleanRoot(v, def string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return def, nil
	}
	if !path.IsAbs(v) {
		return "", fmt.Errorf("path must be absolute")
	}
	return path.Clean(v), nil
}

func nginxConfigRoot(r *http.Request) (string, error) {
	return cleanRoot(r.URL.Query().Get("root"), defaultNginxConfigRoot)
}

func nginxLogDir(r *http.Request) (string, error) {
	return cleanRoot(r.URL.Query().Get("dir"), defaultNginxLogRoot)
}

// nginxBin returns the validated nginx binary name from the request.
func nginxBin(r *http.Request) string {
	if strings.TrimSpace(r.URL.Query().Get("bin")) == "openresty" {
		return "openresty"
	}
	return "nginx"
}

func nginxUseSudo(r *http.Request) bool {
	v := strings.TrimSpace(r.URL.Query().Get("sudo"))
	return v == "1" || v == "true"
}

// runHostPrivileged runs args on the host, optionally elevating with sudo. With
// a password-auth host it uses `sudo -S` and feeds the SSH password on stdin
// (the SSH password doubles as the sudo password — the common case); with key
// auth it uses `sudo -n` (NOPASSWD). Any extra command stdin follows the
// password line, which is exactly how `sudo -S` consumes it.
func runHostPrivileged(h *DockerHost, sudo bool, args []string, stdin string) (string, error) {
	if !sudo {
		return runHostCommand(h, args, stdin)
	}
	if h.SSHPassword != "" {
		full := append([]string{"sudo", "-S", "-p", ""}, args...)
		return runHostCommand(h, full, h.SSHPassword+"\n"+stdin)
	}
	return runHostCommand(h, append([]string{"sudo", "-n"}, args...), stdin)
}

// nginxSafeJoin resolves a client-supplied path against root (POSIX semantics)
// and rejects anything that escapes root.
func nginxSafeJoin(root, rel string) (string, error) {
	p := strings.TrimSpace(rel)
	if p == "" {
		return root, nil
	}
	if !path.IsAbs(p) {
		p = path.Join(root, p)
	}
	p = path.Clean(p)
	if p != root && !strings.HasPrefix(p, root+"/") {
		return "", fmt.Errorf("path escapes %s", root)
	}
	return p, nil
}

type nginxDirEnt struct {
	name  string
	isDir bool
}

// nginxReadDir lists one directory level. In sudo mode it shells out to `find`
// (so root-only dirs are readable); otherwise it uses the open SFTP client.
func nginxReadDir(h *DockerHost, client *sftp.Client, sudo bool, dir string) ([]nginxDirEnt, error) {
	if sudo {
		out, err := runHostPrivileged(h, true, []string{
			"find", dir, "-mindepth", "1", "-maxdepth", "1", "-printf", "%y\t%f\n",
		}, "")
		if err != nil {
			return nil, fmt.Errorf("%s", strings.TrimSpace(out))
		}
		var ents []nginxDirEnt
		for _, l := range strings.Split(out, "\n") {
			l = strings.TrimRight(l, "\r")
			if l == "" {
				continue
			}
			typ, name, ok := strings.Cut(l, "\t")
			if !ok {
				continue
			}
			ents = append(ents, nginxDirEnt{name: name, isDir: typ == "d"})
		}
		return ents, nil
	}
	infos, err := client.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	ents := make([]nginxDirEnt, 0, len(infos))
	for _, fi := range infos {
		ents = append(ents, nginxDirEnt{name: fi.Name(), isDir: fi.IsDir()})
	}
	return ents, nil
}

// ── Auto-detection ────────────────────────────────────────────────────────

func nginxField(out, key string) string {
	i := strings.Index(out, key)
	if i < 0 {
		return ""
	}
	rest := out[i+len(key):]
	end := strings.IndexFunc(rest, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// detectNginx probes the host with `<bin> -V` and parses the compile-time
// paths. Falls back to sensible defaults when nothing is detectable.
func detectNginx(h *DockerHost) nginxInfo {
	info := nginxInfo{Bin: "nginx", ConfigRoot: defaultNginxConfigRoot, LogRoot: defaultNginxLogRoot}

	out, err := runHostCommand(h, []string{"nginx", "-V"}, "")
	if err != nil {
		if o2, e2 := runHostCommand(h, []string{"openresty", "-V"}, ""); e2 == nil {
			info.Bin, out, err = "openresty", o2, nil
		}
	}
	if err == nil && strings.TrimSpace(out) != "" {
		for _, line := range strings.Split(out, "\n") {
			l := strings.TrimSpace(line)
			if strings.Contains(l, "version:") {
				info.Version = strings.TrimSpace(strings.SplitN(l, ":", 2)[1])
				break
			}
		}
		prefix := nginxField(out, "--prefix=")
		abs := func(p string) string {
			if p != "" && !path.IsAbs(p) && prefix != "" {
				p = path.Join(prefix, p)
			}
			return p
		}
		if conf := abs(nginxField(out, "--conf-path=")); path.IsAbs(conf) {
			info.ConfigRoot = path.Dir(conf)
		}
		logPath := nginxField(out, "--http-log-path=")
		if logPath == "" {
			logPath = nginxField(out, "--error-log-path=")
		}
		if logPath = abs(logPath); path.IsAbs(logPath) {
			info.LogRoot = path.Dir(logPath)
		}
	}

	if active, e := runHostCommand(h, []string{"systemctl", "is-active", info.Bin}, ""); e == nil || strings.TrimSpace(active) != "" {
		info.Active = strings.TrimSpace(active)
	}
	info.SitesLayout = detectSitesLayout(h, info.ConfigRoot)
	return info
}

// detectSitesLayout reports whether the host uses the Debian symlink layout or
// the conf.d include layout (or neither).
func detectSitesLayout(h *DockerHost, root string) string {
	if out, err := runHostCommand(h, []string{"test", "-d", root + "/sites-available"}, ""); err == nil && strings.TrimSpace(out) == "" {
		return "symlink"
	}
	if out, err := runHostCommand(h, []string{"test", "-d", root + "/conf.d"}, ""); err == nil && strings.TrimSpace(out) == "" {
		return "confd"
	}
	return ""
}

// NginxInfo returns the detected binary, version, paths, and sites layout.
func NginxInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(detectNginx(h))
	}
}

// ── Per-host saved settings ───────────────────────────────────────────────

// NginxGetSettings returns the remembered settings for a host (or exists:false).
func NginxGetSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var useSudo int
		var configRoot, logDir, bin string
		err = appdb.DB.QueryRow(appdb.ConvertQuery(
			`SELECT use_sudo, config_root, log_dir, bin FROM nginx_host_settings WHERE host_id = ?`), id).
			Scan(&useSudo, &configRoot, &logDir, &bin)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"exists": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"exists": true, "use_sudo": useSudo != 0,
			"config_root": configRoot, "log_dir": logDir, "bin": bin,
		})
	}
}

// NginxSaveSettings upserts the host's settings. Gated by nginx.view (anyone
// who can use the page can remember their own paths/sudo preference).
func NginxSaveSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			UseSudo    bool   `json:"use_sudo"`
			ConfigRoot string `json:"config_root"`
			LogDir     string `json:"log_dir"`
			Bin        string `json:"bin"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		sudoInt := 0
		if body.UseSudo {
			sudoInt = 1
		}
		bin := "nginx"
		if body.Bin == "openresty" {
			bin = "openresty"
		}
		// Driver-agnostic upsert: UPDATE, then INSERT if nothing was updated.
		res, err := appdb.DB.Exec(appdb.ConvertQuery(
			`UPDATE nginx_host_settings SET use_sudo=?, config_root=?, log_dir=?, bin=?,
			        updated_at=CURRENT_TIMESTAMP WHERE host_id=?`),
			sudoInt, body.ConfigRoot, body.LogDir, bin, id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := appdb.DB.Exec(appdb.ConvertQuery(
				`INSERT INTO nginx_host_settings (host_id, use_sudo, config_root, log_dir, bin)
				 VALUES (?, ?, ?, ?, ?)`),
				id, sudoInt, body.ConfigRoot, body.LogDir, bin); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// ── Config browsing & editing ─────────────────────────────────────────────

// NginxConfigTree returns a flat list of every file under the config root.
func NginxConfigTree() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		root, err := nginxConfigRoot(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		sudo := nginxUseSudo(r)
		files := make([]nginxFileEntry, 0, 32)

		if sudo {
			h, e := loadDockerHost(id)
			if e != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
			out, e := runHostPrivileged(h, true, []string{
				"find", root, "(", "-type", "f", "-o", "-type", "l", ")", "-printf", "%P\t%s\n",
			}, "")
			if e != nil {
				http.Error(w, jsonError(strings.TrimSpace(out)), http.StatusBadGateway)
				return
			}
			for _, l := range strings.Split(out, "\n") {
				l = strings.TrimRight(l, "\r")
				if l == "" {
					continue
				}
				rel, szStr, ok := strings.Cut(l, "\t")
				if !ok {
					continue
				}
				sz, _ := strconv.ParseInt(strings.TrimSpace(szStr), 10, 64)
				files = append(files, nginxFileEntry{Path: rel, Size: sz})
			}
		} else {
			client, cleanup, e := sftpSession(id)
			if e != nil {
				http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
				return
			}
			defer cleanup()
			walker := client.Walk(root)
			for walker.Step() {
				if walker.Err() != nil {
					continue
				}
				fi := walker.Stat()
				if fi == nil || fi.IsDir() {
					continue
				}
				rel := strings.TrimPrefix(walker.Path(), root+"/")
				files = append(files, nginxFileEntry{Path: rel, Size: fi.Size()})
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		json.NewEncoder(w).Encode(map[string]interface{}{"root": root, "files": files})
	}
}

// NginxConfigRead returns the contents of one config file.
func NginxConfigRead() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		root, err := nginxConfigRoot(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		full, err := nginxSafeJoin(root, r.URL.Query().Get("path"))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var content string
		if nginxUseSudo(r) {
			h, e := loadDockerHost(id)
			if e != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
			out, e := runHostPrivileged(h, true, []string{"cat", full}, "")
			if e != nil {
				http.Error(w, jsonError(strings.TrimSpace(out)), http.StatusBadGateway)
				return
			}
			if len(out) > nginxMaxFile {
				out = out[:nginxMaxFile]
			}
			content = out
		} else {
			client, cleanup, e := sftpSession(id)
			if e != nil {
				http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
				return
			}
			defer cleanup()
			f, e := client.Open(full)
			if e != nil {
				http.Error(w, jsonError(e.Error()), http.StatusNotFound)
				return
			}
			defer f.Close()
			b, e := io.ReadAll(io.LimitReader(f, nginxMaxFile))
			if e != nil {
				http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
				return
			}
			content = string(b)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"path": full, "content": content})
	}
}

// nginxBackup copies an existing config file to "<file>.bak.<unix>" before it
// is overwritten. Returns the backup path (empty when the file is new).
func nginxBackup(h *DockerHost, sudo bool, full string) (string, error) {
	if _, err := runHostPrivileged(h, sudo, []string{"test", "-e", full}, ""); err != nil {
		return "", nil // nothing to back up — new file
	}
	bak := full + ".bak." + strconv.FormatInt(time.Now().Unix(), 10)
	if out, err := runHostPrivileged(h, sudo, []string{"cp", "-p", "--", full, bak}, ""); err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return bak, nil
}

// NginxConfigWrite saves new contents to a config file, backing up the previous
// version first. Gated by nginx.manage.
func NginxConfigWrite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			Root    string `json:"root"`
			Path    string `json:"path"`
			Content string `json:"content"`
			Sudo    bool   `json:"sudo"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid body"), http.StatusBadRequest)
			return
		}
		root, err := cleanRoot(body.Root, defaultNginxConfigRoot)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		full, err := nginxSafeJoin(root, body.Path)
		if err != nil || full == root {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		if len(body.Content) > nginxMaxFile {
			http.Error(w, jsonError("file too large"), http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusBadRequest)
			return
		}
		backup, err := nginxBackup(h, body.Sudo, full)
		if err != nil {
			http.Error(w, jsonError("backup failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		if body.Sudo {
			// `tee` writes stdin to the (root-owned) file and echoes it back.
			if out, e := runHostPrivileged(h, true, []string{"tee", full}, body.Content); e != nil {
				http.Error(w, jsonError(strings.TrimSpace(out)+": "+e.Error()), http.StatusBadGateway)
				return
			}
		} else {
			client, cleanup, e := sftpSession(id)
			if e != nil {
				http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
				return
			}
			defer cleanup()
			f, e := client.Create(full) // truncates
			if e != nil {
				http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
				return
			}
			if _, e := io.Copy(f, strings.NewReader(body.Content)); e != nil {
				f.Close()
				http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
				return
			}
			f.Close()
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "backup": backup})
	}
}

// NginxConfigBackups lists the "*.bak.<unix>" snapshots for a config file,
// newest first.
func NginxConfigBackups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		root, err := nginxConfigRoot(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		full, err := nginxSafeJoin(root, r.URL.Query().Get("path"))
		if err != nil || full == root {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		sudo := nginxUseSudo(r)
		dir, base := path.Dir(full), path.Base(full)

		var h *DockerHost
		var client *sftp.Client
		if sudo {
			if h, err = loadDockerHost(id); err != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
		} else {
			var cleanup func()
			if client, cleanup, err = sftpSession(id); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			defer cleanup()
		}
		ents, err := nginxReadDir(h, client, sudo, dir)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		prefix := base + ".bak."
		type backup struct {
			Path string `json:"path"`
			Name string `json:"name"`
			Time int64  `json:"time"`
		}
		backups := make([]backup, 0, 8)
		for _, en := range ents {
			if en.isDir || !strings.HasPrefix(en.name, prefix) {
				continue
			}
			ts, _ := strconv.ParseInt(strings.TrimPrefix(en.name, prefix), 10, 64)
			backups = append(backups, backup{Path: dir + "/" + en.name, Name: en.name, Time: ts})
		}
		sort.Slice(backups, func(i, j int) bool { return backups[i].Time > backups[j].Time })
		json.NewEncoder(w).Encode(map[string]interface{}{"backups": backups})
	}
}

// ── Sites ─────────────────────────────────────────────────────────────────

// nginxDisabledSuffixes are the trailing markers people append to park a
// conf.d server block so the `*.conf` include glob no longer matches it.
var nginxDisabledSuffixes = []string{".disabled", ".save", ".bak", ".old", ".orig"}

// classifyConfd determines whether a conf.d file is loaded by nginx and, when
// it is a recognizably-parked .conf file, the filename that would re-enable it.
//   - "enabled":  ends in .conf (matched by `include conf.d/*.conf`)
//   - "disabled": a .conf with a known parking suffix (e.g. foo.conf.save)
//   - "inactive": anything else (foo.txt, foo.bak with no .conf) — shown but
//     not auto-toggled, since the target name is ambiguous.
func classifyConfd(name string) (state, enableTarget string) {
	if strings.HasSuffix(name, ".conf") {
		return "enabled", ""
	}
	for _, suf := range nginxDisabledSuffixes {
		if strings.HasSuffix(name, ".conf"+suf) {
			return "disabled", strings.TrimSuffix(name, suf)
		}
	}
	return "inactive", ""
}

// NginxSites lists the host's server blocks for whichever layout it uses:
// Debian sites-available/sites-enabled, or conf.d/*.conf includes.
func NginxSites() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		root, err := nginxConfigRoot(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		layout := strings.TrimSpace(r.URL.Query().Get("layout"))
		sudo := nginxUseSudo(r)

		// h is needed for sudo; client for the non-sudo path.
		var h *DockerHost
		var client *sftp.Client
		if sudo {
			if h, err = loadDockerHost(id); err != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
		} else {
			var cleanup func()
			if client, cleanup, err = sftpSession(id); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			defer cleanup()
		}

		if layout == "" {
			if _, e := nginxReadDir(h, client, sudo, root+"/sites-available"); e == nil {
				layout = "symlink"
			} else {
				layout = "confd"
			}
		}

		sites := make([]nginxSite, 0, 16)
		switch layout {
		case "symlink":
			enabled := map[string]bool{}
			if ents, e := nginxReadDir(h, client, sudo, root+"/sites-enabled"); e == nil {
				for _, en := range ents {
					enabled[en.name] = true
				}
			}
			available, e := nginxReadDir(h, client, sudo, root+"/sites-available")
			if e != nil {
				http.Error(w, jsonError("sites-available not found: "+e.Error()), http.StatusBadGateway)
				return
			}
			for _, en := range available {
				if en.isDir {
					continue
				}
				on := enabled[en.name]
				state := "disabled"
				if on {
					state = "enabled"
				}
				sites = append(sites, nginxSite{Name: en.name, Enabled: on, State: state, Toggleable: true})
			}
		case "confd":
			ents, e := nginxReadDir(h, client, sudo, root+"/conf.d")
			if e != nil {
				http.Error(w, jsonError("conf.d not found: "+e.Error()), http.StatusBadGateway)
				return
			}
			for _, en := range ents {
				if en.isDir {
					continue // e.g. conf.d/kibana — visible in the Config tab instead
				}
				state, target := classifyConfd(en.name)
				sites = append(sites, nginxSite{
					Name:       en.name,
					Enabled:    state == "enabled",
					State:      state,
					Toggleable: state == "enabled" || (state == "disabled" && target != ""),
				})
			}
		default:
			http.Error(w, jsonError("no sites-available or conf.d directory under "+root), http.StatusBadGateway)
			return
		}
		sort.Slice(sites, func(i, j int) bool { return sites[i].Name < sites[j].Name })
		json.NewEncoder(w).Encode(map[string]interface{}{"layout": layout, "sites": sites})
	}
}

// NginxSiteToggle enables/disables a site for either layout. Gated by
// nginx.manage. Never overwrites an existing target.
func NginxSiteToggle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			Root    string `json:"root"`
			Name    string `json:"name"` // actual filename (conf.d) or site name (symlink)
			Layout  string `json:"layout"`
			Enabled bool   `json:"enabled"`
			Sudo    bool   `json:"sudo"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !nginxNamePattern.MatchString(body.Name) {
			http.Error(w, jsonError("invalid site name"), http.StatusBadRequest)
			return
		}
		root, err := cleanRoot(body.Root, defaultNginxConfigRoot)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		// Resolve source/destination paths first.
		var src, dest, link, target string
		if body.Layout == "confd" {
			src = root + "/conf.d/" + body.Name
			if body.Enabled {
				state, t := classifyConfd(body.Name)
				if state == "enabled" {
					json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
					return
				}
				if t == "" {
					http.Error(w, jsonError("can't auto-enable "+body.Name+" — rename it to end in .conf manually"), http.StatusBadRequest)
					return
				}
				dest = root + "/conf.d/" + t
			} else {
				dest = src + ".disabled"
			}
		} else { // symlink
			link = root + "/sites-enabled/" + body.Name
			target = root + "/sites-available/" + body.Name
		}

		if body.Sudo {
			h, e := loadDockerHost(id)
			if e != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
			var args []string
			switch {
			case body.Layout == "confd":
				args = []string{"mv", "-n", src, dest} // -n: never clobber
			case body.Enabled:
				args = []string{"ln", "-sfn", target, link}
			default:
				args = []string{"rm", "-f", link}
			}
			if out, e := runHostPrivileged(h, true, args, ""); e != nil {
				http.Error(w, jsonError(strings.TrimSpace(out)+": "+e.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
			return
		}

		// Non-sudo: SFTP rename/symlink with an explicit no-clobber check.
		client, cleanup, e := sftpSession(id)
		if e != nil {
			http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		exists := func(p string) bool { _, er := client.Lstat(p); return er == nil }

		if body.Layout == "confd" {
			if exists(dest) {
				http.Error(w, jsonError("target already exists: "+path.Base(dest)), http.StatusConflict)
				return
			}
			if er := client.Rename(src, dest); er != nil {
				http.Error(w, jsonError(er.Error()), http.StatusBadGateway)
				return
			}
		} else if body.Enabled {
			_ = client.Remove(link)
			if er := client.Symlink(target, link); er != nil {
				http.Error(w, jsonError(er.Error()), http.StatusBadGateway)
				return
			}
		} else if er := client.Remove(link); er != nil {
			http.Error(w, jsonError(er.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// ── Test / reload / status ────────────────────────────────────────────────

// NginxTest runs `<bin> -t`. Gated by nginx.reload.
func NginxTest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		out, err := runHostPrivileged(h, nginxUseSudo(r), []string{nginxBin(r), "-t"}, "")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": err == nil, "output": out})
	}
}

// NginxReload reloads nginx, falling back to systemctl. It validates the config
// with `<bin> -t` first and refuses to reload a broken config unless ?force=1.
// Gated by nginx.reload.
func NginxReload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		sudo := nginxUseSudo(r)
		bin := nginxBin(r)
		force := r.URL.Query().Get("force") == "1"

		if !force {
			testOut, testErr := runHostPrivileged(h, sudo, []string{bin, "-t"}, "")
			if testErr != nil {
				// Config is invalid — abort and report (200 so the UI can offer Force).
				json.NewEncoder(w).Encode(map[string]interface{}{
					"ok": false, "stage": "test", "output": testOut,
				})
				return
			}
		}

		out, err := runHostPrivileged(h, sudo, []string{bin, "-s", "reload"}, "")
		if err != nil {
			out2, err2 := runHostPrivileged(h, sudo, []string{"systemctl", "reload", bin}, "")
			if err2 != nil {
				http.Error(w, jsonError(strings.TrimSpace(out+out2)+": "+err2.Error()), http.StatusBadGateway)
				return
			}
			out = out2
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "output": out})
	}
}

// NginxStatus reports the nginx version and active state.
func NginxStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		bin := nginxBin(r)
		version, _ := runHostCommand(h, []string{bin, "-v"}, "")
		active, _ := runHostCommand(h, []string{"systemctl", "is-active", bin}, "")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"version": strings.TrimSpace(version),
			"active":  strings.TrimSpace(active),
		})
	}
}

// ── Logs ──────────────────────────────────────────────────────────────────

// NginxLogList lists log files under the log directory. Like the config views
// it is sudo-aware: /var/log/nginx is often 750 root:adm and not even listable
// by a non-root SSH user, so under sudo it shells out to `find` instead of SFTP.
func NginxLogList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		dir, err := nginxLogDir(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		files := make([]nginxFileEntry, 0, 16)
		if nginxUseSudo(r) {
			h, e := loadDockerHost(id)
			if e != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
			out, e := runHostPrivileged(h, true, []string{
				"find", dir, "-maxdepth", "1", "-type", "f", "-printf", "%f\t%s\n",
			}, "")
			if e != nil {
				http.Error(w, jsonError(strings.TrimSpace(out)), http.StatusBadGateway)
				return
			}
			for _, l := range strings.Split(out, "\n") {
				l = strings.TrimRight(l, "\r")
				if l == "" {
					continue
				}
				name, szStr, ok := strings.Cut(l, "\t")
				if !ok {
					continue
				}
				sz, _ := strconv.ParseInt(strings.TrimSpace(szStr), 10, 64)
				files = append(files, nginxFileEntry{Path: name, Size: sz})
			}
		} else {
			client, cleanup, e := sftpSession(id)
			if e != nil {
				http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
				return
			}
			defer cleanup()
			infos, e := client.ReadDir(dir)
			if e != nil {
				http.Error(w, jsonError(e.Error()), http.StatusBadGateway)
				return
			}
			for _, fi := range infos {
				if fi.IsDir() {
					continue
				}
				files = append(files, nginxFileEntry{Path: fi.Name(), Size: fi.Size()})
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		json.NewEncoder(w).Encode(map[string]interface{}{"root": dir, "files": files})
	}
}

// nginxTailScript builds a shell command that yields the last n lines of a log
// file, transparently decompressing rotated logs (.gz/.bz2/.xz/.zst). `--`
// guards against filenames starting with a dash; the path is single-quoted.
func nginxTailScript(full string, n int) string {
	q := "'" + strings.ReplaceAll(full, "'", `'\''`) + "'"
	lines := strconv.Itoa(n)
	lower := strings.ToLower(full)
	switch {
	case strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".tgz"):
		return "zcat -- " + q + " | tail -n " + lines
	case strings.HasSuffix(lower, ".bz2"):
		return "bzcat -- " + q + " | tail -n " + lines
	case strings.HasSuffix(lower, ".xz"):
		return "xzcat -- " + q + " | tail -n " + lines
	case strings.HasSuffix(lower, ".zst"):
		return "zstdcat -- " + q + " | tail -n " + lines
	default:
		return "tail -n " + lines + " -- " + q
	}
}

// NginxLogTail returns the last N lines of a log file (snapshot). Compressed
// rotated logs are decompressed on the fly.
func NginxLogTail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		dir, err := nginxLogDir(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		full, err := nginxSafeJoin(dir, r.URL.Query().Get("file"))
		if err != nil || full == dir {
			http.Error(w, jsonError("invalid file"), http.StatusBadRequest)
			return
		}
		lines := 200
		if n, e := strconv.Atoi(r.URL.Query().Get("lines")); e == nil && n > 0 && n <= 5000 {
			lines = n
		}
		out, err := runHostPrivileged(h, nginxUseSudo(r), []string{"sh", "-c", nginxTailScript(full, lines)}, "")
		if err != nil {
			http.Error(w, jsonError(strings.TrimSpace(out)+": "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"output": out})
	}
}

// nginxSearchScript greps a log (decompressing rotated logs first) for a fixed,
// case-insensitive string, numbering matched lines and capping the result.
func nginxSearchScript(full, pattern string, n int) string {
	q := "'" + strings.ReplaceAll(full, "'", `'\''`) + "'"
	p := "'" + strings.ReplaceAll(pattern, "'", `'\''`) + "'"
	lines := strconv.Itoa(n)
	grep := "grep -F -i -n -e " + p
	lower := strings.ToLower(full)
	var decomp string
	switch {
	case strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".tgz"):
		decomp = "zcat"
	case strings.HasSuffix(lower, ".bz2"):
		decomp = "bzcat"
	case strings.HasSuffix(lower, ".xz"):
		decomp = "xzcat"
	case strings.HasSuffix(lower, ".zst"):
		decomp = "zstdcat"
	}
	if decomp != "" {
		// `| grep` masks grep's exit-1-on-no-match, so empty result isn't an error.
		return decomp + " -- " + q + " | " + grep + " | tail -n " + lines
	}
	return grep + " -- " + q + " | tail -n " + lines
}

// NginxLogSearch greps a log file (server-side, gz-aware) for a pattern.
func NginxLogSearch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		dir, err := nginxLogDir(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		full, err := nginxSafeJoin(dir, r.URL.Query().Get("file"))
		if err != nil || full == dir {
			http.Error(w, jsonError("invalid file"), http.StatusBadRequest)
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			http.Error(w, jsonError("search query required"), http.StatusBadRequest)
			return
		}
		lines := 500
		if n, e := strconv.Atoi(r.URL.Query().Get("lines")); e == nil && n > 0 && n <= 2000 {
			lines = n
		}
		out, err := runHostPrivileged(h, nginxUseSudo(r), []string{"sh", "-c", nginxSearchScript(full, q, lines)}, "")
		if err != nil {
			http.Error(w, jsonError(strings.TrimSpace(out)+": "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"output": out})
	}
}

// NginxLogStream live-follows a log file over SSE. It self-authenticates via a
// ?token= query param (the browser EventSource can't send headers).
func NginxLogStream() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(jwtSecret) > 0 {
			uid, err := dockerWSUserID(r) // validates ?token=
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !appdb.HasUserAppPermission(uid, PermNginxView) &&
				!appdb.HasUserAppPermission(uid, PermNginxManage) &&
				!appdb.HasUserAppPermission(uid, PermNginxReload) {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
		}
		id, err := nginxHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		dir, err := nginxLogDir(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		full, err := nginxSafeJoin(dir, r.URL.Query().Get("file"))
		if err != nil || full == dir {
			http.Error(w, jsonError("invalid file"), http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusBadRequest)
			return
		}
		sc, err := sshClientForHost(h)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer sc.Close()
		sess, err := sc.NewSession()
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer sess.Close()
		stdout, err := sess.StdoutPipe()
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		flushSSE(w)

		// tail -F follows across rotation. Quote the path defensively. 2>&1 folds
		// stderr into the stream so failures (e.g. "Permission denied", "sudo: a
		// password is required") show up as log lines instead of a silent close.
		quoted := "'" + strings.ReplaceAll(full, "'", `'\''`) + "'"
		cmd := "tail -n 100 -F " + quoted + " 2>&1"
		if nginxUseSudo(r) {
			if h.SSHPassword != "" {
				cmd = "sudo -S -p '' " + cmd
				sess.Stdin = strings.NewReader(h.SSHPassword + "\n")
			} else {
				cmd = "sudo -n " + cmd
			}
		}
		if err := sess.Start(cmd); err != nil {
			sendSSE(w, StreamError{err.Error()})
			flushSSE(w)
			return
		}

		// Stop the remote tail when the client disconnects.
		go func() {
			<-r.Context().Done()
			sess.Close()
			sc.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			sendSSE(w, map[string]string{"line": scanner.Text()})
			flushSSE(w)
		}
	}
}
