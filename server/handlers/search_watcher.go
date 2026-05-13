package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const searchCacheTTLWatcher = 10 * time.Second

// ── Watcher Stats ─────────────────────────────────────────────────────────────

func SearchWatcherStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "watcher-stats")
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, "/_watcher/stats?metric=_all", nil, &result); err != nil {
			http.Error(w, jsonError("watcher stats failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		out, _ := json.Marshal(result)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLWatcher)
		w.Write(out)
	}
}

// ── List Watches ──────────────────────────────────────────────────────────────

func SearchListWatches() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		cacheKey := searchCacheKey(connID, "watches")
		if searchCacheGet(r.Context(), cacheKey, w) {
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		// Search .watches index to list all defined watches
		body := []byte(`{"query":{"match_all":{}},"size":200,"sort":[{"_id":{"order":"asc"}}]}`)
		var result map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, "/.watches/_search?allow_no_indices=true", body, &result); err != nil {
			// Watcher might not be enabled — return empty list
			out, _ := json.Marshal(map[string]any{"hits": map[string]any{"hits": []any{}, "total": 0}})
			w.Write(out)
			return
		}
		out, _ := json.Marshal(result)
		searchCacheSet(r.Context(), cacheKey, out, searchCacheTTLWatcher)
		w.Write(out)
	}
}

// ── Get Watch ─────────────────────────────────────────────────────────────────

func SearchGetWatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		watchID := strings.TrimSpace(r.URL.Query().Get("id"))
		if watchID == "" {
			http.Error(w, jsonError("watch id is required"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/_watcher/watch/%s", url.PathEscape(watchID))
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &result); err != nil {
			http.Error(w, jsonError("get watch failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

// ── Save Watch (create or update) ─────────────────────────────────────────────

func SearchSaveWatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		watchID := strings.TrimSpace(r.URL.Query().Get("id"))
		if watchID == "" {
			http.Error(w, jsonError("watch id is required"), http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, jsonError("read body failed"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/_watcher/watch/%s", url.PathEscape(watchID))
		if err := client.doJSON(r.Context(), http.MethodPut, path, body, &result); err != nil {
			http.Error(w, jsonError("save watch failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		searchCacheInvalidate(r.Context(), connID, "watches")
		json.NewEncoder(w).Encode(result)
	}
}

// ── Delete Watch ──────────────────────────────────────────────────────────────

func SearchDeleteWatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		watchID := strings.TrimSpace(r.URL.Query().Get("id"))
		if watchID == "" {
			http.Error(w, jsonError("watch id is required"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/_watcher/watch/%s", url.PathEscape(watchID))
		if err := client.doJSON(r.Context(), http.MethodDelete, path, nil, &result); err != nil {
			http.Error(w, jsonError("delete watch failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		searchCacheInvalidate(r.Context(), connID, "watches")
		json.NewEncoder(w).Encode(result)
	}
}

// ── Execute Watch ─────────────────────────────────────────────────────────────

func SearchExecuteWatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		watchID := strings.TrimSpace(r.URL.Query().Get("id"))
		if watchID == "" {
			http.Error(w, jsonError("watch id is required"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		// Execute with action_mode=simulate to preview without side effects unless force=true
		mode := "simulate"
		if r.URL.Query().Get("force") == "true" {
			mode = "force_execute"
		}
		body := fmt.Sprintf(`{"action_mode":"%s"}`, mode)
		var result map[string]any
		path := fmt.Sprintf("/_watcher/watch/%s/_execute", url.PathEscape(watchID))
		if err := client.doJSON(r.Context(), http.MethodPost, path, []byte(body), &result); err != nil {
			http.Error(w, jsonError("execute watch failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

// ── Activate / Deactivate Watch ───────────────────────────────────────────────

func SearchActivateWatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		watchID := strings.TrimSpace(r.URL.Query().Get("id"))
		if watchID == "" {
			http.Error(w, jsonError("watch id is required"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/_watcher/watch/%s/_activate", url.PathEscape(watchID))
		if err := client.doJSON(r.Context(), http.MethodPut, path, nil, &result); err != nil {
			http.Error(w, jsonError("activate watch failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		searchCacheInvalidate(r.Context(), connID, "watches")
		json.NewEncoder(w).Encode(result)
	}
}

func SearchDeactivateWatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		watchID := strings.TrimSpace(r.URL.Query().Get("id"))
		if watchID == "" {
			http.Error(w, jsonError("watch id is required"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/_watcher/watch/%s/_deactivate", url.PathEscape(watchID))
		if err := client.doJSON(r.Context(), http.MethodPut, path, nil, &result); err != nil {
			http.Error(w, jsonError("deactivate watch failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		searchCacheInvalidate(r.Context(), connID, "watches")
		json.NewEncoder(w).Encode(result)
	}
}

// ── Watch History ─────────────────────────────────────────────────────────────

func SearchWatchHistory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		watchID := strings.TrimSpace(r.URL.Query().Get("id"))
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		// Query watcher history index
		var filterClause any
		if watchID != "" {
			filterClause = map[string]any{
				"term": map[string]any{"watch_id": watchID},
			}
		} else {
			filterClause = map[string]any{"match_all": map[string]any{}}
		}
		body, _ := json.Marshal(map[string]any{
			"query": filterClause,
			"size":  50,
			"sort":  []any{map[string]any{"result.execution_time": map[string]any{"order": "desc"}}},
		})
		var result map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, "/.watcher-history*/_search?allow_no_indices=true", body, &result); err != nil {
			out, _ := json.Marshal(map[string]any{"hits": map[string]any{"hits": []any{}, "total": 0}})
			w.Write(out)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}
