package handlers

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type redisClient struct {
	address  string
	username string
	password string
	db       int
	tls      bool
	timeout  time.Duration
}

type redisKeySummary struct {
	Key  string `json:"key"`
	Type string `json:"type"`
	TTL  int64  `json:"ttl"`
}

type redisKeysResponse struct {
	Cursor string            `json:"cursor"`
	Keys   []redisKeySummary `json:"keys"`
}

type redisValueResponse struct {
	Key       string `json:"key"`
	Type      string `json:"type"`
	TTL       int64  `json:"ttl"`
	Length    int64  `json:"length,omitempty"`
	Value     any    `json:"value"`
	Truncated bool   `json:"truncated"`
}

type redisWriteRequest struct {
	Key   string          `json:"key"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
	TTL   int64           `json:"ttl"`
	DB    *int            `json:"db"`
}

type redisRenameRequest struct {
	OldKey string `json:"old_key"`
	NewKey string `json:"new_key"`
	DB     *int   `json:"db"`
}

type redisCommandRequest struct {
	Command string `json:"command"`
	DB      *int   `json:"db"`
}

type redisScriptRequest struct {
	Script string `json:"script"`
	DB     *int   `json:"db"`
}

type redisScriptResult struct {
	Line    int    `json:"line"`
	Command string `json:"command"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

type redisMoveRequest struct {
	Key       string `json:"key"`
	FromDB    int    `json:"from_db"`
	ToDB      int    `json:"to_db"`
	Overwrite bool   `json:"overwrite"`
}

type redisZSetItem struct {
	Member string  `json:"member"`
	Score  float64 `json:"score"`
}

func testRedisInput(ctx context.Context, in ConnectionInput) error {
	client, err := newRedisClientFromInput(in)
	if err != nil {
		return err
	}
	resp, err := client.command(ctx, "PING")
	if err != nil {
		return err
	}
	pong, ok := resp.(string)
	if !ok || strings.ToUpper(pong) != "PONG" {
		return fmt.Errorf("unexpected PING response: %v", resp)
	}
	return nil
}

func RedisKeys() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		client, connName, err := openRedisClient(connID, redisDBFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
		if pattern == "" {
			pattern = "*"
		}
		cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
		if cursor == "" {
			cursor = "0"
		}
		count := queryInt(r, "count", 100, 10, 500)

		resp, err := client.command(r.Context(), "SCAN", cursor, "MATCH", pattern, "COUNT", strconv.Itoa(count))
		if err != nil {
			http.Error(w, jsonError("redis scan failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		values, ok := resp.([]any)
		if !ok || len(values) != 2 {
			http.Error(w, jsonError("unexpected redis scan response"), http.StatusBadGateway)
			return
		}
		nextCursor, _ := values[0].(string)
		rawKeys, _ := values[1].([]any)

		keys := make([]redisKeySummary, 0, len(rawKeys))
		for _, raw := range rawKeys {
			key, ok := raw.(string)
			if !ok {
				continue
			}
			keyType := "unknown"
			if t, err := client.command(r.Context(), "TYPE", key); err == nil {
				if s, ok := t.(string); ok {
					keyType = s
				}
			}
			ttl := int64(-2)
			if t, err := client.command(r.Context(), "TTL", key); err == nil {
				if n, ok := t.(int64); ok {
					ttl = n
				}
			}
			keys = append(keys, redisKeySummary{Key: key, Type: keyType, TTL: ttl})
		}

		writeRedisAudit(r, "redis_scan_keys", connID, connName, pattern, "")
		json.NewEncoder(w).Encode(redisKeysResponse{Cursor: nextCursor, Keys: keys})
	}
}

func RedisPing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}

		client, connName, err := openRedisClient(connID, redisDBFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		start := time.Now()
		resp, err := client.command(r.Context(), "PING")
		if err != nil {
			writeRedisAudit(r, "redis_ping", connID, connName, "", err.Error())
			http.Error(w, jsonError("redis ping failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		pong, ok := resp.(string)
		if !ok || strings.ToUpper(pong) != "PONG" {
			errMsg := fmt.Sprintf("unexpected PING response: %v", resp)
			writeRedisAudit(r, "redis_ping", connID, connName, "", errMsg)
			http.Error(w, jsonError("redis ping failed: "+errMsg), http.StatusBadGateway)
			return
		}
		writeRedisAudit(r, "redis_ping", connID, connName, "", "")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"message":    "PONG",
			"latency_ms": time.Since(start).Milliseconds(),
		})
	}
}

func RedisKeyValue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, `{"error":"key is required"}`, http.StatusBadRequest)
			return
		}
		client, connName, err := openRedisClient(connID, redisDBFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		result, err := readRedisValue(r.Context(), client, key)
		if err != nil {
			http.Error(w, jsonError("redis read failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		writeRedisAudit(r, "redis_read_key", connID, connName, key, "")
		json.NewEncoder(w).Encode(result)
	}
}

func RedisWriteKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		var payload redisWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		payload.Key = strings.TrimSpace(payload.Key)
		payload.Type = strings.ToLower(strings.TrimSpace(payload.Type))
		if payload.Key == "" {
			http.Error(w, `{"error":"key is required"}`, http.StatusBadRequest)
			return
		}
		if err := validateRedisKeyType(payload.Type); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		client, connName, err := openRedisClient(connID, payload.DB)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if err := writeRedisValue(r.Context(), client, payload); err != nil {
			writeRedisAudit(r, "redis_write_key", connID, connName, payload.Key, err.Error())
			http.Error(w, jsonError("redis write failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		writeRedisAudit(r, "redis_write_key", connID, connName, payload.Key, "")
		json.NewEncoder(w).Encode(map[string]string{"message": "Redis key saved"})
	}
}

func RedisDeleteKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, `{"error":"key is required"}`, http.StatusBadRequest)
			return
		}
		client, connName, err := openRedisClient(connID, redisDBFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if _, err := client.command(r.Context(), "DEL", key); err != nil {
			writeRedisAudit(r, "redis_delete_key", connID, connName, key, err.Error())
			http.Error(w, jsonError("redis delete failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		writeRedisAudit(r, "redis_delete_key", connID, connName, key, "")
		json.NewEncoder(w).Encode(map[string]string{"message": "Redis key deleted"})
	}
}

func RedisRenameKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		var payload redisRenameRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		payload.OldKey = strings.TrimSpace(payload.OldKey)
		payload.NewKey = strings.TrimSpace(payload.NewKey)
		if payload.OldKey == "" || payload.NewKey == "" {
			http.Error(w, `{"error":"old_key and new_key are required"}`, http.StatusBadRequest)
			return
		}
		client, connName, err := openRedisClient(connID, payload.DB)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		result, err := client.command(r.Context(), "RENAMENX", payload.OldKey, payload.NewKey)
		if err != nil {
			writeRedisAudit(r, "redis_rename_key", connID, connName, payload.OldKey, err.Error())
			http.Error(w, jsonError("redis rename failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		if renamed, ok := result.(int64); ok && renamed == 0 {
			http.Error(w, jsonError("redis rename failed: target key already exists"), http.StatusConflict)
			return
		}
		writeRedisAudit(r, "redis_rename_key", connID, connName, payload.OldKey+" -> "+payload.NewKey, "")
		json.NewEncoder(w).Encode(map[string]string{"message": "Redis key renamed"})
	}
}

func RedisCommand() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		var payload redisCommandRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		args, err := parseRedisCommand(payload.Command)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if err := validateRedisCommand(args); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusForbidden)
			return
		}
		client, connName, err := openRedisClient(connID, payload.DB)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		result, err := client.command(r.Context(), args...)
		if err != nil {
			writeRedisAudit(r, "redis_command", connID, connName, args[0], err.Error())
			http.Error(w, jsonError("redis command failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		writeRedisAudit(r, "redis_command", connID, connName, args[0], "")
		json.NewEncoder(w).Encode(map[string]any{"result": result})
	}
}

func RedisGenerateScript() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		key := strings.TrimSpace(r.URL.Query().Get("key"))
		pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
		if key == "" && pattern == "" {
			http.Error(w, `{"error":"key or pattern is required"}`, http.StatusBadRequest)
			return
		}
		client, connName, err := openRedisClient(connID, redisDBFromRequest(r))
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		keys := []string{key}
		if pattern != "" {
			keys, err = scanRedisKeys(r.Context(), client, pattern, 200)
			if err != nil {
				http.Error(w, jsonError("redis scan failed: "+err.Error()), http.StatusBadGateway)
				return
			}
		}
		var lines []string
		for _, k := range keys {
			if strings.TrimSpace(k) == "" {
				continue
			}
			value, err := readRedisValue(r.Context(), client, k)
			if err != nil {
				continue
			}
			lines = append(lines, redisScriptForValue(value)...)
		}
		writeRedisAudit(r, "redis_generate_script", connID, connName, key+pattern, "")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(strings.Join(lines, "\n")))
	}
}

