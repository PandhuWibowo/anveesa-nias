package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type HealthResult struct {
	ConnID    int64   `json:"conn_id"`
	ConnName  string  `json:"conn_name"`
	Driver    string  `json:"driver"`
	Status    string  `json:"status"` // "ok" | "error" | "unknown"
	LatencyMs int64   `json:"latency_ms"`
	Error     string  `json:"error,omitempty"`
	PoolStats PoolStat `json:"pool"`
}

type PoolStat struct {
	OpenConns   int `json:"open_conns"`
	InUse       int `json:"in_use"`
	Idle        int `json:"idle"`
	MaxOpen     int `json:"max_open"`
}

func PingConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		result := pingConn(connID)
		json.NewEncoder(w).Encode(result)
	}
}

func PingAllConnections() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := appdb.DB.Query(`SELECT id, name, driver FROM connections ORDER BY id`)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type conn struct {
			ID     int64
			Name   string
			Driver string
		}
		var conns []conn
		for rows.Next() {
			var c conn
			rows.Scan(&c.ID, &c.Name, &c.Driver)
			conns = append(conns, c)
		}
		rows.Close()

		results := make([]HealthResult, 0, len(conns))
		ch := make(chan HealthResult, len(conns))
		for _, c := range conns {
			go func(c conn) {
				ch <- pingConn(c.ID)
			}(c)
		}
		for range conns {
			results = append(results, <-ch)
		}
		json.NewEncoder(w).Encode(results)
	}
}

func pingConn(connID int64) HealthResult {
	var name, driver string
	appdb.DB.QueryRow(`SELECT name, driver FROM connections WHERE id=?`, connID).Scan(&name, &driver)

	result := HealthResult{
		ConnID: connID, ConnName: name, Driver: driver, Status: "unknown",
	}

	db, drv, err := GetDB(connID)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	result.Driver = drv

	start := time.Now()
	pingSQL := "SELECT 1"
	if drv == "sqlserver" {
		pingSQL = "SELECT 1"
	}
	if err := db.Ping(); err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	row := db.QueryRow(pingSQL)
	var v int
	row.Scan(&v)
	result.LatencyMs = time.Since(start).Milliseconds()
	result.Status = "ok"

	stats := db.Stats()
	result.PoolStats = PoolStat{
		OpenConns: stats.OpenConnections,
		InUse:     stats.InUse,
		Idle:      stats.Idle,
		MaxOpen:   stats.MaxOpenConnections,
	}

	return result
}
