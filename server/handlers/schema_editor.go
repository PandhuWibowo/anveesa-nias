package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type ColumnDef struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null"`
	PrimaryKey bool   `json:"primary_key"`
	Default    string `json:"default"`
}

// CreateTable handles POST /api/connections/{id}/schema/{db}/tables
func CreateTable() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 4 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		dbName := parts[2]

		var body struct {
			TableName string      `json:"table_name"`
			Columns   []ColumnDef `json:"columns"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TableName == "" || len(body.Columns) == 0 {
			http.Error(w, `{"error":"table_name and columns required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		colDefs := make([]string, 0, len(body.Columns))
		var pkCols []string
		for _, c := range body.Columns {
			def := fmt.Sprintf("  %s %s", quoteIdent(driver, c.Name), c.Type)
			if c.NotNull {
				def += " NOT NULL"
			}
			if c.Default != "" {
				def += " DEFAULT " + c.Default
			}
			if c.PrimaryKey {
				pkCols = append(pkCols, quoteIdent(driver, c.Name))
			}
			colDefs = append(colDefs, def)
		}
		if len(pkCols) == 1 {
			// Inline PK on single column
			for i, c := range body.Columns {
				if c.PrimaryKey {
					colDefs[i] += " PRIMARY KEY"
					break
				}
			}
		} else if len(pkCols) > 1 {
			colDefs = append(colDefs, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(pkCols, ", ")))
		}

		tableRef := qualifiedTableName(driver, dbName, body.TableName)
		ddl := fmt.Sprintf("CREATE TABLE %s (\n%s\n)", tableRef, strings.Join(colDefs, ",\n"))

		if _, err := db.ExecContext(r.Context(), ddl); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "ddl": ddl})
	}
}

// DropTable handles DELETE /api/connections/{id}/schema/{db}/tables/{table}
func DropTable() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 5 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		dbName := parts[2]
		tableName := parts[4]

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		ddl := fmt.Sprintf("DROP TABLE IF EXISTS %s", qualifiedTableName(driver, dbName, tableName))
		if _, err := db.ExecContext(r.Context(), ddl); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// AddColumn handles POST /api/connections/{id}/schema/{db}/tables/{table}/columns
func AddColumn() http.HandlerFunc {
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

		var col ColumnDef
		if err := json.NewDecoder(r.Body).Decode(&col); err != nil || col.Name == "" || col.Type == "" {
			http.Error(w, `{"error":"name and type required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		colDef := fmt.Sprintf("%s %s", quoteIdent(driver, col.Name), col.Type)
		if col.NotNull {
			colDef += " NOT NULL"
		}
		if col.Default != "" {
			colDef += " DEFAULT " + col.Default
		}

		ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", qualifiedTableName(driver, dbName, tableName), colDef)
		if _, err := db.ExecContext(r.Context(), ddl); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

// DropColumn handles DELETE /api/connections/{id}/schema/{db}/tables/{table}/columns/{col}
func DropColumn() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 7 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		dbName := parts[2]
		tableName := parts[4]
		colName := parts[6]

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", qualifiedTableName(driver, dbName, tableName), quoteIdent(driver, colName))
		if _, err := db.ExecContext(r.Context(), ddl); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// RenameTable handles PATCH /api/connections/{id}/schema/{db}/tables/{table}
func RenameTable() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 5 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, _ := strconv.ParseInt(parts[0], 10, 64)
		dbName := parts[2]
		oldName := parts[4]

		var body struct {
			NewName string `json:"new_name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.NewName == "" {
			http.Error(w, `{"error":"new_name required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var ddl string
		switch driver {
		case "mysql":
			ddl = fmt.Sprintf("RENAME TABLE %s TO %s", qualifiedTableName(driver, dbName, oldName), qualifiedTableName(driver, dbName, body.NewName))
		case "sqlserver":
			ddl = fmt.Sprintf("EXEC sp_rename '%s', '%s'", oldName, body.NewName)
		default:
			ddl = fmt.Sprintf("ALTER TABLE %s RENAME TO %s", qualifiedTableName(driver, dbName, oldName), quoteIdent(driver, body.NewName))
		}

		if _, err := db.ExecContext(r.Context(), ddl); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}
