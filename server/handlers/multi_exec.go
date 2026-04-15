package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ScriptResult struct {
	Index      int             `json:"index"`
	SQL        string          `json:"sql"`
	Columns    []string        `json:"columns"`
	Rows       [][]interface{} `json:"rows"`
	RowCount   int             `json:"row_count"`
	Affected   int64           `json:"affected_rows"`
	DurationMs int64           `json:"duration_ms"`
	Error      string          `json:"error,omitempty"`
}

// RunScript handles POST /api/connections/{id}/script
// It splits the SQL by ; and executes each statement in order.
func RunScript() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		var req struct {
			SQL      string `json:"sql"`
			Database string `json:"database"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SQL) == "" {
			http.Error(w, `{"error":"sql required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		if req.Database != "" {
			switch driver {
			case "mysql":
				db.ExecContext(r.Context(), "USE `"+req.Database+"`")
			case "sqlserver":
				db.ExecContext(r.Context(), "USE ["+req.Database+"]")
			}
		}

		stmts := splitStatements(req.SQL)
		results := make([]ScriptResult, 0, len(stmts))

		for i, stmt := range stmts {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			sr := ScriptResult{Index: i, SQL: stmt, Columns: []string{}, Rows: [][]interface{}{}}
			start := time.Now()

			upper := strings.ToUpper(strings.TrimLeft(stmt, " \t\n\r"))
			isSelect := strings.HasPrefix(upper, "SELECT") ||
				strings.HasPrefix(upper, "WITH") ||
				strings.HasPrefix(upper, "SHOW") ||
				strings.HasPrefix(upper, "DESCRIBE") ||
				strings.HasPrefix(upper, "EXPLAIN") ||
				strings.HasPrefix(upper, "PRAGMA")

			if isSelect {
				rows, err := db.QueryContext(r.Context(), stmt)
				if err != nil {
					sr.Error = err.Error()
				} else {
					cols, _ := rows.Columns()
					sr.Columns = cols
					for rows.Next() {
						vals := make([]interface{}, len(cols))
						ptrs := make([]interface{}, len(cols))
						for j := range vals {
							ptrs[j] = &vals[j]
						}
						rows.Scan(ptrs...)
						row := make([]interface{}, len(cols))
						for j, v := range vals {
							if b, ok := v.([]byte); ok {
								row[j] = string(b)
							} else {
								row[j] = v
							}
						}
						sr.Rows = append(sr.Rows, row)
					}
					rows.Close()
					sr.RowCount = len(sr.Rows)
				}
			} else {
				res, err := db.ExecContext(r.Context(), stmt)
				if err != nil {
					sr.Error = err.Error()
				} else {
					sr.Affected, _ = res.RowsAffected()
				}
			}

			sr.DurationMs = time.Since(start).Milliseconds()
			results = append(results, sr)
		}

		json.NewEncoder(w).Encode(results)
	}
}

// splitStatements splits SQL by ; respecting string literals.
func splitStatements(sql string) []string {
	var stmts []string
	var cur strings.Builder
	inStr := false
	strChar := byte(0)
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if inStr {
			cur.WriteByte(ch)
			if ch == strChar && (i == 0 || sql[i-1] != '\\') {
				inStr = false
			}
		} else {
			if ch == '\'' || ch == '"' || ch == '`' {
				inStr = true
				strChar = ch
				cur.WriteByte(ch)
			} else if ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
				// Skip line comment
				for i < len(sql) && sql[i] != '\n' {
					i++
				}
			} else if ch == ';' {
				if s := strings.TrimSpace(cur.String()); s != "" {
					stmts = append(stmts, s)
				}
				cur.Reset()
			} else {
				cur.WriteByte(ch)
			}
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}
