package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// buildTSFilter returns an ES range filter for @timestamp.
// If fromStr/toStr are provided (ISO-8601) they take priority over the relative rangeStr.
func buildTSFilter(rangeStr, fromStr, toStr string) map[string]any {
	if fromStr != "" && toStr != "" {
		return map[string]any{"gte": fromStr, "lte": toStr}
	}
	return map[string]any{"gte": fmt.Sprintf("now-%s", rangeStr), "lte": "now"}
}

// intervalForCustomRange picks a date_histogram fixed_interval based on wall-clock duration.
func intervalForCustomRange(fromStr, toStr string) string {
	from, err1 := time.Parse(time.RFC3339, fromStr)
	to, err2 := time.Parse(time.RFC3339, toStr)
	if err1 != nil || err2 != nil {
		// Try without seconds precision
		from, err1 = time.Parse("2006-01-02T15:04", fromStr)
		to, err2 = time.Parse("2006-01-02T15:04", toStr)
	}
	if err1 != nil || err2 != nil {
		return "5m"
	}
	dur := to.Sub(from)
	switch {
	case dur <= 30*time.Minute:
		return "1m"
	case dur <= 2*time.Hour:
		return "2m"
	case dur <= 4*time.Hour:
		return "5m"
	case dur <= 8*time.Hour:
		return "10m"
	case dur <= 24*time.Hour:
		return "30m"
	case dur <= 3*24*time.Hour:
		return "1h"
	case dur <= 7*24*time.Hour:
		return "3h"
	default:
		return "12h"
	}
}

const searchCacheTTLMetrics = 15 * time.Second

// ── Node Stats ─────────────────────────────────────────────────────────────

// SearchMetricsNodes returns per-node OS, JVM, process and indices stats.
func SearchMetricsNodes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "metrics:nodes")
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var raw map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, "/_nodes/stats/os,jvm,process,indices,fs", nil, &raw); err != nil {
			http.Error(w, jsonError("nodes/stats failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(raw)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLMetrics)
		w.Write(out)
	}
}

// ── Cluster Stats ──────────────────────────────────────────────────────────

func SearchMetricsCluster() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "metrics:cluster")
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		type clusterOut struct {
			Health      map[string]any   `json:"health"`
			Stats       map[string]any   `json:"stats"`
			NodesCat    []map[string]any `json:"nodes_cat"`
			IndicesCat  []map[string]any `json:"indices_cat"`
		}

		var health map[string]any
		client.doJSON(r.Context(), http.MethodGet, "/_cluster/health", nil, &health)

		var stats map[string]any
		client.doJSON(r.Context(), http.MethodGet, "/_cluster/stats?human", nil, &stats)

		catFields := "name,cpu,ram.percent,heap.percent,disk.used_percent,load_1m,uptime,node.role,master"
		var nodesCat []map[string]any
		client.doJSON(r.Context(), http.MethodGet,
			"/_cat/nodes?format=json&h="+url.QueryEscape(catFields), nil, &nodesCat)

		idxFields := "index,docs.count,store.size,indexing.index_total,search.query_total,pri,rep,status"
		var idxCat []map[string]any
		client.doJSON(r.Context(), http.MethodGet,
			"/_cat/indices?format=json&h="+url.QueryEscape(idxFields)+"&s=store.size:desc", nil, &idxCat)

		out, _ := json.Marshal(clusterOut{
			Health:     health,
			Stats:      stats,
			NodesCat:   nodesCat,
			IndicesCat: idxCat,
		})
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLMetrics)
		w.Write(out)
	}
}

// ── Index-level I/O stats ──────────────────────────────────────────────────

func SearchMetricsIndexStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		index := strings.Trim(r.URL.Query().Get("index"), "/ ")
		if index == "" {
			index = "*"
		}
		cacheKey := searchCacheKey(connID, "metrics:idx:"+index)
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		path := fmt.Sprintf("/%s/_stats/indexing,search,store,docs?level=indices", url.PathEscape(index))
		var raw map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &raw); err != nil {
			http.Error(w, jsonError("index stats failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(raw)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLMetrics)
		w.Write(out)
	}
}

// ── Infrastructure Inventory ───────────────────────────────────────────────
// Replicates Kibana's Observability → Infrastructure → Inventory view.
// Queries Metricbeat / Elastic Agent metrics indices to produce per-host
// (or per-pod / per-container) Last-1m, Avg, Max values for a chosen metric.

func SearchMetricsInventory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		index := strings.TrimSpace(r.URL.Query().Get("index"))
		metricField := strings.TrimSpace(r.URL.Query().Get("field"))
		groupField := strings.TrimSpace(r.URL.Query().Get("group"))
		rangeStr := r.URL.Query().Get("range")
		fromStr := strings.TrimSpace(r.URL.Query().Get("from"))
		toStr := strings.TrimSpace(r.URL.Query().Get("to"))
		search := strings.TrimSpace(r.URL.Query().Get("q"))

		if index == "" {
			index = "metricbeat-*,.ds-metrics-*"
		}
		if metricField == "" {
			metricField = "system.cpu.total.pct"
		}
		if groupField == "" {
			groupField = "host.name"
		}
		if rangeStr == "" {
			rangeStr = "5m"
		}

		tsFilter := buildTSFilter(rangeStr, fromStr, toStr)

		// Build the bool filter
		filters := []map[string]any{
			{"range": map[string]any{"@timestamp": tsFilter}},
			{"exists": map[string]any{"field": metricField}},
		}
		if search != "" {
			filters = append(filters, map[string]any{
				"wildcard": map[string]any{groupField: map[string]any{"value": "*" + search + "*"}},
			})
		}

		body, _ := json.Marshal(map[string]any{
			"size": 0,
			"query": map[string]any{
				"bool": map[string]any{"filter": filters},
			},
			"aggs": map[string]any{
				"hosts": map[string]any{
					"terms": map[string]any{
						"field": groupField,
						"size":  500,
						"order": map[string]any{"last_1m>metric_last": "desc"},
					},
					"aggs": map[string]any{
						// Last 1 minute value
						"last_1m": map[string]any{
							"filter": map[string]any{
								"range": map[string]any{
									"@timestamp": map[string]any{"gte": "now-1m"},
								},
							},
							"aggs": map[string]any{
								"metric_last": map[string]any{"avg": map[string]any{"field": metricField}},
							},
						},
						// Average over the full range
						"metric_avg": map[string]any{"avg": map[string]any{"field": metricField}},
						// Maximum over the full range
						"metric_max": map[string]any{"max": map[string]any{"field": metricField}},
						// Latest timestamp to know when data was last seen
						"latest_ts": map[string]any{"max": map[string]any{"field": "@timestamp"}},
					},
				},
			},
		})

		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var raw map[string]any
		path := fmt.Sprintf("/%s/_search", url.PathEscape(index))
		if err := client.doJSON(r.Context(), http.MethodPost, path, body, &raw); err != nil {
			http.Error(w, jsonError("inventory query failed: "+err.Error()), http.StatusBadGateway)
			return
		}

		// Transform into a flat list for the frontend
		type HostRow struct {
			Name     string   `json:"name"`
			Last1m   *float64 `json:"last_1m"`
			Avg      *float64 `json:"avg"`
			Max      *float64 `json:"max"`
			LatestTs string   `json:"latest_ts"`
		}

		var rows []HostRow
		if aggs, ok := raw["aggregations"].(map[string]any); ok {
			if hosts, ok := aggs["hosts"].(map[string]any); ok {
				if buckets, ok := hosts["buckets"].([]any); ok {
					for _, b := range buckets {
						bucket, _ := b.(map[string]any)
						name, _ := bucket["key"].(string)

						toFloat := func(path ...string) *float64 {
							cur := bucket
							for i, k := range path {
								if i == len(path)-1 {
									if val, ok := cur[k].(float64); ok {
										v := val
										return &v
									}
									return nil
								}
								next, _ := cur[k].(map[string]any)
								cur = next
							}
							return nil
						}

						latestTs := ""
						if ts, ok := bucket["latest_ts"].(map[string]any); ok {
							if s, ok := ts["value_as_string"].(string); ok {
								latestTs = s
							}
						}

						rows = append(rows, HostRow{
							Name:     name,
							Last1m:   toFloat("last_1m", "metric_last", "value"),
							Avg:      toFloat("metric_avg", "value"),
							Max:      toFloat("metric_max", "value"),
							LatestTs: latestTs,
						})
					}
				}
			}
		}

		if rows == nil {
			rows = []HostRow{}
		}

		type response struct {
			Rows  []HostRow `json:"rows"`
			Total int       `json:"total"`
		}
		out, _ := json.Marshal(response{Rows: rows, Total: len(rows)})
		w.Write(out)
	}
}

// ── Host Detail — multi-chart time series ──────────────────────────────────
// Uses _msearch to batch all chart queries in a single round-trip.

