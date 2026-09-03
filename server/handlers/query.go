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

// dbErrorStatus returns the appropriate HTTP status for a sanitized DB error message.
// Connection-level failures are infrastructure errors (502); permission/syntax errors
// are client errors (403/400).
func dbErrorStatus(sanitized string) int {
	switch sanitized {
	case "database connection error":
		return http.StatusBadGateway
	case "permission denied":
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
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

		// Check if this is a write operation and if user has permission. Uses
		// firstSQLKeyword rather than a raw prefix check on the trimmed text —
		// SQL handed to this endpoint can start with a "-- comment" line (e.g.
		// a leading commented-out statement left in an editor), and a naive
		// HasPrefix(upper, "SELECT") would then misclassify a real SELECT as a
		// write, routing it through ExecContext and silently discarding its
		// result rows instead of returning them.
		isReadKeyword := isReadSQLKeyword(firstSQLKeyword(req.SQL))
		isWrite := !isReadKeyword

		// Enforce the *specific* DB permission the statement requires (insert vs
		// update vs delete vs create/alter/drop) rather than a coarse "any write"
		// bucket — a user granted only SELECT must not be able to run writes, and
		// a user granted only INSERT must not be able to DELETE or DROP.
		if isWrite {
			required := RequiredPermForSQL(req.SQL)
			if !CheckOperationPermission(r, connID, required) {
				msg := "write permission denied"
				if required != "" {
					msg = "permission denied for operation: " + string(required)
				}
				http.Error(w, jsonError(msg), http.StatusForbidden)
				return
			}
		}

		if isWrite && isAuthEnabled() {
			userID, _, role := currentUserFromHeaders(r)
			if userID > 0 && role != "admin" {
				workflows, err := findApplicableWorkflows(userID, role, connID)
				if err != nil {
					http.Error(w, jsonError("failed to resolve approval workflows"), http.StatusInternalServerError)
					return
				}
				if len(workflows) > 0 {
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error":             "approval required before executing write SQL",
						"approval_required": true,
						"workflows":         workflows,
					})
					return
				}
			}
		}

		execUserID, _, _ := currentUserFromHeaders(r)
		db, driver, err := GetDBForUser(execUserID, connID)
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
			var useErr error
			switch driver {
			case "mysql":
				// Use backticks for MySQL, escape any embedded backticks
				safeName := strings.ReplaceAll(req.Database, "`", "``")
				_, useErr = db.ExecContext(r.Context(), "USE `"+safeName+"`")
			case "sqlserver":
				// Use brackets for SQL Server, escape any embedded brackets
				safeName := strings.ReplaceAll(req.Database, "]", "]]")
				_, useErr = db.ExecContext(r.Context(), "USE ["+safeName+"]")
			}
			// A failed USE must not fall through to running the query against
			// whatever database the pooled connection happened to be on
			// before this request — that silently queries the wrong database
			// instead of the one the caller explicitly asked for.
			if useErr != nil {
				http.Error(w, jsonError("failed to switch database: "+useErr.Error()), http.StatusBadGateway)
				return
			}
		}

		start := time.Now()

		isSelect := isReadKeyword

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
				msg := sanitizeDBError(err)
				http.Error(w, jsonError(msg), dbErrorStatus(msg))
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
					msg := sanitizeDBError(err)
					http.Error(w, jsonError(msg), dbErrorStatus(msg))
					return
				}
				affected, _ = res.RowsAffected()
			} else {
				res, err := db.ExecContext(r.Context(), req.SQL)
				if err != nil {
					msg := sanitizeDBError(err)
					http.Error(w, jsonError(msg), dbErrorStatus(msg))
					return
				}
				affected, _ = res.RowsAffected()
			}
			result.AffectedRows = affected
			if isSchemaChangingSQL(req.SQL) {
				invalidateSchemaListCache(connID)
			}
		}

		result.DurationMs = time.Since(start).Milliseconds()

		// Write to audit log (non-blocking)
		go func() {
			username := r.Header.Get("X-Username")
			if username == "" {
				username = "user"
			}
			WriteAuditLog(username, connID, connectionNameByID(connID), req.SQL, result.DurationMs, int64(result.RowCount+int(result.AffectedRows)), "")
		}()

		json.NewEncoder(w).Encode(result)
	}
}

// isReadSQLKeyword reports whether keyword (as returned by firstSQLKeyword)
// is one of the read-only statement types — shared by ExecuteQuery and
// RunScript so both classify a statement identically instead of each
// keeping its own drifting copy of this list.
func isReadSQLKeyword(keyword string) bool {
	switch keyword {
	case "SELECT", "WITH", "SHOW", "DESCRIBE", "EXPLAIN", "PRAGMA":
		return true
	default:
		return false
	}
}

func isSchemaChangingSQL(sqlText string) bool {
	switch firstSQLKeyword(sqlText) {
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME", "COMMENT":
		return true
	default:
		return false
	}
}

func firstSQLKeyword(sqlText string) string {
	sqlText = strings.TrimSpace(sqlText)
	for {
		switch {
		case strings.HasPrefix(sqlText, ";"):
			sqlText = strings.TrimSpace(sqlText[1:])
		case strings.HasPrefix(sqlText, "/*"):
			end := strings.Index(sqlText, "*/")
			if end < 0 {
				return ""
			}
			sqlText = strings.TrimSpace(sqlText[end+2:])
		case strings.HasPrefix(sqlText, "--"):
			end := strings.IndexAny(sqlText, "\r\n")
			if end < 0 {
				return ""
			}
			sqlText = strings.TrimSpace(sqlText[end+1:])
		default:
			fields := strings.Fields(sqlText)
			if len(fields) == 0 {
				return ""
			}
			return strings.ToUpper(fields[0])
		}
	}
}

func jsonError(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}
