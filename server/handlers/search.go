package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/anveesa/nias/cache"
	appdb "github.com/anveesa/nias/db"
)

const (
	searchCacheTTLInfo     = 30 * time.Second
	searchCacheTTLIndices  = 60 * time.Second
	searchCacheTTLPolicies = 5 * time.Minute
	searchCacheTTLSettings = 2 * time.Minute
)

func searchCacheKey(connID int64, resource string) string {
	return fmt.Sprintf("search:%d:%s", connID, resource)
}

func searchCacheGet(ctx context.Context, key string, w http.ResponseWriter) bool {
	val, ok, _ := cache.Default().Get(ctx, key)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "HIT")
	w.Write([]byte(val))
	return true
}

func searchCacheSet(ctx context.Context, key string, data []byte, ttl time.Duration) {
	_ = cache.Default().Set(ctx, key, string(data), ttl)
}

func searchCacheInvalidate(ctx context.Context, connID int64, resources ...string) {
	for _, r := range resources {
		_ = cache.Default().Delete(ctx, searchCacheKey(connID, r))
	}
}

type SearchIndexInfo struct {
	Health        string `json:"health"`
	Status        string `json:"status"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	UUID          string `json:"uuid"`
	PrimaryShards string `json:"primary_shards"`
	ReplicaShards string `json:"replica_shards"`
	DocsCount     string `json:"docs_count"`
	StoreSize     string `json:"store_size"`
	StoreBytes    int64  `json:"store_bytes"`
	CreatedAt     string `json:"created_at"`
	BackingCount  int    `json:"backing_count"`
}

func (i *SearchIndexInfo) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	i.Health = stringValue(raw["health"])
	i.Status = stringValue(raw["status"])
	i.Name = stringValue(raw["index"])
	i.UUID = stringValue(raw["uuid"])
	i.PrimaryShards = stringValue(raw["pri"])
	i.ReplicaShards = stringValue(raw["rep"])
	i.DocsCount = stringValue(raw["docs.count"])
	i.StoreSize = stringValue(raw["store.size"])
	i.StoreBytes = int64Value(raw["store.size"])
	i.CreatedAt = stringValue(raw["creation.date.string"])
	if i.CreatedAt == "" {
		i.CreatedAt = unixMillisString(raw["creation.date"])
	}
	return nil
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		return 0
	}
}

func unixMillisString(value any) string {
	ms := int64Value(value)
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func parseCatIndexInfo(data []byte) ([]SearchIndexInfo, error) {
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	out := make([]SearchIndexInfo, 0, len(rows))
	for _, row := range rows {
		b, _ := json.Marshal(row)
		var info SearchIndexInfo
		if err := json.Unmarshal(b, &info); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

type SearchDataStreamsResponse struct {
	DataStreams []SearchDataStreamInfo `json:"data_streams"`
}

type SearchDataStreamInfo struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Template       string `json:"template"`
	Hidden         bool   `json:"hidden"`
	BackingIndices []struct {
		IndexName string `json:"index_name"`
	} `json:"indices"`
}

type SearchQueryInput struct {
	Index string          `json:"index"`
	Query json.RawMessage `json:"query"`
	Size  int             `json:"size"`
	From  int             `json:"from"`
}

type SearchDocumentInput struct {
	Index    string          `json:"index"`
	ID       string          `json:"id"`
	Document json.RawMessage `json:"document"`
}

type searchClient struct {
	baseURL  string
	driver   string
	username string
	password string
	http     *http.Client
}

func SearchInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "info")
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		start := time.Now()
		var info map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, "/", nil, &info); err != nil {
			http.Error(w, jsonError("search info failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		result := map[string]any{
			"status":     "ok",
			"driver":     client.driver,
			"cluster":    info,
			"latency_ms": time.Since(start).Milliseconds(),
		}
		out, _ := json.Marshal(result)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLInfo)
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}
}

func SearchIndices() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "indices")
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var indices []SearchIndexInfo
		path := "/_cat/indices/*?format=json&bytes=b&s=index&expand_wildcards=all&h=health,status,index,uuid,pri,rep,docs.count,store.size,creation.date,creation.date.string"
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &indices); err != nil {
			http.Error(w, jsonError("list indices failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		for i := range indices {
			indices[i].Kind = "index"
		}
		indexByName := make(map[string]SearchIndexInfo, len(indices))
		for _, idx := range indices {
			indexByName[idx.Name] = idx
		}
		var streams SearchDataStreamsResponse
		if err := client.doJSON(r.Context(), http.MethodGet, "/_data_stream/*?expand_wildcards=all", nil, &streams); err == nil {
			for _, stream := range streams.DataStreams {
				var docs int64
				var bytes int64
				var createdAt string
				for _, backing := range stream.BackingIndices {
					idx := indexByName[backing.IndexName]
					docs += int64Value(idx.DocsCount)
					bytes += idx.StoreBytes
					if createdAt == "" || (idx.CreatedAt != "" && idx.CreatedAt < createdAt) {
						createdAt = idx.CreatedAt
					}
				}
				indices = append(indices, SearchIndexInfo{
					Name:          stream.Name,
					Kind:          "data_stream",
					Status:        stream.Status,
					PrimaryShards: strconv.Itoa(len(stream.BackingIndices)),
					DocsCount:     strconv.FormatInt(docs, 10),
					StoreSize:     strconv.FormatInt(bytes, 10),
					StoreBytes:    bytes,
					CreatedAt:     createdAt,
					BackingCount:  len(stream.BackingIndices),
				})
			}
		}
		out, _ := json.Marshal(indices)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLIndices)
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}
}

func SearchQuery() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		var payload SearchQueryInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, jsonError("invalid JSON body"), http.StatusBadRequest)
			return
		}
		payload.Index = strings.Trim(payload.Index, "/ ")
		if payload.Index == "" {
			http.Error(w, jsonError("index is required"), http.StatusBadRequest)
			return
		}
		if payload.Size <= 0 || payload.Size > 500 {
			payload.Size = 50
		}
		if payload.From < 0 {
			payload.From = 0
		}
		body := payload.Query
		if len(bytes.TrimSpace(body)) == 0 {
			body = []byte(`{"query":{"match_all":{}}}`)
		}

		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		path := fmt.Sprintf("/%s/_search?size=%d&from=%d", searchIndexExpressionPath(payload.Index), payload.Size, payload.From)
		var result map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, path, body, &result); err != nil {
			http.Error(w, jsonError("search query failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func searchResponseError(result map[string]any) string {
	if result == nil {
		return ""
	}
	if status, _ := result["status"].(float64); status >= 400 {
		if reason := stringValue(result["error"]); reason != "" {
			return reason
		}
	}
	if errObj, ok := result["error"].(map[string]any); ok {
		if causes, ok := errObj["root_cause"].([]any); ok && len(causes) > 0 {
			if first, ok := causes[0].(map[string]any); ok {
				if reason := stringValue(first["reason"]); reason != "" {
					if typ := stringValue(first["type"]); typ != "" {
						return typ + ": " + reason
					}
					return reason
				}
			}
		}
		if reason := stringValue(errObj["reason"]); reason != "" {
			if typ := stringValue(errObj["type"]); typ != "" {
				return typ + ": " + reason
			}
			return reason
		}
	}
	return ""
}

func searchIndexExpressionPath(index string) string {
	parts := strings.Split(index, ",")
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value := url.PathEscape(part)
		value = strings.ReplaceAll(value, "%2A", "*")
		value = strings.ReplaceAll(value, "%3F", "?")
		escaped = append(escaped, value)
	}
	return strings.Join(escaped, ",")
}

func SearchDocument() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		switch r.Method {
		case http.MethodGet:
			index := strings.Trim(r.URL.Query().Get("index"), "/ ")
			id := strings.TrimSpace(r.URL.Query().Get("id"))
			if index == "" || id == "" {
				http.Error(w, jsonError("index and id are required"), http.StatusBadRequest)
				return
			}
			var result map[string]any
			path := fmt.Sprintf("/%s/_doc/%s", url.PathEscape(index), url.PathEscape(id))
			if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &result); err != nil {
				http.Error(w, jsonError("read document failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(result)
		case http.MethodPost, http.MethodPut:
			var payload SearchDocumentInput
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, jsonError("invalid JSON body"), http.StatusBadRequest)
				return
			}
			payload.Index = strings.Trim(payload.Index, "/ ")
			if payload.Index == "" || len(bytes.TrimSpace(payload.Document)) == 0 {
				http.Error(w, jsonError("index and document are required"), http.StatusBadRequest)
				return
			}
			method := http.MethodPost
			path := fmt.Sprintf("/%s/_doc", url.PathEscape(payload.Index))
			if strings.TrimSpace(payload.ID) != "" {
				method = http.MethodPut
				path += "/" + url.PathEscape(strings.TrimSpace(payload.ID))
			}
			var result map[string]any
			if err := client.doJSON(r.Context(), method, path, payload.Document, &result); err != nil {
				http.Error(w, jsonError("write document failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(result)
		case http.MethodDelete:
			index := strings.Trim(r.URL.Query().Get("index"), "/ ")
			id := strings.TrimSpace(r.URL.Query().Get("id"))
			if index == "" || id == "" {
				http.Error(w, jsonError("index and id are required"), http.StatusBadRequest)
				return
			}
			var result map[string]any
			path := fmt.Sprintf("/%s/_doc/%s", url.PathEscape(index), url.PathEscape(id))
			if err := client.doJSON(r.Context(), http.MethodDelete, path, nil, &result); err != nil {
				http.Error(w, jsonError("delete document failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(result)
		default:
			http.NotFound(w, r)
		}
	}
}

func SearchDeleteIndex() http.HandlerFunc {
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
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/%s", url.PathEscape(index))
		if err := client.doJSON(r.Context(), http.MethodDelete, path, nil, &result); err != nil {
			http.Error(w, jsonError("delete index failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		searchCacheInvalidate(r.Context(), connID, "indices", "index-settings")
		json.NewEncoder(w).Encode(result)
	}
}

func openSearchClient(connID int64) (*searchClient, error) {
	var in ConnectionInput
	var ssl, disconnected int
	var encPassword string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT driver, COALESCE(host,''), COALESCE(port,0), database, COALESCE(username,''), COALESCE(password,''), ssl, COALESCE(disconnected,0) FROM connections WHERE id=?`), connID,
	).Scan(&in.Driver, &in.Host, &in.Port, &in.Database, &in.Username, &encPassword, &ssl, &disconnected)
	if err != nil {
		return nil, fmt.Errorf("connection not found")
	}
	if disconnected == 1 {
		return nil, fmt.Errorf("connection is disconnected")
	}
	if !isSearchDriver(in.Driver) {
		return nil, fmt.Errorf("connection is not Elasticsearch or OpenSearch")
	}
	password, err := decryptCredential(encPassword)
	if err != nil {
		return nil, fmt.Errorf("decryption error")
	}
	in.Password = password
	in.SSL = ssl == 1

	baseURL, err := searchBaseURL(in)
	if err != nil {
		return nil, err
	}
	return &searchClient{
		baseURL:  baseURL,
		driver:   in.Driver,
		username: in.Username,
		password: in.Password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func searchBaseURL(in ConnectionInput) (string, error) {
	raw := strings.TrimSpace(in.Host)
	if raw == "" {
		return "", fmt.Errorf("host is required")
	}

	scheme := "http"
	if in.SSL || in.Port == 443 {
		scheme = "https"
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		if parsed.Scheme != "" {
			scheme = parsed.Scheme
		}
		raw = parsed.Host + strings.TrimRight(parsed.EscapedPath(), "/")
	} else {
		raw = strings.TrimRight(raw, "/")
	}

	hostPart := raw
	pathPart := ""
	if slash := strings.Index(raw, "/"); slash >= 0 {
		hostPart = raw[:slash]
		pathPart = raw[slash:]
	}
	if hostPart == "" {
		return "", fmt.Errorf("host is required")
	}

	port := in.Port
	if port == 0 {
		if scheme == "https" {
			port = 443
		} else {
			port = 9200
		}
	}
	if port > 0 && !hasPort(hostPart) {
		hostPart = net.JoinHostPort(hostPart, strconv.Itoa(port))
	}
	return strings.TrimRight(scheme+"://"+hostPart+pathPart, "/"), nil
}

func hasPort(host string) bool {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return true
	}
	if strings.Count(host, ":") == 1 && strings.LastIndex(host, ":") > strings.LastIndex(host, "]") {
		return true
	}
	return false
}

func (c *searchClient) doJSON(ctx context.Context, method, path string, body []byte, out any) error {
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	authMode := strings.ToLower(strings.TrimSpace(c.username))
	if c.password != "" && (authMode == "" || authMode == "apikey" || authMode == "api_key") {
		req.Header.Set("Authorization", "ApiKey "+c.password)
	} else if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("%s: %s", resp.Status, msg)
	}
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}
