package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// UpdateRow handles PUT /api/connections/{id}/schema/{db}/tables/{table}/rows
func UpdateRow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 6 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		tableName := parts[4]

		var body struct {
			PKColumn string                 `json:"pk_column"`
			PKValue  interface{}            `json:"pk_value"`
			Updates  map[string]interface{} `json:"updates"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PKColumn == "" || len(body.Updates) == 0 {
			http.Error(w, `{"error":"pk_column and updates required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		setClauses := make([]string, 0, len(body.Updates))
		args := make([]interface{}, 0, len(body.Updates)+1)
		for col, val := range body.Updates {
			setClauses = append(setClauses, fmt.Sprintf("%s = ?", quoteIdent(driver, col)))
			args = append(args, val)
		}
		args = append(args, body.PKValue)

		var query string
		switch driver {
		case "postgres":
			// Convert ? to $1, $2 ...
			query = fmt.Sprintf(
				`UPDATE "public".%s SET %s WHERE %s = $%d`,
				quoteIdent(driver, tableName),
				strings.Join(setClauses, ", "),
				quoteIdent(driver, body.PKColumn),
				len(setClauses)+1,
			)
			for i := range setClauses {
				setClauses[i] = strings.Replace(setClauses[i], "?", fmt.Sprintf("$%d", i+1), 1)
			}
			query = fmt.Sprintf(
				`UPDATE "public".%s SET %s WHERE %s = $%d`,
				quoteIdent(driver, tableName),
				strings.Join(setClauses, ", "),
				quoteIdent(driver, body.PKColumn),
				len(setClauses)+1,
			)
		default:
			query = fmt.Sprintf(
				`UPDATE %s SET %s WHERE %s = ?`,
				quoteIdent(driver, tableName),
				strings.Join(setClauses, ", "),
				quoteIdent(driver, body.PKColumn),
			)
		}

		res, err := db.ExecContext(r.Context(), query, args...)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		affected, _ := res.RowsAffected()
		json.NewEncoder(w).Encode(map[string]any{"affected_rows": affected})
	}
}

// InsertRow handles POST /api/connections/{id}/schema/{db}/tables/{table}/rows
func InsertRow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 6 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		tableName := parts[4]

		var body struct {
			Values map[string]interface{} `json:"values"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Values) == 0 {
			http.Error(w, `{"error":"values required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		cols := make([]string, 0, len(body.Values))
		placeholders := make([]string, 0, len(body.Values))
		args := make([]interface{}, 0, len(body.Values))
		i := 1
		for col, val := range body.Values {
			cols = append(cols, quoteIdent(driver, col))
			if driver == "postgres" {
				placeholders = append(placeholders, fmt.Sprintf("$%d", i))
				i++
			} else {
				placeholders = append(placeholders, "?")
			}
			args = append(args, val)
		}

		tableRef := quoteIdent(driver, tableName)
		if driver == "postgres" {
			tableRef = `"public".` + tableRef
		}
		query := fmt.Sprintf(
			`INSERT INTO %s (%s) VALUES (%s)`,
			tableRef, strings.Join(cols, ", "), strings.Join(placeholders, ", "),
		)

		_, err = db.ExecContext(r.Context(), query, args...)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// DeleteRow handles DELETE /api/connections/{id}/schema/{db}/tables/{table}/rows
func DeleteRow() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 6 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		tableName := parts[4]

		var body struct {
			PKColumn string      `json:"pk_column"`
			PKValue  interface{} `json:"pk_value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PKColumn == "" {
			http.Error(w, `{"error":"pk_column required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		tableRef := quoteIdent(driver, tableName)
		placeholder := "?"
		if driver == "postgres" {
			tableRef = `"public".` + tableRef
			placeholder = "$1"
		}
		query := fmt.Sprintf(`DELETE FROM %s WHERE %s = %s`,
			tableRef, quoteIdent(driver, body.PKColumn), placeholder)

		res, err := db.ExecContext(r.Context(), query, body.PKValue)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		affected, _ := res.RowsAffected()
		json.NewEncoder(w).Encode(map[string]any{"affected_rows": affected})
	}
}
