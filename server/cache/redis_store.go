package cache

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/anveesa/nias/config"
)

type RedisStore struct {
	address      string
	password     string
	db           int
	prefix       string
	dialTimeout  time.Duration
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func NewRedisStore(cfg *config.Config) (*RedisStore, error) {
	u, err := url.Parse(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	if u.Scheme != "redis" {
		return nil, fmt.Errorf("unsupported REDIS_URL scheme: %s", u.Scheme)
	}

	password := cfg.RedisPassword
	if password == "" && u.User != nil {
		if p, ok := u.User.Password(); ok {
			password = p
		}
	}

	db := cfg.RedisDB
	if path := strings.Trim(strings.TrimSpace(u.Path), "/"); path != "" {
		if parsed, parseErr := strconv.Atoi(path); parseErr == nil {
			db = parsed
		}
	}

	store := &RedisStore{
		address:      u.Host,
		password:     password,
		db:           db,
		prefix:       cfg.RedisPrefix,
		dialTimeout:  2 * time.Second,
		readTimeout:  2 * time.Second,
		writeTimeout: 2 * time.Second,
	}
	if store.address == "" {
		store.address = "127.0.0.1:6379"
	}
	if err := store.Ping(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *RedisStore) BackendName() string { return "redis" }

func (s *RedisStore) Close() error { return nil }

func (s *RedisStore) Ping(ctx context.Context) error {
	resp, err := s.command(ctx, "PING")
	if err != nil {
		return err
	}
	if pong, ok := resp.(string); !ok || strings.ToUpper(pong) != "PONG" {
		return fmt.Errorf("unexpected PING response: %v", resp)
	}
	return nil
}

func (s *RedisStore) Get(ctx context.Context, key string) (string, bool, error) {
	resp, err := s.command(ctx, "GET", s.prefixed(key))
	if err != nil {
		return "", false, err
	}
	if resp == nil {
		return "", false, nil
	}
	str, ok := resp.(string)
	if !ok {
		return "", false, fmt.Errorf("unexpected GET response type %T", resp)
	}
	return str, true, nil
}

func (s *RedisStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	seconds := strconv.FormatInt(maxInt64(1, int64(ttl/time.Second)), 10)
	resp, err := s.command(ctx, "SET", s.prefixed(key), value, "EX", seconds)
	if err != nil {
		return err
	}
	if status, ok := resp.(string); !ok || strings.ToUpper(status) != "OK" {
		return fmt.Errorf("unexpected SET response: %v", resp)
	}
	return nil
}

func (s *RedisStore) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	fullKey := s.prefixed(key)
	resp, err := s.command(ctx, "INCR", fullKey)
	if err != nil {
		return 0, err
	}
	value, ok := resp.(int64)
	if !ok {
		return 0, fmt.Errorf("unexpected INCR response: %v", resp)
	}
	if value == 1 && ttl > 0 {
		_, expireErr := s.command(ctx, "EXPIRE", fullKey, strconv.FormatInt(maxInt64(1, int64(ttl/time.Second)), 10))
		if expireErr != nil {
			return 0, expireErr
		}
	}
	return value, nil
}

func (s *RedisStore) AcquireLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	fullKey := s.prefixed(key)
	seconds := strconv.FormatInt(maxInt64(1, int64(ttl/time.Second)), 10)
	resp, err := s.command(ctx, "SET", fullKey, owner, "NX", "EX", seconds)
	if err != nil {
		if strings.Contains(strings.ToUpper(err.Error()), "NIL") {
			return false, nil
		}
		return false, err
	}
	status, ok := resp.(string)
	if !ok {
		return false, fmt.Errorf("unexpected SET NX response: %v", resp)
	}
	return strings.ToUpper(status) == "OK", nil
}

func (s *RedisStore) ReleaseLock(ctx context.Context, key, owner string) error {
	fullKey := s.prefixed(key)
	value, found, err := s.Get(ctx, key)
	if err != nil || !found {
		return err
	}
	if value != owner {
		return nil
	}
	_, err = s.command(ctx, "DEL", fullKey)
	return err
}

func (s *RedisStore) Delete(ctx context.Context, key string) error {
	_, err := s.command(ctx, "DEL", s.prefixed(key))
	return err
}

func (s *RedisStore) prefixed(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + ":" + key
}

func (s *RedisStore) command(ctx context.Context, args ...string) (any, error) {
	conn, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	if s.password != "" {
		if err := writeRESPCommand(conn, "AUTH", s.password); err != nil {
			return nil, err
		}
		if _, err := readRESP(reader); err != nil {
			return nil, err
		}
	}
	if s.db > 0 {
		if err := writeRESPCommand(conn, "SELECT", strconv.Itoa(s.db)); err != nil {
			return nil, err
		}
		if _, err := readRESP(reader); err != nil {
			return nil, err
		}
	}
	if err := writeRESPCommand(conn, args...); err != nil {
		return nil, err
	}
	return readRESP(reader)
}

func (s *RedisStore) dial(ctx context.Context) (net.Conn, error) {
	deadline := time.Now().Add(s.dialTimeout)
	if ctx != nil {
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
	}
	conn, err := net.DialTimeout("tcp", s.address, time.Until(deadline))
	if err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(s.readTimeout))
	_ = conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
	return conn, nil
}

func writeRESPCommand(w io.Writer, args ...string) error {
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

func readRESP(r *bufio.Reader) (any, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return readRESPLine(r)
	case '-':
		line, lineErr := readRESPLine(r)
		if lineErr != nil {
			return nil, lineErr
		}
		return nil, errors.New(line)
	case ':':
		line, lineErr := readRESPLine(r)
		if lineErr != nil {
			return nil, lineErr
		}
		value, parseErr := strconv.ParseInt(line, 10, 64)
		if parseErr != nil {
			return nil, parseErr
		}
		return value, nil
	case '$':
		line, lineErr := readRESPLine(r)
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
		line, lineErr := readRESPLine(r)
		if lineErr != nil {
			return nil, lineErr
		}
		size, parseErr := strconv.Atoi(line)
		if parseErr != nil {
			return nil, parseErr
		}
		values := make([]any, 0, size)
		for i := 0; i < size; i++ {
			value, itemErr := readRESP(r)
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

func readRESPLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