var hostMetricGroups = []struct {
	Category string
	Charts   []struct {
		Key    string
		Label  string
		Fields []struct{ Key, Field, Agg, Unit string }
	}
}{
	{
		Category: "CPU",
		Charts: []struct {
			Key    string
			Label  string
			Fields []struct{ Key, Field, Agg, Unit string }
		}{
			{
				Key: "cpu_total", Label: "CPU Usage",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"total", "system.cpu.total.pct", "avg", "%"},
				},
			},
			{
				Key: "cpu_detail", Label: "CPU Breakdown",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"user", "system.cpu.user.pct", "avg", "%"},
					{"system", "system.cpu.system.pct", "avg", "%"},
					{"iowait", "system.cpu.iowait.pct", "avg", "%"},
					{"nice", "system.cpu.nice.pct", "avg", "%"},
					{"irq", "system.cpu.irq.pct", "avg", "%"},
					{"steal", "system.cpu.steal.pct", "avg", "%"},
				},
			},
			{
				Key: "load", Label: "Load",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"load_1m", "system.load.1", "avg", ""},
					{"load_5m", "system.load.5", "avg", ""},
					{"load_15m", "system.load.15", "avg", ""},
				},
			},
			{
				Key: "load_norm", Label: "Normalized Load",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"norm_1m", "system.load.norm.1", "avg", ""},
					{"norm_5m", "system.load.norm.5", "avg", ""},
					{"norm_15m", "system.load.norm.15", "avg", ""},
				},
			},
		},
	},
	{
		Category: "Memory",
		Charts: []struct {
			Key    string
			Label  string
			Fields []struct{ Key, Field, Agg, Unit string }
		}{
			{
				Key: "mem_pct", Label: "Memory Usage %",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"used_pct", "system.memory.actual.used.pct", "avg", "%"},
				},
			},
			{
				Key: "mem_bytes", Label: "Memory Usage",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"used", "system.memory.actual.used.bytes", "avg", "B"},
					{"free", "system.memory.free", "avg", "B"},
				},
			},
			{
				Key: "swap_pct", Label: "Swap Usage %",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"swap_used", "system.memory.swap.used.pct", "avg", "%"},
				},
			},
		},
	},
	{
		Category: "Network",
		Charts: []struct {
			Key    string
			Label  string
			Fields []struct{ Key, Field, Agg, Unit string }
		}{
			{
				Key: "net_in", Label: "Inbound Traffic",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"in_bytes", "system.network.in.bytes", "avg", "B/s"},
				},
			},
			{
				Key: "net_out", Label: "Outbound Traffic",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"out_bytes", "system.network.out.bytes", "avg", "B/s"},
				},
			},
			{
				Key: "net_errors", Label: "Network Errors",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"in_errors", "system.network.in.errors", "sum", ""},
					{"out_errors", "system.network.out.errors", "sum", ""},
				},
			},
		},
	},
	{
		Category: "Disk",
		Charts: []struct {
			Key    string
			Label  string
			Fields []struct{ Key, Field, Agg, Unit string }
		}{
			{
				Key: "disk_pct", Label: "Disk Usage %",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"used_pct", "system.filesystem.used.pct", "avg", "%"},
				},
			},
			{
				Key: "disk_io_read", Label: "Disk Read",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"read_bytes", "system.diskio.read.bytes", "avg", "B/s"},
				},
			},
			{
				Key: "disk_io_write", Label: "Disk Write",
				Fields: []struct{ Key, Field, Agg, Unit string }{
					{"write_bytes", "system.diskio.write.bytes", "avg", "B/s"},
				},
			},
		},
	},
}

func SearchMetricsHostDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		hostName := strings.TrimSpace(r.URL.Query().Get("host"))
		groupField := strings.TrimSpace(r.URL.Query().Get("group"))
		index := strings.TrimSpace(r.URL.Query().Get("index"))
		rangeStr := r.URL.Query().Get("range")
		fromStr := strings.TrimSpace(r.URL.Query().Get("from"))
		toStr := strings.TrimSpace(r.URL.Query().Get("to"))
		category := strings.TrimSpace(r.URL.Query().Get("category"))

		if hostName == "" {
			http.Error(w, jsonError("host is required"), http.StatusBadRequest)
			return
		}
		if groupField == "" {
			groupField = "host.name"
		}
		if index == "" {
			index = "metricbeat-*"
		}
		if rangeStr == "" {
			rangeStr = "1h"
		}

		var interval string
		if fromStr != "" && toStr != "" {
			interval = intervalForCustomRange(fromStr, toStr)
		} else {
			intervalMap := map[string]string{
				"15m": "30s", "30m": "1m", "1h": "1m", "3h": "5m",
				"6h": "5m", "12h": "10m", "24h": "30m", "7d": "3h",
			}
			var ok bool
			interval, ok = intervalMap[rangeStr]
			if !ok {
				interval = "1m"
			}
		}

		tsFilter := buildTSFilter(rangeStr, fromStr, toStr)

		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		// Filter which groups to query
		var groups []struct {
			Category string
			Charts   []struct {
				Key    string
				Label  string
				Fields []struct{ Key, Field, Agg, Unit string }
			}
		}
		for _, g := range hostMetricGroups {
			if category == "" || strings.EqualFold(g.Category, category) {
				groups = append(groups, g)
			}
		}

		// Build _msearch body — one query per chart
		type chartQuery struct {
			GroupCat   string
			ChartKey   string
			ChartLabel string
			Fields     []struct{ Key, Field, Agg, Unit string }
		}
		var queries []chartQuery
		for _, grp := range groups {
			for _, ch := range grp.Charts {
				queries = append(queries, chartQuery{
					GroupCat:   grp.Category,
					ChartKey:   ch.Key,
					ChartLabel: ch.Label,
					Fields:     ch.Fields,
				})
			}
		}

		// Build msearch payload (NDJSON)
		var msearchBuf strings.Builder
		indexHeader, _ := json.Marshal(map[string]any{"index": index})
		baseFilter := []map[string]any{
			{"range": map[string]any{"@timestamp": tsFilter}},
			{"term": map[string]any{groupField: hostName}},
		}
		extBoundsMin := tsFilter["gte"]
		extBoundsMax := tsFilter["lte"]

		for _, q := range queries {
			// Build per-field sub-aggs
			subAggs := map[string]any{}
			for _, f := range q.Fields {
				subAggs[f.Key] = map[string]any{f.Agg: map[string]any{"field": f.Field}}
			}
			body, _ := json.Marshal(map[string]any{
				"size": 0,
				"query": map[string]any{
					"bool": map[string]any{"filter": baseFilter},
				},
				"aggs": map[string]any{
					"over_time": map[string]any{
						"date_histogram": map[string]any{
							"field":          "@timestamp",
							"fixed_interval": interval,
							"min_doc_count":  0,
							"extended_bounds": map[string]any{
								"min": extBoundsMin, "max": extBoundsMax,
							},
						},
						"aggs": subAggs,
					},
				},
			})
			msearchBuf.Write(indexHeader)
			msearchBuf.WriteByte('\n')
			msearchBuf.Write(body)
			msearchBuf.WriteByte('\n')
		}

		var msearchResult map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, "/_msearch",
			[]byte(msearchBuf.String()), &msearchResult); err != nil {
			http.Error(w, jsonError("msearch failed: "+err.Error()), http.StatusBadGateway)
			return
		}

		// Parse responses
		type Bucket struct {
			Key     int64              `json:"key"`
			KeyStr  string             `json:"key_as_string"`
			Values  map[string]float64 `json:"values"`
		}
		type ChartResult struct {
			Category string   `json:"category"`
			Key      string   `json:"key"`
			Label    string   `json:"label"`
			Fields   []struct {
				Key   string `json:"key"`
				Unit  string `json:"unit"`
			} `json:"fields"`
			Buckets []map[string]any `json:"buckets"`
		}

		var charts []ChartResult
		if responses, ok := msearchResult["responses"].([]any); ok {
			for i, resp := range responses {
				if i >= len(queries) {
					break
				}
				q := queries[i]
				respMap, _ := resp.(map[string]any)
				var buckets []map[string]any
				if aggs, ok := respMap["aggregations"].(map[string]any); ok {
					if ot, ok := aggs["over_time"].(map[string]any); ok {
						if bkts, ok := ot["buckets"].([]any); ok {
							for _, b := range bkts {
								bm, _ := b.(map[string]any)
								// Flatten sub-agg values into the bucket
								flat := map[string]any{
									"key":            bm["key"],
									"key_as_string":  bm["key_as_string"],
									"doc_count":      bm["doc_count"],
								}
								for _, f := range q.Fields {
									if sub, ok := bm[f.Key].(map[string]any); ok {
										flat[f.Key] = sub["value"]
									}
								}
								buckets = append(buckets, flat)
							}
						}
					}
				}

				var fieldMeta []struct {
					Key  string `json:"key"`
					Unit string `json:"unit"`
				}
				for _, f := range q.Fields {
					fieldMeta = append(fieldMeta, struct {
						Key  string `json:"key"`
						Unit string `json:"unit"`
					}{f.Key, f.Unit})
				}

				if buckets == nil {
					buckets = []map[string]any{}
				}
				charts = append(charts, ChartResult{
					Category: q.GroupCat,
					Key:      q.ChartKey,
					Label:    q.ChartLabel,
					Fields:   fieldMeta,
					Buckets:  buckets,
				})
			}
		}

		// Build category structure
		type CategoryResult struct {
			Name   string        `json:"name"`
			Charts []ChartResult `json:"charts"`
		}
		catMap := map[string]*CategoryResult{}
		var catOrder []string
		for _, ch := range charts {
			if _, ok := catMap[ch.Category]; !ok {
				catMap[ch.Category] = &CategoryResult{Name: ch.Category}
				catOrder = append(catOrder, ch.Category)
			}
			catMap[ch.Category].Charts = append(catMap[ch.Category].Charts, ch)
		}
		var categories []CategoryResult
		for _, name := range catOrder {
			categories = append(categories, *catMap[name])
		}

		if categories == nil {
			categories = []CategoryResult{}
		}
		out, _ := json.Marshal(map[string]any{
			"host":       hostName,
			"range":      rangeStr,
			"interval":   interval,
			"categories": categories,
		})
		w.Write(out)
	}
}

