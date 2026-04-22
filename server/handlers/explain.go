package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type ExplainResult struct {
	Driver string          `json:"driver"`
	Format string          `json:"format"`
	Raw    [][]interface{} `json:"raw"`
	JSON   interface{}     `json:"json,omitempty"`
}

func ExplainQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		var req struct {
			SQL      string `json:"sql"`
			Database string `json:"database"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SQL == "" {
			http.Error(w, `{"error":"sql required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		if req.Database != "" {
			if !validIdentifier.MatchString(req.Database) {
				http.Error(w, `{"error":"invalid database name"}`, http.StatusBadRequest)
				return
			}
			switch driver {
			case "mysql":
				safeName := strings.ReplaceAll(req.Database, "`", "``")
				_, _ = db.ExecContext(r.Context(), "USE `"+safeName+"`")
			case "sqlserver":
				safeName := strings.ReplaceAll(req.Database, "]", "]]")
				_, _ = db.ExecContext(r.Context(), "USE ["+safeName+"]")
			}
		}

		result := ExplainResult{Driver: driver}

		switch driver {
		case "postgres":
			result.Format = "json"
			rows, err := db.QueryContext(r.Context(), "EXPLAIN (ANALYZE false, FORMAT JSON) "+req.SQL)
			if err != nil {
				// fallback to text
				rows2, err2 := db.QueryContext(r.Context(), "EXPLAIN "+req.SQL)
				if err2 != nil {
					http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
					return
				}
				defer rows2.Close()
				result.Format = "text"
				for rows2.Next() {
					var line string
					rows2.Scan(&line)
					result.Raw = append(result.Raw, []interface{}{line})
				}
			} else {
				defer rows.Close()
				result.Format = "json"
				var jsonStr string
				for rows.Next() {
					rows.Scan(&jsonStr)
				}
				var parsed interface{}
				if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
					result.JSON = parsed
				}
			}

		case "mysql":
			result.Format = "json"
			rows, err := db.QueryContext(r.Context(), "EXPLAIN FORMAT=JSON "+req.SQL)
			if err != nil {
				// fallback to tabular
				rows2, err2 := db.QueryContext(r.Context(), "EXPLAIN "+req.SQL)
				if err2 != nil {
					http.Error(w, jsonError(err2.Error()), http.StatusBadRequest)
					return
				}
				defer rows2.Close()
				result.Format = "table"
				cols, _ := rows2.Columns()
				result.Raw = append(result.Raw, func() []interface{} {
					r := make([]interface{}, len(cols))
					for i, c := range cols {
						r[i] = c
					}
					return r
				}())
				for rows2.Next() {
					vals := make([]interface{}, len(cols))
					ptrs := make([]interface{}, len(cols))
					for i := range vals {
						ptrs[i] = &vals[i]
					}
					rows2.Scan(ptrs...)
					row := make([]interface{}, len(vals))
					for i, v := range vals {
						if b, ok := v.([]byte); ok {
							row[i] = string(b)
						} else {
							row[i] = v
						}
					}
					result.Raw = append(result.Raw, row)
				}
			} else {
				defer rows.Close()
				result.Format = "json"
				var jsonStr string
				for rows.Next() {
					rows.Scan(&jsonStr)
				}
				var parsed interface{}
				if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
					result.JSON = parsed
				}
			}

		default:
			rows, err := db.QueryContext(r.Context(), "EXPLAIN "+req.SQL)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			defer rows.Close()
			result.Format = "text"
			for rows.Next() {
				var line string
				rows.Scan(&line)
				result.Raw = append(result.Raw, []interface{}{line})
			}
		}

		json.NewEncoder(w).Encode(result)
	}
}
