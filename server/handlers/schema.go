package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type SchemaTable struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	RowCount *int64 `json:"row_count,omitempty"`
}

type SchemaDatabase struct {
	Name   string        `json:"name"`
	Tables []SchemaTable `json:"tables"`
}

type SchemaColumn struct {
	Name         string  `json:"name"`
	DataType     string  `json:"data_type"`
	IsNullable   bool    `json:"is_nullable"`
	IsPrimaryKey bool    `json:"is_primary_key"`
	DefaultValue *string `json:"default_value,omitempty"`
}

func GetSchema() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 2 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var dbs []SchemaDatabase

		switch driver {
		case "postgres":
			rows, err := db.Query(
				`SELECT table_catalog, table_name, table_type
				 FROM information_schema.tables
				 WHERE table_schema = 'public'
				 ORDER BY table_name`,
			)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			dbMap := map[string]*SchemaDatabase{}
			for rows.Next() {
				var dbName, tableName, tableType string
				rows.Scan(&dbName, &tableName, &tableType)
				tType := "table"
				if tableType == "VIEW" {
					tType = "view"
				}
				if _, ok := dbMap[dbName]; !ok {
					dbMap[dbName] = &SchemaDatabase{Name: dbName}
				}
				dbMap[dbName].Tables = append(dbMap[dbName].Tables, SchemaTable{Name: tableName, Type: tType})
			}
			for _, d := range dbMap {
				dbs = append(dbs, *d)
			}

	case "mysql", "mariadb":
		rows, err := db.Query(
			`SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE
			 FROM information_schema.TABLES
			 ORDER BY TABLE_SCHEMA, TABLE_NAME`,
		)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			dbMap := map[string]*SchemaDatabase{}
			for rows.Next() {
				var dbName, tableName, tableType string
				rows.Scan(&dbName, &tableName, &tableType)
				tType := "table"
				if tableType == "VIEW" {
					tType = "view"
				}
				if _, ok := dbMap[dbName]; !ok {
					dbMap[dbName] = &SchemaDatabase{Name: dbName}
				}
				dbMap[dbName].Tables = append(dbMap[dbName].Tables, SchemaTable{Name: tableName, Type: tType})
			}
			for _, d := range dbMap {
				dbs = append(dbs, *d)
			}

		case "sqlite":
			rows, err := db.Query(`SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY name`)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			mainDB := SchemaDatabase{Name: "main"}
			for rows.Next() {
				var name, tType string
				rows.Scan(&name, &tType)
				mainDB.Tables = append(mainDB.Tables, SchemaTable{Name: name, Type: tType})
			}
			dbs = []SchemaDatabase{mainDB}

		case "sqlserver":
			rows, err := db.Query(
				`SELECT TABLE_CATALOG, TABLE_NAME, TABLE_TYPE
				 FROM INFORMATION_SCHEMA.TABLES
				 ORDER BY TABLE_NAME`,
			)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			dbMap := map[string]*SchemaDatabase{}
			for rows.Next() {
				var dbName, tableName, tableType string
				rows.Scan(&dbName, &tableName, &tableType)
				tType := "table"
				if tableType == "VIEW" {
					tType = "view"
				}
				if _, ok := dbMap[dbName]; !ok {
					dbMap[dbName] = &SchemaDatabase{Name: dbName}
				}
				dbMap[dbName].Tables = append(dbMap[dbName].Tables, SchemaTable{Name: tableName, Type: tType})
			}
			for _, d := range dbMap {
				dbs = append(dbs, *d)
			}
		}

		if dbs == nil {
			dbs = []SchemaDatabase{}
		}
		json.NewEncoder(w).Encode(dbs)
	}
}

