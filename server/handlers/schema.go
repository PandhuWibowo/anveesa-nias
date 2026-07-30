package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var uuidWhereRe = regexp.MustCompile(`(?i)(=\s*)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\b`)

// primaryKeyColumns returns a table's primary key column(s), in ordinal
// order — used as a deterministic ORDER BY tiebreaker. Without one,
// LIMIT/OFFSET pagination has no guaranteed row order for ties (rows sharing
// the same value in the sorted column, or every row when no sort is
// requested at all): the database is free to return them in a different
// order on each page fetch, which surfaces as rows that look duplicated,
// missing, or simply don't match what asc/desc sorting should show.
func primaryKeyColumns(db *sql.DB, driver, dbName, tableName string) []string {
	var query string
	var args []interface{}
	switch driver {
	case "sqlite3":
		rows, err := db.Query(`PRAGMA table_info(` + quoteIdent(driver, tableName) + `)`)
		if err != nil {
			return nil
		}
		defer rows.Close()
		type pkCol struct {
			name string
			pos  int
		}
		var pks []pkCol
		for rows.Next() {
			var cid int
			var name, dataType string
			var notNull, pk int
			var defVal *string
			if rows.Scan(&cid, &name, &dataType, &notNull, &defVal, &pk) != nil {
				continue
			}
			if pk > 0 {
				pks = append(pks, pkCol{name, pk})
			}
		}
		sort.Slice(pks, func(i, j int) bool { return pks[i].pos < pks[j].pos })
		cols := make([]string, len(pks))
		for i, p := range pks {
			cols[i] = p.name
		}
		return cols
	case "postgres":
		schemaName := dbName
		if schemaName == "" {
			schemaName = "public"
		}
		query = `
			SELECT kcu.column_name
			FROM information_schema.key_column_usage kcu
			JOIN information_schema.table_constraints tc
			  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
			WHERE tc.constraint_type = 'PRIMARY KEY' AND kcu.table_name = $1 AND kcu.table_schema = $2
			ORDER BY kcu.ordinal_position`
		args = []interface{}{tableName, schemaName}
	case "mysql", "mariadb":
		query = `
			SELECT COLUMN_NAME FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_KEY = 'PRI'
			ORDER BY ORDINAL_POSITION`
		args = []interface{}{dbName, tableName}
	case "sqlserver":
		query = `
			SELECT ku.COLUMN_NAME
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY' AND tc.TABLE_NAME = @p1
			ORDER BY ku.ORDINAL_POSITION`
		args = []interface{}{tableName}
	default:
		return nil
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			cols = append(cols, name)
		}
	}
	return cols
}

// orderByTerms builds the ORDER BY term list: the user's requested sort
// column/direction (if any) followed by tiebreaker columns, so identical
// values in the sorted column — or "no sort requested" entirely — still get
// a stable, repeatable row order across page fetches.
func orderByTerms(driver, orderBy, orderDir string, tieBreakers []string) []string {
	var terms []string
	if orderBy != "" {
		terms = append(terms, fmt.Sprintf("%s %s", quoteIdent(driver, orderBy), orderDir))
	}
	for _, c := range tieBreakers {
		if strings.EqualFold(c, orderBy) {
			continue
		}
		if c == "ctid" || c == "rowid" {
			terms = append(terms, c) // pseudo-columns, not real identifiers
		} else {
			terms = append(terms, quoteIdent(driver, c))
		}
	}
	return terms
}

