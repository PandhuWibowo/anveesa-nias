package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
	"github.com/pkg/sftp"
)

// SFTP rides on the existing SSH host credentials (the same records used for
// remote Docker hosts). Local hosts (no SSH) are not supported.

type sftpEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	IsLink  bool   `json:"isLink"`
	Mode    string `json:"mode"`
	ModTime int64  `json:"modTime"`
}

// sftpSession opens an SSH + SFTP session to a host. Caller must call cleanup.
func sftpSession(id int64) (*sftp.Client, func(), error) {
	h, err := loadDockerHost(id)
	if err != nil {
		return nil, nil, fmt.Errorf("host not found")
	}
	if strings.TrimSpace(h.SSHHost) == "" {
		return nil, nil, fmt.Errorf("SFTP needs an SSH host — local hosts aren't supported")
	}
	sc, err := sshClientForHost(h)
	if err != nil {
		return nil, nil, err
	}
	client, err := sftp.NewClient(sc,
		sftp.UseConcurrentWrites(true),
		sftp.UseConcurrentReads(true),
		sftp.MaxConcurrentRequestsPerFile(64),
	)
	if err != nil {
		sc.Close()
		return nil, nil, err
	}
	return client, func() { client.Close(); sc.Close() }, nil
}

func sftpPathParts(r *http.Request) []string {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/sftp/hosts/"), "/")
	return strings.Split(rest, "/")
}

func sftpHostID(r *http.Request) (int64, error) {
	parts := sftpPathParts(r)
	if len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("missing host id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

// SftpList lists a remote directory.
func SftpList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
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

		p := r.URL.Query().Get("path")
		if strings.TrimSpace(p) == "" {
			if home, e := client.Getwd(); e == nil && home != "" {
				p = home
			} else {
				p = "/"
			}
		}
		infos, err := client.ReadDir(p)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		list := make([]sftpEntry, 0, len(infos))
		for _, fi := range infos {
			isLink := fi.Mode()&os.ModeSymlink != 0
			list = append(list, sftpEntry{
				Name: fi.Name(), Size: fi.Size(), IsDir: fi.IsDir(), IsLink: isLink,
				Mode: fi.Mode().String(), ModTime: fi.ModTime().Unix(),
			})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].IsDir != list[j].IsDir {
				return list[i].IsDir
			}
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		})
		json.NewEncoder(w).Encode(map[string]interface{}{"path": p, "entries": list})
	}
}

// SftpDownload streams a remote file to the browser. It self-authenticates via
// a ?token= query param so the browser can download natively (no in-memory blob).
func SftpDownload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(jwtSecret) > 0 {
			uid, err := dockerWSUserID(r) // validates ?token=
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !appdb.HasUserAppPermission(uid, PermSftpAccess) && !appdb.HasUserAppPermission(uid, PermSftpManage) {
				http.Error(w, "insufficient permissions", http.StatusForbidden)
				return
			}
		}
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		p := r.URL.Query().Get("path")
		if strings.TrimSpace(p) == "" {
			http.Error(w, jsonError("path is required"), http.StatusBadRequest)
			return
		}
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		f, err := client.Open(p)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusNotFound)
			return
		}
		defer f.Close()
		if st, e := f.Stat(); e == nil && !st.IsDir() {
			w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(p)+`"`)
		io.Copy(w, f)
	}
}

// sftpMaxRead caps how much of a file SftpRead will return.
const sftpMaxRead = 2 << 20 // 2 MiB

// SftpRead returns a text preview of a remote file's content.
func SftpRead() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		p := r.URL.Query().Get("path")
		if strings.TrimSpace(p) == "" {
			http.Error(w, jsonError("path is required"), http.StatusBadRequest)
			return
		}
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		f, err := client.Open(p)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusNotFound)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if st.IsDir() {
			http.Error(w, jsonError("cannot read a directory"), http.StatusBadRequest)
			return
		}
		buf, err := io.ReadAll(io.LimitReader(f, sftpMaxRead))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		binary := false
		if i := bytes.IndexByte(buf, 0); i != -1 {
			binary = true
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"path":      p,
			"content":   string(buf),
			"size":      st.Size(),
			"truncated": st.Size() > int64(len(buf)),
			"binary":    binary,
		})
	}
}

// SftpUpload streams uploaded files into a remote directory (no server-side
// buffering — handles large files efficiently with concurrent SFTP writes).
func SftpUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		dest := r.URL.Query().Get("path")
		if strings.TrimSpace(dest) == "" {
			http.Error(w, jsonError("destination path is required"), http.StatusBadRequest)
			return
		}
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()

		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, jsonError("expected multipart upload"), http.StatusBadRequest)
			return
		}
		count := 0
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			if part.FormName() != "file" || part.FileName() == "" {
				part.Close()
				continue
			}
			remote := path.Join(dest, path.Base(part.FileName()))
			f, err := client.Create(remote)
			if err != nil {
				part.Close()
				http.Error(w, jsonError("create "+remote+": "+err.Error()), http.StatusBadGateway)
				return
			}
			// io.Copy uses File.ReadFrom → concurrent/pipelined writes for speed.
			if _, err := io.Copy(f, part); err != nil {
				f.Close()
				part.Close()
				http.Error(w, jsonError("write "+remote+": "+err.Error()), http.StatusBadGateway)
				return
			}
			f.Close()
			part.Close()
			count++
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "uploaded": count})
	}
}

