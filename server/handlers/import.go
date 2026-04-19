package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ImportRows handles POST /api/connections/{id}/schema/{db}/tables/{table}/import
// Body: { "columns": [...], "rows": [[...], ...], "skip_errors": bool }
func ImportRows() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 6 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		dbName := parts[2]
		tableName := parts[4]

		var body struct {
			Columns    []string        `json:"columns"`
			Rows       [][]interface{} `json:"rows"`
			SkipErrors bool            `json:"skip_errors"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Columns) == 0 || len(body.Rows) == 0 {
			http.Error(w, `{"error":"columns and rows required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		quotedCols := make([]string, len(body.Columns))
		for i, c := range body.Columns {
			quotedCols[i] = quoteIdent(driver, c)
		}

		placeholders := make([]string, len(body.Columns))
		for i := range body.Columns {
			if driver == "postgres" {
				placeholders[i] = fmt.Sprintf("$%d", i+1)
			} else {
				placeholders[i] = "?"
			}
		}

		tableRef := qualifiedTableName(driver, dbName, tableName)

		stmt := fmt.Sprintf(
			`INSERT INTO %s (%s) VALUES (%s)`,
			tableRef,
			strings.Join(quotedCols, ", "),
			strings.Join(placeholders, ", "),
		)

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		prepared, err := tx.PrepareContext(r.Context(), stmt)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer prepared.Close()

		inserted := 0
		var errs []string
		for rowIdx, row := range body.Rows {
			if len(row) != len(body.Columns) {
				msg := fmt.Sprintf("row %d: column count mismatch", rowIdx)
				if body.SkipErrors {
					errs = append(errs, msg)
					continue
				}
				http.Error(w, jsonError(msg), http.StatusBadRequest)
				return
			}
			args := make([]interface{}, len(row))
			copy(args, row)
			if _, err := prepared.ExecContext(r.Context(), args...); err != nil {
				msg := fmt.Sprintf("row %d: %s", rowIdx, err.Error())
				if body.SkipErrors {
					errs = append(errs, msg)
					continue
				}
				http.Error(w, jsonError(msg), http.StatusInternalServerError)
				return
			}
			inserted++
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"inserted": inserted,
			"errors":   errs,
		})
	}
}
