package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type SchemaObjectItem struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	ParentName string `json:"parent_name,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

type SchemaObjectGroup struct {
	Key   string             `json:"key"`
	Label string             `json:"label"`
	Items []SchemaObjectItem `json:"items"`
}

type SchemaMetadataCatalog struct {
	Database string              `json:"database"`
	Groups   []SchemaObjectGroup `json:"groups"`
}

type SchemaProperty struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type SchemaIndexDetail struct {
	Name       string   `json:"name"`
	TableName  string   `json:"table_name"`
	Method     string   `json:"method"`
	IsUnique   bool     `json:"is_unique"`
	IsPrimary  bool     `json:"is_primary"`
	Columns    []string `json:"columns"`
	Definition string   `json:"definition"`
}

type SchemaConstraintDetail struct {
	Name            string   `json:"name"`
	ConstraintType  string   `json:"constraint_type"`
	Columns         []string `json:"columns"`
	Definition      string   `json:"definition"`
	ReferencedTable string   `json:"referenced_table,omitempty"`
}

type SchemaTriggerDetail struct {
	Name       string `json:"name"`
	TableName  string `json:"table_name"`
	Timing     string `json:"timing"`
	Events     string `json:"events"`
	Definition string `json:"definition"`
}

type SchemaSequenceDetail struct {
	Name        string `json:"name"`
	StartValue  string `json:"start_value"`
	IncrementBy string `json:"increment_by"`
	MinValue    string `json:"min_value"`
	MaxValue    string `json:"max_value"`
	CacheSize   string `json:"cache_size"`
	Cycle       bool   `json:"cycle"`
	OwnedBy     string `json:"owned_by,omitempty"`
	Definition  string `json:"definition,omitempty"`
}

type SchemaRoutineDetail struct {
	Name        string `json:"name"`
	RoutineType string `json:"routine_type"`
	Identity    string `json:"identity"`
	ReturnType  string `json:"return_type,omitempty"`
	Definition  string `json:"definition"`
}

type SchemaObjectDetail struct {
	Type         string                   `json:"type"`
	Name         string                   `json:"name"`
	Database     string                   `json:"database"`
	DDL          string                   `json:"ddl"`
	Properties   []SchemaProperty         `json:"properties"`
	Columns      []SchemaColumn           `json:"columns"`
	Indexes      []SchemaIndexDetail      `json:"indexes"`
	Constraints  []SchemaConstraintDetail `json:"constraints"`
	Triggers     []SchemaTriggerDetail    `json:"triggers"`
	Sequences    []SchemaSequenceDetail   `json:"sequences"`
	Routine      *SchemaRoutineDetail     `json:"routine,omitempty"`
	EnumValues   []string                 `json:"enum_values,omitempty"`
	Dependencies []SchemaProperty         `json:"dependencies"`
}

func ListSchemaMetadata() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, dbName, err := schemaTargetFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		catalog, err := fetchSchemaMetadataCatalog(db, normalizeSchemaDriver(driver), dbName)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(catalog)
	}
}

func GetSchemaObjectDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, dbName, err := schemaTargetFromPath(strings.TrimSuffix(r.URL.Path, "/object-detail"))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		objectType := strings.TrimSpace(r.URL.Query().Get("type"))
		objectName, err := url.QueryUnescape(strings.TrimSpace(r.URL.Query().Get("name")))
		if err != nil {
			http.Error(w, jsonError("invalid object name"), http.StatusBadRequest)
			return
		}
		if objectType == "" || objectName == "" {
			http.Error(w, jsonError("type and name are required"), http.StatusBadRequest)
			return
		}
		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		detail, err := fetchSchemaObjectDetail(db, normalizeSchemaDriver(driver), dbName, objectType, objectName)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(detail)
	}
}

func schemaTargetFromPath(path string) (int64, string, error) {
	parts := strings.Split(strings.TrimPrefix(path, "/api/connections/"), "/")
	if len(parts) < 3 || parts[1] != "schema" {
		return 0, "", fmt.Errorf("invalid schema path: expected /api/connections/{id}/schema/{db}, got %q", path)
	}
	connID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid connection id")
	}
	dbName, err := url.PathUnescape(parts[2])
	if err != nil {
		return 0, "", fmt.Errorf("invalid database name")
	}
	return connID, dbName, nil
}

func normalizeSchemaDriver(driver string) string {
	if driver == "mssql" {
		return "sqlserver"
	}
	return driver
}

func fetchSchemaMetadataCatalog(db *sql.DB, driver, dbName string) (SchemaMetadataCatalog, error) {
	switch driver {
	case "postgres":
		return listPostgresMetadataCatalog(db, dbName)
	case "mysql", "mariadb":
		return listMySQLMetadataCatalog(db, dbName)
	case "sqlite":
		return listSQLiteMetadataCatalog(db, dbName)
	case "sqlserver":
		return listSQLServerMetadataCatalog(db, dbName)
	default:
		return SchemaMetadataCatalog{Database: dbName, Groups: []SchemaObjectGroup{}}, nil
	}
}

func fetchSchemaObjectDetail(db *sql.DB, driver, dbName, objectType, objectName string) (SchemaObjectDetail, error) {
	switch driver {
	case "postgres":
		return postgresObjectDetail(db, dbName, objectType, objectName)
	case "mysql", "mariadb":
		return mySQLObjectDetail(db, dbName, objectType, objectName)
	case "sqlite":
		return sqliteObjectDetail(db, dbName, objectType, objectName)
	case "sqlserver":
		return sqlServerObjectDetail(db, dbName, objectType, objectName)
	default:
		return emptyObjectDetail(dbName, objectType, objectName), nil
	}
}

func emptyObjectDetail(dbName, objectType, objectName string) SchemaObjectDetail {
	return SchemaObjectDetail{
		Type:         objectType,
		Name:         objectName,
		Database:     dbName,
		Properties:   []SchemaProperty{},
		Columns:      []SchemaColumn{},
		Indexes:      []SchemaIndexDetail{},
		Constraints:  []SchemaConstraintDetail{},
		Triggers:     []SchemaTriggerDetail{},
		Sequences:    []SchemaSequenceDetail{},
		Dependencies: []SchemaProperty{},
	}
}

func newCatalog(database string) SchemaMetadataCatalog {
	return SchemaMetadataCatalog{
		Database: database,
		Groups: []SchemaObjectGroup{
			{Key: "tables", Label: "Tables", Items: []SchemaObjectItem{}},
			{Key: "views", Label: "Views", Items: []SchemaObjectItem{}},
			{Key: "materialized_views", Label: "Materialized Views", Items: []SchemaObjectItem{}},
			{Key: "indexes", Label: "Indexes", Items: []SchemaObjectItem{}},
			{Key: "sequences", Label: "Sequences", Items: []SchemaObjectItem{}},
			{Key: "triggers", Label: "Triggers", Items: []SchemaObjectItem{}},
			{Key: "functions", Label: "Functions", Items: []SchemaObjectItem{}},
			{Key: "procedures", Label: "Procedures", Items: []SchemaObjectItem{}},
			{Key: "types", Label: "Types", Items: []SchemaObjectItem{}},
		},
	}
}

func addCatalogItem(catalog *SchemaMetadataCatalog, key string, item SchemaObjectItem) {
	for i := range catalog.Groups {
		if catalog.Groups[i].Key == key {
			catalog.Groups[i].Items = append(catalog.Groups[i].Items, item)
			return
		}
	}
}

func listPostgresMetadataCatalog(db *sql.DB, schemaName string) (SchemaMetadataCatalog, error) {
	catalog := newCatalog(schemaName)
	rows, err := db.Query(`
		SELECT
			CASE c.relkind
				WHEN 'r' THEN 'table'
				WHEN 'v' THEN 'view'
				WHEN 'm' THEN 'materialized_view'
				WHEN 'i' THEN 'index'
				WHEN 'S' THEN 'sequence'
			END AS object_type,
			c.relname,
			COALESCE(parent.relname, '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_index pi ON pi.indexrelid = c.oid
		LEFT JOIN pg_class parent ON parent.oid = pi.indrelid
		WHERE n.nspname = $1
		  AND c.relkind IN ('r', 'v', 'm', 'i', 'S')
		ORDER BY object_type, c.relname
	`, schemaName)
	if err != nil {
		return catalog, err
	}
	defer rows.Close()
	for rows.Next() {
		var objectType, name, parent string
		if err := rows.Scan(&objectType, &name, &parent); err != nil {
			return catalog, err
		}
		switch objectType {
		case "table":
			addCatalogItem(&catalog, "tables", SchemaObjectItem{Name: name, Type: objectType})
		case "view":
			addCatalogItem(&catalog, "views", SchemaObjectItem{Name: name, Type: objectType})
		case "materialized_view":
			addCatalogItem(&catalog, "materialized_views", SchemaObjectItem{Name: name, Type: objectType})
		case "index":
			addCatalogItem(&catalog, "indexes", SchemaObjectItem{Name: name, Type: objectType, ParentName: parent})
		case "sequence":
			addCatalogItem(&catalog, "sequences", SchemaObjectItem{Name: name, Type: objectType})
		}
	}
	triggerRows, err := db.Query(`
		SELECT t.tgname, c.relname
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND NOT t.tgisinternal
		ORDER BY t.tgname
	`, schemaName)
	if err == nil {
		defer triggerRows.Close()
		for triggerRows.Next() {
			var name, tableName string
			if err := triggerRows.Scan(&name, &tableName); err == nil {
				addCatalogItem(&catalog, "triggers", SchemaObjectItem{Name: name, Type: "trigger", ParentName: tableName})
			}
		}
	}
	routineRows, err := db.Query(`
		SELECT
			CASE p.prokind WHEN 'p' THEN 'procedure' ELSE 'function' END AS routine_type,
			p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')' AS routine_name
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1
		ORDER BY routine_type, routine_name
	`, schemaName)
	if err == nil {
		defer routineRows.Close()
		for routineRows.Next() {
			var routineType, routineName string
			if err := routineRows.Scan(&routineType, &routineName); err == nil {
				group := "functions"
				if routineType == "procedure" {
					group = "procedures"
				}
				addCatalogItem(&catalog, group, SchemaObjectItem{Name: routineName, Type: routineType})
			}
		}
	}
	typeRows, err := db.Query(`
		SELECT
			CASE t.typtype WHEN 'e' THEN 'enum' ELSE 'type' END AS object_type,
			t.typname
		FROM pg_type t
		JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname = $1
		  AND t.typtype IN ('e', 'c', 'd')
		  AND t.typelem = 0
		ORDER BY t.typname
	`, schemaName)
	if err == nil {
		defer typeRows.Close()
		for typeRows.Next() {
			var objectType, typeName string
			if err := typeRows.Scan(&objectType, &typeName); err == nil {
				addCatalogItem(&catalog, "types", SchemaObjectItem{Name: typeName, Type: objectType})
			}
		}
	}
	return catalog, nil
}

func listMySQLMetadataCatalog(db *sql.DB, dbName string) (SchemaMetadataCatalog, error) {
	catalog := newCatalog(dbName)
	rows, err := db.Query(`
		SELECT TABLE_NAME, TABLE_TYPE
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_TYPE, TABLE_NAME
	`, dbName)
	if err != nil {
		return catalog, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, tableType string
		if err := rows.Scan(&name, &tableType); err != nil {
			return catalog, err
		}
		if tableType == "VIEW" {
			addCatalogItem(&catalog, "views", SchemaObjectItem{Name: name, Type: "view"})
		} else {
			addCatalogItem(&catalog, "tables", SchemaObjectItem{Name: name, Type: "table"})
		}
	}
	indexRows, err := db.Query(`
		SELECT DISTINCT INDEX_NAME, TABLE_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ?
		ORDER BY INDEX_NAME
	`, dbName)
	if err == nil {
		defer indexRows.Close()
		for indexRows.Next() {
			var name, tableName string
			if err := indexRows.Scan(&name, &tableName); err == nil {
				addCatalogItem(&catalog, "indexes", SchemaObjectItem{Name: name, Type: "index", ParentName: tableName})
			}
		}
	}
	triggerRows, err := db.Query(`
		SELECT TRIGGER_NAME, EVENT_OBJECT_TABLE
		FROM information_schema.TRIGGERS
		WHERE TRIGGER_SCHEMA = ?
		ORDER BY TRIGGER_NAME
	`, dbName)
	if err == nil {
		defer triggerRows.Close()
		for triggerRows.Next() {
			var name, tableName string
			if err := triggerRows.Scan(&name, &tableName); err == nil {
				addCatalogItem(&catalog, "triggers", SchemaObjectItem{Name: name, Type: "trigger", ParentName: tableName})
			}
		}
	}
	routineRows, err := db.Query(`
		SELECT ROUTINE_NAME, ROUTINE_TYPE
		FROM information_schema.ROUTINES
		WHERE ROUTINE_SCHEMA = ?
		ORDER BY ROUTINE_TYPE, ROUTINE_NAME
	`, dbName)
	if err == nil {
		defer routineRows.Close()
		for routineRows.Next() {
			var name, routineType string
			if err := routineRows.Scan(&name, &routineType); err == nil {
				group := "functions"
				typeName := "function"
				if strings.EqualFold(routineType, "PROCEDURE") {
					group = "procedures"
					typeName = "procedure"
				}
				addCatalogItem(&catalog, group, SchemaObjectItem{Name: name, Type: typeName})
			}
		}
	}
	return catalog, nil
}

func listSQLiteMetadataCatalog(db *sql.DB, dbName string) (SchemaMetadataCatalog, error) {
	catalog := newCatalog(dbName)
	rows, err := db.Query(`
		SELECT name, type, COALESCE(tbl_name, '')
		FROM sqlite_master
		WHERE type IN ('table', 'view', 'index', 'trigger')
		  AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name
	`)
	if err != nil {
		return catalog, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, objectType, parent string
		if err := rows.Scan(&name, &objectType, &parent); err != nil {
			return catalog, err
		}
		switch objectType {
		case "table":
			addCatalogItem(&catalog, "tables", SchemaObjectItem{Name: name, Type: "table"})
		case "view":
			addCatalogItem(&catalog, "views", SchemaObjectItem{Name: name, Type: "view"})
		case "index":
			addCatalogItem(&catalog, "indexes", SchemaObjectItem{Name: name, Type: "index", ParentName: parent})
		case "trigger":
			addCatalogItem(&catalog, "triggers", SchemaObjectItem{Name: name, Type: "trigger", ParentName: parent})
		}
	}
	return catalog, nil
}

func listSQLServerMetadataCatalog(db *sql.DB, dbName string) (SchemaMetadataCatalog, error) {
	catalog := newCatalog(dbName)
	rows, err := db.Query(`
		SELECT TABLE_NAME, TABLE_TYPE
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_CATALOG = @p1
		ORDER BY TABLE_TYPE, TABLE_NAME
	`, dbName)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, tableType string
			if err := rows.Scan(&name, &tableType); err == nil {
				if tableType == "VIEW" {
					addCatalogItem(&catalog, "views", SchemaObjectItem{Name: name, Type: "view"})
				} else {
					addCatalogItem(&catalog, "tables", SchemaObjectItem{Name: name, Type: "table"})
				}
			}
		}
	}
	indexRows, err := db.Query(`
		SELECT i.name, t.name
		FROM sys.indexes i
		JOIN sys.tables t ON t.object_id = i.object_id
		WHERE i.name IS NOT NULL
		ORDER BY i.name
	`)
	if err == nil {
		defer indexRows.Close()
		for indexRows.Next() {
			var name, tableName string
			if err := indexRows.Scan(&name, &tableName); err == nil {
				addCatalogItem(&catalog, "indexes", SchemaObjectItem{Name: name, Type: "index", ParentName: tableName})
			}
		}
	}
	sequenceRows, err := db.Query(`
		SELECT name FROM sys.sequences ORDER BY name
	`)
	if err == nil {
		defer sequenceRows.Close()
		for sequenceRows.Next() {
			var name string
			if err := sequenceRows.Scan(&name); err == nil {
				addCatalogItem(&catalog, "sequences", SchemaObjectItem{Name: name, Type: "sequence"})
			}
		}
	}
	return catalog, nil
}

func postgresObjectDetail(db *sql.DB, schemaName, objectType, objectName string) (SchemaObjectDetail, error) {
	detail := emptyObjectDetail(schemaName, objectType, objectName)
	switch objectType {
	case "table", "view", "materialized_view":
		columns, err := fetchTableColumnsForMetadata(db, "postgres", schemaName, objectName)
		if err != nil {
			return detail, err
		}
		indexes, _ := fetchPostgresIndexes(db, schemaName, objectName)
		constraints, _ := fetchPostgresConstraints(db, schemaName, objectName)
		triggers, _ := fetchPostgresTriggers(db, schemaName, objectName)
		detail.Columns = columns
		detail.Indexes = indexes
		detail.Constraints = constraints
		detail.Triggers = triggers
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: objectType},
			{Label: "Schema", Value: schemaName},
		}
		switch objectType {
		case "table":
			detail.DDL = buildPostgresCreateTableDDL(schemaName, objectName, columns, constraints)
		case "view":
			var definition string
			_ = db.QueryRow(`SELECT COALESCE(pg_get_viewdef(($1 || '.' || $2)::regclass, true), '')`, schemaName, objectName).Scan(&definition)
			detail.DDL = fmt.Sprintf("CREATE VIEW %s.%s AS\n%s", quoteIdent("postgres", schemaName), quoteIdent("postgres", objectName), definition)
		case "materialized_view":
			var definition string
			_ = db.QueryRow(`SELECT COALESCE(pg_get_viewdef(($1 || '.' || $2)::regclass, true), '')`, schemaName, objectName).Scan(&definition)
			detail.DDL = fmt.Sprintf("CREATE MATERIALIZED VIEW %s.%s AS\n%s", quoteIdent("postgres", schemaName), quoteIdent("postgres", objectName), definition)
		}
	case "index":
		index, err := fetchPostgresIndexDetail(db, schemaName, objectName)
		if err != nil {
			return detail, err
		}
		detail.Indexes = []SchemaIndexDetail{index}
		detail.DDL = index.Definition
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: "index"},
			{Label: "Table", Value: index.TableName},
			{Label: "Method", Value: index.Method},
			{Label: "Unique", Value: boolLabel(index.IsUnique)},
			{Label: "Primary", Value: boolLabel(index.IsPrimary)},
		}
	case "sequence":
		sequence, err := fetchPostgresSequenceDetail(db, schemaName, objectName)
		if err != nil {
			return detail, err
		}
		detail.Sequences = []SchemaSequenceDetail{sequence}
		detail.DDL = sequence.Definition
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: "sequence"},
			{Label: "Owned By", Value: defaultString(sequence.OwnedBy, "Not linked")},
			{Label: "Increment", Value: sequence.IncrementBy},
			{Label: "Cache", Value: sequence.CacheSize},
		}
	case "trigger":
		trigger, err := fetchPostgresTriggerDetail(db, schemaName, objectName)
		if err != nil {
			return detail, err
		}
		detail.Triggers = []SchemaTriggerDetail{trigger}
		detail.DDL = trigger.Definition
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: "trigger"},
			{Label: "Table", Value: trigger.TableName},
			{Label: "Timing", Value: trigger.Timing},
			{Label: "Events", Value: trigger.Events},
		}
	case "function", "procedure":
		routine, err := fetchPostgresRoutineDetail(db, schemaName, objectName)
		if err != nil {
			return detail, err
		}
		detail.Routine = &routine
		detail.DDL = routine.Definition
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: routine.RoutineType},
			{Label: "Identity", Value: routine.Identity},
			{Label: "Return Type", Value: defaultString(routine.ReturnType, "-")},
		}
	case "enum", "type":
		enumValues, ddl, err := fetchPostgresTypeDetail(db, schemaName, objectName)
		if err != nil {
			return detail, err
		}
		detail.EnumValues = enumValues
		detail.DDL = ddl
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: objectType},
			{Label: "Schema", Value: schemaName},
		}
	}
	return detail, nil
}

func mySQLObjectDetail(db *sql.DB, dbName, objectType, objectName string) (SchemaObjectDetail, error) {
	detail := emptyObjectDetail(dbName, objectType, objectName)
	switch objectType {
	case "table", "view":
		columns, err := fetchTableColumnsForMetadata(db, "mysql", dbName, objectName)
		if err != nil {
			return detail, err
		}
		indexes, _ := fetchMySQLIndexes(db, dbName, objectName)
		constraints, _ := fetchMySQLConstraints(db, dbName, objectName)
		triggers, _ := fetchMySQLTriggers(db, dbName, objectName)
		detail.Columns = columns
		detail.Indexes = indexes
		detail.Constraints = constraints
		detail.Triggers = triggers
		detail.DDL = fetchMySQLShowCreate(db, objectType, dbName, objectName)
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: objectType},
			{Label: "Database", Value: dbName},
		}
	case "index":
		indexes, _ := fetchMySQLIndexByName(db, dbName, objectName)
		detail.Indexes = indexes
		if len(indexes) > 0 {
			detail.DDL = buildMySQLIndexDDL(indexes[0])
		}
	case "trigger":
		trigger, _ := fetchMySQLTriggerDetail(db, dbName, objectName)
		detail.Triggers = []SchemaTriggerDetail{trigger}
		detail.DDL = trigger.Definition
	case "function", "procedure":
		routine := fetchMySQLRoutineDetail(db, dbName, objectType, objectName)
		detail.Routine = &routine
		detail.DDL = routine.Definition
	}
	return detail, nil
}

func sqliteObjectDetail(db *sql.DB, dbName, objectType, objectName string) (SchemaObjectDetail, error) {
	detail := emptyObjectDetail(dbName, objectType, objectName)
	switch objectType {
	case "table", "view":
		columns, err := fetchTableColumnsForMetadata(db, "sqlite", dbName, objectName)
		if err != nil {
			return detail, err
		}
		indexes, _ := fetchSQLiteIndexes(db, objectName)
		constraints, _ := fetchSQLiteConstraints(db, objectName)
		triggers, _ := fetchSQLiteTriggers(db, objectName)
		detail.Columns = columns
		detail.Indexes = indexes
		detail.Constraints = constraints
		detail.Triggers = triggers
		detail.DDL = fetchSQLiteObjectSQL(db, objectType, objectName)
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: objectType},
			{Label: "Database", Value: dbName},
		}
	case "index":
		indexes, _ := fetchSQLiteIndexByName(db, objectName)
		detail.Indexes = indexes
		if len(indexes) > 0 {
			detail.DDL = indexes[0].Definition
		}
	case "trigger":
		detail.DDL = fetchSQLiteObjectSQL(db, objectType, objectName)
		var parent string
		_ = db.QueryRow(`SELECT COALESCE(tbl_name, '') FROM sqlite_master WHERE type='trigger' AND name=?`, objectName).Scan(&parent)
		detail.Triggers = []SchemaTriggerDetail{{Name: objectName, TableName: parent, Definition: detail.DDL}}
	}
	return detail, nil
}

func sqlServerObjectDetail(db *sql.DB, dbName, objectType, objectName string) (SchemaObjectDetail, error) {
	detail := emptyObjectDetail(dbName, objectType, objectName)
	switch objectType {
	case "table", "view":
		columns, err := fetchTableColumnsForMetadata(db, "sqlserver", dbName, objectName)
		if err != nil {
			return detail, err
		}
		detail.Columns = columns
		detail.Indexes, _ = fetchSQLServerIndexes(db, objectName)
		detail.DDL = fetchSQLServerDefinition(db, objectName)
		detail.Properties = []SchemaProperty{
			{Label: "Object Type", Value: objectType},
			{Label: "Database", Value: dbName},
		}
	case "index":
		indexes, _ := fetchSQLServerIndexByName(db, objectName)
		detail.Indexes = indexes
		if len(indexes) > 0 {
			detail.DDL = indexes[0].Definition
		}
	case "sequence":
		sequence, _ := fetchSQLServerSequenceDetail(db, objectName)
		detail.Sequences = []SchemaSequenceDetail{sequence}
		detail.DDL = sequence.Definition
	}
	return detail, nil
}

func fetchTableColumnsForMetadata(db *sql.DB, driver, dbName, tableName string) ([]SchemaColumn, error) {
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
				AND kcu.table_schema = c.table_schema
				AND kcu.constraint_name IN (
					SELECT constraint_name
					FROM information_schema.table_constraints
					WHERE constraint_type = 'PRIMARY KEY' AND table_name = $1 AND table_schema = $2
				)
			WHERE c.table_name = $1 AND c.table_schema = $2
			ORDER BY c.ordinal_position
		`, tableName, dbName)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var cols []SchemaColumn
		for rows.Next() {
			var col SchemaColumn
			var nullable string
			var isPK bool
			var defVal *string
			if err := rows.Scan(&col.Name, &col.DataType, &nullable, &isPK, &defVal); err != nil {
				return nil, err
			}
			col.IsNullable = nullable == "YES"
			col.IsPrimaryKey = isPK
			col.DefaultValue = defVal
			cols = append(cols, col)
		}
		return cols, rows.Err()
	case "mysql":
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
		var cols []SchemaColumn
		for rows.Next() {
			var col SchemaColumn
			var nullable, key string
			var defVal *string
			if err := rows.Scan(&col.Name, &col.DataType, &nullable, &key, &defVal); err != nil {
				return nil, err
			}
			col.IsNullable = nullable == "YES"
			col.IsPrimaryKey = key == "PRI"
			col.DefaultValue = defVal
			cols = append(cols, col)
		}
		return cols, rows.Err()
	case "sqlite":
		rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, quoteIdent("sqlite", tableName)))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var cols []SchemaColumn
		for rows.Next() {
			var cid, notNull, pk int
			var name, typeName string
			var dflt *string
			if err := rows.Scan(&cid, &name, &typeName, &notNull, &dflt, &pk); err != nil {
				return nil, err
			}
			cols = append(cols, SchemaColumn{
				Name:         name,
				DataType:     typeName,
				IsNullable:   notNull == 0,
				IsPrimaryKey: pk > 0,
				DefaultValue: dflt,
			})
		}
		return cols, rows.Err()
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
				JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
				WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY' AND tc.TABLE_NAME = @p1
			) pk ON pk.COLUMN_NAME = c.COLUMN_NAME
			WHERE c.TABLE_CATALOG = @p2 AND c.TABLE_NAME = @p1
			ORDER BY c.ORDINAL_POSITION
		`, tableName, dbName)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var cols []SchemaColumn
		for rows.Next() {
			var col SchemaColumn
			var nullable string
			var isPK int
			var defVal *string
			if err := rows.Scan(&col.Name, &col.DataType, &nullable, &isPK, &defVal); err != nil {
				return nil, err
			}
			col.IsNullable = nullable == "YES"
			col.IsPrimaryKey = isPK == 1
			col.DefaultValue = defVal
			cols = append(cols, col)
		}
		return cols, rows.Err()
	default:
		return []SchemaColumn{}, nil
	}
}

func fetchPostgresIndexes(db *sql.DB, schemaName, tableName string) ([]SchemaIndexDetail, error) {
	rows, err := db.Query(`
		SELECT
			i.relname,
			t.relname,
			am.amname,
			ix.indisunique,
			ix.indisprimary,
			pg_get_indexdef(i.oid),
			COALESCE(array_to_string(ARRAY(
				SELECT a.attname
				FROM unnest(ix.indkey) WITH ORDINALITY AS key(attnum, ord)
				JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key.attnum
				ORDER BY key.ord
			), ','), '')
		FROM pg_class i
		JOIN pg_index ix ON ix.indexrelid = i.oid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_am am ON am.oid = i.relam
		WHERE n.nspname = $1 AND t.relname = $2
		ORDER BY i.relname
	`, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []SchemaIndexDetail
	for rows.Next() {
		var idx SchemaIndexDetail
		var columnsCSV string
		if err := rows.Scan(&idx.Name, &idx.TableName, &idx.Method, &idx.IsUnique, &idx.IsPrimary, &idx.Definition, &columnsCSV); err != nil {
			return nil, err
		}
		idx.Columns = splitCSV(columnsCSV)
		indexes = append(indexes, idx)
	}
	return indexes, rows.Err()
}

func fetchPostgresIndexDetail(db *sql.DB, schemaName, objectName string) (SchemaIndexDetail, error) {
	rows, err := db.Query(`
		SELECT
			i.relname,
			t.relname,
			am.amname,
			ix.indisunique,
			ix.indisprimary,
			pg_get_indexdef(i.oid),
			COALESCE(array_to_string(ARRAY(
				SELECT a.attname
				FROM unnest(ix.indkey) WITH ORDINALITY AS key(attnum, ord)
				JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key.attnum
				ORDER BY key.ord
			), ','), '')
		FROM pg_class i
		JOIN pg_index ix ON ix.indexrelid = i.oid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_am am ON am.oid = i.relam
		WHERE n.nspname = $1 AND i.relname = $2
	`, schemaName, objectName)
	if err != nil {
		return SchemaIndexDetail{}, err
	}
	defer rows.Close()
	if rows.Next() {
		var idx SchemaIndexDetail
		var columnsCSV string
		if err := rows.Scan(&idx.Name, &idx.TableName, &idx.Method, &idx.IsUnique, &idx.IsPrimary, &idx.Definition, &columnsCSV); err != nil {
			return SchemaIndexDetail{}, err
		}
		idx.Columns = splitCSV(columnsCSV)
		return idx, nil
	}
	return SchemaIndexDetail{}, nil
}

func fetchPostgresConstraints(db *sql.DB, schemaName, tableName string) ([]SchemaConstraintDetail, error) {
	rows, err := db.Query(`
		SELECT
			c.conname,
			c.contype,
			pg_get_constraintdef(c.oid, true),
			COALESCE(array_to_string(ARRAY(
				SELECT a.attname
				FROM unnest(c.conkey) AS key(attnum)
				JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = key.attnum
			), ','), ''),
			COALESCE(ref.relname, '')
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		LEFT JOIN pg_class ref ON ref.oid = c.confrelid
		WHERE n.nspname = $1 AND t.relname = $2
		ORDER BY c.conname
	`, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var constraints []SchemaConstraintDetail
	for rows.Next() {
		var constraint SchemaConstraintDetail
		var constraintType, columnsCSV string
		if err := rows.Scan(&constraint.Name, &constraintType, &constraint.Definition, &columnsCSV, &constraint.ReferencedTable); err != nil {
			return nil, err
		}
		constraint.ConstraintType = postgresConstraintType(constraintType)
		constraint.Columns = splitCSV(columnsCSV)
		constraints = append(constraints, constraint)
	}
	return constraints, rows.Err()
}

func fetchPostgresTriggers(db *sql.DB, schemaName, tableName string) ([]SchemaTriggerDetail, error) {
	rows, err := db.Query(`
		SELECT t.tgname, c.relname, pg_get_triggerdef(t.oid, true)
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND NOT t.tgisinternal
		ORDER BY t.tgname
	`, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var triggers []SchemaTriggerDetail
	for rows.Next() {
		var trigger SchemaTriggerDetail
		if err := rows.Scan(&trigger.Name, &trigger.TableName, &trigger.Definition); err != nil {
			return nil, err
		}
		trigger.Timing, trigger.Events = parseTriggerDefinition(trigger.Definition)
		triggers = append(triggers, trigger)
	}
	return triggers, rows.Err()
}

func fetchPostgresTriggerDetail(db *sql.DB, schemaName, objectName string) (SchemaTriggerDetail, error) {
	var trigger SchemaTriggerDetail
	err := db.QueryRow(`
		SELECT t.tgname, c.relname, pg_get_triggerdef(t.oid, true)
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND t.tgname = $2 AND NOT t.tgisinternal
	`, schemaName, objectName).Scan(&trigger.Name, &trigger.TableName, &trigger.Definition)
	trigger.Timing, trigger.Events = parseTriggerDefinition(trigger.Definition)
	return trigger, err
}

func fetchPostgresSequenceDetail(db *sql.DB, schemaName, objectName string) (SchemaSequenceDetail, error) {
	var seq SchemaSequenceDetail
	var cycle string
	err := db.QueryRow(`
		SELECT
			sequencename,
			start_value::text,
			increment_by::text,
			min_value::text,
			max_value::text,
			cache_size::text,
			cycle::text
		FROM pg_sequences
		WHERE schemaname = $1 AND sequencename = $2
	`, schemaName, objectName).Scan(&seq.Name, &seq.StartValue, &seq.IncrementBy, &seq.MinValue, &seq.MaxValue, &seq.CacheSize, &cycle)
	if err != nil {
		return seq, err
	}
	seq.Cycle = cycle == "t" || strings.EqualFold(cycle, "true")
	_ = db.QueryRow(`
		SELECT COALESCE(format('%I.%I', n2.nspname, c2.relname) || '.' || a.attname, '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_depend d ON d.objid = c.oid AND d.deptype = 'a'
		LEFT JOIN pg_class c2 ON c2.oid = d.refobjid
		LEFT JOIN pg_namespace n2 ON n2.oid = c2.relnamespace
		LEFT JOIN pg_attribute a ON a.attrelid = c2.oid AND a.attnum = d.refobjsubid
		WHERE n.nspname = $1 AND c.relname = $2
	`, schemaName, objectName).Scan(&seq.OwnedBy)
	seq.Definition = fmt.Sprintf(
		"CREATE SEQUENCE %s.%s\n    START WITH %s\n    INCREMENT BY %s\n    MINVALUE %s\n    MAXVALUE %s\n    CACHE %s%s;",
		quoteIdent("postgres", schemaName),
		quoteIdent("postgres", seq.Name),
		seq.StartValue,
		seq.IncrementBy,
		seq.MinValue,
		seq.MaxValue,
		seq.CacheSize,
		func() string {
			if seq.Cycle {
				return "\n    CYCLE"
			}
			return "\n    NO CYCLE"
		}(),
	)
	return seq, nil
}

func fetchPostgresRoutineDetail(db *sql.DB, schemaName, routineName string) (SchemaRoutineDetail, error) {
	var routine SchemaRoutineDetail
	err := db.QueryRow(`
		SELECT
			CASE p.prokind WHEN 'p' THEN 'procedure' ELSE 'function' END,
			p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')' AS identity,
			CASE WHEN p.prokind = 'p' THEN '' ELSE pg_get_function_result(p.oid) END,
			pg_get_functiondef(p.oid)
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1
		  AND (p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')') = $2
	`, schemaName, routineName).Scan(&routine.RoutineType, &routine.Identity, &routine.ReturnType, &routine.Definition)
	routine.Name = routineName
	return routine, err
}

func fetchPostgresTypeDetail(db *sql.DB, schemaName, typeName string) ([]string, string, error) {
	rows, err := db.Query(`
		SELECT e.enumlabel
		FROM pg_type t
		JOIN pg_namespace n ON n.oid = t.typnamespace
		JOIN pg_enum e ON e.enumtypid = t.oid
		WHERE n.nspname = $1 AND t.typname = $2
		ORDER BY e.enumsortorder
	`, schemaName, typeName)
	if err != nil {
		return []string{}, "", err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err == nil {
			values = append(values, value)
		}
	}
	if len(values) > 0 {
		quoted := make([]string, 0, len(values))
		for _, value := range values {
			quoted = append(quoted, fmt.Sprintf("'%s'", strings.ReplaceAll(value, "'", "''")))
		}
		return values, fmt.Sprintf(
			"CREATE TYPE %s.%s AS ENUM (%s);",
			quoteIdent("postgres", schemaName),
			quoteIdent("postgres", typeName),
			strings.Join(quoted, ", "),
		), nil
	}
	return []string{}, fmt.Sprintf("-- custom type %s.%s", schemaName, typeName), nil
}

func buildPostgresCreateTableDDL(schemaName, tableName string, columns []SchemaColumn, constraints []SchemaConstraintDetail) string {
	lines := make([]string, 0, len(columns)+len(constraints))
	for _, col := range columns {
		line := fmt.Sprintf("    %s %s", quoteIdent("postgres", col.Name), col.DataType)
		if !col.IsNullable {
			line += " NOT NULL"
		}
		if col.DefaultValue != nil && *col.DefaultValue != "" {
			line += " DEFAULT " + *col.DefaultValue
		}
		lines = append(lines, line)
	}
	for _, constraint := range constraints {
		lines = append(lines, fmt.Sprintf("    CONSTRAINT %s %s", quoteIdent("postgres", constraint.Name), constraint.Definition))
	}
	return fmt.Sprintf("CREATE TABLE %s.%s (\n%s\n);", quoteIdent("postgres", schemaName), quoteIdent("postgres", tableName), strings.Join(lines, ",\n"))
}

func postgresConstraintType(code string) string {
	switch code {
	case "p":
		return "PRIMARY KEY"
	case "f":
		return "FOREIGN KEY"
	case "u":
		return "UNIQUE"
	case "c":
		return "CHECK"
	default:
		return code
	}
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return []string{}
	}
	parts := strings.Split(v, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func parseTriggerDefinition(definition string) (string, string) {
	upper := strings.ToUpper(definition)
	timing := ""
	switch {
	case strings.Contains(upper, " BEFORE "):
		timing = "BEFORE"
	case strings.Contains(upper, " AFTER "):
		timing = "AFTER"
	case strings.Contains(upper, " INSTEAD OF "):
		timing = "INSTEAD OF"
	}
	events := []string{}
	for _, event := range []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
		if strings.Contains(upper, event) {
			events = append(events, event)
		}
	}
	return timing, strings.Join(events, ", ")
}

func boolLabel(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func fetchMySQLShowCreate(db *sql.DB, objectType, dbName, objectName string) string {
	switch objectType {
	case "view":
		var definition sql.NullString
		_ = db.QueryRow(`
			SELECT VIEW_DEFINITION
			FROM information_schema.VIEWS
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		`, dbName, objectName).Scan(&definition)
		if definition.String == "" {
			return ""
		}
		return fmt.Sprintf("CREATE VIEW %s.%s AS\n%s", quoteIdent("mysql", dbName), quoteIdent("mysql", objectName), definition.String)
	default:
		var name, createSQL string
		_ = db.QueryRow(fmt.Sprintf("SHOW CREATE TABLE %s.%s", quoteIdent("mysql", dbName), quoteIdent("mysql", objectName))).Scan(&name, &createSQL)
		return createSQL
	}
}

func fetchMySQLIndexes(db *sql.DB, dbName, tableName string) ([]SchemaIndexDetail, error) {
	rows, err := db.Query(`
		SELECT INDEX_NAME, NON_UNIQUE, INDEX_TYPE, GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ','), TABLE_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		GROUP BY INDEX_NAME, NON_UNIQUE, INDEX_TYPE, TABLE_NAME
		ORDER BY INDEX_NAME
	`, dbName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []SchemaIndexDetail
	for rows.Next() {
		var idx SchemaIndexDetail
		var nonUnique int
		var columnsCSV sql.NullString
		if err := rows.Scan(&idx.Name, &nonUnique, &idx.Method, &columnsCSV, &idx.TableName); err != nil {
			return nil, err
		}
		idx.IsUnique = nonUnique == 0
		idx.IsPrimary = idx.Name == "PRIMARY"
		idx.Columns = splitCSV(columnsCSV.String)
		idx.Definition = buildMySQLIndexDDL(idx)
		indexes = append(indexes, idx)
	}
	return indexes, rows.Err()
}

func fetchMySQLIndexByName(db *sql.DB, dbName, indexName string) ([]SchemaIndexDetail, error) {
	rows, err := db.Query(`
		SELECT INDEX_NAME, NON_UNIQUE, INDEX_TYPE, GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ','), TABLE_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND INDEX_NAME = ?
		GROUP BY INDEX_NAME, NON_UNIQUE, INDEX_TYPE, TABLE_NAME
		ORDER BY TABLE_NAME
	`, dbName, indexName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []SchemaIndexDetail
	for rows.Next() {
		var idx SchemaIndexDetail
		var nonUnique int
		var columnsCSV sql.NullString
		if err := rows.Scan(&idx.Name, &nonUnique, &idx.Method, &columnsCSV, &idx.TableName); err != nil {
			return nil, err
		}
		idx.IsUnique = nonUnique == 0
		idx.IsPrimary = idx.Name == "PRIMARY"
		idx.Columns = splitCSV(columnsCSV.String)
		idx.Definition = buildMySQLIndexDDL(idx)
		indexes = append(indexes, idx)
	}
	return indexes, rows.Err()
}

func buildMySQLIndexDDL(idx SchemaIndexDetail) string {
	if idx.Name == "" || idx.TableName == "" {
		return ""
	}
	if idx.IsPrimary {
		return fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s);", quoteIdent("mysql", idx.TableName), strings.Join(quoteColumns(idx.Columns, "mysql"), ", "))
	}
	prefix := "CREATE INDEX"
	if idx.IsUnique {
		prefix = "CREATE UNIQUE INDEX"
	}
	return fmt.Sprintf("%s %s ON %s (%s);", prefix, quoteIdent("mysql", idx.Name), quoteIdent("mysql", idx.TableName), strings.Join(quoteColumns(idx.Columns, "mysql"), ", "))
}

func fetchMySQLConstraints(db *sql.DB, dbName, tableName string) ([]SchemaConstraintDetail, error) {
	rows, err := db.Query(`
		SELECT tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE,
		       GROUP_CONCAT(kcu.COLUMN_NAME ORDER BY kcu.ORDINAL_POSITION SEPARATOR ','),
		       COALESCE(rc.REFERENCED_TABLE_NAME, '')
		FROM information_schema.TABLE_CONSTRAINTS tc
		LEFT JOIN information_schema.KEY_COLUMN_USAGE kcu
		  ON tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
		 AND tc.TABLE_NAME = kcu.TABLE_NAME
		 AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
		LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
		  ON tc.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA
		 AND tc.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
		WHERE tc.TABLE_SCHEMA = ? AND tc.TABLE_NAME = ?
		GROUP BY tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE, rc.REFERENCED_TABLE_NAME
		ORDER BY tc.CONSTRAINT_NAME
	`, dbName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var constraints []SchemaConstraintDetail
	for rows.Next() {
		var item SchemaConstraintDetail
		var columnsCSV sql.NullString
		if err := rows.Scan(&item.Name, &item.ConstraintType, &columnsCSV, &item.ReferencedTable); err != nil {
			return nil, err
		}
		item.Columns = splitCSV(columnsCSV.String)
		item.Definition = item.ConstraintType
		if len(item.Columns) > 0 {
			item.Definition += " (" + strings.Join(item.Columns, ", ") + ")"
		}
		if item.ReferencedTable != "" {
			item.Definition += " REFERENCES " + item.ReferencedTable
		}
		constraints = append(constraints, item)
	}
	return constraints, rows.Err()
}

func fetchMySQLTriggers(db *sql.DB, dbName, tableName string) ([]SchemaTriggerDetail, error) {
	rows, err := db.Query(`
		SELECT TRIGGER_NAME, EVENT_OBJECT_TABLE, ACTION_TIMING, EVENT_MANIPULATION, ACTION_STATEMENT
		FROM information_schema.TRIGGERS
		WHERE TRIGGER_SCHEMA = ? AND EVENT_OBJECT_TABLE = ?
		ORDER BY TRIGGER_NAME
	`, dbName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var triggers []SchemaTriggerDetail
	for rows.Next() {
		var trigger SchemaTriggerDetail
		if err := rows.Scan(&trigger.Name, &trigger.TableName, &trigger.Timing, &trigger.Events, &trigger.Definition); err != nil {
			return nil, err
		}
		triggers = append(triggers, trigger)
	}
	return triggers, rows.Err()
}

func fetchMySQLTriggerDetail(db *sql.DB, dbName, objectName string) (SchemaTriggerDetail, error) {
	var trigger SchemaTriggerDetail
	err := db.QueryRow(`
		SELECT TRIGGER_NAME, EVENT_OBJECT_TABLE, ACTION_TIMING, EVENT_MANIPULATION, ACTION_STATEMENT
		FROM information_schema.TRIGGERS
		WHERE TRIGGER_SCHEMA = ? AND TRIGGER_NAME = ?
	`, dbName, objectName).Scan(&trigger.Name, &trigger.TableName, &trigger.Timing, &trigger.Events, &trigger.Definition)
	return trigger, err
}

func fetchMySQLRoutineDetail(db *sql.DB, dbName, objectType, objectName string) SchemaRoutineDetail {
	var routine SchemaRoutineDetail
	routine.Name = objectName
	routine.RoutineType = objectType
	routine.Identity = objectName
	var definition, returnType sql.NullString
	_ = db.QueryRow(`
		SELECT ROUTINE_DEFINITION, DTD_IDENTIFIER
		FROM information_schema.ROUTINES
		WHERE ROUTINE_SCHEMA = ? AND ROUTINE_NAME = ?
	`, dbName, objectName).Scan(&definition, &returnType)
	routine.ReturnType = returnType.String
	if definition.String != "" {
		header := "FUNCTION"
		if objectType == "procedure" {
			header = "PROCEDURE"
		}
		routine.Definition = fmt.Sprintf("CREATE %s %s.%s\n%s", header, quoteIdent("mysql", dbName), quoteIdent("mysql", objectName), definition.String)
	}
	return routine
}

func fetchSQLiteIndexes(db *sql.DB, tableName string) ([]SchemaIndexDetail, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA index_list(%s)`, quoteIdent("sqlite", tableName)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []SchemaIndexDetail
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}
		cols, _ := fetchSQLiteIndexColumns(db, name)
		indexes = append(indexes, SchemaIndexDetail{
			Name:       name,
			TableName:  tableName,
			Method:     origin,
			IsUnique:   unique == 1,
			IsPrimary:  origin == "pk",
			Columns:    cols,
			Definition: fetchSQLiteObjectSQL(db, "index", name),
		})
	}
	return indexes, rows.Err()
}