func RedisExecuteScript() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		var payload redisScriptRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		commands, err := parseRedisScript(payload.Script, 100)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		client, connName, err := openRedisClient(connID, payload.DB)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		results := make([]redisScriptResult, 0, len(commands))
		for _, command := range commands {
			args, err := parseRedisCommand(command.text)
			if err != nil {
				results = append(results, redisScriptResult{Line: command.line, Command: command.text, Error: err.Error()})
				break
			}
			if err := validateRedisCommand(args); err != nil {
				results = append(results, redisScriptResult{Line: command.line, Command: command.text, Error: err.Error()})
				break
			}
			result, err := client.command(r.Context(), args...)
			if err != nil {
				results = append(results, redisScriptResult{Line: command.line, Command: command.text, Error: err.Error()})
				break
			}
			results = append(results, redisScriptResult{Line: command.line, Command: command.text, Result: result})
		}
		writeRedisAudit(r, "redis_execute_script", connID, connName, strconv.Itoa(len(results))+" commands", "")
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}
}

func RedisMoveKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, `{"error":"invalid connection id"}`, http.StatusBadRequest)
			return
		}
		var payload redisMoveRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		payload.Key = strings.TrimSpace(payload.Key)
		if payload.Key == "" {
			http.Error(w, `{"error":"key is required"}`, http.StatusBadRequest)
			return
		}
		if payload.FromDB < 0 || payload.ToDB < 0 {
			http.Error(w, `{"error":"db indexes must be non-negative"}`, http.StatusBadRequest)
			return
		}
		if payload.FromDB == payload.ToDB {
			http.Error(w, `{"error":"target db must be different"}`, http.StatusBadRequest)
			return
		}

		client, connName, err := openRedisClient(connID, &payload.FromDB)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}
		if payload.Overwrite {
			target, _, err := openRedisClient(connID, &payload.ToDB)
			if err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			if _, err := target.command(r.Context(), "DEL", payload.Key); err != nil {
				http.Error(w, jsonError("redis target delete failed: "+err.Error()), http.StatusBadGateway)
				return
			}
		}
		result, err := client.command(r.Context(), "MOVE", payload.Key, strconv.Itoa(payload.ToDB))
		if err != nil {
			writeRedisAudit(r, "redis_move_key", connID, connName, payload.Key, err.Error())
			http.Error(w, jsonError("redis move failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		if moved, ok := result.(int64); ok && moved == 0 {
			http.Error(w, jsonError("redis move failed: target key already exists or source key is missing"), http.StatusConflict)
			return
		}
		writeRedisAudit(r, "redis_move_key", connID, connName, fmt.Sprintf("%s db%d -> db%d", payload.Key, payload.FromDB, payload.ToDB), "")
		json.NewEncoder(w).Encode(map[string]string{"message": "Redis key moved"})
	}
}

