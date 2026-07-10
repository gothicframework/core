package helpers

import (
	"log"
	"sync"
	"time"

	"github.com/gothicframework/core/config"
)

// The routing/caching config types and enums are defined canonically in the
// lightweight, user-facing pkg/config package (so a project can write them as
// gothic.* in gothic.config.go). They are re-exported here as aliases so the rest
// of this package — and existing user code — keeps using the gothicRoutes.* names
// unchanged, while there is exactly one definition.
type (
	CacheType         = config.CacheType
	StaticFilesMode   = config.StaticFilesMode
	CompressionMethod = config.CompressionMethod
	AppConfig         = config.RuntimeConfig
	CacheConfig       = config.CacheConfig
)

const (
	CACHE_CONTROL_HEADERS = config.CACHE_CONTROL_HEADERS
	IN_MEMORY             = config.IN_MEMORY
	REDIS                 = config.REDIS
	LOCAL_FILES           = config.LOCAL_FILES

	HOT_RELOAD_ONLY = config.HOT_RELOAD_ONLY
	ALL_ENVS        = config.ALL_ENVS

	GZIP   = config.GZIP
	BROTLI = config.BROTLI
)

type CacheStore interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte, ttl time.Duration)
	Flush() error
	Close() error
}

var (
	globalMu         sync.Mutex
	globalCacheStore CacheStore
	globalCacheType  CacheType
	globalInitDone   bool
)

// InitCache explicitly initializes the global cache store with the given type and config.
// This should be called from the server entry point before routes are registered.
// If called multiple times, only the first call takes effect.
func InitCache(cacheType CacheType, config *CacheConfig) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalInitDone {
		return
	}

	globalCacheType = cacheType
	globalCacheStore = buildCacheStore(cacheType, config)
	globalInitDone = true
}

// getGlobalCacheStore returns the global CacheStore, lazily initializing with CACHE_CONTROL_HEADERS defaults if needed.
func getGlobalCacheStore() CacheStore {
	globalMu.Lock()
	defer globalMu.Unlock()

	if !globalInitDone {
		globalCacheType = CACHE_CONTROL_HEADERS
		globalCacheStore = buildCacheStore(CACHE_CONTROL_HEADERS, nil)
		globalInitDone = true
	}
	return globalCacheStore
}

// getGlobalCacheType returns the global CacheType, lazily initializing with CACHE_CONTROL_HEADERS defaults if needed.
func getGlobalCacheType() CacheType {
	globalMu.Lock()
	defer globalMu.Unlock()

	if !globalInitDone {
		globalCacheType = CACHE_CONTROL_HEADERS
		globalCacheStore = buildCacheStore(CACHE_CONTROL_HEADERS, nil)
		globalInitDone = true
	}
	return globalCacheType
}

// resetGlobalCache resets the global cache state. Used in tests for isolation.
func resetGlobalCache() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalCacheStore != nil {
		globalCacheStore.Close()
	}
	globalCacheStore = nil
	globalCacheType = CACHE_CONTROL_HEADERS
	globalInitDone = false
}

func buildCacheStore(cacheType CacheType, cacheConfig *CacheConfig) CacheStore {
	switch cacheType {
	case IN_MEMORY:
		return NewInMemoryCacheStore(cacheConfig)
	case REDIS:
		store, err := NewRedisCacheStore(cacheConfig)
		if err != nil {
			log.Printf("gothic: failed to initialize Redis cache, falling back to in-memory: %v", err)
			return NewInMemoryCacheStore(cacheConfig)
		}
		return store
	case LOCAL_FILES:
		store, err := NewLocalFilesCacheStore(cacheConfig)
		if err != nil {
			log.Printf("gothic: failed to initialize file cache, falling back to in-memory: %v", err)
			return NewInMemoryCacheStore(cacheConfig)
		}
		return store
	default:
		// CACHE_CONTROL_HEADERS or unknown: return in-memory as a no-op placeholder.
		// CACHE_CONTROL_HEADERS handlers set Cache-Control headers and skip the store.
		return NewInMemoryCacheStore(cacheConfig)
	}
}