func fetchSQLiteIndexByName(db *sql.DB, indexName string) ([]SchemaIndexDetail, error) {
	var tableName string
	_ = db.QueryRow(`SELECT COALESCE(tbl_name, '') FROM sqlite_master WHERE type='index' AND name=?`, indexName).Scan(&tableName)
	cols, _ := fetchSQLiteIndexColumns(db, indexName)
	return []SchemaIndexDetail{{
		Name:       indexName,
		TableName:  tableName,
		Columns:    cols,
		Definition: fetchSQLiteObjectSQL(db, "index", indexName),
	}}, nil
}

func fetchSQLiteIndexColumns(db *sql.DB, indexName string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA index_info(%s)`, quoteIdent("sqlite", indexName)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var seqno, cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err == nil {
			cols = append(cols, name)
		}
	}
	return cols, rows.Err()
}

func fetchSQLiteConstraints(db *sql.DB, tableName string) ([]SchemaConstraintDetail, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA foreign_key_list(%s)`, quoteIdent("sqlite", tableName)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var constraints []SchemaConstraintDetail
	for rows.Next() {
		var id, seq int
		var refTable, fromCol, toCol, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &refTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err == nil {
			constraints = append(constraints, SchemaConstraintDetail{
				Name:            fmt.Sprintf("fk_%s_%d", tableName, id),
				ConstraintType:  "FOREIGN KEY",
				Columns:         []string{fromCol},
				ReferencedTable: refTable,
				Definition:      fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s(%s)", fromCol, refTable, toCol),
			})
		}
	}
	return constraints, rows.Err()
}

