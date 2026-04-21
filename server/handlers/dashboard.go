package handlers

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	appdb "github.com/anveesa/nias/db"
)

type TableStat struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
	SizeBytes int64 `json:"size_bytes"`
}

type SlowQueryStat struct {
	SQL        string `json:"sql"`
	DurationMs int64  `json:"duration_ms"`
	RowCount   int    `json:"row_count"`
	Error      string `json:"error"`
	ExecutedAt string `json:"executed_at"`
}

type SlowQuerySummary struct {
	ThresholdMs   int64           `json:"threshold_ms"`
	Count         int             `json:"count"`
	AvgDurationMs int64           `json:"avg_duration_ms"`
	MaxDurationMs int64           `json:"max_duration_ms"`
	Queries       []SlowQueryStat `json:"queries"`
}

type DashboardData struct {
	Driver      string           `json:"driver"`
	Database    string           `json:"database"`
	Version     string           `json:"version"`
	SizeBytes   int64            `json:"size_bytes"`
	TableCount  int              `json:"table_count"`
	ViewCount   int              `json:"view_count"`
	Tables      []TableStat      `json:"tables"`
	SlowQueries SlowQuerySummary `json:"slow_queries"`
}

const slowQueryThresholdMs int64 = 1000

func loadSlowQueries(connID int64) SlowQuerySummary {
	summary := SlowQuerySummary{
		ThresholdMs: slowQueryThresholdMs,
		Queries:     []SlowQueryStat{},
	}

	var avgDuration float64
	_ = appdb.DB.QueryRow(appdb.ConvertQuery(`
		SELECT COUNT(*), COALESCE(AVG(duration_ms), 0), COALESCE(MAX(duration_ms), 0)
		FROM query_history
		WHERE conn_id = ? AND duration_ms >= ?
	`), connID, slowQueryThresholdMs).Scan(&summary.Count, &avgDuration, &summary.MaxDurationMs)
	summary.AvgDurationMs = int64(math.Round(avgDuration))

	rows, err := appdb.DB.Query(appdb.ConvertQuery(`
		SELECT sql, duration_ms, row_count, COALESCE(error, ''), executed_at
		FROM query_history
		WHERE conn_id = ? AND duration_ms >= ?
		ORDER BY duration_ms DESC, executed_at DESC
		LIMIT 5
	`), connID, slowQueryThresholdMs)
	if err != nil {
		return summary
	}
	defer rows.Close()

	for rows.Next() {
		var q SlowQueryStat
		if err := rows.Scan(&q.SQL, &q.DurationMs, &q.RowCount, &q.Error, &q.ExecutedAt); err != nil {
			continue
		}
		summary.Queries = append(summary.Queries, q)
	}

	return summary
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

		data := DashboardData{
			Driver:      driver,
			Tables:      []TableStat{},
			SlowQueries: loadSlowQueries(connID),
		}

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