// ── Time-series aggregation ────────────────────────────────────────────────
// Queries a Metricbeat / ECS-compatible index with a date_histogram bucket
// aggregation so the frontend can draw trend charts.

func SearchMetricsTimeSeries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		index := strings.TrimSpace(r.URL.Query().Get("index"))
		field := strings.TrimSpace(r.URL.Query().Get("field"))
		rangeStr := r.URL.Query().Get("range") // e.g. "15m", "1h", "6h", "24h", "7d"
		metric := r.URL.Query().Get("metric")  // "avg", "max", "sum", "rate"
		if index == "" {
			index = "metricbeat-*"
		}
		if field == "" {
			field = "system.cpu.total.pct"
		}
		if rangeStr == "" {
			rangeStr = "1h"
		}
		if metric == "" {
			metric = "avg"
		}

		// Derive interval from range
		intervalMap := map[string]string{
			"15m": "1m", "30m": "2m", "1h": "5m",
			"3h": "10m", "6h": "15m", "12h": "30m",
			"24h": "1h", "3d": "3h", "7d": "12h",
		}
		interval, ok := intervalMap[rangeStr]
		if !ok {
			interval = "5m"
		}

		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var agg map[string]any
		if metric == "rate" {
			agg = map[string]any{"rate_agg": map[string]any{"rate": map[string]any{"field": field}}}
		} else {
			agg = map[string]any{"value": map[string]any{metric: map[string]any{"field": field}}}
		}

		body, _ := json.Marshal(map[string]any{
			"size": 0,
			"query": map[string]any{
				"range": map[string]any{
					"@timestamp": map[string]any{"gte": fmt.Sprintf("now-%s", rangeStr), "lte": "now"},
				},
			},
			"aggs": map[string]any{
				"over_time": map[string]any{
					"date_histogram": map[string]any{
						"field":             "@timestamp",
						"fixed_interval":    interval,
						"min_doc_count":     0,
						"extended_bounds":   map[string]any{"min": fmt.Sprintf("now-%s", rangeStr), "max": "now"},
					},
					"aggs": agg,
				},
			},
		})

		path := fmt.Sprintf("/%s/_search", url.PathEscape(index))
		var raw map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, path, body, &raw); err != nil {
			http.Error(w, jsonError("time-series query failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(raw)
		w.Write(out)
	}
}

// ── Process List ───────────────────────────────────────────────────────────
// SearchMetricsProcessList returns the top processes on a host ordered by CPU or memory.
// GET /api/connections/{id}/search/metrics-process-list?host=...&group=host.name&index=metricbeat-*&range=5m&sort=cpu
func SearchMetricsProcessList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		q := r.URL.Query()
		hostName := strings.TrimSpace(q.Get("host"))
		groupField := strings.TrimSpace(q.Get("group"))
		index := strings.TrimSpace(q.Get("index"))
		rangeStr := q.Get("range")
		sortBy := strings.TrimSpace(q.Get("sort")) // "cpu" or "mem"
		fromStr := strings.TrimSpace(q.Get("from"))
		toStr := strings.TrimSpace(q.Get("to"))

		if hostName == "" {
			http.Error(w, jsonError("host is required"), http.StatusBadRequest)
			return
		}
		if groupField == "" {
			groupField = "host.name"
		}
		if index == "" {
			index = "metricbeat-*"
		}
		if rangeStr == "" {
			rangeStr = "5m"
		}
		if sortBy == "" {
			sortBy = "cpu"
		}

		tsFilter := buildTSFilter(rangeStr, fromStr, toStr)

		orderField := "cpu_avg"
		if sortBy == "mem" {
			orderField = "mem_avg"
		}

		body, _ := json.Marshal(map[string]any{
			"size": 0,
			"query": map[string]any{
				"bool": map[string]any{
					"filter": []map[string]any{
						{"range": map[string]any{"@timestamp": tsFilter}},
						{"term": map[string]any{groupField: hostName}},
						{"exists": map[string]any{"field": "system.process.name"}},
					},
				},
			},
			"aggs": map[string]any{
				"processes": map[string]any{
					"terms": map[string]any{
						"field": "system.process.name",
						"size":  50,
						"order": map[string]any{orderField: "desc"},
					},
					"aggs": map[string]any{
						"cpu_avg": map[string]any{
							"avg": map[string]any{"field": "system.process.cpu.total.pct"},
						},
						"mem_avg": map[string]any{
							"avg": map[string]any{"field": "system.process.memory.rss.bytes"},
						},
						"latest_doc": map[string]any{
							"top_hits": map[string]any{
								"size": 1,
								"sort": []map[string]any{
									{"@timestamp": map[string]any{"order": "desc"}},
								},
								"_source": map[string]any{
									"includes": []string{
										"system.process.pid",
										"system.process.state",
									},
								},
							},
						},
					},
				},
			},
		})

		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		path := fmt.Sprintf("/%s/_search", url.PathEscape(index))
		var raw map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, path, body, &raw); err != nil {
			http.Error(w, jsonError("process list query failed: "+err.Error()), http.StatusBadGateway)
			return
		}

		type ProcessRow struct {
			Name     string   `json:"name"`
			PID      *int64   `json:"pid"`
			State    string   `json:"state"`
			CPUPct   *float64 `json:"cpu_pct"`
			MemBytes *float64 `json:"mem_bytes"`
		}

		var processes []ProcessRow
		if aggs, ok := raw["aggregations"].(map[string]any); ok {
			if procs, ok := aggs["processes"].(map[string]any); ok {
				if buckets, ok := procs["buckets"].([]any); ok {
					for _, b := range buckets {
						bm, _ := b.(map[string]any)
						name, _ := bm["key"].(string)

						toFloatPtr := func(key string) *float64 {
							if sub, ok := bm[key].(map[string]any); ok {
								if v, ok := sub["value"].(float64); ok {
									return &v
								}
							}
							return nil
						}

						var pid *int64
						var state string
						if latestDoc, ok := bm["latest_doc"].(map[string]any); ok {
							if hits, ok := latestDoc["hits"].(map[string]any); ok {
								if hitsArr, ok := hits["hits"].([]any); ok && len(hitsArr) > 0 {
									if hit, ok := hitsArr[0].(map[string]any); ok {
										if src, ok := hit["_source"].(map[string]any); ok {
											if proc, ok := src["system"].(map[string]any); ok {
												if procInner, ok := proc["process"].(map[string]any); ok {
													if pidVal, ok := procInner["pid"].(float64); ok {
														pidInt := int64(pidVal)
														pid = &pidInt
													}
													if stateVal, ok := procInner["state"].(string); ok {
														state = stateVal
													}
												}
											}
										}
									}
								}
							}
						}

						processes = append(processes, ProcessRow{
							Name:     name,
							PID:      pid,
							State:    state,
							CPUPct:   toFloatPtr("cpu_avg"),
							MemBytes: toFloatPtr("mem_avg"),
						})
					}
				}
			}
		}
		if processes == nil {
			processes = []ProcessRow{}
		}

		out, _ := json.Marshal(map[string]any{"processes": processes})
		w.Write(out)
	}
}

