package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// validIdentifier validates database/table names to prevent SQL injection
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_\-]*$`)

type QueryRequest struct {
	SQL      string `json:"sql"`
	Database string `json:"database"`
}

type QueryResult struct {
	Columns      []string        `json:"columns"`
	Rows         [][]interface{} `json:"rows"`
	RowCount     int             `json:"row_count"`
	AffectedRows int64           `json:"affected_rows"`
	DurationMs   int64           `json:"duration_ms"`
}

// sanitizeDBError removes sensitive details from database errors
func sanitizeDBError(err error) string {
	msg := err.Error()
	// Remove connection strings, file paths, and internal details
	if strings.Contains(msg, "connection") || strings.Contains(msg, "dial") {
		return "database connection error"
	}
	if strings.Contains(msg, "syntax") {
		return "SQL syntax error"
	}
	if strings.Contains(msg, "denied") || strings.Contains(msg, "permission") {
		return "permission denied"
	}
	// Keep error message but limit length
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}

func ExecuteQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 2 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}

		var req QueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SQL) == "" {
			http.Error(w, `{"error":"sql is required"}`, http.StatusBadRequest)
			return
		}

		// Check if this is a write operation and if user has permission
		upper := strings.ToUpper(strings.TrimSpace(req.SQL))
		isWrite := !strings.HasPrefix(upper, "SELECT") &&
			!strings.HasPrefix(upper, "WITH") &&
			!strings.HasPrefix(upper, "SHOW") &&
			!strings.HasPrefix(upper, "DESCRIBE") &&
			!strings.HasPrefix(upper, "EXPLAIN") &&
			!strings.HasPrefix(upper, "PRAGMA")

		if isWrite && !CheckWritePermission(r, connID) {
			http.Error(w, `{"error":"write permission denied"}`, http.StatusForbidden)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, `{"error":"database connection error"}`, http.StatusBadGateway)
			return
		}

		// Switch database context if requested (with SQL injection protection)
		if req.Database != "" {
			if !validIdentifier.MatchString(req.Database) {
				http.Error(w, `{"error":"invalid database name"}`, http.StatusBadRequest)
				return
			}
			switch driver {
			case "mysql":
				// Use backticks for MySQL, escape any embedded backticks
				safeName := strings.ReplaceAll(req.Database, "`", "``")
				db.ExecContext(r.Context(), "USE `"+safeName+"`")
			case "sqlserver":
				// Use brackets for SQL Server, escape any embedded brackets
				safeName := strings.ReplaceAll(req.Database, "]", "]]")
				db.ExecContext(r.Context(), "USE ["+safeName+"]")
			}
		}

		start := time.Now()

		isSelect := strings.HasPrefix(upper, "SELECT") ||
			strings.HasPrefix(upper, "WITH") ||
			strings.HasPrefix(upper, "SHOW") ||
			strings.HasPrefix(upper, "DESCRIBE") ||
			strings.HasPrefix(upper, "EXPLAIN") ||
			strings.HasPrefix(upper, "PRAGMA")

		result := &QueryResult{
			Columns: []string{},
			Rows:    [][]interface{}{},
		}

		// Check for active transaction
		activeTx, _, hasTx := GetActiveTx(connID)

		if isSelect {
			var rows interface {
				Columns() ([]string, error)
				Next() bool
				Scan(dest ...interface{}) error
				Close() error
			}

			if hasTx {
				rows, err = activeTx.QueryContext(r.Context(), req.SQL)
			} else {
				rows, err = db.QueryContext(r.Context(), req.SQL)
			}
			if err != nil {
				http.Error(w, jsonError(sanitizeDBError(err)), http.StatusBadRequest)
				return
			}
			defer rows.Close()

			cols, _ := rows.Columns()
			result.Columns = cols

			for rows.Next() {
				vals := make([]interface{}, len(cols))
				ptrs := make([]interface{}, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					continue
				}
				row := make([]interface{}, len(cols))
				for i, v := range vals {
					switch t := v.(type) {
					case []byte:
						row[i] = string(t)
					default:
						row[i] = t
					}
				}
				result.Rows = append(result.Rows, row)
			}
			result.RowCount = len(result.Rows)
		} else {
			var affected int64
			if hasTx {
				res, err := activeTx.ExecContext(r.Context(), req.SQL)
				if err != nil {
					http.Error(w, jsonError(sanitizeDBError(err)), http.StatusBadRequest)
					return
				}
				affected, _ = res.RowsAffected()
			} else {
				res, err := db.ExecContext(r.Context(), req.SQL)
				if err != nil {
					http.Error(w, jsonError(sanitizeDBError(err)), http.StatusBadRequest)
					return
				}
				affected, _ = res.RowsAffected()
			}
			result.AffectedRows = affected
		}

		result.DurationMs = time.Since(start).Milliseconds()

		// Write to audit log (non-blocking)
		go func() {
			connName := ""
			if db != nil {
				// best-effort: look up conn name
			}
			username := r.Header.Get("X-Username")
			if username == "" {
				username = "user"
			}
			WriteAuditLog(username, connID, connName, req.SQL, result.DurationMs, int64(result.RowCount+int(result.AffectedRows)), "")
		}()

		json.NewEncoder(w).Encode(result)
	}
}

func jsonError(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}
