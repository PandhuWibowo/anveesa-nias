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

	appdb "github.com/anveesa/nias/db"
)

// Nginx management rides on the same SSH host records as Docker and SFTP
// (the docker_hosts table). It reads/edits config, toggles sites, tests/reloads
// nginx, and tails logs — all over SSH. Paths and the binary (nginx vs
// openresty) are auto-detected per host and can be overridden by the caller,
// so non-Debian layouts (conf.d/*.conf, openresty, custom prefixes) work too.
// Local hosts (no SSH) are not supported.

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
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()

		files := make([]nginxFileEntry, 0, 32)
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
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		f, err := client.Open(full)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusNotFound)
			return
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, nginxMaxFile))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"path": full, "content": string(b)})
	}
}

// NginxConfigWrite saves new contents to a config file. Gated by nginx.manage.
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
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		f, err := client.Create(full) // truncates
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if _, err := io.Copy(f, strings.NewReader(body.Content)); err != nil {
			f.Close()
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		f.Close()
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
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

		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()

		// Auto-pick a layout if the caller didn't pin one.
		if layout == "" {
			if _, e := client.Stat(root + "/sites-available"); e == nil {
				layout = "symlink"
			} else if _, e := client.Stat(root + "/conf.d"); e == nil {
				layout = "confd"
			}
		}

		sites := make([]nginxSite, 0, 16)
		switch layout {
		case "symlink":
			enabled := map[string]bool{}
			if infos, e := client.ReadDir(root + "/sites-enabled"); e == nil {
				for _, fi := range infos {
					enabled[fi.Name()] = true
				}
			}
			available, e := client.ReadDir(root + "/sites-available")
			if e != nil {
				http.Error(w, jsonError("sites-available not found: "+e.Error()), http.StatusBadGateway)
				return
			}
			for _, fi := range available {
				if fi.IsDir() {
					continue
				}
				on := enabled[fi.Name()]
				state := "disabled"
				if on {
					state = "enabled"
				}
				sites = append(sites, nginxSite{Name: fi.Name(), Enabled: on, State: state, Toggleable: true})
			}
		case "confd":
			infos, e := client.ReadDir(root + "/conf.d")
			if e != nil {
				http.Error(w, jsonError("conf.d not found: "+e.Error()), http.StatusBadGateway)
				return
			}
			for _, fi := range infos {
				if fi.IsDir() {
					continue // e.g. conf.d/kibana — visible in the Config tab instead
				}
				state, target := classifyConfd(fi.Name())
				sites = append(sites, nginxSite{
					Name:       fi.Name(),
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
// nginx.manage. Uses SFTP rename/symlink with a no-clobber check so an existing
// target is never overwritten.
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
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()

		exists := func(p string) bool { _, e := client.Lstat(p); return e == nil }

		if body.Layout == "confd" {
			src := root + "/conf.d/" + body.Name
			var dest string
			if body.Enabled {
				state, target := classifyConfd(body.Name)
				if state == "enabled" {
					json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) // already on
					return
				}
				if target == "" {
					http.Error(w, jsonError("can't auto-enable "+body.Name+" — rename it to end in .conf manually"), http.StatusBadRequest)
					return
				}
				dest = root + "/conf.d/" + target
			} else {
				dest = src + ".disabled"
			}
			if exists(dest) {
				http.Error(w, jsonError("target already exists: "+path.Base(dest)), http.StatusConflict)
				return
			}
			if err := client.Rename(src, dest); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
		} else { // symlink layout
			link := root + "/sites-enabled/" + body.Name
			if body.Enabled {
				_ = client.Remove(link) // clear any stale entry first
				if err := client.Symlink(root+"/sites-available/"+body.Name, link); err != nil {
					http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
					return
				}
			} else if err := client.Remove(link); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
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
		out, err := runHostCommand(h, []string{nginxBin(r), "-t"}, "")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": err == nil, "output": out})
	}
}

// NginxReload reloads nginx, falling back to systemctl. Gated by nginx.reload.
func NginxReload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h, err := nginxRunHost(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		bin := nginxBin(r)
		out, err := runHostCommand(h, []string{bin, "-s", "reload"}, "")
		if err != nil {
			out2, err2 := runHostCommand(h, []string{"systemctl", "reload", bin}, "")
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

// NginxLogList lists log files under the log directory.
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
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		infos, err := client.ReadDir(dir)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		files := make([]nginxFileEntry, 0, len(infos))
		for _, fi := range infos {
			if fi.IsDir() {
				continue
			}
			files = append(files, nginxFileEntry{Path: fi.Name(), Size: fi.Size()})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		json.NewEncoder(w).Encode(map[string]interface{}{"root": dir, "files": files})
	}
}

// NginxLogTail returns the last N lines of a log file (snapshot).
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
		out, err := runHostCommand(h, []string{"tail", "-n", strconv.Itoa(lines), full}, "")
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

		// tail -F follows across rotation. Quote the path defensively.
		cmd := "tail -n 100 -F '" + strings.ReplaceAll(full, "'", `'\''`) + "'"
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
