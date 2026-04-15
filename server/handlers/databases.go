package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func ListDatabases() http.HandlerFunc {
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

		var dbs []string
		switch driver {
		case "postgres":
			rows, err := db.QueryContext(r.Context(), `SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var name string
					rows.Scan(&name)
					dbs = append(dbs, name)
				}
			}
		case "mysql", "mariadb":
			rows, err := db.QueryContext(r.Context(), `SHOW DATABASES`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var name string
					rows.Scan(&name)
					dbs = append(dbs, name)
				}
			}
		case "sqlserver":
			rows, err := db.QueryContext(r.Context(), `SELECT name FROM sys.databases WHERE state = 0 ORDER BY name`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var name string
					rows.Scan(&name)
					dbs = append(dbs, name)
				}
			}
		default:
			dbs = []string{"main"}
		}

		if dbs == nil {
			dbs = []string{}
		}
		json.NewEncoder(w).Encode(dbs)
	}
}
