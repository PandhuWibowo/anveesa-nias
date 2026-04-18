package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// allowedRestoreStatements defines SQL statement prefixes allowed during restore
var allowedRestoreStatements = []string{
	"INSERT ",
	"CREATE TABLE",
	"CREATE INDEX",
	"CREATE UNIQUE INDEX",
	"DROP TABLE",
	"DROP INDEX",
	"ALTER TABLE",
	"SET ",
	"BEGIN",
	"COMMIT",
	"ROLLBACK",
}

// isAllowedRestoreStatement checks if a statement is safe to execute during restore
func isAllowedRestoreStatement(stmt string) bool {
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	for _, prefix := range allowedRestoreStatements {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

// GetBackup streams a SQL dump as a downloadable file.
// GET /api/connections/{id}/backup?database=name
func GetBackup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		// Check read permission
		if !CheckReadPermission(r, connID) {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}

		dbName := r.URL.Query().Get("database")
		// Validate database name
		if dbName != "" && !validIdentifier.MatchString(dbName) {
			http.Error(w, "invalid database name", http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, "connection error", http.StatusBadGateway)
			return
		}

		filename := fmt.Sprintf("backup_%s_%s.sql", dbName, time.Now().Format("20060102_150405"))
		w.Header().Set("Content-Type", "application/sql")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

		fmt.Fprintf(w, "-- Anveesa Nias Database Dump\n")
		fmt.Fprintf(w, "-- Driver: %s | Database: %s\n", driver, dbName)
		fmt.Fprintf(w, "-- Generated: %s\n\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(w, "SET FOREIGN_KEY_CHECKS=0;\n\n")

		// Get tables
		var tableQ string
		switch driver {
		case "postgres":
			schema := "public"
			tableQ = fmt.Sprintf(`SELECT table_name FROM information_schema.tables WHERE table_schema='%s' AND table_type='BASE TABLE' ORDER BY table_name`, schema)
		case "mysql":
			tableQ = `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME`
		case "sqlite":
			tableQ = `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
		default:
			tableQ = `SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME`
		}

		rows, err := db.QueryContext(r.Context(), tableQ)
		if err != nil {
			fmt.Fprintf(w, "-- ERROR fetching tables\n")
			return
		}
		var tables []string
		for rows.Next() {
			var t string
			rows.Scan(&t)
			tables = append(tables, t)
		}
		rows.Close()

		for _, table := range tables {
			if err := r.Context().Err(); err != nil {
				return
			}

			tbl := quoteIdent(driver, table)

			// DDL
			if driver == "sqlite" {
				var ddl string
				db.QueryRowContext(r.Context(), `SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl)
				if ddl != "" {
					fmt.Fprintf(w, "DROP TABLE IF EXISTS %s;\n", tbl)
					fmt.Fprintf(w, "%s;\n\n", ddl)
				}
			} else {
				fmt.Fprintf(w, "-- Table: %s\n", table)
			}

			// Data
			dataRows, err := db.QueryContext(r.Context(), fmt.Sprintf(`SELECT * FROM %s`, tbl))
			if err != nil {
				fmt.Fprintf(w, "-- ERROR dumping %s\n\n", table)
				continue
			}
			cols, _ := dataRows.Columns()
			colList := make([]string, len(cols))
			for i, c := range cols {
				colList[i] = quoteIdent(driver, c)
			}

			rowCount := 0
			for dataRows.Next() {
				vals := make([]interface{}, len(cols))
				ptrs := make([]interface{}, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := dataRows.Scan(ptrs...); err != nil {
					continue
				}
				sqlVals := make([]string, len(vals))
				for i, v := range vals {
					sqlVals[i] = sqlLiteral(v)
				}
				fmt.Fprintf(w, "INSERT INTO %s (%s) VALUES (%s);\n",
					tbl,
					strings.Join(colList, ", "),
					strings.Join(sqlVals, ", "),
				)
				rowCount++
			}
			dataRows.Close()
			fmt.Fprintf(w, "-- %d rows dumped from %s\n\n", rowCount, table)
		}

		fmt.Fprintf(w, "SET FOREIGN_KEY_CHECKS=1;\n")
		fmt.Fprintf(w, "-- End of dump\n")
	}
}

// RestoreBackup executes uploaded SQL statements.
// POST /api/connections/{id}/restore
func RestoreBackup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		// Check write permission - restore requires admin or readwrite access
		if !CheckWritePermission(r, connID) {
			http.Error(w, `{"error":"permission denied"}`, http.StatusForbidden)
			return
		}

		// Check for admin role for restore operations (high-risk)
		role := r.Header.Get("X-User-Role")
		if role != "admin" && isAuthEnabled() {
			http.Error(w, `{"error":"admin access required for restore"}`, http.StatusForbidden)
			return
		}

		var req struct {
			SQL string `json:"sql"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SQL) == "" {
			http.Error(w, `{"error":"sql required"}`, http.StatusBadRequest)
			return
		}

		// Limit SQL size to prevent DoS
		if len(req.SQL) > 50*1024*1024 { // 50MB limit
			http.Error(w, `{"error":"SQL too large (max 50MB)"}`, http.StatusBadRequest)
			return
		}

		db, _, err := GetDB(connID)
		if err != nil {
			http.Error(w, `{"error":"connection error"}`, http.StatusBadGateway)
			return
		}

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, `{"error":"transaction error"}`, http.StatusInternalServerError)
			return
		}

		stmts := splitSQL(req.SQL)
		executed := 0
		skipped := 0
		for _, stmt := range stmts {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" || strings.HasPrefix(stmt, "--") {
				continue
			}

			// Validate statement type
			if !isAllowedRestoreStatement(stmt) {
				skipped++
				continue
			}

			if _, err := tx.ExecContext(r.Context(), stmt); err != nil {
				tx.Rollback()
				http.Error(w, `{"error":"execution error at statement `+strconv.Itoa(executed+1)+`"}`, http.StatusBadRequest)
				return
			}
			executed++
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, `{"error":"commit error"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":       true,
			"executed": executed,
			"skipped":  skipped,
		})
	}
}

func sqlLiteral(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case []byte:
		return "'" + strings.ReplaceAll(string(t), "'", "''") + "'"
	case string:
		return "'" + strings.ReplaceAll(t, "'", "''") + "'"
	case bool:
		if t {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func splitSQL(sql string) []string {
	var stmts []string
	var cur strings.Builder
	inStr := false
	for i, ch := range sql {
		if ch == '\'' && (i == 0 || sql[i-1] != '\\') {
			inStr = !inStr
		}
		if ch == ';' && !inStr {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			cur.Reset()
		} else {
			cur.WriteRune(ch)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}
