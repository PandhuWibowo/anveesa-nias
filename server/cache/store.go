package cache

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/anveesa/nias/config"
)

type Store interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
	AcquireLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key, owner string) error
	Delete(ctx context.Context, key string) error
	Close() error
	BackendName() string
}

var (
	defaultStore Store = NewMemoryStore()
	storeMu      sync.RWMutex
)

func Init(cfg *config.Config) Store {
	storeMu.Lock()
	defer storeMu.Unlock()

	if defaultStore != nil {
		_ = defaultStore.Close()
	}

	if cfg == nil || cfg.RedisURL == "" {
		defaultStore = NewMemoryStore()
		log.Println("Cache backend: in-memory (REDIS_URL not set)")
		return defaultStore
	}

	redisStore, err := NewRedisStore(cfg)
	if err != nil {
		log.Printf("WARNING: Redis unavailable, using in-memory cache instead: %v", err)
		defaultStore = NewMemoryStore()
		return defaultStore
	}

	defaultStore = redisStore
	log.Printf("Cache backend: %s", defaultStore.BackendName())
	return defaultStore
}

func Default() Store {
	storeMu.RLock()
	store := defaultStore
	storeMu.RUnlock()
	if store != nil {
		return store
	}

	storeMu.Lock()
	defer storeMu.Unlock()
	if defaultStore == nil {
		defaultStore = NewMemoryStore()
	}
	return defaultStore
}

func Close() error {
	storeMu.Lock()
	defer storeMu.Unlock()
	if defaultStore == nil {
		return nil
	}
	return defaultStore.Close()
}
