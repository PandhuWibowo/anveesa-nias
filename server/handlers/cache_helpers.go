package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anveesa/nias/cache"
)

func cachedJSONResponse(w http.ResponseWriter, r *http.Request, key string, ttl time.Duration, load func() (any, error)) {
	w.Header().Set("Content-Type", "application/json")
	refresh := strings.TrimSpace(r.URL.Query().Get("refresh")) == "1"
	if !refresh {
		cached, found, err := cache.Default().Get(r.Context(), key)
		if err == nil && found {
			_, _ = w.Write([]byte(cached))
			return
		}
	}

	value, err := load()
	if err != nil {
		http.Error(w, jsonError(err.Error()), http.StatusInternalServerError)
		return
	}
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, jsonError("failed to encode response"), http.StatusInternalServerError)
		return
	}
	_ = cache.Default().Set(r.Context(), key, string(body), ttl)
	_, _ = w.Write(body)
}

func invalidateNotificationCountCache(userIDs ...int64) {
	deduped := map[int64]bool{0: true}
	for _, userID := range userIDs {
		deduped[userID] = true
	}
	for userID := range deduped {
		_ = cache.Default().Delete(context.Background(), "notifications:unread:"+strconv.FormatInt(userID, 10))
	}
}
