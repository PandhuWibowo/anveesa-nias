package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
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

// sftpSessionTimeout bounds starting the SFTP subsystem once the SSH
// connection itself is up. sshClientForHost's ssh.Dial already has its own
// 10s timeout for the TCP+handshake phase, but the SFTP subsystem request
// that follows has no timeout of its own in golang.org/x/crypto/ssh — if the
// remote sshd never answers it (a stalled connection, a dropped packet after
// the handshake, etc.), the request hung forever with no error surfaced
// anywhere. This bounds that second phase too.
const sftpSessionTimeout = 20 * time.Second

// sftpSession opens an SSH + SFTP session to a host. Caller must call cleanup.
func sftpSession(id int64) (*sftp.Client, func(), error) {
	h, err := loadDockerHost(id)
	if err != nil {
		return nil, nil, fmt.Errorf("host not found")
	}
	if strings.TrimSpace(h.SSHHost) == "" {
		return nil, nil, fmt.Errorf("SFTP needs an SSH host — local hosts aren't supported")
	}

	type result struct {
		client *sftp.Client
		sc     *ssh.Client
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		sc, err := sshClientForHost(h)
		if err != nil {
			ch <- result{err: err}
			return
		}
		client, err := sftp.NewClient(sc,
			sftp.UseConcurrentWrites(true),
			sftp.UseConcurrentReads(true),
			sftp.MaxConcurrentRequestsPerFile(64),
		)
		if err != nil {
			sc.Close()
			ch <- result{err: err}
			return
		}
		ch <- result{client: client, sc: sc}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, nil, res.err
		}
		return res.client, func() { res.client.Close(); res.sc.Close() }, nil
	case <-time.After(sftpSessionTimeout):
		// Don't leak the connection if it eventually does come through late.
		go func() {
			if res := <-ch; res.err == nil {
				res.client.Close()
				res.sc.Close()
			}
		}()
		return nil, nil, fmt.Errorf("timed out connecting to %s over SSH/SFTP", h.SSHHost)
	}
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

// sftpWalkFiles recursively visits every FILE under p (or p itself, if p is a
// file), calling visit with the file's full remote path and the name it
// should get inside the zip (entryPrefix + its path relative to p) — this is
// what preserves folder structure when a whole directory is selected for
// bulk download, e.g. selecting "Intraday" produces entries like
// "Intraday/file.xlsx", "Intraday/sub/file2.xlsx", not a flat file list.
func sftpWalkFiles(c *sftp.Client, p, entryName string, visit func(fullPath, entryName string) error) error {
	st, err := c.Stat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return visit(p, entryName)
	}
	infos, err := c.ReadDir(p)
	if err != nil {
		return err
	}
	for _, fi := range infos {
		if err := sftpWalkFiles(c, path.Join(p, fi.Name()), path.Join(entryName, fi.Name()), visit); err != nil {
			return err
		}
	}
	return nil
}

func sftpZipFilename(paths []string) string {
	if len(paths) == 1 {
		base := path.Base(strings.TrimRight(paths[0], "/"))
		if base == "" || base == "." || base == "/" {
			base = "download"
		}
		return base + ".zip"
	}
	return fmt.Sprintf("download-%d.zip", time.Now().Unix())
}

// SftpDownloadZip streams one or more remote files/folders as a single ZIP
// archive — folders are included recursively with their structure preserved.
// Self-authenticates via ?token= like SftpDownload, since it's a plain
// browser GET (an anchor-tag download can't carry an Authorization header).
func SftpDownloadZip() http.HandlerFunc {
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

		seen := map[string]bool{}
		var paths []string
		for _, p := range r.URL.Query()["path"] {
			p = strings.TrimSpace(p)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			paths = append(paths, p)
		}
		if len(paths) == 0 {
			http.Error(w, jsonError("at least one path is required"), http.StatusBadRequest)
			return
		}

		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		defer cleanup()

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+sftpZipFilename(paths)+`"`)

		zw := zip.NewWriter(w)
		defer zw.Close()

		for _, p := range paths {
			entryRoot := path.Base(strings.TrimRight(p, "/"))
			// Best-effort: a path that's missing, permission-denied, or a
			// broken symlink just contributes nothing to the zip rather than
			// failing the whole download — headers are already sent by the
			// time any individual file read could fail, so there's no clean
			// way to report a partial failure other than an incomplete zip.
			_ = sftpWalkFiles(client, p, entryRoot, func(fullPath, entryName string) error {
				f, err := client.Open(fullPath)
				if err != nil {
					return nil
				}
				defer f.Close()
				fw, err := zw.Create(entryName)
				if err != nil {
					return nil
				}
				io.Copy(fw, f)
				return nil
			})
		}
	}
}

