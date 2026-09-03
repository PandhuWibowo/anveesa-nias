package handlers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// decodeRawField handles two forms that frontend clients may send:
//   - A proper JSON value (object/array/literal): used as-is after unmarshal.
//   - A JSON-encoded string wrapping a JSON value (double-encoded): the string
//     content is decoded once more.  This prevents a parsing_exception when
//     the frontend calls JSON.stringify() before placing the value in the body.
func decodeRawField(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" || trimmed == "" {
		return nil
	}
	// If it's a JSON string (double-encoded), unwrap it first.
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			var inner any
			if json.Unmarshal([]byte(s), &inner) == nil {
				return inner
			}
		}
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		return v
	}
	return nil
}

type AggregateInput struct {
	Index       string          `json:"index"`
	Query       json.RawMessage `json:"query"`
	Aggs        json.RawMessage `json:"aggs"`
	Sort        json.RawMessage `json:"sort"`
	Size        int             `json:"size"`
	From        int             `json:"from"`
	SearchAfter json.RawMessage `json:"search_after,omitempty"`
}

// SearchAggregate runs arbitrary aggregation queries (date_histogram, terms, stats…).
// POST /api/connections/{id}/search/aggregate
func SearchAggregate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var payload AggregateInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, jsonError("invalid JSON body"), http.StatusBadRequest)
			return
		}
		payload.Index = strings.Trim(payload.Index, "/ ")
		if payload.Index == "" {
			http.Error(w, jsonError("index is required"), http.StatusBadRequest)
			return
		}

		body := map[string]any{"size": payload.Size, "track_total_hits": true}
		if sa := decodeRawField(payload.SearchAfter); sa != nil {
			// search_after and from are mutually exclusive; search_after takes precedence.
			body["search_after"] = sa
		} else {
			body["from"] = payload.From
		}
		if v := decodeRawField(payload.Query); v != nil {
			body["query"] = v
		}
		if v := decodeRawField(payload.Aggs); v != nil {
			if aggs, ok := v.(map[string]any); !ok || len(aggs) > 0 {
				body["aggs"] = v
			}
		}
		if v := decodeRawField(payload.Sort); v != nil {
			body["sort"] = v
		}

		bodyBytes, _ := json.Marshal(body)
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		path := fmt.Sprintf("/%s/_search", searchIndexExpressionPath(payload.Index))
		var result map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, path, bodyBytes, &result); err != nil {
			http.Error(w, jsonError("aggregate failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		if esErr := searchResponseError(result); esErr != "" {
			http.Error(w, jsonError(esErr), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

// SearchBackupToBucket exports every document matching a Discover/Search
// query as gzip-compressed NDJSON and streams the archive straight to an
// existing object storage connection — the same async job pattern SFTP and
// database backups use. It pages through the index with search_after (the
// client-supplied sort must include a unique tiebreaker, e.g. "_id") so an
// index far larger than memory streams through in bounded chunks.
//
// POST /api/connections/{id}/search/backup-to-bucket
func SearchBackupToBucket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var req struct {
			Index      string          `json:"index"`
			Query      json.RawMessage `json:"query"`
			Sort       json.RawMessage `json:"sort"`
			DestConnID int64           `json:"dest_conn_id"`
			Prefix     string          `json:"prefix"`
			Subfolder  string          `json:"subfolder"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, jsonError("invalid request body"), http.StatusBadRequest)
			return
		}
		req.Index = strings.Trim(req.Index, "/ ")
		if req.Index == "" {
			http.Error(w, jsonError("index is required"), http.StatusBadRequest)
			return
		}
		if req.DestConnID == 0 {
			http.Error(w, jsonError("dest_conn_id is required"), http.StatusBadRequest)
			return
		}
		sortVal := decodeRawField(req.Sort)
		if sortVal == nil {
			http.Error(w, jsonError("sort is required (must include a unique tiebreaker field)"), http.StatusBadRequest)
			return
		}
		queryVal := decodeRawField(req.Query)

		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		dest, err := fetchBucketConn(req.DestConnID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		ts := time.Now().UTC().Format("20060102_150405")
		prefix := strings.TrimSpace(req.Prefix)
		if prefix == "" {
			prefix = "logs"
		}
		objectName := fmt.Sprintf("%s_%s.ndjson.gz", prefix, ts)
		if sub := strings.Trim(strings.TrimSpace(req.Subfolder), "/"); sub != "" {
			objectName = sub + "/" + objectName
		}

		jobCtx, jobCancel := context.WithCancel(context.Background())
		job := &BackupJob{
			ID:        newJobID(),
			Status:    BackupJobRunning,
			Stage:     "exporting",
			StartedAt: time.Now(),
			cancel:    jobCancel,
		}
		backupJobs.Store(job.ID, job)

		searchPath := fmt.Sprintf("/%s/_search", searchIndexExpressionPath(req.Index))

		go func() {
			defer jobCancel()

			pr, pw := io.Pipe()
			exportErrCh := make(chan error, 1)
			var exported int64

			go func() {
				gz, _ := gzip.NewWriterLevel(pw, gzip.BestCompression)
				var searchAfter any
				const pageSize = 1000
				const maxDocs = 5_000_000
				var exportErr error

				for {
					if jobCtx.Err() != nil {
						exportErr = jobCtx.Err()
						break
					}
					body := map[string]any{"size": pageSize, "sort": sortVal, "track_total_hits": false}
					if queryVal != nil {
						body["query"] = queryVal
					}
					if searchAfter != nil {
						body["search_after"] = searchAfter
					}
					bodyBytes, _ := json.Marshal(body)
					var result map[string]any
					if err := client.doJSON(jobCtx, http.MethodPost, searchPath, bodyBytes, &result); err != nil {
						exportErr = err
						break
					}
					if esErr := searchResponseError(result); esErr != "" {
						exportErr = fmt.Errorf("%s", esErr)
						break
					}
					hitsWrap, _ := result["hits"].(map[string]any)
					hitsArr, _ := hitsWrap["hits"].([]any)
					if len(hitsArr) == 0 {
						break
					}
					for _, h := range hitsArr {
						hit, _ := h.(map[string]any)
						line, err := json.Marshal(hit["_source"])
						if err != nil {
							continue
						}
						gz.Write(line)
						gz.Write([]byte("\n"))
						exported++
						if sv, ok := hit["sort"]; ok {
							searchAfter = sv
						}
					}
					if len(hitsArr) < pageSize || exported >= maxDocs {
						break
					}
				}
				if exportErr == nil {
					exportErr = gz.Close()
				}
				pw.CloseWithError(exportErr)
				exportErrCh <- exportErr
			}()

			cr := &countingReader{r: pr}
			job.mu.Lock()
			job.Stage = "uploading"
			job.uploadCounter = &cr.n
			job.mu.Unlock()
			uploadErr := uploadToBucketStream(jobCtx, dest, objectName, cr)
			if uploadErr != nil {
				pr.CloseWithError(uploadErr)
			}
			exportErr := <-exportErrCh

			now := time.Now()
			job.mu.Lock()
			defer job.mu.Unlock()
			job.DoneAt = &now
			if uploadErr != nil || exportErr != nil || jobCtx.Err() != nil {
				if jobCtx.Err() != nil && uploadErr == nil && exportErr == nil {
					job.Status = BackupJobCanceled
				} else {
					job.Status = BackupJobFailed
					switch {
					case uploadErr != nil:
						job.Error = uploadErr.Error()
					case exportErr != nil:
						job.Error = exportErr.Error()
					default:
						job.Error = jobCtx.Err().Error()
					}
				}
				return
			}
			job.Status = BackupJobDone
			job.ObjectKey = objectName
			job.Bucket = dest.Bucket
			job.SizeBytes = atomic.LoadInt64(&cr.n)
			job.RecordsExported = exported
		}()

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
	}
}

// SearchIndexFields fetches field names and types from the mapping for autocomplete.
// GET /api/connections/{id}/search/fields?index=...
func SearchIndexFields() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		index := strings.Trim(r.URL.Query().Get("index"), "/ ")
		if index == "" {
			http.Error(w, jsonError("index is required"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "fields:"+index)
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var mapping map[string]any
		path := fmt.Sprintf("/%s/_mapping", searchIndexExpressionPath(index))
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &mapping); err != nil {
			http.Error(w, jsonError("get fields failed: "+err.Error()), http.StatusBadGateway)
			return
		}

		fields := extractMappingFields(mapping)
		out, _ := json.Marshal(fields)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLIndices)
		w.Write(out)
	}
}

type FieldInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func extractMappingFields(mapping map[string]any) []FieldInfo {
	var fields []FieldInfo
	for _, indexData := range mapping {
		idx, ok := indexData.(map[string]any)
		if !ok {
			continue
		}
		mappings, ok := idx["mappings"].(map[string]any)
		if !ok {
			continue
		}
		props, ok := mappings["properties"].(map[string]any)
		if !ok {
			continue
		}
		fields = append(fields, flattenProps(props, "")...)
	}
	return fields
}

func flattenProps(props map[string]any, prefix string) []FieldInfo {
	var out []FieldInfo
	for key, val := range props {
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		prop, ok := val.(map[string]any)
		if !ok {
			continue
		}
		fieldType := ""
		if t, ok := prop["type"].(string); ok {
			fieldType = t
		} else if prop["properties"] != nil {
			fieldType = "object"
		}
		out = append(out, FieldInfo{Name: name, Type: fieldType})
		if nested, ok := prop["properties"].(map[string]any); ok {
			out = append(out, flattenProps(nested, name)...)
		}
		if nested, ok := prop["fields"].(map[string]any); ok {
			out = append(out, flattenProps(nested, name)...)
		}
	}
	return out
}
