package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

type ForeignKey struct {
	ConstraintName string `json:"constraint_name"`
	TableName      string `json:"table_name"`
	ColumnName     string `json:"column_name"`
	RefTableName   string `json:"ref_table_name"`
	RefColumnName  string `json:"ref_column_name"`
}

type ERTable struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Columns []SchemaColumn `json:"columns"`
}

type ERDiagram struct {
	Tables      []ERTable    `json:"tables"`
	ForeignKeys []ForeignKey `json:"foreign_keys"`
}

// GetERDiagram returns tables + columns + FK relationships for SVG rendering.
// Path: /api/connections/{id}/er/{db}
// Also accepts /api/connections/{id}/er and falls back to the connection's configured database.
func GetERDiagram() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		if len(parts) < 2 {
			http.Error(w, `{"error":"path must be /api/connections/{id}/er/{db}"}`, http.StatusBadRequest)
			return
		}
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		var dbName string
		if len(parts) >= 3 {
			dbName, _ = url.PathUnescape(strings.Join(parts[2:], "/"))
		}
		if strings.TrimSpace(dbName) == "" {
			err = appdb.DB.QueryRow(appdb.ConvertQuery(`SELECT database FROM connections WHERE id = ?`), connID).Scan(&dbName)
			if err != nil || strings.TrimSpace(dbName) == "" {
				http.Error(w, `{"error":"database name is required"}`, http.StatusBadRequest)
				return
			}
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		diagram := ERDiagram{Tables: []ERTable{}, ForeignKeys: []ForeignKey{}}

		switch driver {
		case "postgres":
			tRows, err := db.Query(`
				SELECT table_name, table_type
				FROM information_schema.tables
				WHERE table_catalog = $1 AND table_schema = 'public'
				ORDER BY table_name
			`, dbName)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			for tRows.Next() {
				var name, tType string
				tRows.Scan(&name, &tType)
				t := ERTable{Name: name, Type: "table"}
				if tType == "VIEW" {
					t.Type = "view"
				}
				diagram.Tables = append(diagram.Tables, t)
			}
			tRows.Close()

			for i, t := range diagram.Tables {
				cRows, err := db.Query(`
					SELECT c.column_name, c.data_type, c.is_nullable,
						CASE WHEN kcu.column_name IS NOT NULL THEN true ELSE false END,
						c.column_default
					FROM information_schema.columns c
					LEFT JOIN information_schema.key_column_usage kcu
						ON kcu.table_name = c.table_name AND kcu.column_name = c.column_name
						AND kcu.constraint_name IN (
							SELECT constraint_name FROM information_schema.table_constraints
							WHERE constraint_type = 'PRIMARY KEY' AND table_name = $1
						)
					WHERE c.table_catalog = $2 AND c.table_name = $1 AND c.table_schema = 'public'
					ORDER BY c.ordinal_position
				`, t.Name, dbName)
				if err == nil {
					for cRows.Next() {
						var col SchemaColumn
						var nullable, pk string
						var defVal *string
						cRows.Scan(&col.Name, &col.DataType, &nullable, &pk, &defVal)
						col.IsNullable = nullable == "YES"
						col.IsPrimaryKey = pk == "true" || pk == "1"
						col.DefaultValue = defVal
						diagram.Tables[i].Columns = append(diagram.Tables[i].Columns, col)
					}
					cRows.Close()
				}
			}

			fkRows, err := db.Query(`
				SELECT tc.constraint_name, kcu.table_name, kcu.column_name,
					ccu.table_name, ccu.column_name
				FROM information_schema.table_constraints tc
				JOIN information_schema.key_column_usage kcu
					ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
				JOIN information_schema.constraint_column_usage ccu
					ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
				WHERE tc.constraint_type = 'FOREIGN KEY'
					AND tc.table_catalog = $1 AND tc.table_schema = 'public'
			`, dbName)
			if err == nil {
				defer fkRows.Close()
				for fkRows.Next() {
					var fk ForeignKey
					fkRows.Scan(&fk.ConstraintName, &fk.TableName, &fk.ColumnName, &fk.RefTableName, &fk.RefColumnName)
					diagram.ForeignKeys = append(diagram.ForeignKeys, fk)
				}
			}

		case "mysql":
			tRows, err := db.Query(`
				SELECT TABLE_NAME, TABLE_TYPE FROM information_schema.TABLES
				WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME
			`, dbName)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			for tRows.Next() {
				var name, tType string
				tRows.Scan(&name, &tType)
				t := ERTable{Name: name, Type: "table"}
				if tType == "VIEW" {
					t.Type = "view"
				}
				diagram.Tables = append(diagram.Tables, t)
			}
			tRows.Close()

			for i, t := range diagram.Tables {
				cRows, err := db.Query(`
					SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT
					FROM information_schema.COLUMNS
					WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
					ORDER BY ORDINAL_POSITION
				`, dbName, t.Name)
				if err == nil {
					for cRows.Next() {
						var col SchemaColumn
						var nullable, key string
						var defVal *string
						cRows.Scan(&col.Name, &col.DataType, &nullable, &key, &defVal)
						col.IsNullable = nullable == "YES"
						col.IsPrimaryKey = key == "PRI"
						col.DefaultValue = defVal
						diagram.Tables[i].Columns = append(diagram.Tables[i].Columns, col)
					}
					cRows.Close()
				}
			}

			fkRows, err := db.Query(`
				SELECT CONSTRAINT_NAME, TABLE_NAME, COLUMN_NAME,
					REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
				FROM information_schema.KEY_COLUMN_USAGE
				WHERE TABLE_SCHEMA = ? AND REFERENCED_TABLE_NAME IS NOT NULL
			`, dbName)
			if err == nil {
				defer fkRows.Close()
				for fkRows.Next() {
					var fk ForeignKey
					fkRows.Scan(&fk.ConstraintName, &fk.TableName, &fk.ColumnName, &fk.RefTableName, &fk.RefColumnName)
					diagram.ForeignKeys = append(diagram.ForeignKeys, fk)
				}
			}

		case "sqlite":
			tRows, err := db.Query(`SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY name`)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
				return
			}
			for tRows.Next() {
				var name, tType string
				tRows.Scan(&name, &tType)
				diagram.Tables = append(diagram.Tables, ERTable{Name: name, Type: tType})
			}
			tRows.Close()

			for i, t := range diagram.Tables {
				cRows, err := db.Query(`PRAGMA table_info("` + t.Name + `")`)
				if err == nil {
					for cRows.Next() {
						var cid, notNull, pk int
						var name, typeName string
						var dflt *string
						cRows.Scan(&cid, &name, &typeName, &notNull, &dflt, &pk)
						diagram.Tables[i].Columns = append(diagram.Tables[i].Columns, SchemaColumn{
							Name: name, DataType: typeName,
							IsNullable: notNull == 0, IsPrimaryKey: pk > 0, DefaultValue: dflt,
						})
					}
					cRows.Close()
				}
				fkRows, err := db.Query(`PRAGMA foreign_key_list("` + t.Name + `")`)
				if err == nil {
					for fkRows.Next() {
						var id, seq int
						var refTable, from, to, onUpdate, onDelete, match string
						fkRows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match)
						diagram.ForeignKeys = append(diagram.ForeignKeys, ForeignKey{
							ConstraintName: t.Name + "_" + from + "_fk",
							TableName:      t.Name,
							ColumnName:     from,
							RefTableName:   refTable,
							RefColumnName:  to,
						})
					}
					fkRows.Close()
				}
			}
		}

		json.NewEncoder(w).Encode(diagram)
	}
}
