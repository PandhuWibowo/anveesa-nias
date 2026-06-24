package handlers

import (
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
