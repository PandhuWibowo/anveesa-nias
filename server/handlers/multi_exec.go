package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
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

// sqlExecutor is satisfied by both *sql.DB and *sql.Tx — lets the
// per-statement loop below run against whichever one applies (an open
// transaction for this connection, or the pooled DB) without duplicating
// the loop body for each case.
type sqlExecutor interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
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

		stmts := splitStatements(req.SQL)
		hasWrite := false
		for _, stmt := range stmts {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			// firstSQLKeyword skips leading "-- comment"/"/* */" text before
			// looking at the actual keyword — a naive upper-case prefix check
			// here misclassifies a commented statement like "-- note\nSELECT
			// ..." as a write, which both over-triggers the write-permission
			// gate below and (in the per-statement loop further down) makes
			// a real SELECT get routed through ExecContext, silently
			// discarding its result rows.
			if isReadSQLKeyword(firstSQLKeyword(stmt)) {
				continue
			}
			hasWrite = true
			// Enforce each write statement's specific permission — a batch that
			// contains a DELETE requires the delete grant even if the user can
			// insert/update.
			required := RequiredPermForSQL(stmt)
			if !CheckOperationPermission(r, connID, required) {
				msg := "write permission denied"
				if required != "" {
					msg = "permission denied for operation: " + string(required)
				}
				http.Error(w, jsonError(msg), http.StatusForbidden)
				return
			}
		}
		if hasWrite && isAuthEnabled() {
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
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		if req.Database != "" {
			if !validIdentifier.MatchString(req.Database) {
				http.Error(w, jsonError("invalid database name"), http.StatusBadRequest)
				return
			}
			var useErr error
			switch driver {
			case "mysql":
				safeName := strings.ReplaceAll(req.Database, "`", "``")
				_, useErr = db.ExecContext(r.Context(), "USE `"+safeName+"`")
			case "sqlserver":
				safeName := strings.ReplaceAll(req.Database, "]", "]]")
				_, useErr = db.ExecContext(r.Context(), "USE ["+safeName+"]")
			}
			if useErr != nil {
				http.Error(w, jsonError("failed to switch database: "+useErr.Error()), http.StatusBadGateway)
				return
			}
		}

		// Run on the connection's active transaction if one is open (the
		// user clicked BEGIN before running the script) — matching
		// ExecuteQuery's behavior. Without this, every statement in a
		// script silently ran on the pooled *sql.DB regardless of the
		// transaction the UI was showing as active, so a script run inside
		// a BEGIN…COMMIT never actually participated in it.
		var exec sqlExecutor = db
		if activeTx, _, hasTx := GetActiveTx(connID); hasTx {
			exec = activeTx
		}

		results := make([]ScriptResult, 0, len(stmts))
		schemaChanged := false

		for i, stmt := range stmts {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			sr := ScriptResult{Index: i, SQL: stmt, Columns: []string{}, Rows: [][]interface{}{}}
			start := time.Now()

			isSelect := isReadSQLKeyword(firstSQLKeyword(stmt))

			if isSelect {
				rows, err := exec.QueryContext(r.Context(), stmt)
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
				res, err := exec.ExecContext(r.Context(), stmt)
				if err != nil {
					sr.Error = err.Error()
				} else {
					sr.Affected, _ = res.RowsAffected()
					if isSchemaChangingSQL(stmt) {
						schemaChanged = true
					}
				}
			}

			sr.DurationMs = time.Since(start).Milliseconds()
			results = append(results, sr)
		}

		if schemaChanged {
			invalidateSchemaListCache(connID)
		}

		json.NewEncoder(w).Encode(results)
	}
}

// dollarQuoteTagRe matches a Postgres dollar-quote delimiter at the start of
// a substring: the bare form ($$) or a tagged form ($tag$).
var dollarQuoteTagRe = regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*\$|^\$\$`)

// splitStatements splits a block of SQL text into individual statements on
// unquoted, uncommented semicolons. It understands:
//   - single/double/backtick-quoted strings, including both escape styles
//     SQL engines actually use — doubled quotes ('', "", ``, the
//     SQL-standard form Postgres/SQLite/ANSI-mode MySQL use) and
//     backslash-escaped quotes (\', MySQL's default non-ANSI mode)
//   - "-- line comments" and "/* block comments */"
//   - Postgres dollar-quoted blocks ($$ ... $$ or $tag$ ... $tag$), so a
//     semicolon inside a DO block or function body doesn't split it —
//     without this, a `CREATE FUNCTION ... AS $$ ... ; ... $$` statement
//     gets fragmented into invalid pieces at every internal ';'.
func splitStatements(sqlText string) []string {
	var stmts []string
	var cur strings.Builder
	n := len(sqlText)

	var quoteChar byte
	inLineComment := false
	inBlockComment := false
	inDollar := false
	var dollarDelim string

	flush := func() {
		// firstSQLKeyword returning "" means the segment is nothing but
		// comments (or truly empty) — e.g. a trailing "-- note" left after
		// the last real statement's ';'. Dropping those here, not just
		// blank segments, matters because RunScript executes every entry
		// this returns: a comment-only "statement" was previously getting
		// sent to ExecContext as its own phantom result entry.
		if s := strings.TrimSpace(cur.String()); s != "" && firstSQLKeyword(s) != "" {
			stmts = append(stmts, s)
		}
		cur.Reset()
	}

	i := 0
	for i < n {
		ch := sqlText[i]

		if inLineComment {
			cur.WriteByte(ch)
			i++
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && i+1 < n && sqlText[i+1] == '/' {
				cur.WriteString("*/")
				i += 2
				inBlockComment = false
			} else {
				cur.WriteByte(ch)
				i++
			}
			continue
		}
		if inDollar {
			if strings.HasPrefix(sqlText[i:], dollarDelim) {
				cur.WriteString(dollarDelim)
				i += len(dollarDelim)
				inDollar = false
				dollarDelim = ""
			} else {
				cur.WriteByte(ch)
				i++
			}
			continue
		}
		if quoteChar != 0 {
			if ch == '\\' && i+1 < n {
				// Backslash-escape: the escaped character is never treated
				// as a closing quote.
				cur.WriteByte(ch)
				cur.WriteByte(sqlText[i+1])
				i += 2
				continue
			}
			cur.WriteByte(ch)
			i++
			if ch == quoteChar {
				if i < n && sqlText[i] == quoteChar {
					// Doubled-quote escape: '' stays inside the string
					// rather than closing it.
					cur.WriteByte(quoteChar)
					i++
				} else {
					quoteChar = 0
				}
			}
			continue
		}

		switch {
		case ch == '\'' || ch == '"' || ch == '`':
			quoteChar = ch
			cur.WriteByte(ch)
			i++
		case ch == '-' && i+1 < n && sqlText[i+1] == '-':
			inLineComment = true
			cur.WriteByte(ch)
			i++
		case ch == '/' && i+1 < n && sqlText[i+1] == '*':
			inBlockComment = true
			cur.WriteByte(ch)
			i++
		case ch == '$':
			if m := dollarQuoteTagRe.FindString(sqlText[i:]); m != "" {
				inDollar = true
				dollarDelim = m
				cur.WriteString(m)
				i += len(m)
			} else {
				cur.WriteByte(ch)
				i++
			}
		case ch == ';':
			flush()
			i++
		default:
			cur.WriteByte(ch)
			i++
		}
	}
	flush()
	return stmts
}
