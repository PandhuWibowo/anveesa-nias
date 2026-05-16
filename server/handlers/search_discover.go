package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
			body["aggs"] = v
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
		path := fmt.Sprintf("/%s/_search", url.PathEscape(payload.Index))
		var result map[string]any
		if err := client.doJSON(r.Context(), http.MethodPost, path, bodyBytes, &result); err != nil {
			http.Error(w, jsonError("aggregate failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(result)
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
		path := fmt.Sprintf("/%s/_mapping", url.PathEscape(index))
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
