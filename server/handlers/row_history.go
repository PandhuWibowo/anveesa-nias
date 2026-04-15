package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type RowChange struct {
	ID         int64  `json:"id"`
	ConnID     int64  `json:"conn_id"`
	Database   string `json:"database"`
	TableName  string `json:"table_name"`
	Operation  string `json:"operation"` // INSERT | UPDATE | DELETE
	PKColumn   string `json:"pk_column"`
	PKValue    string `json:"pk_value"`
	BeforeData string `json:"before_data"` // JSON
	AfterData  string `json:"after_data"`  // JSON
	Username   string `json:"username"`
	ChangedAt  string `json:"changed_at"`
}

// RecordRowChange saves a before/after snapshot of a row change.
func RecordRowChange(connID int64, database, table, operation, pkCol, pkVal, before, after, username string) {
	appdb.DB.Exec(
		`INSERT INTO row_changes (conn_id, database, table_name, operation, pk_column, pk_value, before_data, after_data, username, changed_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		connID, database, table, operation, pkCol, pkVal, before, after, username,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	// Keep last 50k rows
	appdb.DB.Exec(`DELETE FROM row_changes WHERE id NOT IN (SELECT id FROM row_changes ORDER BY id DESC LIMIT 50000)`)
}

// ListRowHistory handles GET /api/connections/{id}/history/{db}/{table}
func ListRowHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Path: /api/connections/{id}/row-history/{db}/{table}
		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.SplitN(path, "/", 4)
		if len(parts) < 4 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID := parts[0]
		database := parts[2]
		table := parts[3]

		pkFilter := r.URL.Query().Get("pk")
		limit := 100

		query := `SELECT id, conn_id, COALESCE(database,''), table_name, operation, COALESCE(pk_column,''), COALESCE(pk_value,''), COALESCE(before_data,''), COALESCE(after_data,''), COALESCE(username,''), changed_at
		          FROM row_changes WHERE conn_id=? AND table_name=?`
		args := []interface{}{connID, table}

		if database != "" && database != "_" {
			query += " AND database=?"
			args = append(args, database)
		}
		if pkFilter != "" {
			query += " AND pk_value=?"
			args = append(args, pkFilter)
		}
		query += fmt.Sprintf(" ORDER BY id DESC LIMIT %d", limit)

		rows, err := appdb.DB.Query(query, args...)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var changes []RowChange
		for rows.Next() {
			var c RowChange
			rows.Scan(&c.ID, &c.ConnID, &c.Database, &c.TableName, &c.Operation,
				&c.PKColumn, &c.PKValue, &c.BeforeData, &c.AfterData, &c.Username, &c.ChangedAt)
			changes = append(changes, c)
		}
		if changes == nil {
			changes = []RowChange{}
		}
		json.NewEncoder(w).Encode(changes)
	}
}

// UndoRowChange re-applies the inverse of a recorded change.
func UndoRowChange() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/connections/")
		parts := strings.SplitN(path, "/", 5)
		if len(parts) < 5 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID := parts[0]

		var req struct {
			ChangeID int64 `json:"change_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"change_id required"}`, http.StatusBadRequest)
			return
		}

		var c RowChange
		err := appdb.DB.QueryRow(
			`SELECT conn_id, database, table_name, operation, pk_column, pk_value, before_data, after_data FROM row_changes WHERE id=?`,
			req.ChangeID,
		).Scan(&c.ConnID, &c.Database, &c.TableName, &c.Operation, &c.PKColumn, &c.PKValue, &c.BeforeData, &c.AfterData)
		if err != nil {
			http.Error(w, `{"error":"change not found"}`, http.StatusNotFound)
			return
		}
		if fmt.Sprintf("%d", c.ConnID) != connID {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		db, driver, err := GetDB(c.ConnID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		tbl := quoteIdent(driver, c.TableName)
		pkCol := quoteIdent(driver, c.PKColumn)
		var placeholder string
		if driver == "sqlserver" {
			placeholder = "@p1"
		} else {
			placeholder = "?"
		}

		switch c.Operation {
		case "INSERT":
			// Undo insert = delete the row
			_, err = db.ExecContext(r.Context(),
				fmt.Sprintf("DELETE FROM %s WHERE %s = %s", tbl, pkCol, placeholder),
				c.PKValue,
			)
		case "DELETE":
			// Undo delete = re-insert using before_data
			var before map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(c.BeforeData), &before); jsonErr != nil {
				http.Error(w, `{"error":"cannot parse before snapshot"}`, http.StatusInternalServerError)
				return
			}
			cols := make([]string, 0, len(before))
			vals := make([]interface{}, 0, len(before))
			placeholders := make([]string, 0, len(before))
			pi := 1
			for k, v := range before {
				cols = append(cols, quoteIdent(driver, k))
				vals = append(vals, v)
				if driver == "sqlserver" {
					placeholders = append(placeholders, fmt.Sprintf("@p%d", pi))
					pi++
				} else {
					placeholders = append(placeholders, "?")
				}
			}
			_, err = db.ExecContext(r.Context(),
				fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tbl, strings.Join(cols, ","), strings.Join(placeholders, ",")),
				vals...,
			)
		case "UPDATE":
			// Undo update = restore before_data values
			var before map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(c.BeforeData), &before); jsonErr != nil {
				http.Error(w, `{"error":"cannot parse before snapshot"}`, http.StatusInternalServerError)
				return
			}
			sets := make([]string, 0, len(before))
			vals := make([]interface{}, 0, len(before)+1)
			pi := 1
			for k, v := range before {
				if k == c.PKColumn {
					continue
				}
				if driver == "sqlserver" {
					sets = append(sets, fmt.Sprintf("%s=@p%d", quoteIdent(driver, k), pi))
					pi++
				} else {
					sets = append(sets, quoteIdent(driver, k)+"=?")
				}
				vals = append(vals, v)
			}
			vals = append(vals, c.PKValue)
			if driver == "sqlserver" {
				_, err = db.ExecContext(r.Context(),
					fmt.Sprintf("UPDATE %s SET %s WHERE %s=@p%d", tbl, strings.Join(sets, ","), pkCol, pi),
					vals...,
				)
			} else {
				_, err = db.ExecContext(r.Context(),
					fmt.Sprintf("UPDATE %s SET %s WHERE %s=?", tbl, strings.Join(sets, ","), pkCol),
					vals...,
				)
			}
		}

		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "operation": c.Operation})
	}
}