func readRedisValue(ctx context.Context, client *redisClient, key string) (redisValueResponse, error) {
	resp := redisValueResponse{Key: key, Type: "none", TTL: -2}
	if ttl, err := client.command(ctx, "TTL", key); err == nil {
		if n, ok := ttl.(int64); ok {
			resp.TTL = n
		}
	}
	rawType, err := client.command(ctx, "TYPE", key)
	if err != nil {
		return resp, err
	}
	keyType, _ := rawType.(string)
	resp.Type = keyType

	switch normalizeRedisType(keyType) {
	case "string":
		value, err := client.command(ctx, "GET", key)
		if err != nil {
			return resp, err
		}
		if s, ok := value.(string); ok {
			resp.Value = s
			resp.Length = int64(len(s))
		}
	case "hash":
		length, _ := redisInt(client.command(ctx, "HLEN", key))
		items, err := client.command(ctx, "HGETALL", key)
		if err != nil {
			return resp, err
		}
		resp.Length = length
		resp.Value = stringPairsToMap(items)
	case "list":
		length, _ := redisInt(client.command(ctx, "LLEN", key))
		items, err := client.command(ctx, "LRANGE", key, "0", "99")
		if err != nil {
			return resp, err
		}
		resp.Length = length
		resp.Truncated = length > 100
		resp.Value = anySliceToStrings(items)
	case "set":
		length, _ := redisInt(client.command(ctx, "SCARD", key))
		items, err := client.command(ctx, "SSCAN", key, "0", "COUNT", "100")
		if err != nil {
			return resp, err
		}
		resp.Length = length
		resp.Value = scanValues(items)
		resp.Truncated = length > 100
	case "zset":
		length, _ := redisInt(client.command(ctx, "ZCARD", key))
		items, err := client.command(ctx, "ZRANGE", key, "0", "99", "WITHSCORES")
		if err != nil {
			return resp, err
		}
		resp.Length = length
		resp.Value = zsetPairs(items)
		resp.Truncated = length > 100
	case "stream":
		length, _ := redisInt(client.command(ctx, "XLEN", key))
		items, err := client.command(ctx, "XRANGE", key, "-", "+", "COUNT", "50")
		if err != nil {
			return resp, err
		}
		resp.Length = length
		resp.Value = streamEntries(items)
		resp.Truncated = length > 50
	case "json":
		value, err := client.command(ctx, "JSON.GET", key)
		if err != nil {
			return resp, err
		}
		if s, ok := value.(string); ok {
			resp.Type = "json"
			resp.Length = int64(len(s))
			var decoded any
			if json.Unmarshal([]byte(s), &decoded) == nil {
				resp.Value = decoded
			} else {
				resp.Value = s
			}
		}
	default:
		resp.Value = nil
	}
	return resp, nil
}