// ── Sparklines ────────────────────────────────────────────────────────────
// SearchMetricsSparklines returns mini time-series for a list of hosts using _msearch.
// POST /api/connections/{id}/search/metrics-sparklines
// Body: { hosts: [...], field: "system.cpu.total.pct", group: "host.name", index: "metricbeat-*", range: "1h" }
func SearchMetricsSparklines() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		var body struct {
			Hosts   []string `json:"hosts"`
			Field   string   `json:"field"`
			Group   string   `json:"group"`
			Index   string   `json:"index"`
			Range   string   `json:"range"`
			From    string   `json:"from"`
			To      string   `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		if len(body.Hosts) == 0 {
			http.Error(w, jsonError("hosts is required"), http.StatusBadRequest)
			return
		}
		if body.Field == "" {
			body.Field = "system.cpu.total.pct"
		}
		if body.Group == "" {
			body.Group = "host.name"
		}
		if body.Index == "" {
			body.Index = "metricbeat-*"
		}
		if body.Range == "" {
			body.Range = "1h"
		}

		tsFilter := buildTSFilter(body.Range, body.From, body.To)

		// Pick an interval that produces ~10 buckets
		intervalMap := map[string]string{
			"5m": "30s", "15m": "1m", "30m": "3m", "1h": "6m",
			"3h": "18m", "6h": "36m", "12h": "1h", "24h": "2h",
			"3d": "7h", "7d": "16h",
		}
		interval, ok := intervalMap[body.Range]
		if !ok {
			interval = "6m"
		}

		indexHeader, _ := json.Marshal(map[string]any{"index": body.Index})

		var msearchBuf bytes.Buffer
		for _, host := range body.Hosts {
			qBody, _ := json.Marshal(map[string]any{
				"size": 0,
				"query": map[string]any{
					"bool": map[string]any{
						"filter": []map[string]any{
							{"range": map[string]any{"@timestamp": tsFilter}},
							{"term": map[string]any{body.Group: host}},
							{"exists": map[string]any{"field": body.Field}},
						},
					},
				},
				"aggs": map[string]any{
					"over_time": map[string]any{
						"date_histogram": map[string]any{
							"field":          "@timestamp",
							"fixed_interval": interval,
							"min_doc_count":  0,
						},
						"aggs": map[string]any{
							"value": map[string]any{"avg": map[string]any{"field": body.Field}},
						},
					},
				},
			})
			msearchBuf.Write(indexHeader)
			msearchBuf.WriteByte('\n')
			msearchBuf.Write(qBody)
			msearchBuf.WriteByte('\n')
		}

		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var msearchResult map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, "/_msearch",
			msearchBuf.Bytes(), &msearchResult); err != nil {
			http.Error(w, jsonError("msearch failed: "+err.Error()), http.StatusBadGateway)
			return
		}

		sparklines := map[string][]float64{}
		if responses, ok := msearchResult["responses"].([]any); ok {
			for i, resp := range responses {
				if i >= len(body.Hosts) {
					break
				}
				host := body.Hosts[i]
				var vals []float64
				if respMap, ok := resp.(map[string]any); ok {
					if aggs, ok := respMap["aggregations"].(map[string]any); ok {
						if ot, ok := aggs["over_time"].(map[string]any); ok {
							if buckets, ok := ot["buckets"].([]any); ok {
								for _, b := range buckets {
									bm, _ := b.(map[string]any)
									if valAgg, ok := bm["value"].(map[string]any); ok {
										if v, ok := valAgg["value"].(float64); ok {
											vals = append(vals, v)
										} else {
											vals = append(vals, 0)
										}
									}
								}
							}
						}
					}
				}
				if vals == nil {
					vals = []float64{}
				}
				sparklines[host] = vals
			}
		}

		out, _ := json.Marshal(map[string]any{"sparklines": sparklines})
		w.Write(out)
	}
}

// ── Correlation ───────────────────────────────────────────────────────────
// SearchMetricsCorrelation finds metrics that correlate with a spike at anchor_time.
// GET /api/connections/{id}/search/metrics-correlation?host=...&anchor_time=...
func SearchMetricsCorrelation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}

		q := r.URL.Query()
		hostName := strings.TrimSpace(q.Get("host"))
		groupField := strings.TrimSpace(q.Get("group"))
		index := strings.TrimSpace(q.Get("index"))
		rangeStr := q.Get("range")
		fromStr := strings.TrimSpace(q.Get("from"))
		toStr := strings.TrimSpace(q.Get("to"))
		anchorTime := strings.TrimSpace(q.Get("anchor_time"))

		if hostName == "" {
			http.Error(w, jsonError("host is required"), http.StatusBadRequest)
			return
		}
		if anchorTime == "" {
			http.Error(w, jsonError("anchor_time is required"), http.StatusBadRequest)
			return
		}
		if groupField == "" {
			groupField = "host.name"
		}
		if index == "" {
			index = "metricbeat-*"
		}
		if rangeStr == "" {
			rangeStr = "1h"
		}

		tsFilter := buildTSFilter(rangeStr, fromStr, toStr)

		// Candidate fields to correlate
		type candidateField struct {
			Field string
			Label string
		}
		candidates := []candidateField{
			{"system.cpu.total.pct", "CPU Usage"},
			{"system.cpu.user.pct", "CPU User"},
			{"system.cpu.iowait.pct", "CPU I/O Wait"},
			{"system.memory.actual.used.pct", "Memory Used %"},
			{"system.memory.swap.used.pct", "Swap Used %"},
			{"system.filesystem.used.pct", "Disk Used %"},
			{"system.diskio.read.bytes", "Disk Read Bytes"},
			{"system.diskio.write.bytes", "Disk Write Bytes"},
			{"system.network.in.bytes", "Network In"},
			{"system.network.out.bytes", "Network Out"},
			{"system.load.1", "Load 1m"},
			{"system.load.5", "Load 5m"},
			{"system.load.15", "Load 15m"},
		}

		// Build _msearch: for each field, two queries — before and after anchor_time
		indexHeader, _ := json.Marshal(map[string]any{"index": index})

		var msearchBuf bytes.Buffer
		baseFilter := []map[string]any{
			{"range": map[string]any{"@timestamp": tsFilter}},
			{"term": map[string]any{groupField: hostName}},
		}

		for _, c := range candidates {
			// Before anchor
			beforeFilter := append(baseFilter, map[string]any{
				"range": map[string]any{"@timestamp": map[string]any{"lte": anchorTime}},
			}, map[string]any{"exists": map[string]any{"field": c.Field}})
			beforeBody, _ := json.Marshal(map[string]any{
				"size":  0,
				"query": map[string]any{"bool": map[string]any{"filter": beforeFilter}},
				"aggs":  map[string]any{"val": map[string]any{"avg": map[string]any{"field": c.Field}}},
			})
			msearchBuf.Write(indexHeader)
			msearchBuf.WriteByte('\n')
			msearchBuf.Write(beforeBody)
			msearchBuf.WriteByte('\n')

			// After anchor
			afterFilter := append(baseFilter, map[string]any{
				"range": map[string]any{"@timestamp": map[string]any{"gte": anchorTime}},
			}, map[string]any{"exists": map[string]any{"field": c.Field}})
			afterBody, _ := json.Marshal(map[string]any{
				"size":  0,
				"query": map[string]any{"bool": map[string]any{"filter": afterFilter}},
				"aggs":  map[string]any{"val": map[string]any{"avg": map[string]any{"field": c.Field}}},
			})
			msearchBuf.Write(indexHeader)
			msearchBuf.WriteByte('\n')
			msearchBuf.Write(afterBody)
			msearchBuf.WriteByte('\n')
		}

		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		var msearchResult map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, "/_msearch",
			msearchBuf.Bytes(), &msearchResult); err != nil {
			http.Error(w, jsonError("msearch failed: "+err.Error()), http.StatusBadGateway)
			return
		}

		type CorrelationResult struct {
			Field      string  `json:"field"`
			Label      string  `json:"label"`
			BeforeAvg  float64 `json:"before_avg"`
			AfterAvg   float64 `json:"after_avg"`
			PctChange  float64 `json:"pct_change"`
		}

		var correlations []CorrelationResult

		extractAvg := func(resp any) (float64, bool) {
			rm, ok := resp.(map[string]any)
			if !ok {
				return 0, false
			}
			aggs, ok := rm["aggregations"].(map[string]any)
			if !ok {
				return 0, false
			}
			val, ok := aggs["val"].(map[string]any)
			if !ok {
				return 0, false
			}
			v, ok := val["value"].(float64)
			return v, ok
		}

		if responses, ok := msearchResult["responses"].([]any); ok {
			for i, c := range candidates {
				beforeIdx := i * 2
				afterIdx := i*2 + 1
				if afterIdx >= len(responses) {
					break
				}
				beforeAvg, hasBefore := extractAvg(responses[beforeIdx])
				afterAvg, hasAfter := extractAvg(responses[afterIdx])
				if !hasBefore || !hasAfter {
					continue
				}
				var pctChange float64
				if beforeAvg != 0 {
					pctChange = (afterAvg - beforeAvg) / math.Abs(beforeAvg) * 100
				} else if afterAvg != 0 {
					pctChange = 100
				}
				// Only include if change exceeds 20%
				if math.Abs(pctChange) > 20 {
					correlations = append(correlations, CorrelationResult{
						Field:     c.Field,
						Label:     c.Label,
						BeforeAvg: beforeAvg,
						AfterAvg:  afterAvg,
						PctChange: pctChange,
					})
				}
			}
		}

		if correlations == nil {
			correlations = []CorrelationResult{}
		}

		out, _ := json.Marshal(map[string]any{"correlations": correlations})
		w.Write(out)
	}
}
