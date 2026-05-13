package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const searchCacheTTLObserv = 20 * time.Second

// ── Cluster Health ────────────────────────────────────────────────────────────

func SearchClusterHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "cluster-health")
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var health map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, "/_cluster/health?level=indices", nil, &health); err != nil {
			http.Error(w, jsonError("cluster health failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(health)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLObserv)
		w.Write(out)
	}
}

// ── Nodes ─────────────────────────────────────────────────────────────────────

func SearchNodes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "nodes")
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		fields := "name,ip,heap.percent,heap.max,ram.percent,ram.max,cpu,disk.used_percent,disk.avail,node.role,master,load_1m,uptime"
		var nodes []map[string]any
		path := fmt.Sprintf("/_cat/nodes?format=json&h=%s", url.QueryEscape(fields))
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &nodes); err != nil {
			http.Error(w, jsonError("list nodes failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(nodes)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLObserv)
		w.Write(out)
	}
}

// ── Shards ────────────────────────────────────────────────────────────────────

func SearchShards() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		indexFilter := strings.TrimSpace(r.URL.Query().Get("index"))
		cacheKeyResource := "shards"
		if indexFilter != "" {
			cacheKeyResource = "shards:" + indexFilter
		}
		cacheKey := searchCacheKey(connID, cacheKeyResource)
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		pathBase := "/_cat/shards"
		if indexFilter != "" {
			pathBase += "/" + url.PathEscape(indexFilter)
		}
		var shards []map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, pathBase+"?format=json&s=index,shard", nil, &shards); err != nil {
			http.Error(w, jsonError("list shards failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(shards)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLObserv)
		w.Write(out)
	}
}

// ── Index Mapping ─────────────────────────────────────────────────────────────

func SearchIndexMapping() http.HandlerFunc {
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
		cacheKey := searchCacheKey(connID, "mapping:"+index)
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/%s/_mapping", url.PathEscape(index))
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &result); err != nil {
			http.Error(w, jsonError("get mapping failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(result)
		searchCacheSet(r.Context(), cacheKey, out, 2*time.Minute)
		w.Write(out)
	}
}

// ── List Indices ──────────────────────────────────────────────────────────────

func SearchListIndices() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
		if pattern == "" {
			pattern = "*"
		}
		cacheKey := searchCacheKey(connID, "list-indices:"+pattern)
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		fields := "index,docs.count,store.size,health,status"
		// expand_wildcards=all exposes hidden backing indices (.ds-*) for data streams
		path := fmt.Sprintf("/_cat/indices/%s?format=json&h=%s&s=index&expand_wildcards=all", url.PathEscape(pattern), url.QueryEscape(fields))
		var indices []map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &indices); err != nil {
			http.Error(w, jsonError("list indices failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(indices)
		searchCacheSet(r.Context(), cacheKey, out, 15*time.Second)
		w.Write(out)
	}
}

// ── Index Stats ───────────────────────────────────────────────────────────────

func SearchIndexStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		index := strings.Trim(r.URL.Query().Get("index"), "/ ")
		if index == "" {
			index = "_all"
		}
		cacheKey := searchCacheKey(connID, "index-stats:"+index)
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/%s/_stats", url.PathEscape(index))
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &result); err != nil {
			http.Error(w, jsonError("get index stats failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(result)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLObserv)
		w.Write(out)
	}
}