func validateRedisKeyType(keyType string) error {
	switch keyType {
	case "string", "hash", "list", "set", "zset", "stream", "json":
		return nil
	default:
		return fmt.Errorf("unsupported redis type: must be string, hash, list, set, zset, stream, or json")
	}
}

func writeRedisValue(ctx context.Context, client *redisClient, payload redisWriteRequest) error {
	if _, err := client.command(ctx, "DEL", payload.Key); err != nil {
		return err
	}
	switch payload.Type {
	case "json":
		if !json.Valid(payload.Value) {
			return fmt.Errorf("json value must be valid JSON")
		}
		if _, err := client.command(ctx, "JSON.SET", payload.Key, "$", string(payload.Value)); err != nil {
			return err
		}
	case "string":
		var value string
		if err := json.Unmarshal(payload.Value, &value); err != nil {
			return fmt.Errorf("string value must be a JSON string")
		}
		if _, err := client.command(ctx, "SET", payload.Key, value); err != nil {
			return err
		}
	case "hash":
		values := map[string]string{}
		if err := json.Unmarshal(payload.Value, &values); err != nil {
			return fmt.Errorf("hash value must be a JSON object")
		}
		if len(values) == 0 {
			return fmt.Errorf("hash must contain at least one field")
		}
		args := []string{"HSET", payload.Key}
		for field, value := range values {
			args = append(args, field, value)
		}
		if _, err := client.command(ctx, args...); err != nil {
			return err
		}
	case "list":
		values, err := stringListFromRaw(payload.Value, "list")
		if err != nil {
			return err
		}
		args := append([]string{"RPUSH", payload.Key}, values...)
		if _, err := client.command(ctx, args...); err != nil {
			return err
		}
	case "set":
		values, err := stringListFromRaw(payload.Value, "set")
		if err != nil {
			return err
		}
		args := append([]string{"SADD", payload.Key}, values...)
		if _, err := client.command(ctx, args...); err != nil {
			return err
		}
	case "zset":
		var values []redisZSetItem
		if err := json.Unmarshal(payload.Value, &values); err != nil {
			return fmt.Errorf("zset value must be a JSON array of {member, score}")
		}
		if len(values) == 0 {
			return fmt.Errorf("zset must contain at least one member")
		}
		args := []string{"ZADD", payload.Key}
		for _, item := range values {
			args = append(args, strconv.FormatFloat(item.Score, 'f', -1, 64), item.Member)
		}
		if _, err := client.command(ctx, args...); err != nil {
			return err
		}
	case "stream":
		values := map[string]string{}
		if err := json.Unmarshal(payload.Value, &values); err != nil {
			return fmt.Errorf("stream value must be a JSON object")
		}
		if len(values) == 0 {
			return fmt.Errorf("stream entry must contain at least one field")
		}
		args := []string{"XADD", payload.Key, "*"}
		for field, value := range values {
			args = append(args, field, value)
		}
		if _, err := client.command(ctx, args...); err != nil {
			return err
		}
	}
	if payload.TTL > 0 {
		if _, err := client.command(ctx, "EXPIRE", payload.Key, strconv.FormatInt(payload.TTL, 10)); err != nil {
			return err
		}
	}
	return nil
}

