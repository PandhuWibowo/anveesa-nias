package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"golang.org/x/crypto/ssh"
)

// This feature manages the *native* crontab on each SSH host. Anveesa does not
// store cron jobs itself — it reads `crontab -l` and writes back via `crontab -`
// over SSH, so existing schedulers already on the VMs are shown and editable.
// Targets are the shared SSH Hosts (docker_hosts); execution reuses
// loadDockerHost + sshClientForHost from docker.go.

const cronOutputCap = 128 * 1024

func truncateCronOutput(s string) string {
	if len(s) > cronOutputCap {
		return s[:cronOutputCap] + "\n…(truncated)"
	}
	return s
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CronHostRef is the minimal host info the crontab manager needs. Cron reuses
// the shared SSH Hosts (docker_hosts, managed under "SSH Hosts").
type CronHostRef struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	SSHHost string `json:"ssh_host"`
	SSHUser string `json:"ssh_user"`
}

// ── SSH command runner ────────────────────────────────────────────────────

// runSSHCommand runs command on the host, feeding stdin, and returns captured
// stdout/stderr and the exit code. A non-zero exit is reported via exitCode
// (err stays nil); err is only set for transport, auth, or timeout failures.
func runSSHCommand(h *DockerHost, command, stdin string, timeoutSec int) (stdout, stderr string, exitCode int, err error) {
	client, err := sshClientForHost(h)
	if err != nil {
		return "", "", -1, err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return "", "", -1, err
	}
	defer sess.Close()

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf
	sess.Stdin = strings.NewReader(stdin)

	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if err := sess.Start(command); err != nil {
		return "", "", -1, err
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case werr := <-done:
		err = werr
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return truncateCronOutput(outBuf.String()), truncateCronOutput(errBuf.String()), -1,
			fmt.Errorf("timed out after %ds", timeoutSec)
	}

	exitCode = 0
	if err != nil {
		if ee, ok := err.(*ssh.ExitError); ok {
			exitCode = ee.ExitStatus()
			err = nil
		} else {
			exitCode = -1
		}
	}
	return truncateCronOutput(outBuf.String()), truncateCronOutput(errBuf.String()), exitCode, err
}

// ── Hosts ─────────────────────────────────────────────────────────────────

// ListCronHosts returns the shared SSH Hosts (docker_hosts) so users can pick
// which server's crontab to manage. Gated by cron perms, no credentials.
func ListCronHosts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, ssh_host, ssh_user FROM docker_hosts
			 WHERE ssh_host IS NOT NULL AND ssh_host != '' ORDER BY name ASC`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		list := []CronHostRef{}
		for rows.Next() {
			var h CronHostRef
			if err := rows.Scan(&h.ID, &h.Name, &h.SSHHost, &h.SSHUser); err != nil {
				continue
			}
			list = append(list, h)
		}
		json.NewEncoder(w).Encode(list)
	}
}

// cronHostIDFromPath extracts {id} from /api/cron/hosts/{id}/...
func cronHostIDFromPath(r *http.Request) (int64, error) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/cron/hosts/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return 0, fmt.Errorf("missing host id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

// ── Crontab read/write ────────────────────────────────────────────────────

// systemCronCmd cats the system-wide cron sources for read-only visibility.
const systemCronCmd = `cat /etc/crontab 2>/dev/null; ` +
	`for f in /etc/cron.d/*; do [ -f "$f" ] && printf '\n# ===== %s =====\n' "$f" && cat "$f" 2>/dev/null; done`

// GetHostCrontab reads the connecting user's crontab plus (best-effort,
// read-only) the system cron sources.
func GetHostCrontab() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, `{"error":"host not found"}`, http.StatusNotFound)
			return
		}

		out, errOut, code, err := runSSHCommand(h, "crontab -l", "", 20)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		userCrontab := ""
		exists := code == 0
		if exists {
			userCrontab = out
		} else if !strings.Contains(strings.ToLower(errOut), "no crontab") {
			// A real failure (permission, etc.) — surface it; empty otherwise.
			http.Error(w, jsonError(strings.TrimSpace(errOut)), http.StatusBadGateway)
			return
		}

		// Best-effort: also fetch root's crontab via sudo so non-root SSH
		// users can see and manage the root scheduler.
		sudoCrontab := ""
		sudoOut, sudoErr, sudoCode, _ := runSSHCommand(h, "sudo crontab -l", "", 20)
		if sudoCode == 0 {
			sudoCrontab = sudoOut
		} else if sudoCode != 0 && sudoErr != "" &&
			!strings.Contains(strings.ToLower(sudoErr), "no crontab") &&
			!strings.Contains(strings.ToLower(sudoErr), "not allowed") {
			// sudo denied or no sudo — just skip, don't error.
		}

		sysOut, _, _, _ := runSSHCommand(h, systemCronCmd, "", 20)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"host_id":        h.ID,
			"host_name":      h.Name,
			"user":           h.SSHUser,
			"exists":         exists,
			"user_crontab":   userCrontab,
			"sudo_crontab":   sudoCrontab,
			"system_crontab": strings.TrimSpace(sysOut),
		})
	}
}

// PutHostCrontab replaces the connecting user's crontab via `crontab -`.
func PutHostCrontab() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
	var body struct {
		Raw        string `json:"raw"`
		TargetUser string `json:"target_user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	h, err := loadDockerHost(id)
	if err != nil {
		http.Error(w, `{"error":"host not found"}`, http.StatusNotFound)
		return
	}
	raw := body.Raw
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	// When target_user is set (e.g. "root"), write via sudo crontab -u so a
	// non-root SSH user can manage another user's crontab.
	cmd := "crontab -"
	if body.TargetUser != "" && body.TargetUser != h.SSHUser {
		cmd = fmt.Sprintf("sudo crontab -u %s -", shellQuote(body.TargetUser))
	}
	// `crontab -` reads the new crontab from stdin and validates it; a
	// syntax error exits non-zero with a message on stderr.
	out, errOut, code, err := runSSHCommand(h, cmd, raw, 20)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		if code != 0 {
			msg := strings.TrimSpace(errOut + " " + out)
			if msg == "" {
				msg = fmt.Sprintf("crontab install failed (exit %d)", code)
			}
			http.Error(w, jsonError(msg), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}

// RunHostCommand runs a single command immediately over SSH (manual run of a
// crontab line). Gated by cron.exec.
func RunHostCommand() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := cronHostIDFromPath(r)
		if err != nil {
			http.Error(w, `{"error":"invalid host id"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Command    string `json:"command"`
			WorkingDir string `json:"working_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Command) == "" {
			http.Error(w, jsonError("command is required"), http.StatusBadRequest)
			return
		}
		h, err := loadDockerHost(id)
		if err != nil {
			http.Error(w, `{"error":"host not found"}`, http.StatusNotFound)
			return
		}
		cmd := body.Command
		if strings.TrimSpace(body.WorkingDir) != "" {
			cmd = fmt.Sprintf("cd %s && %s", shellQuote(body.WorkingDir), body.Command)
		}
		started := time.Now()
		out, errOut, code, err := runSSHCommand(h, cmd, "", 120)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"exit_code":   code,
			"stdout":      out,
			"stderr":      errOut,
			"duration_ms": time.Since(started).Milliseconds(),
		})
	}
}