// SftpBackupToBucket starts an async job that zips one or more remote
// files/folders (same recursive-structure-preserving walk as
// SftpDownloadZip) and uploads the archive straight to an existing object
// storage connection — server-to-server, so a multi-GB folder never
// round-trips through the browser. Reuses the backup job store/endpoints
// (GET/DELETE /api/backup/jobs/:id) so the frontend can poll it exactly like
// a database-to-bucket backup.
//
// POST /api/sftp/hosts/{id}/backup-to-bucket
func SftpBackupToBucket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		var req struct {
			Paths      []string `json:"paths"`
			DestConnID int64    `json:"dest_conn_id"`
			Prefix     string   `json:"prefix"`
			Subfolder  string   `json:"subfolder"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		if req.DestConnID == 0 {
			http.Error(w, jsonError("dest_conn_id is required"), http.StatusBadRequest)
			return
		}

		seen := map[string]bool{}
		var paths []string
		for _, p := range req.Paths {
			p = strings.TrimSpace(p)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			paths = append(paths, p)
		}
		if len(paths) == 0 {
			http.Error(w, jsonError("at least one path is required"), http.StatusBadRequest)
			return
		}

		dest, err := fetchBucketConn(req.DestConnID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		client, cleanup, err := sftpSession(id)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		ts := time.Now().UTC().Format("20060102_150405")
		prefix := strings.TrimSpace(req.Prefix)
		if prefix == "" {
			prefix = "sftp"
		}
		objectName := fmt.Sprintf("%s_%s.zip", prefix, ts)
		if sub := strings.Trim(strings.TrimSpace(req.Subfolder), "/"); sub != "" {
			objectName = sub + "/" + objectName
		}

		jobCtx, jobCancel := context.WithCancel(context.Background())
		job := &BackupJob{
			ID:        newJobID(),
			Status:    BackupJobRunning,
			Stage:     "zipping",
			StartedAt: time.Now(),
			cancel:    jobCancel,
		}
		backupJobs.Store(job.ID, job)

		go func() {
			defer jobCancel()
			defer cleanup()

			pr, pw := io.Pipe()
			go func() {
				zw := zip.NewWriter(pw)
				var walkErr error
				for _, p := range paths {
					entryRoot := path.Base(strings.TrimRight(p, "/"))
					if err := sftpWalkFiles(client, p, entryRoot, func(fullPath, entryName string) error {
						f, err := client.Open(fullPath)
						if err != nil {
							return nil
						}
						defer f.Close()
						fw, err := zw.Create(entryName)
						if err != nil {
							return nil
						}
						_, err = io.Copy(fw, f)
						return err
					}); err != nil {
						walkErr = err
					}
				}
				closeErr := zw.Close()
				if walkErr == nil {
					walkErr = closeErr
				}
				pw.CloseWithError(walkErr)
			}()

			cr := &countingReader{r: pr}
			job.mu.Lock()
			job.Stage = "uploading"
			job.uploadCounter = &cr.n
			job.mu.Unlock()
			uploadErr := uploadToBucketStream(jobCtx, dest, objectName, cr)
			if uploadErr != nil {
				pr.CloseWithError(uploadErr)
			}

			now := time.Now()
			job.mu.Lock()
			defer job.mu.Unlock()
			job.DoneAt = &now
			if uploadErr != nil || jobCtx.Err() != nil {
				if jobCtx.Err() != nil && uploadErr == nil {
					job.Status = BackupJobCanceled
				} else {
					job.Status = BackupJobFailed
					if uploadErr != nil {
						job.Error = uploadErr.Error()
					} else {
						job.Error = jobCtx.Err().Error()
					}
				}
				return
			}
			job.Status = BackupJobDone
			job.ObjectKey = objectName
			job.Bucket = dest.Bucket
			job.SizeBytes = atomic.LoadInt64(&cr.n)
		}()

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
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
		sudo := nginxUseSudo(r)
		var h *DockerHost
		if sudo {
			h, err = loadDockerHost(id)
			if err != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
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
			name := path.Base(part.FileName())
			remote := path.Join(dest, name)

			// Without sudo, write straight to the destination as before. With
			// sudo, the destination directory usually isn't writable by the
			// plain SSH user — write to a scratch path in /tmp (world-writable
			// almost everywhere) over the same unprivileged SFTP session, then
			// move it into place with an elevated shell command. This avoids
			// re-architecting the upload as a privileged shell stream just to
			// support restricted destinations.
			writePath := remote
			if sudo {
				writePath = fmt.Sprintf("/tmp/.nias-upload-%d-%s", time.Now().UnixNano(), name)
			}
			f, err := client.Create(writePath)
			if err != nil {
				part.Close()
				http.Error(w, jsonError("create "+writePath+": "+err.Error()), http.StatusBadGateway)
				return
			}
			// io.Copy uses File.ReadFrom → concurrent/pipelined writes for speed.
			_, copyErr := io.Copy(f, part)
			f.Close()
			part.Close()
			if copyErr != nil {
				if sudo {
					client.Remove(writePath)
				}
				http.Error(w, jsonError("write "+writePath+": "+copyErr.Error()), http.StatusBadGateway)
				return
			}
			if sudo {
				cmd := fmt.Sprintf("mkdir -p %s && mv %s %s && chmod 644 %s",
					shellQuote(dest), shellQuote(writePath), shellQuote(remote), shellQuote(remote))
				stdout, stderr, exitCode, err := runShellMaybeSudo(h, r, cmd, 60)
				if err != nil {
					client.Remove(writePath)
					http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
					return
				}
				if exitCode != 0 {
					client.Remove(writePath)
					http.Error(w, jsonError(shellFailureMessage(stdout, stderr, exitCode)), http.StatusBadGateway)
					return
				}
			}
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
		if nginxUseSudo(r) {
			h, err := loadDockerHost(id)
			if err != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
			stdout, stderr, exitCode, err := runShellMaybeSudo(h, r, "mkdir -p "+shellQuote(body.Path), 30)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			if exitCode != 0 {
				http.Error(w, jsonError(shellFailureMessage(stdout, stderr, exitCode)), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
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
		if nginxUseSudo(r) {
			h, err := loadDockerHost(id)
			if err != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
			stdout, stderr, exitCode, err := runShellMaybeSudo(h, r, "rm -rf "+shellQuote(body.Path), 60)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			if exitCode != 0 {
				http.Error(w, jsonError(shellFailureMessage(stdout, stderr, exitCode)), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
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

		stdout, stderr, exitCode, err := runShellMaybeSudo(h, r, innerCmd, 300)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if exitCode != 0 {
			http.Error(w, jsonError(shellFailureMessage(stdout, stderr, exitCode)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "name": archiveName, "path": archivePath})
	}
}

// runShellMaybeSudo runs shellCmd on h over SSH, elevating with sudo when the
// request asks for it (?sudo=1 — same convention as Nginx config management):
// `sudo -S -p ”` with the SSH password piped on stdin for password-auth
// hosts, or `sudo -n` (requires NOPASSWD) for key auth. Used for paths the
// SSH user can't read/write directly, like root-owned /etc/ssl.
func runShellMaybeSudo(h *DockerHost, r *http.Request, shellCmd string, timeoutSec int) (stdout, stderr string, exitCode int, err error) {
	cmd := shellCmd
	stdin := ""
	if nginxUseSudo(r) {
		if h.SSHPassword != "" {
			cmd = "sudo -S -p '' sh -c " + shellQuote(shellCmd)
			stdin = h.SSHPassword + "\n"
		} else {
			cmd = "sudo -n sh -c " + shellQuote(shellCmd)
		}
	}
	return runSSHCommand(h, cmd, stdin, timeoutSec)
}

// shellFailureMessage picks the most useful text to surface from a failed
// shell command.
func shellFailureMessage(stdout, stderr string, exitCode int) string {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = strings.TrimSpace(stdout)
	}
	if msg == "" {
		msg = fmt.Sprintf("command exited with code %d", exitCode)
	}
	return msg
}

// archiveKind sniffs a filename to decide which tool reads/writes it — GNU
// tar auto-detects gzip/bzip2/xz compression on read (list/extract) from the
// file's own magic bytes, so ".tar", ".tar.gz" and ".tgz" all share one path.
func archiveKind(p string) (string, bool) {
	lower := strings.ToLower(p)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return "zip", true
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar"):
		return "tar", true
	default:
		return "", false
	}
}

// SftpArchiveList previews a .zip/.tar(.gz) archive by listing its entries,
// without extracting anything.
func SftpArchiveList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := sftpHostID(r)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		p := strings.TrimSpace(r.URL.Query().Get("path"))
		if p == "" {
			http.Error(w, jsonError("path is required"), http.StatusBadRequest)
			return
		}
		kind, ok := archiveKind(p)
		if !ok {
			http.Error(w, jsonError("not a recognized archive (.zip, .tar, .tar.gz, .tgz)"), http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusBadRequest)
			return
		}
		var cmd string
		if kind == "zip" {
			cmd = "unzip -l " + shellQuote(p)
		} else {
			cmd = "tar -tvf " + shellQuote(p)
		}
		stdout, stderr, exitCode, err := runShellMaybeSudo(h, r, cmd, 30)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if exitCode != 0 {
			http.Error(w, jsonError(shellFailureMessage(stdout, stderr, exitCode)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"path": p, "listing": stdout})
	}
}

// SftpExtract extracts a .zip/.tar(.gz) archive into the directory it lives
// in ("extract here"), overwriting any existing files with the same names.
func SftpExtract() http.HandlerFunc {
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
		p := strings.TrimSpace(body.Path)
		if p == "" {
			http.Error(w, jsonError("path is required"), http.StatusBadRequest)
			return
		}
		kind, ok := archiveKind(p)
		if !ok {
			http.Error(w, jsonError("not a recognized archive (.zip, .tar, .tar.gz, .tgz)"), http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, jsonError("host not found"), http.StatusBadRequest)
			return
		}
		dir := path.Dir(p)
		var cmd string
		if kind == "zip" {
			cmd = fmt.Sprintf("unzip -o %s -d %s", shellQuote(p), shellQuote(dir))
		} else {
			cmd = fmt.Sprintf("tar -xf %s -C %s", shellQuote(p), shellQuote(dir))
		}
		stdout, stderr, exitCode, err := runShellMaybeSudo(h, r, cmd, 300)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if exitCode != 0 {
			http.Error(w, jsonError(shellFailureMessage(stdout, stderr, exitCode)), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "path": dir})
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
		if nginxUseSudo(r) {
			h, err := loadDockerHost(id)
			if err != nil {
				http.Error(w, jsonError("host not found"), http.StatusBadRequest)
				return
			}
			cmd := "mv " + shellQuote(body.From) + " " + shellQuote(body.To)
			stdout, stderr, exitCode, err := runShellMaybeSudo(h, r, cmd, 60)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
				return
			}
			if exitCode != 0 {
				http.Error(w, jsonError(shellFailureMessage(stdout, stderr, exitCode)), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
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
