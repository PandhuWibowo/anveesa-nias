package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	appdb "github.com/anveesa/nias/db"
)

// The fleet view probes every SSH host in one screen: nginx version, running
// state, `nginx -t` result, and soonest TLS-cert expiry. Each host is probed
// with a single SSH command (using its saved sudo/binary settings) and the
// hosts are queried concurrently with a per-host timeout.

type fleetHost struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SSHHost     string `json:"ssh_host"`
	Reachable   bool   `json:"reachable"`
	Version     string `json:"version"`
	Active      string `json:"active"`
	TestOK      bool   `json:"test_ok"`
	TestOutput  string `json:"test_output"`
	Certs       int    `json:"certs"`
	SoonestDays int    `json:"soonest_days"` // -1 when no certs
	Expiring    int    `json:"expiring"`     // certs < 30 days (incl. expired)
	Error       string `json:"error,omitempty"`
}

// loadNginxSettings returns a host's saved fleet-relevant settings (defaults
// when none saved).
func loadNginxSettings(id int64) (sudo bool, bin string) {
	bin = "nginx"
	var s int
	var b, cr, ld string
	err := appdb.DB.QueryRow(appdb.ConvertQuery(
		`SELECT use_sudo, bin, config_root, log_dir FROM nginx_host_settings WHERE host_id = ?`), id).
		Scan(&s, &b, &cr, &ld)
	if err == nil {
		sudo = s != 0
		if b == "openresty" {
			bin = "openresty"
		}
	}
	return
}

func nginxFleetScript(bin string) string {
	return strings.Join([]string{
		`echo "@VER"; ` + bin + ` -v 2>&1`,
		`echo "@ACTIVE"; systemctl is-active ` + bin + ` 2>&1`,
		`echo "@TEST"; ` + bin + ` -t 2>&1; echo "@RC:$?"`,
		`echo "@CERTS"; for f in $(` + bin + ` -T 2>/dev/null | grep -oP 'ssl_certificate\s+\K[^;]+' | tr -d '"' | sort -u); do echo "@C:$f"; openssl x509 -in "$f" -noout -enddate 2>&1; done`,
	}, "\n")
}

// runNginxProbe runs the per-host script with a hard timeout.
func runNginxProbe(h *DockerHost, sudo bool, script string, d time.Duration) (string, error) {
	type res struct {
		out string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		out, err := runHostPrivileged(h, sudo, []string{"sh", "-c", script}, "")
		ch <- res{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(d):
		return "", fmt.Errorf("timed out")
	}
}

func probeFleetHost(id int64, name, sshHost string) fleetHost {
	fh := fleetHost{ID: id, Name: name, SSHHost: sshHost, SoonestDays: math.MaxInt32}
	h, err := loadDockerHost(id)
	if err != nil {
		fh.Error = "load host: " + err.Error()
		fh.SoonestDays = -1
		return fh
	}
	sudo, bin := loadNginxSettings(id)
	out, err := runNginxProbe(h, sudo, nginxFleetScript(bin), 20*time.Second)
	if out == "" && err != nil {
		fh.Error = err.Error() // SSH unreachable / timed out
		fh.SoonestDays = -1
		return fh
	}
	fh.Reachable = true

	section := ""
	var testLines []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "@VER":
			section = "ver"
		case line == "@ACTIVE":
			section = "active"
		case line == "@TEST":
			section = "test"
		case strings.HasPrefix(line, "@RC:"):
			fh.TestOK = strings.TrimSpace(strings.TrimPrefix(line, "@RC:")) == "0"
			section = ""
		case line == "@CERTS":
			section = "certs"
		case strings.HasPrefix(line, "@C:"):
			fh.Certs++
		default:
			switch section {
			case "ver":
				if fh.Version == "" && strings.TrimSpace(line) != "" {
					fh.Version = strings.TrimSpace(line)
				}
			case "active":
				if fh.Active == "" && strings.TrimSpace(line) != "" {
					fh.Active = strings.TrimSpace(line)
				}
			case "test":
				if strings.TrimSpace(line) != "" {
					testLines = append(testLines, line)
				}
			case "certs":
				if strings.HasPrefix(line, "notAfter=") {
					if t, e := time.Parse("Jan _2 15:04:05 2006 MST", strings.TrimPrefix(line, "notAfter=")); e == nil {
						days := int(time.Until(t).Hours() / 24)
						if days < fh.SoonestDays {
							fh.SoonestDays = days
						}
						if days < 30 {
							fh.Expiring++
						}
					}
				}
			}
		}
	}
	fh.TestOutput = strings.Join(testLines, "\n")
	if fh.SoonestDays == math.MaxInt32 {
		fh.SoonestDays = -1 // no certs
	}
	return fh
}

// NginxFleet probes every SSH host and returns a fleet-health summary.
func NginxFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := appdb.DB.Query(appdb.ConvertQuery(
			`SELECT id, name, COALESCE(ssh_host,'') FROM docker_hosts WHERE ssh_host <> '' ORDER BY name`))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		type ref struct {
			id      int64
			name    string
			sshHost string
		}
		var list []ref
		for rows.Next() {
			var rr ref
			if rows.Scan(&rr.id, &rr.name, &rr.sshHost) == nil {
				list = append(list, rr)
			}
		}
		rows.Close()

		results := make([]fleetHost, len(list))
		sem := make(chan struct{}, 6) // cap concurrent SSH sessions
		var wg sync.WaitGroup
		for i, rr := range list {
			wg.Add(1)
			go func(i int, rr ref) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				results[i] = probeFleetHost(rr.id, rr.name, rr.sshHost)
			}(i, rr)
		}
		wg.Wait()

		// Unhealthy first: unreachable, then failing test, then soonest cert expiry.
		sort.SliceStable(results, func(i, j int) bool {
			a, b := results[i], results[j]
			if a.Reachable != b.Reachable {
				return !a.Reachable
			}
			if a.TestOK != b.TestOK {
				return !a.TestOK
			}
			da, db := a.SoonestDays, b.SoonestDays
			if da < 0 {
				da = math.MaxInt32
			}
			if db < 0 {
				db = math.MaxInt32
			}
			return da < db
		})
		json.NewEncoder(w).Encode(map[string]interface{}{"hosts": results})
	}
}
