package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
)

type memcacheClient struct {
	host string
	port int
}

type memcacheValueResponse struct {
	Key   string `json:"key"`
	Flags int    `json:"flags"`
	Bytes int    `json:"bytes"`
	Value string `json:"value"`
	Found bool   `json:"found"`
}

type memcacheWriteRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Flags int    `json:"flags"`
	TTL   int    `json:"ttl"`
}

type memcacheFlushRequest struct {
	Delay int `json:"delay"`
}

func MemcachePing() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		client, _, err := openMemcacheClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		start := time.Now()
		line, err := client.singleLine(r.Context(), "version")
		if err != nil {
			http.Error(w, jsonError("memcache ping failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		if !strings.HasPrefix(strings.ToUpper(line), "VERSION") {
			http.Error(w, jsonError("memcache ping failed: "+line), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"message":    line,
			"latency_ms": time.Since(start).Milliseconds(),
		})
	}
}

func MemcacheStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		client, _, err := openMemcacheClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		stats, err := client.stats(r.Context())
		if err != nil {
			http.Error(w, jsonError("memcache stats failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(stats)
	}
}

func MemcacheKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		client, _, err := openMemcacheClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}

		switch r.Method {
		case http.MethodGet:
			key := strings.TrimSpace(r.URL.Query().Get("key"))
			if err := validateMemcacheKey(key); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			value, err := client.get(r.Context(), key)
			if err != nil {
				http.Error(w, jsonError("memcache read failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(value)
		case http.MethodPost, http.MethodPut:
			var payload memcacheWriteRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, jsonError("bad request"), http.StatusBadRequest)
				return
			}
			if err := validateMemcacheKey(payload.Key); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			if payload.TTL < 0 {
				http.Error(w, jsonError("ttl must be zero or greater"), http.StatusBadRequest)
				return
			}
			if err := client.set(r.Context(), payload); err != nil {
				http.Error(w, jsonError("memcache write failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"message": "Stored"})
		case http.MethodDelete:
			key := strings.TrimSpace(r.URL.Query().Get("key"))
			if err := validateMemcacheKey(key); err != nil {
				http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
				return
			}
			deleted, err := client.delete(r.Context(), key)
			if err != nil {
				http.Error(w, jsonError("memcache delete failed: "+err.Error()), http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"deleted": deleted})
		default:
			http.NotFound(w, r)
		}
	}
}

func MemcacheFlush() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, jsonError("invalid connection id"), http.StatusBadRequest)
			return
		}
		client, _, err := openMemcacheClient(connID)
		if err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadGateway)
			return
		}
		var payload memcacheFlushRequest
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Delay < 0 {
			http.Error(w, jsonError("delay must be zero or greater"), http.StatusBadRequest)
			return
		}
		line, err := client.singleLine(r.Context(), fmt.Sprintf("flush_all %d", payload.Delay))
		if err != nil {
			http.Error(w, jsonError("memcache flush failed: "+err.Error()), http.StatusBadGateway)
			return
		}
		if strings.ToUpper(line) != "OK" {
			http.Error(w, jsonError("memcache flush failed: "+line), http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"message": "Flushed"})
	}
}

func openMemcacheClient(connID int64) (*memcacheClient, string, error) {
	var in ConnectionInput
	var connName string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT name, driver, COALESCE(host,''), COALESCE(port,0) FROM connections WHERE id=?`), connID,
	).Scan(&connName, &in.Driver, &in.Host, &in.Port)
	if err != nil {
		return nil, "", fmt.Errorf("connection not found")
	}
	if in.Driver != "memcache" {
		return nil, "", fmt.Errorf("connection is not memcache")
	}
	host := strings.TrimSpace(in.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := in.Port
	if port == 0 {
		port = 11211
	}
	return &memcacheClient{host: host, port: port}, connName, nil
}

func (c *memcacheClient) dial(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", c.host, c.port))
	if err != nil {
		return nil, nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	return conn, bufio.NewReader(conn), nil
}

func (c *memcacheClient) singleLine(ctx context.Context, command string) (string, error) {
	conn, reader, err := c.dial(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "%s\r\n", command); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	return strings.TrimSpace(line), err
}

func (c *memcacheClient) stats(ctx context.Context) (map[string]string, error) {
	conn, reader, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := fmt.Fprint(conn, "stats\r\n"); err != nil {
		return nil, err
	}
	stats := map[string]string{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "END" {
			return stats, nil
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) == 3 && parts[0] == "STAT" {
			stats[parts[1]] = parts[2]
		}
	}
}

func (c *memcacheClient) get(ctx context.Context, key string) (memcacheValueResponse, error) {
	conn, reader, err := c.dial(ctx)
	if err != nil {
		return memcacheValueResponse{}, err
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "get %s\r\n", key); err != nil {
		return memcacheValueResponse{}, err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return memcacheValueResponse{}, err
	}
	line = strings.TrimSpace(line)
	if line == "END" {
		return memcacheValueResponse{Key: key, Found: false}, nil
	}
	parts := strings.Split(line, " ")
	if len(parts) < 4 || parts[0] != "VALUE" {
		return memcacheValueResponse{}, fmt.Errorf("unexpected response: %s", line)
	}
	flags, _ := strconv.Atoi(parts[2])
	size, _ := strconv.Atoi(parts[3])
	buf := make([]byte, size)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return memcacheValueResponse{}, err
	}
	if _, err := reader.ReadString('\n'); err != nil {
		return memcacheValueResponse{}, err
	}
	end, err := reader.ReadString('\n')
	if err != nil {
		return memcacheValueResponse{}, err
	}
	if strings.TrimSpace(end) != "END" {
		return memcacheValueResponse{}, fmt.Errorf("unexpected response terminator")
	}
	return memcacheValueResponse{Key: parts[1], Flags: flags, Bytes: size, Value: string(buf), Found: true}, nil
}

func (c *memcacheClient) set(ctx context.Context, payload memcacheWriteRequest) error {
	conn, reader, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	value := []byte(payload.Value)
	if _, err := fmt.Fprintf(conn, "set %s %d %d %d\r\n%s\r\n", payload.Key, payload.Flags, payload.TTL, len(value), value); err != nil {
		return err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	line = strings.TrimSpace(line)
	if line != "STORED" {
		return fmt.Errorf("%s", line)
	}
	return nil
}

func (c *memcacheClient) delete(ctx context.Context, key string) (bool, error) {
	line, err := c.singleLine(ctx, "delete "+key)
	if err != nil {
		return false, err
	}
	switch line {
	case "DELETED":
		return true, nil
	case "NOT_FOUND":
		return false, nil
	default:
		return false, fmt.Errorf("%s", line)
	}
}

func validateMemcacheKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if len(key) > 250 {
		return fmt.Errorf("key must be 250 bytes or less")
	}
	if strings.ContainsAny(key, " \r\n\t") {
		return fmt.Errorf("key cannot contain whitespace")
	}
	return nil
}