func quoteUnquotedUUIDs(where string) string {
	return uuidWhereRe.ReplaceAllStringFunc(where, func(match string) string {
		sub := uuidWhereRe.FindStringSubmatch(match)
		return sub[1] + "'" + sub[2] + "'"
	})
}

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

		cachedJSONResponse(w, r, fmt.Sprintf("schema:list:%d", connID), 2*time.Minute, func() (any, error) {
			db, driver, err := GetDB(connID)
			if err != nil {
				return nil, fmt.Errorf("%s", err.Error())
			}

			var dbs []SchemaDatabase

			switch driver {
			case "sqlite3":
				rows, err := db.Query(
					`SELECT name, type
					 FROM sqlite_master
					 WHERE type IN ('table', 'view')
					   AND name NOT LIKE 'sqlite_%'
					 ORDER BY type, name`,
				)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				mainDB := SchemaDatabase{Name: "main"}
				for rows.Next() {
					var tableName, tableType string
					rows.Scan(&tableName, &tableType)
					mainDB.Tables = append(mainDB.Tables, SchemaTable{Name: tableName, Type: tableType})
				}
				dbs = append(dbs, mainDB)

			case "postgres":
				rows, err := db.Query(
					`SELECT table_schema, table_name, table_type
					 FROM information_schema.tables
					 WHERE table_schema NOT IN ('information_schema', 'pg_catalog')
					   AND table_schema NOT LIKE 'pg_toast%'
					   AND table_schema NOT LIKE 'pg_temp_%'
					 ORDER BY table_schema, table_name`,
				)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				dbMap := map[string]*SchemaDatabase{}
				for rows.Next() {
					var schemaName, tableName, tableType string
					rows.Scan(&schemaName, &tableName, &tableType)
					tType := "table"
					if tableType == "VIEW" {
						tType = "view"
					}
					if _, ok := dbMap[schemaName]; !ok {
						dbMap[schemaName] = &SchemaDatabase{Name: schemaName}
					}
					dbMap[schemaName].Tables = append(dbMap[schemaName].Tables, SchemaTable{Name: tableName, Type: tType})
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
					return nil, err
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

			case "sqlserver":
				rows, err := db.Query(
					`SELECT TABLE_CATALOG, TABLE_NAME, TABLE_TYPE
					 FROM INFORMATION_SCHEMA.TABLES
					 ORDER BY TABLE_NAME`,
				)
				if err != nil {
					return nil, err
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
			for i := range dbs {
				if dbs[i].Tables == nil {
					dbs[i].Tables = []SchemaTable{}
				}
			}
			return dbs, nil
		})
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

		cacheKey := fmt.Sprintf("schema:columns:%d:%s:%s", connID, dbName, tableName)
		cachedJSONResponse(w, r, cacheKey, 2*time.Minute, func() (any, error) {
			db, driver, err := GetDB(connID)
			if err != nil {
				return nil, fmt.Errorf("%s", err.Error())
			}

			var cols []SchemaColumn

			switch driver {
			case "sqlite3":
				rows, err := db.Query(`PRAGMA table_info(` + quoteIdent(driver, tableName) + `)`)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				for rows.Next() {
					var cid int
					var col SchemaColumn
					var notNull, pk int
					var defVal *string
					rows.Scan(&cid, &col.Name, &col.DataType, &notNull, &defVal, &pk)
					col.IsNullable = notNull == 0
					col.IsPrimaryKey = pk > 0
					col.DefaultValue = defVal
					cols = append(cols, col)
				}

			case "postgres":
				schemaName := dbName
				if schemaName == "" {
					schemaName = "public"
				}
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
					AND kcu.table_schema = c.table_schema
					AND kcu.constraint_name IN (
						SELECT constraint_name FROM information_schema.table_constraints
						WHERE constraint_type = 'PRIMARY KEY' AND table_name = $1 AND table_schema = $2
					)
				WHERE c.table_name = $1 AND c.table_schema = $2
				ORDER BY c.ordinal_position
			`, tableName, schemaName)
				if err != nil {
					return nil, err
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
					return nil, err
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
					return nil, err
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
			return cols, nil
		})
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

func qualifiedTableName(driver, namespace, table string) string {
	switch driver {
	case "postgres":
		if namespace == "" {
			namespace = "public"
		}
		return fmt.Sprintf(`%s.%s`, quoteIdent(driver, namespace), quoteIdent(driver, table))
	case "mysql":
		return fmt.Sprintf("%s.%s", quoteIdent(driver, namespace), quoteIdent(driver, table))
	case "sqlserver":
		return fmt.Sprintf("%s.[dbo].%s", quoteIdent(driver, namespace), quoteIdent(driver, table))
	default:
		return quoteIdent(driver, table)
	}
}

// isValidSortDirection checks if the orderDir is valid
func isValidSortDirection(dir string) bool {
	upper := strings.ToUpper(strings.TrimSpace(dir))
	return upper == "ASC" || upper == "DESC" || upper == ""
}

// buildSearchWhereFragment builds a WHERE clause that searches for `search`
// across all columns in the table by casting each column to text and using
// ILIKE/LIKE. Returns empty string if no columns found or driver unsupported.
func buildSearchWhereFragment(db *sql.DB, driver, namespace, tableName, search string) string {
	escaped := strings.ReplaceAll(search, "'", "''")
	pattern := "%" + escaped + "%"

	var columnNames []string

	switch driver {
	case "sqlite3":
		rows, err := db.Query(`PRAGMA table_info(` + quoteIdent(driver, tableName) + `)`)
		if err != nil {
			return ""
		}
		defer rows.Close()
		for rows.Next() {
			var cid, notNull, pk int
			var name, dataType string
			var defVal *string
			rows.Scan(&cid, &name, &dataType, &notNull, &defVal, &pk)
			columnNames = append(columnNames, name)
		}
	case "postgres":
		if namespace == "" {
			namespace = "public"
		}
		rows, err := db.Query(`SELECT column_name FROM information_schema.columns WHERE table_name = $1 AND table_schema = $2 ORDER BY ordinal_position`, tableName, namespace)
		if err != nil {
			return ""
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			rows.Scan(&name)
			columnNames = append(columnNames, name)
		}
	case "mysql", "mariadb":
		rows, err := db.Query(`SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, namespace, tableName)
		if err != nil {
			return ""
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			rows.Scan(&name)
			columnNames = append(columnNames, name)
		}
	case "sqlserver":
		rows, err := db.Query(`SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_CATALOG = @p2 AND TABLE_NAME = @p1 ORDER BY ORDINAL_POSITION`, tableName, namespace)
		if err != nil {
			return ""
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			rows.Scan(&name)
			columnNames = append(columnNames, name)
		}
	default:
		return ""
	}

	if len(columnNames) == 0 {
		return ""
	}

	parts := make([]string, 0, len(columnNames))
	for _, col := range columnNames {
		quoted := quoteIdent(driver, col)
		switch driver {
		case "postgres":
			parts = append(parts, fmt.Sprintf("%s::text ILIKE '%s'", quoted, pattern))
		case "mysql", "mariadb":
			parts = append(parts, fmt.Sprintf("%s LIKE '%s'", quoted, pattern))
		case "sqlserver":
			parts = append(parts, fmt.Sprintf("TRY_CAST(%s AS NVARCHAR(MAX)) LIKE '%s'", quoted, pattern))
		case "sqlite3":
			parts = append(parts, fmt.Sprintf("CAST(%s AS TEXT) LIKE '%s'", quoted, pattern))
		}
	}
	return " WHERE (" + strings.Join(parts, " OR ") + ")"
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
		whereClause := q.Get("where")
		searchQuery := q.Get("search")

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		offset := (page - 1) * pageSize

		whereFragment := ""
		if searchQuery != "" {
			whereFragment = buildSearchWhereFragment(db, driver, dbName, tableName, searchQuery)
		} else if whereClause != "" {
			whereFragment = " WHERE " + quoteUnquotedUUIDs(whereClause)
		}

		// Build table reference and count query per driver
		var tableRef, countSQL string
		switch driver {
		case "postgres":
			tableRef = qualifiedTableName(driver, dbName, tableName)
			countSQL = fmt.Sprintf(`SELECT COUNT(*) FROM %s%s`, tableRef, whereFragment)
		case "mysql":
			tableRef = qualifiedTableName(driver, dbName, tableName)
			countSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s%s", tableRef, whereFragment)
		case "sqlserver":
			tableRef = qualifiedTableName(driver, dbName, tableName)
			countSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s%s", tableRef, whereFragment)
		default:
			tableRef = quoteIdent(driver, tableName)
			countSQL = fmt.Sprintf(`SELECT COUNT(*) FROM %s%s`, tableRef, whereFragment)
		}

		var total int64
		db.QueryRow(countSQL).Scan(&total)

		// Validate sort direction
		if !isValidSortDirection(orderDir) {
			orderDir = "ASC"
		}

		// A deterministic tiebreaker (the PK, or a per-row pseudo-column when
		// there isn't one) so paginated results stay stable and consistent
		// with the requested sort — see primaryKeyColumns/orderByTerms above.
		tieBreakers := primaryKeyColumns(db, driver, dbName, tableName)
		if len(tieBreakers) == 0 {
			switch driver {
			case "postgres":
				tieBreakers = []string{"ctid"}
			case "sqlite3":
				tieBreakers = []string{"rowid"}
			}
		}
		orderTerms := orderByTerms(driver, orderBy, orderDir, tieBreakers)

		// Build SELECT with optional WHERE and ORDER BY
		var dataSQL string
		switch driver {
		case "sqlserver":
			orderClause := "ORDER BY (SELECT NULL)"
			if len(orderTerms) > 0 {
				orderClause = "ORDER BY " + strings.Join(orderTerms, ", ")
			}
			dataSQL = fmt.Sprintf(
				`SELECT * FROM %s%s %s OFFSET %d ROWS FETCH NEXT %d ROWS ONLY`,
				tableRef, whereFragment, orderClause, offset, pageSize,
			)
		case "postgres":
			dataSQL = fmt.Sprintf("SELECT * FROM %s%s", tableRef, whereFragment)
			if len(orderTerms) > 0 {
				dataSQL += " ORDER BY " + strings.Join(orderTerms, ", ")
			}
			dataSQL += fmt.Sprintf(" LIMIT $1 OFFSET $2")
		default:
			dataSQL = fmt.Sprintf("SELECT * FROM %s%s", tableRef, whereFragment)
			if len(orderTerms) > 0 {
				dataSQL += " ORDER BY " + strings.Join(orderTerms, ", ")
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