func stringListFromRaw(raw json.RawMessage, label string) ([]string, error) {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s value must be a JSON array of strings", label)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s must contain at least one item", label)
	}
	return values, nil
}

func normalizeRedisType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "rejson-rl", "json":
		return "json"
	default:
		return normalized
	}
}

func scanRedisKeys(ctx context.Context, client *redisClient, pattern string, limit int) ([]string, error) {
	cursor := "0"
	keys := []string{}
	for {
		resp, err := client.command(ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", "100")
		if err != nil {
			return nil, err
		}
		values, ok := resp.([]any)
		if !ok || len(values) != 2 {
			return nil, fmt.Errorf("unexpected redis scan response")
		}
		cursor, _ = values[0].(string)
		for _, raw := range anySliceToStrings(values[1]) {
			keys = append(keys, raw)
			if len(keys) >= limit {
				return keys, nil
			}
		}
		if cursor == "0" {
			return keys, nil
		}
	}
}

func redisScriptForValue(value redisValueResponse) []string {
	lines := []string{"DEL " + redisQuote(value.Key)}
	switch normalizeRedisType(value.Type) {
	case "string":
		lines = append(lines, "SET "+redisQuote(value.Key)+" "+redisQuote(fmt.Sprint(value.Value)))
	case "hash":
		if values, ok := value.Value.(map[string]string); ok {
			args := []string{"HSET", redisQuote(value.Key)}
			for field, item := range values {
				args = append(args, redisQuote(field), redisQuote(item))
			}
			lines = append(lines, strings.Join(args, " "))
		}
	case "list":
		if values, ok := value.Value.([]string); ok && len(values) > 0 {
			args := []string{"RPUSH", redisQuote(value.Key)}
			for _, item := range values {
				args = append(args, redisQuote(item))
			}
			lines = append(lines, strings.Join(args, " "))
		}
	case "set":
		if values, ok := value.Value.([]string); ok && len(values) > 0 {
			args := []string{"SADD", redisQuote(value.Key)}
			for _, item := range values {
				args = append(args, redisQuote(item))
			}
			lines = append(lines, strings.Join(args, " "))
		}
	case "zset":
		if values, ok := value.Value.([]map[string]string); ok && len(values) > 0 {
			args := []string{"ZADD", redisQuote(value.Key)}
			for _, item := range values {
				args = append(args, redisQuote(item["score"]), redisQuote(item["member"]))
			}
			lines = append(lines, strings.Join(args, " "))
		}
	case "stream":
		if values, ok := value.Value.([]map[string]any); ok {
			for _, entry := range values {
				fields, _ := entry["fields"].(map[string]string)
				if len(fields) == 0 {
					continue
				}
				args := []string{"XADD", redisQuote(value.Key), "*"}
				for field, item := range fields {
					args = append(args, redisQuote(field), redisQuote(item))
				}
				lines = append(lines, strings.Join(args, " "))
			}
		}
	case "json":
		body, _ := json.Marshal(value.Value)
		lines = append(lines, "JSON.SET "+redisQuote(value.Key)+" $ "+redisQuote(string(body)))
	}
	if value.TTL > 0 {
		lines = append(lines, "EXPIRE "+redisQuote(value.Key)+" "+strconv.FormatInt(value.TTL, 10))
	}
	return lines
}

func redisQuote(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func parseRedisCommand(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	return args, nil
}

type parsedRedisScriptCommand struct {
	line int
	text string
}

func parseRedisScript(script string, maxCommands int) ([]parsedRedisScriptCommand, error) {
	var commands []parsedRedisScriptCommand
	for idx, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "--") {
			continue
		}
		commands = append(commands, parsedRedisScriptCommand{line: idx + 1, text: trimmed})
		if len(commands) > maxCommands {
			return nil, fmt.Errorf("script is limited to %d commands", maxCommands)
		}
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("script is empty")
	}
	return commands, nil
}

func validateRedisCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("command is required")
	}
	blocked := map[string]bool{
		"ACL":       true,
		"CONFIG":    true,
		"DEBUG":     true,
		"EVAL":      true,
		"EVALSHA":   true,
		"FLUSHALL":  true,
		"FLUSHDB":   true,
		"FUNCTION":  true,
		"MIGRATE":   true,
		"MODULE":    true,
		"REPLICAOF": true,
		"SCRIPT":    true,
		"SHUTDOWN":  true,
		"SLAVEOF":   true,
	}
	name := strings.ToUpper(args[0])
	if blocked[name] {
		return fmt.Errorf("redis command %s is blocked in the web console", name)
	}
	return nil
}

func openRedisClient(connID int64, dbOverride *int) (*redisClient, string, error) {
	var in ConnectionInput
	var ssl int
	var encPassword string
	var connName string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT name, driver, COALESCE(host,''), COALESCE(port,0), database, COALESCE(username,''), COALESCE(password,''), ssl FROM connections WHERE id=?`), connID,
	).Scan(&connName, &in.Driver, &in.Host, &in.Port, &in.Database, &in.Username, &encPassword, &ssl)
	if err != nil {
		return nil, "", fmt.Errorf("connection not found")
	}
	if in.Driver != "redis" {
		return nil, "", fmt.Errorf("connection is not redis")
	}
	password, err := decryptCredential(encPassword)
	if err != nil {
		return nil, "", fmt.Errorf("decryption error")
	}
	in.Password = password
	in.SSL = ssl == 1
	client, err := newRedisClientFromInput(in)
	if err == nil && dbOverride != nil {
		if *dbOverride < 0 {
			return nil, "", fmt.Errorf("redis database must be a non-negative number")
		}
		client.db = *dbOverride
	}
	return client, connName, err
}

func redisDBFromRequest(r *http.Request) *int {
	raw := strings.TrimSpace(r.URL.Query().Get("db"))
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return nil
	}
	return &value
}

func newRedisClientFromInput(in ConnectionInput) (*redisClient, error) {
	if strings.TrimSpace(in.Host) == "" {
		return nil, fmt.Errorf("host is required")
	}
	if in.Port == 0 {
		in.Port = 6379
	}
	db := 0
	if strings.TrimSpace(in.Database) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(in.Database))
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("redis database must be a non-negative number")
		}
		db = parsed
	}
	return &redisClient{
		address:  fmt.Sprintf("%s:%d", in.Host, in.Port),
		username: in.Username,
		password: in.Password,
		db:       db,
		tls:      in.SSL,
		timeout:  3 * time.Second,
	}, nil
}

func (c *redisClient) command(ctx context.Context, args ...string) (any, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	if c.password != "" {
		authArgs := []string{"AUTH", c.password}
		if c.username != "" {
			authArgs = []string{"AUTH", c.username, c.password}
		}
		if err := writeRedisCommand(conn, authArgs...); err != nil {
			return nil, err
		}
		if _, err := readRedisRESP(reader); err != nil {
			return nil, err
		}
	}
	if c.db > 0 {
		if err := writeRedisCommand(conn, "SELECT", strconv.Itoa(c.db)); err != nil {
			return nil, err
		}
		if _, err := readRedisRESP(reader); err != nil {
			return nil, err
		}
	}
	if err := writeRedisCommand(conn, args...); err != nil {
		return nil, err
	}
	return readRedisRESP(reader)
}

func (c *redisClient) dial(ctx context.Context) (net.Conn, error) {
	deadline := time.Now().Add(c.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	var conn net.Conn
	var err error
	dialer := &net.Dialer{Timeout: time.Until(deadline)}
	if c.tls {
		conn, err = tls.DialWithDialer(dialer, "tcp", c.address, &tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", c.address)
	}
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	return conn, nil
}

func writeRedisCommand(w io.Writer, args ...string) error {
	if _, err := fmt.Fprintf(w, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

func readRedisRESP(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return readRedisLine(r)
	case '-':
		line, lineErr := readRedisLine(r)
		if lineErr != nil {
			return nil, lineErr
		}
		return nil, errors.New(line)
	case ':':
		line, lineErr := readRedisLine(r)
		if lineErr != nil {
			return nil, lineErr
		}
		return strconv.ParseInt(line, 10, 64)
	case '$':
		line, lineErr := readRedisLine(r)
		if lineErr != nil {
			return nil, lineErr
		}
		size, parseErr := strconv.Atoi(line)
		if parseErr != nil {
			return nil, parseErr
		}
		if size == -1 {
			return nil, nil
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:size]), nil
	case '*':
		line, lineErr := readRedisLine(r)
		if lineErr != nil {
			return nil, lineErr
		}
		size, parseErr := strconv.Atoi(line)
		if parseErr != nil {
			return nil, parseErr
		}
		if size == -1 {
			return nil, nil
		}
		values := make([]any, 0, size)
		for i := 0; i < size; i++ {
			value, itemErr := readRedisRESP(r)
			if itemErr != nil {
				return nil, itemErr
			}
			values = append(values, value)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("unsupported RESP prefix %q", prefix)
	}
}

func readRedisLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func connectionIDFromPath(path string) (int64, error) {
	trimmed := strings.TrimPrefix(path, "/api/connections/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return 0, fmt.Errorf("missing connection id")
	}
	return strconv.ParseInt(parts[0], 10, 64)
}

func queryInt(r *http.Request, key string, fallback, minValue, maxValue int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func redisInt(value any, err error) (int64, bool) {
	if err != nil {
		return 0, false
	}
	n, ok := value.(int64)
	return n, ok
}

func stringPairsToMap(value any) map[string]string {
	items := anySliceToStrings(value)
	result := make(map[string]string, len(items)/2)
	for i := 0; i+1 < len(items); i += 2 {
		result[items[i]] = items[i+1]
	}
	return result
}

func anySliceToStrings(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return []string{}
	}
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			items = append(items, s)
		}
	}
	return items
}

func scanValues(value any) []string {
	raw, ok := value.([]any)
	if !ok || len(raw) != 2 {
		return []string{}
	}
	return anySliceToStrings(raw[1])
}

func zsetPairs(value any) []map[string]string {
	items := anySliceToStrings(value)
	result := make([]map[string]string, 0, len(items)/2)
	for i := 0; i+1 < len(items); i += 2 {
		result = append(result, map[string]string{"member": items[i], "score": items[i+1]})
	}
	return result
}

func streamEntries(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}
	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.([]any)
		if !ok || len(entry) != 2 {
			continue
		}
		id, _ := entry[0].(string)
		entries = append(entries, map[string]any{
			"id":     id,
			"fields": stringPairsToMap(entry[1]),
		})
	}
	return entries
}

func writeRedisAudit(r *http.Request, action string, connID int64, connName, target, errMsg string) {
	username := strings.TrimSpace(r.Header.Get("X-Username"))
	if username == "" {
		username = "anonymous"
	}
	writeAuditEvent("redis", action, target, "", username, &connID, connName, "", 0, 0, errMsg)
}