// sftpRemoveAll removes a file or recursively removes a directory.
func sftpRemoveAll(c *sftp.Client, p string) error {
	st, err := c.Stat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return c.Remove(p)
	}
	infos, err := c.ReadDir(p)
	if err != nil {
		return err
	}
	for _, fi := range infos {
		if err := sftpRemoveAll(c, path.Join(p, fi.Name())); err != nil {
			return err
		}
	}
	return c.RemoveDirectory(p)
}

// SftpMkdir creates a directory.
func SftpMkdir() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.Path) == "" {
			http.Error(w, jsonError("path is required"), http.StatusBadRequest)
			return
		}
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		if err := client.MkdirAll(body.Path); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// SftpDelete removes a file or directory (recursive).
func SftpDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.Path) == "" || body.Path == "/" {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		if err := sftpRemoveAll(client, body.Path); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// SftpCompress archives a file or directory into a .zip or .tar.gz sitting
// next to it, by shelling out to the remote zip/tar binary over SSH (avoids
// pulling the whole tree through the SFTP link just to re-upload it).
func SftpCompress() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			Path   string `json:"path"`
			Format string `json:"format"` // "zip" or "targz"
		}
		json.NewDecoder(r.Body).Decode(&body)
		p := strings.TrimSpace(body.Path)
		if p == "" || p == "/" {
			http.Error(w, jsonError("invalid path"), http.StatusBadRequest)
			return
		}
		var ext string
		switch body.Format {
		case "zip":
			ext = ".zip"
		case "targz":
			ext = ".tar.gz"
		default:
			http.Error(w, jsonError("format must be zip or targz"), http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusBadRequest)
			return
		}

		dir := path.Dir(p)
		base := path.Base(p)
		archiveName := base + ext
		archivePath := path.Join(dir, archiveName)

		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if _, statErr := client.Stat(archivePath); statErr == nil {
			cleanup()
			http.Error(w, jsonError(archiveName+" already exists"), http.StatusConflict)
			return
		}
		cleanup()

		// On failure (e.g. a permission-denied file partway through), clean up
		// whatever partial archive got written — otherwise it lingers and blocks
		// every retry with a false "already exists" conflict. chmod 644 so a
		// sudo-created archive is still downloadable over the unprivileged
		// SFTP session afterward.
		var buildCmd string
		if body.Format == "zip" {
			buildCmd = fmt.Sprintf("zip -rq %s %s", shellQuote(archiveName), shellQuote(base))
		} else {
			buildCmd = fmt.Sprintf("tar -czf %s %s", shellQuote(archiveName), shellQuote(base))
		}
		innerCmd := fmt.Sprintf("cd %s && %s && chmod 644 %s; ec=$?; if [ $ec -ne 0 ]; then rm -f %s; fi; exit $ec",
			shellQuote(dir), buildCmd, shellQuote(archiveName), shellQuote(archiveName))

		// Reuses the same sudo convention as Nginx config management: ?sudo=1
		// elevates via `sudo -S` (password auth, password piped on stdin) or
		// `sudo -n` (key auth, requires NOPASSWD) — for paths the SSH user
		// can't read/write directly, like root-owned /etc/ssl.
		cmd := innerCmd
		stdin := ""
		if nginxUseSudo(r) {
			if h.SSHPassword != "" {
				cmd = "sudo -S -p '' sh -c " + shellQuote(innerCmd)
				stdin = h.SSHPassword + "\n"
			} else {
				cmd = "sudo -n sh -c " + shellQuote(innerCmd)
			}
		}
		stdout, stderr, exitCode, err := runSSHCommand(h, cmd, stdin, 300)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if exitCode != 0 {
			msg := strings.TrimSpace(stderr)
			if msg == "" {
				msg = strings.TrimSpace(stdout)
			}
			if msg == "" {
				msg = fmt.Sprintf("command exited with code %d", exitCode)
			}
			http.Error(w, jsonError(msg), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "name": archiveName, "path": archivePath})
	}
}

// SftpRename renames/moves a file or directory.
func SftpRename() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		var body struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.TrimSpace(body.From) == "" || strings.TrimSpace(body.To) == "" {
			http.Error(w, jsonError("from and to are required"), http.StatusBadRequest)
			return
		}
		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()
		if err := client.Rename(body.From, body.To); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}