func GetTableColumns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Path: /api/connections/{id}/schema/{db}/tables/{table}/columns
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 6 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		dbName := parts[2]
		tableName := parts[4]

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var cols []SchemaColumn

		switch driver {
		case "postgres":
			rows, err := db.Query(`
				SELECT
					c.column_name,
					c.data_type,
					c.is_nullable,
					CASE WHEN kcu.column_name IS NOT NULL THEN true ELSE false END AS is_pk,
					c.column_default
				FROM information_schema.columns c
				LEFT JOIN information_schema.key_column_usage kcu
					ON kcu.table_name = c.table_name
					AND kcu.column_name = c.column_name
					AND kcu.constraint_name IN (
						SELECT constraint_name FROM information_schema.table_constraints
						WHERE constraint_type = 'PRIMARY KEY' AND table_name = $1
					)
				WHERE c.table_catalog = $2 AND c.table_name = $1 AND c.table_schema = 'public'
				ORDER BY c.ordinal_position
			`, tableName, dbName)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			for rows.Next() {
				var col SchemaColumn
				var nullable, pk string
				var defVal *string
				rows.Scan(&col.Name, &col.DataType, &nullable, &pk, &defVal)
				col.IsNullable = nullable == "YES"
				col.IsPrimaryKey = pk == "true" || pk == "1"
				col.DefaultValue = defVal
				cols = append(cols, col)
			}

	case "mysql", "mariadb":
		rows, err := db.Query(`
				SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT
				FROM information_schema.COLUMNS
				WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
				ORDER BY ORDINAL_POSITION
			`, dbName, tableName)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			for rows.Next() {
				var col SchemaColumn
				var nullable, key string
				var defVal *string
				rows.Scan(&col.Name, &col.DataType, &nullable, &key, &defVal)
				col.IsNullable = nullable == "YES"
				col.IsPrimaryKey = key == "PRI"
				col.DefaultValue = defVal
				cols = append(cols, col)
			}

		case "sqlite":
			// Use quoteIdent to safely escape the table name for SQLite PRAGMA
			safeTableName := strings.ReplaceAll(tableName, `"`, `""`)
			rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, safeTableName))
			if err != nil {
				http.Error(w, `{"error":"schema query failed"}`, http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			for rows.Next() {
				var cid, notNull, pk int
				var name, typeName string
				var dflt *string
				rows.Scan(&cid, &name, &typeName, &notNull, &dflt, &pk)
				cols = append(cols, SchemaColumn{
					Name:         name,
					DataType:     typeName,
					IsNullable:   notNull == 0,
					IsPrimaryKey: pk > 0,
					DefaultValue: dflt,
				})
			}

		case "sqlserver":
			rows, err := db.Query(`
				SELECT
					c.COLUMN_NAME, c.DATA_TYPE, c.IS_NULLABLE,
					CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END AS is_pk,
					c.COLUMN_DEFAULT
				FROM INFORMATION_SCHEMA.COLUMNS c
				LEFT JOIN (
					SELECT ku.COLUMN_NAME
					FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
					JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
					  ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
					WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY' AND tc.TABLE_NAME = @p1
				) pk ON pk.COLUMN_NAME = c.COLUMN_NAME
				WHERE c.TABLE_CATALOG = @p2 AND c.TABLE_NAME = @p1
				ORDER BY c.ORDINAL_POSITION
			`, tableName, dbName)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			defer rows.Close()
			for rows.Next() {
				var col SchemaColumn
				var nullable string
				var pk int
				var defVal *string
				rows.Scan(&col.Name, &col.DataType, &nullable, &pk, &defVal)
				col.IsNullable = nullable == "YES"
				col.IsPrimaryKey = pk == 1
				col.DefaultValue = defVal
				cols = append(cols, col)
			}
		}

		if cols == nil {
			cols = []SchemaColumn{}
		}
		json.NewEncoder(w).Encode(cols)
	}
}

