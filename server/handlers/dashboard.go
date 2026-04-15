package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type TableStat struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
	SizeBytes int64 `json:"size_bytes"`
}

type DashboardData struct {
	Driver      string      `json:"driver"`
	Database    string      `json:"database"`
	Version     string      `json:"version"`
	SizeBytes   int64       `json:"size_bytes"`
	TableCount  int         `json:"table_count"`
	ViewCount   int         `json:"view_count"`
	Tables      []TableStat `json:"tables"`
}

func GetDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
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

		data := DashboardData{Driver: driver, Tables: []TableStat{}}

		switch driver {
		case "postgres":
			db.QueryRow(`SELECT current_database()`).Scan(&data.Database)
			db.QueryRow(`SELECT version()`).Scan(&data.Version)
			db.QueryRow(`SELECT pg_database_size(current_database())`).Scan(&data.SizeBytes)
			db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`).Scan(&data.TableCount)
			db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='VIEW'`).Scan(&data.ViewCount)

			rows, err := db.Query(`
				SELECT relname, n_live_tup, pg_total_relation_size(quote_ident(relname))
				FROM pg_stat_user_tables
				ORDER BY pg_total_relation_size(quote_ident(relname)) DESC
				LIMIT 20
			`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var s TableStat
					rows.Scan(&s.Name, &s.RowCount, &s.SizeBytes)
					data.Tables = append(data.Tables, s)
				}
			}

		case "mysql":
			db.QueryRow(`SELECT DATABASE()`).Scan(&data.Database)
			db.QueryRow(`SELECT VERSION()`).Scan(&data.Version)
			db.QueryRow(`SELECT COALESCE(SUM(DATA_LENGTH+INDEX_LENGTH),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()`).Scan(&data.SizeBytes)
			db.QueryRow(`SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE'`).Scan(&data.TableCount)
			db.QueryRow(`SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='VIEW'`).Scan(&data.ViewCount)

			rows, err := db.Query(`
				SELECT TABLE_NAME, COALESCE(TABLE_ROWS,0), COALESCE(DATA_LENGTH+INDEX_LENGTH,0)
				FROM information_schema.TABLES
				WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE'
				ORDER BY DATA_LENGTH+INDEX_LENGTH DESC LIMIT 20
			`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var s TableStat
					rows.Scan(&s.Name, &s.RowCount, &s.SizeBytes)
					data.Tables = append(data.Tables, s)
				}
			}

		case "sqlite":
			data.Database = "main"
			db.QueryRow(`SELECT sqlite_version()`).Scan(&data.Version)
			var pageCount, pageSize int64
			db.QueryRow(`PRAGMA page_count`).Scan(&pageCount)
			db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
			data.SizeBytes = pageCount * pageSize

			tRows, err := db.Query(`SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY name`)
			if err == nil {
				defer tRows.Close()
				var tables []string
				for tRows.Next() {
					var name, tType string
					tRows.Scan(&name, &tType)
					if tType == "table" {
						data.TableCount++
						tables = append(tables, name)
					} else {
						data.ViewCount++
					}
				}
				tRows.Close()
				for _, t := range tables {
					var count int64
					db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, t)).Scan(&count)
					data.Tables = append(data.Tables, TableStat{Name: t, RowCount: count})
				}
			}

		case "sqlserver":
			db.QueryRow(`SELECT DB_NAME()`).Scan(&data.Database)
			db.QueryRow(`SELECT @@VERSION`).Scan(&data.Version)
			db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_TYPE='BASE TABLE'`).Scan(&data.TableCount)
			db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.VIEWS`).Scan(&data.ViewCount)

			rows, err := db.Query(`
				SELECT t.NAME, p.rows, SUM(a.total_pages)*8192
				FROM sys.tables t
				INNER JOIN sys.indexes i ON t.OBJECT_ID = i.object_id
				INNER JOIN sys.partitions p ON i.object_id = p.OBJECT_ID AND i.index_id = p.index_id
				INNER JOIN sys.allocation_units a ON p.partition_id = a.container_id
				GROUP BY t.NAME, p.Rows
				ORDER BY SUM(a.total_pages) DESC
				OFFSET 0 ROWS FETCH NEXT 20 ROWS ONLY
			`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var s TableStat
					rows.Scan(&s.Name, &s.RowCount, &s.SizeBytes)
					data.Tables = append(data.Tables, s)
				}
			}
		}

		json.NewEncoder(w).Encode(data)
	}
}
