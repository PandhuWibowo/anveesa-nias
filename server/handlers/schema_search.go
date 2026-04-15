package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

type SearchResult struct {
	ConnID   int64  `json:"conn_id"`
	ConnName string `json:"conn_name"`
	Driver   string `json:"driver"`
	Type     string `json:"type"` // "table" | "column" | "view"
	Table    string `json:"table"`
	Column   string `json:"column,omitempty"`
	DataType string `json:"data_type,omitempty"`
	Database string `json:"database,omitempty"`
}

// SearchSchema handles GET /api/schema/search?q=term&limit=50
func SearchSchema() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			json.NewEncoder(w).Encode([]SearchResult{})
			return
		}

		rows, err := appdb.DB.Query(`SELECT id, name, driver FROM connections ORDER BY id LIMIT 20`)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		type connRow struct{ ID int64; Name, Driver string }
		var conns []connRow
		for rows.Next() {
			var c connRow
			rows.Scan(&c.ID, &c.Name, &c.Driver)
			conns = append(conns, c)
		}
		rows.Close()

		var results []SearchResult
		for _, conn := range conns {
			db, driver, err := GetDB(conn.ID)
			if err != nil {
				continue
			}

			var tableQ string
			switch driver {
			case "postgres":
				tableQ = fmt.Sprintf(`
					SELECT table_name, 'table' as t, '' as dt FROM information_schema.tables
					WHERE table_schema='public' AND table_type='BASE TABLE' AND table_name ILIKE '%%%s%%'
					UNION ALL
					SELECT column_name, 'column', data_type FROM information_schema.columns
					WHERE table_schema='public' AND (column_name ILIKE '%%%s%%' OR table_name ILIKE '%%%s%%')
					LIMIT 30`, q, q, q)
			case "mysql":
				tableQ = fmt.Sprintf(`
					SELECT TABLE_NAME, 'table', '' FROM information_schema.TABLES
					WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME LIKE '%%%s%%'
					UNION ALL
					SELECT COLUMN_NAME, 'column', DATA_TYPE FROM information_schema.COLUMNS
					WHERE TABLE_SCHEMA=DATABASE() AND (COLUMN_NAME LIKE '%%%s%%' OR TABLE_NAME LIKE '%%%s%%')
					LIMIT 30`, q, q, q)
			case "sqlite":
				tableQ = fmt.Sprintf(`SELECT name, 'table', '' FROM sqlite_master WHERE type='table' AND name LIKE '%%%s%%' LIMIT 30`, q)
			default:
				continue
			}

			srows, err := db.QueryContext(r.Context(), tableQ)
			if err != nil {
				continue
			}
			for srows.Next() {
				var name, kind, dt string
				srows.Scan(&name, &kind, &dt)
				sr := SearchResult{
					ConnID: conn.ID, ConnName: conn.Name, Driver: driver, Type: kind,
				}
				if kind == "column" {
					sr.Column = name
					sr.DataType = dt
				} else {
					sr.Table = name
				}
				results = append(results, sr)
			}
			srows.Close()
		}

		if results == nil {
			results = []SearchResult{}
		}
		json.NewEncoder(w).Encode(results)
	}
}
