package cache

import (
	"context"
	"strconv"
	"sync"
	"time"
)

type memoryEntry struct {
	value     string
	expiresAt time.Time
}

type MemoryStore struct {
	mu    sync.Mutex
	items map[string]memoryEntry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: map[string]memoryEntry{}}
}

func (s *MemoryStore) BackendName() string { return "memory" }

func (s *MemoryStore) Close() error { return nil }

func (s *MemoryStore) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	if !ok {
		return "", false, nil
	}
	if s.expired(item) {
		delete(s.items, key)
		return "", false, nil
	}
	return item.value, true, nil
}

func (s *MemoryStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	s.items[key] = memoryEntry{
		value:     value,
		expiresAt: expiresAt(ttl),
	}
	return nil
}

func (s *MemoryStore) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()

	var next int64 = 1
	if item, ok := s.items[key]; ok && !s.expired(item) {
		if parsed, err := strconv.ParseInt(item.value, 10, 64); err == nil {
			next = parsed + 1
		}
	}
	s.items[key] = memoryEntry{
		value:     strconv.FormatInt(next, 10),
		expiresAt: expiresAt(ttl),
	}
	return next, nil
}

func (s *MemoryStore) AcquireLock(_ context.Context, key, owner string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	if item, ok := s.items[key]; ok && !s.expired(item) {
		return false, nil
	}
	s.items[key] = memoryEntry{
		value:     owner,
		expiresAt: expiresAt(ttl),
	}
	return true, nil
}

func (s *MemoryStore) ReleaseLock(_ context.Context, key, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	if !ok {
		return nil
	}
	if item.value != owner {
		return nil
	}
	delete(s.items, key)
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
	return nil
}

func (s *MemoryStore) gcLocked() {
	now := time.Now()
	for key, item := range s.items {
		if !item.expiresAt.IsZero() && now.After(item.expiresAt) {
			delete(s.items, key)
		}
	}
}

func (s *MemoryStore) expired(item memoryEntry) bool {
	return !item.expiresAt.IsZero() && time.Now().After(item.expiresAt)
}

func expiresAt(ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return time.Now().Add(ttl)
}
