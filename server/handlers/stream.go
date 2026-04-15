package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type StreamMeta struct {
	Columns []string `json:"columns"`
}

type StreamRow struct {
	Row []interface{} `json:"row"`
}

type StreamDone struct {
	Done       bool  `json:"done"`
	RowCount   int   `json:"row_count"`
	DurationMs int64 `json:"duration_ms"`
}

type StreamError struct {
	Error string `json:"error"`
}

// StreamQuery handles POST /api/connections/{id}/query/stream
// Responds with text/event-stream (SSE over POST via fetch ReadableStream)
func StreamQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/connections/"), "/")
		connID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			sendSSE(w, StreamError{"invalid connection id"})
			return
		}

		var req QueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SQL) == "" {
			sendSSE(w, StreamError{"sql required"})
			return
		}

		db, driver, err := GetDB(connID)
		if err != nil {
			sendSSE(w, StreamError{err.Error()})
			return
		}

		if req.Database != "" {
			switch driver {
			case "mysql":
				db.ExecContext(r.Context(), "USE `"+req.Database+"`")
			case "sqlserver":
				db.ExecContext(r.Context(), "USE ["+req.Database+"]")
			}
		}

		start := time.Now()
		rows, err := db.QueryContext(r.Context(), req.SQL)
		if err != nil {
			sendSSE(w, StreamError{err.Error()})
			return
		}
		defer rows.Close()

		cols, _ := rows.Columns()
		sendSSE(w, StreamMeta{Columns: cols})
		flushSSE(w)

		rowCount := 0
		for rows.Next() {
			select {
			case <-r.Context().Done():
				return
			default:
			}

			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				continue
			}
			row := make([]interface{}, len(cols))
			for i, v := range vals {
				if b, ok := v.([]byte); ok {
					row[i] = string(b)
				} else {
					row[i] = v
				}
			}
			sendSSE(w, StreamRow{Row: row})
			rowCount++
			if rowCount%100 == 0 {
				flushSSE(w)
			}
		}

		sendSSE(w, StreamDone{Done: true, RowCount: rowCount, DurationMs: time.Since(start).Milliseconds()})
		flushSSE(w)
	}
}

func sendSSE(w http.ResponseWriter, v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func flushSSE(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
