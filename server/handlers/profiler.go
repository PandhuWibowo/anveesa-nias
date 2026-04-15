package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type TopValue struct {
	Value interface{} `json:"value"`
	Count int64       `json:"count"`
}

type ProfileResult struct {
	Table       string     `json:"table"`
	Column      string     `json:"column"`
	Total       int64      `json:"total"`
	NonNull     int64      `json:"non_null"`
	NullCount   int64      `json:"null_count"`
	NullPct     float64    `json:"null_pct"`
	Distinct    int64      `json:"distinct"`
	Min         string     `json:"min"`
	Max         string     `json:"max"`
	Avg         string     `json:"avg"`
	TopValues   []TopValue `json:"top_values"`
	Histogram   []int64    `json:"histogram"`
}

func ProfileColumn() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
			return
		}

		var req struct {
			Table    string `json:"table"`
			Column   string `json:"column"`
			Database string `json:"database"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Table == "" || req.Column == "" {
			http.Error(w, `{"error":"table and column required"}`, http.StatusBadRequest)
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

	tbl := quoteIdent(driver, req.Table)
	col := quoteIdent(driver, req.Column)

		result := ProfileResult{Table: req.Table, Column: req.Column}

		// Total + non-null + distinct
		row := db.QueryRowContext(r.Context(), fmt.Sprintf(
			`SELECT COUNT(*), COUNT(%s), COUNT(DISTINCT %s) FROM %s`, col, col, tbl,
		))
		row.Scan(&result.Total, &result.NonNull, &result.Distinct)
		result.NullCount = result.Total - result.NonNull
		if result.Total > 0 {
			result.NullPct = float64(result.NullCount) / float64(result.Total) * 100
		}

		// Min / Max / Avg — cast to text for universal support
		minRow := db.QueryRowContext(r.Context(), fmt.Sprintf(`SELECT MIN(%s), MAX(%s) FROM %s`, col, col, tbl))
		var minVal, maxVal interface{}
		minRow.Scan(&minVal, &maxVal)
		result.Min = nullStr(minVal)
		result.Max = nullStr(maxVal)

		// Avg (numeric columns only — ignore errors)
		var avgVal interface{}
		switch driver {
		case "postgres":
			db.QueryRowContext(r.Context(), fmt.Sprintf(`SELECT AVG(%s::numeric) FROM %s`, col, tbl)).Scan(&avgVal)
		default:
			db.QueryRowContext(r.Context(), fmt.Sprintf(`SELECT AVG(CAST(%s AS DOUBLE)) FROM %s`, col, tbl)).Scan(&avgVal)
		}
		result.Avg = nullStr(avgVal)

		// Top 10 values
		topQ := fmt.Sprintf(`SELECT %s, COUNT(*) FROM %s GROUP BY %s ORDER BY COUNT(*) DESC LIMIT 10`, col, tbl, col)
		topRows, err := db.QueryContext(r.Context(), topQ)
		if err == nil {
			defer topRows.Close()
			for topRows.Next() {
				var val interface{}
				var cnt int64
				topRows.Scan(&val, &cnt)
				result.TopValues = append(result.TopValues, TopValue{Value: nullStr(val), Count: cnt})
			}
		}

		// Histogram (10 buckets — numeric only, best effort)
		result.Histogram = buildHistogram(db, driver, tbl, col)

		json.NewEncoder(w).Encode(result)
	}
}

func buildHistogram(db *sql.DB, driver, tbl, col string) []int64 {
	var minV, maxV float64
	row := db.QueryRow(fmt.Sprintf(`SELECT MIN(CAST(%s AS DOUBLE)), MAX(CAST(%s AS DOUBLE)) FROM %s`, col, col, tbl))
	if err := row.Scan(&minV, &maxV); err != nil || minV == maxV {
		return nil
	}
	buckets := 10
	step := (maxV - minV) / float64(buckets)
	counts := make([]int64, buckets)
	for i := 0; i < buckets; i++ {
		lo := minV + float64(i)*step
		hi := lo + step
		var cnt int64
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE CAST(%s AS DOUBLE) >= %f AND CAST(%s AS DOUBLE) < %f`, tbl, col, lo, col, hi)
		db.QueryRow(q).Scan(&cnt)
		counts[i] = cnt
	}
	return counts
}

func nullStr(v interface{}) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}