// quoteIdent returns a driver-appropriate quoted identifier.
func quoteIdent(driver, name string) string {
	switch driver {
	case "mysql":
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case "sqlserver":
		// SQL Server uses brackets; escape embedded ]
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
}

// isValidSortDirection checks if the orderDir is valid
func isValidSortDirection(dir string) bool {
	upper := strings.ToUpper(strings.TrimSpace(dir))
	return upper == "ASC" || upper == "DESC" || upper == ""
}

func GetTableData() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Path: /api/connections/{id}/schema/{db}/tables/{table}/data
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 6 {
			http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}
		dbName := parts[2]
		tableName := parts[4]

		q := r.URL.Query()
		page, pageSize := 1, 100
		if v := q.Get("page"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				page = n
			}
		}
		if v := q.Get("page_size"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
				pageSize = n
			}
		}
		orderBy := q.Get("order_by")
		orderDir := "ASC"
		if q.Get("order_dir") == "desc" {
			orderDir = "DESC"
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		offset := (page - 1) * pageSize

		// Build table reference and count query per driver
		var tableRef, countSQL string
		switch driver {
		case "postgres":
			tableRef = fmt.Sprintf(`"public".%s`, quoteIdent(driver, tableName))
			countSQL = fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tableRef)
		case "mysql":
			tableRef = fmt.Sprintf("%s.%s", quoteIdent(driver, dbName), quoteIdent(driver, tableName))
			countSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s", tableRef)
		case "sqlite":
			tableRef = quoteIdent(driver, tableName)
			countSQL = fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tableRef)
		case "sqlserver":
			tableRef = fmt.Sprintf("%s.[dbo].%s", quoteIdent(driver, dbName), quoteIdent(driver, tableName))
			countSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s", tableRef)
		default:
			tableRef = quoteIdent(driver, tableName)
			countSQL = fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tableRef)
		}

		var total int64
		db.QueryRow(countSQL).Scan(&total)

		// Validate sort direction
		if !isValidSortDirection(orderDir) {
			orderDir = "ASC"
		}

		// Build SELECT with optional ORDER BY
		var dataSQL string
		switch driver {
		case "sqlserver":
			orderClause := "ORDER BY (SELECT NULL)"
			if orderBy != "" {
				orderClause = fmt.Sprintf("ORDER BY %s %s", quoteIdent(driver, orderBy), orderDir)
			}
			dataSQL = fmt.Sprintf(
				`SELECT * FROM %s %s OFFSET %d ROWS FETCH NEXT %d ROWS ONLY`,
				tableRef, orderClause, offset, pageSize,
			)
		case "postgres":
			dataSQL = fmt.Sprintf("SELECT * FROM %s", tableRef)
			if orderBy != "" {
				dataSQL += fmt.Sprintf(` ORDER BY %s %s`, quoteIdent(driver, orderBy), orderDir)
			}
			dataSQL += fmt.Sprintf(" LIMIT $1 OFFSET $2")
		default:
			dataSQL = fmt.Sprintf("SELECT * FROM %s", tableRef)
			if orderBy != "" {
				dataSQL += fmt.Sprintf(` ORDER BY %s %s`, quoteIdent(driver, orderBy), orderDir)
			}
			dataSQL += " LIMIT ? OFFSET ?"
		}

		var sqlRows interface {
			Columns() ([]string, error)
			Next() bool
			Scan(...any) error
			Close() error
		}

		switch driver {
		case "sqlserver":
			sqlRows, err = db.QueryContext(r.Context(), dataSQL)
		case "postgres":
			sqlRows, err = db.QueryContext(r.Context(), dataSQL, pageSize, offset)
		default:
			sqlRows, err = db.QueryContext(r.Context(), dataSQL, pageSize, offset)
		}
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		defer sqlRows.Close()

		cols, _ := sqlRows.Columns()
		var result [][]interface{}
		for sqlRows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			sqlRows.Scan(ptrs...)
			row := make([]interface{}, len(cols))
			for i, v := range vals {
				switch t := v.(type) {
				case []byte:
					row[i] = string(t)
				default:
					row[i] = t
				}
			}
			result = append(result, row)
		}
		if result == nil {
			result = [][]interface{}{}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"columns":    cols,
			"rows":       result,
			"total_rows": total,
			"page":       page,
			"page_size":  pageSize,
		})
	}
}