func fetchSQLiteTriggers(db *sql.DB, tableName string) ([]SchemaTriggerDetail, error) {
	rows, err := db.Query(`SELECT name, tbl_name, sql FROM sqlite_master WHERE type='trigger' AND tbl_name=? ORDER BY name`, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var triggers []SchemaTriggerDetail
	for rows.Next() {
		var trigger SchemaTriggerDetail
		if err := rows.Scan(&trigger.Name, &trigger.TableName, &trigger.Definition); err == nil {
			trigger.Timing, trigger.Events = parseTriggerDefinition(trigger.Definition)
			triggers = append(triggers, trigger)
		}
	}
	return triggers, rows.Err()
}

func fetchSQLiteObjectSQL(db *sql.DB, objectType, objectName string) string {
	var sqlText string
	_ = db.QueryRow(`SELECT COALESCE(sql, '') FROM sqlite_master WHERE type=? AND name=?`, objectType, objectName).Scan(&sqlText)
	return sqlText
}

func fetchSQLServerIndexes(db *sql.DB, tableName string) ([]SchemaIndexDetail, error) {
	rows, err := db.Query(`
		SELECT
			i.name,
			t.name,
			i.type_desc,
			i.is_unique,
			i.is_primary_key
		FROM sys.indexes i
		JOIN sys.tables t ON t.object_id = i.object_id
		WHERE t.name = @p1 AND i.name IS NOT NULL
		ORDER BY i.name
	`, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []SchemaIndexDetail
	for rows.Next() {
		var idx SchemaIndexDetail
		if err := rows.Scan(&idx.Name, &idx.TableName, &idx.Method, &idx.IsUnique, &idx.IsPrimary); err == nil {
			idx.Columns = fetchSQLServerIndexColumns(db, idx.TableName, idx.Name)
			idx.Definition = fmt.Sprintf("-- %s on %s (%s)", idx.Name, idx.TableName, strings.Join(idx.Columns, ", "))
			indexes = append(indexes, idx)
		}
	}
	return indexes, rows.Err()
}

func fetchSQLServerIndexByName(db *sql.DB, indexName string) ([]SchemaIndexDetail, error) {
	rows, err := db.Query(`
		SELECT
			i.name,
			t.name,
			i.type_desc,
			i.is_unique,
			i.is_primary_key
		FROM sys.indexes i
		JOIN sys.tables t ON t.object_id = i.object_id
		WHERE i.name = @p1
	`, indexName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []SchemaIndexDetail
	for rows.Next() {
		var idx SchemaIndexDetail
		if err := rows.Scan(&idx.Name, &idx.TableName, &idx.Method, &idx.IsUnique, &idx.IsPrimary); err == nil {
			idx.Columns = fetchSQLServerIndexColumns(db, idx.TableName, idx.Name)
			idx.Definition = fmt.Sprintf("-- %s on %s (%s)", idx.Name, idx.TableName, strings.Join(idx.Columns, ", "))
			indexes = append(indexes, idx)
		}
	}
	return indexes, rows.Err()
}

func fetchSQLServerIndexColumns(db *sql.DB, tableName, indexName string) []string {
	rows, err := db.Query(`
		SELECT c.name
		FROM sys.indexes i
		JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
		JOIN sys.tables t ON t.object_id = i.object_id
		WHERE t.name = @p1 AND i.name = @p2
		ORDER BY ic.key_ordinal
	`, tableName, indexName)
	if err != nil {
		return []string{}
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			cols = append(cols, name)
		}
	}
	return cols
}

func fetchSQLServerDefinition(db *sql.DB, objectName string) string {
	var definition sql.NullString
	_ = db.QueryRow(`SELECT OBJECT_DEFINITION(OBJECT_ID(@p1))`, objectName).Scan(&definition)
	return definition.String
}

func fetchSQLServerSequenceDetail(db *sql.DB, objectName string) (SchemaSequenceDetail, error) {
	var seq SchemaSequenceDetail
	err := db.QueryRow(`
		SELECT name,
		       CAST(start_value AS nvarchar(100)),
		       CAST(increment AS nvarchar(100)),
		       CAST(minimum_value AS nvarchar(100)),
		       CAST(maximum_value AS nvarchar(100)),
		       CAST(cache_size AS nvarchar(100)),
		       is_cycling
		FROM sys.sequences
		WHERE name = @p1
	`, objectName).Scan(&seq.Name, &seq.StartValue, &seq.IncrementBy, &seq.MinValue, &seq.MaxValue, &seq.CacheSize, &seq.Cycle)
	seq.Definition = fmt.Sprintf("CREATE SEQUENCE %s START WITH %s INCREMENT BY %s;", quoteIdent("sqlserver", seq.Name), seq.StartValue, seq.IncrementBy)
	return seq, err
}

func quoteColumns(columns []string, driver string) []string {
	result := make([]string, 0, len(columns))
	for _, column := range columns {
		result = append(result, quoteIdent(driver, column))
	}
	return result
}
