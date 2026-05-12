package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ── ILM Policies ─────────────────────────────────────────────────────────────

func SearchListILMPolicies() http.HandlerFunc {
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
		var result map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, "/_ilm/policy", nil, &result); err != nil {
			http.Error(w, jsonError("list ILM policies failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func SearchSaveILMPolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			http.Error(w, jsonError("policy name is required"), http.StatusBadRequest)
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
		path := fmt.Sprintf("/_ilm/policy/%s", url.PathEscape(name))
		if err := client.doJSON(r.Context(), http.MethodPut, path, body, &result); err != nil {
			http.Error(w, jsonError("save ILM policy failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func SearchDeleteILMPolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			http.Error(w, jsonError("policy name is required"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/_ilm/policy/%s", url.PathEscape(name))
		if err := client.doJSON(r.Context(), http.MethodDelete, path, nil, &result); err != nil {
			http.Error(w, jsonError("delete ILM policy failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

// ── Index Templates ───────────────────────────────────────────────────────────

func SearchListTemplates() http.HandlerFunc {
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
		var result map[string]any
		if err := client.doJSON(r.Context(), http.MethodGet, "/_index_template", nil, &result); err != nil {
			http.Error(w, jsonError("list templates failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func SearchSaveTemplate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			http.Error(w, jsonError("template name is required"), http.StatusBadRequest)
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
		path := fmt.Sprintf("/_index_template/%s", url.PathEscape(name))
		if err := client.doJSON(r.Context(), http.MethodPut, path, body, &result); err != nil {
			http.Error(w, jsonError("save template failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func SearchDeleteTemplate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			http.Error(w, jsonError("template name is required"), http.StatusBadRequest)
			return
		}
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/_index_template/%s", url.PathEscape(name))
		if err := client.doJSON(r.Context(), http.MethodDelete, path, nil, &result); err != nil {
			http.Error(w, jsonError("delete template failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

// ── Index Settings / Shard Rules ──────────────────────────────────────────────

func SearchGetIndexSettings() http.HandlerFunc {
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
		client, err := openSearchClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var result map[string]any
		path := fmt.Sprintf("/%s/_settings", url.PathEscape(index))
		if err := client.doJSON(r.Context(), http.MethodGet, path, nil, &result); err != nil {
			http.Error(w, jsonError("get settings failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func SearchUpdateIndexSettings() http.HandlerFunc {
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
		path := fmt.Sprintf("/%s/_settings", url.PathEscape(index))
		if err := client.doJSON(r.Context(), http.MethodPut, path, body, &result); err != nil {
			http.Error(w, jsonError("update settings failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}
