package helpers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestInMemoryCacheStore(t *testing.T) {
	store := NewInMemoryCacheStore(nil)

	// Test set and get
	store.Set("key1", []byte("hello"), 0)
	data, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(data, []byte("hello")) {
		t.Errorf("expected 'hello', got '%s'", string(data))
	}

	// Test cache miss
	_, ok = store.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}

	// Test TTL expiry
	store.Set("expire", []byte("temp"), 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	_, ok = store.Get("expire")
	if ok {
		t.Error("expected cache miss after TTL expiry")
	}

	// Test no expiry (TTL = 0)
	store.Set("forever", []byte("persistent"), 0)
	time.Sleep(10 * time.Millisecond)
	_, ok = store.Get("forever")
	if !ok {
		t.Error("expected cache hit for entry with no TTL")
	}

	store.Close()
}

func TestInMemoryCacheStoreWithGzipCompression(t *testing.T) {
	config := &CacheConfig{Compression: true, CompressionMethod: GZIP}
	store := NewInMemoryCacheStore(config)

	original := []byte("<html><body>Hello World</body></html>")
	store.Set("key", original, 0)
	data, ok := store.Get("key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(data, original) {
		t.Errorf("decompressed data doesn't match original")
	}
}

func TestInMemoryCacheStoreWithBrotliCompression(t *testing.T) {
	config := &CacheConfig{Compression: true, CompressionMethod: BROTLI}
	store := NewInMemoryCacheStore(config)

	original := []byte("<html><body>Hello World Brotli</body></html>")
	store.Set("key", original, 0)
	data, ok := store.Get("key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(data, original) {
		t.Errorf("decompressed brotli data doesn't match original")
	}
}

func TestLocalFilesCacheStore(t *testing.T) {
	tmpDir := t.TempDir()
	config := &CacheConfig{CacheFilesPath: tmpDir}

	store, err := NewLocalFilesCacheStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Test set and get
	store.Set("key1", []byte("file-data"), 0)
	data, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(data, []byte("file-data")) {
		t.Errorf("expected 'file-data', got '%s'", string(data))
	}

	// Test cache miss
	_, ok = store.Get("nonexistent")
	if ok {
		t.Error("expected cache miss")
	}

	// Test TTL expiry
	store.Set("expire", []byte("temp"), 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	_, ok = store.Get("expire")
	if ok {
		t.Error("expected cache miss after TTL expiry")
	}
}

func TestLocalFilesCacheStoreWithCompression(t *testing.T) {
	tmpDir := t.TempDir()
	config := &CacheConfig{CacheFilesPath: tmpDir, Compression: true}

	store, err := NewLocalFilesCacheStore(config)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	original := []byte("<html><body>Compressed File Data</body></html>")
	store.Set("key", original, 0)
	data, ok := store.Get("key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(data, original) {
		t.Errorf("decompressed data doesn't match original")
	}
}

func TestBuildCacheStore(t *testing.T) {
	// CACHE_CONTROL_HEADERS -> InMemoryCacheStore (handlers skip it via Cache-Control headers)
	store := buildCacheStore(CACHE_CONTROL_HEADERS, nil)
	if _, ok := store.(*InMemoryCacheStore); !ok {
		t.Error("CACHE_CONTROL_HEADERS should return InMemoryCacheStore")
	}

	// IN_MEMORY -> InMemoryCacheStore
	store = buildCacheStore(IN_MEMORY, nil)
	if _, ok := store.(*InMemoryCacheStore); !ok {
		t.Error("IN_MEMORY should return InMemoryCacheStore")
	}

	// REDIS without config -> fallback to InMemoryCacheStore
	store = buildCacheStore(REDIS, nil)
	if _, ok := store.(*InMemoryCacheStore); !ok {
		t.Error("REDIS without config should fallback to InMemoryCacheStore")
	}

	// LOCAL_FILES -> LocalFilesCacheStore
	tmpDir := t.TempDir()
	store = buildCacheStore(LOCAL_FILES, &CacheConfig{CacheFilesPath: tmpDir})
	if _, ok := store.(*LocalFilesCacheStore); !ok {
		t.Error("LOCAL_FILES should return LocalFilesCacheStore")
	}
}

func TestCompressDecompressGzip(t *testing.T) {
	original := []byte("Hello, this is test data for compression!")

	compressed, err := compressData(original, GZIP)
	if err != nil {
		t.Fatalf("gzip compress error: %v", err)
	}
	decompressed, err := decompressData(compressed, GZIP)
	if err != nil {
		t.Fatalf("gzip decompress error: %v", err)
	}
	if !bytes.Equal(original, decompressed) {
		t.Error("gzip round-trip failed")
	}
}

func TestCompressDecompressBrotli(t *testing.T) {
	original := []byte("Hello, this is test data for brotli compression!")

	compressed, err := compressData(original, BROTLI)
	if err != nil {
		t.Fatalf("brotli compress error: %v", err)
	}
	decompressed, err := decompressData(compressed, BROTLI)
	if err != nil {
		t.Fatalf("brotli decompress error: %v", err)
	}
	if !bytes.Equal(original, decompressed) {
		t.Error("brotli round-trip failed")
	}
}

func TestCompressDecompressDefaultIsGzip(t *testing.T) {
	original := []byte("Testing default compression method")

	compressed, err := compressData(original, GZIP)
	if err != nil {
		t.Fatalf("default compress error: %v", err)
	}
	decompressed, err := decompressData(compressed, GZIP)
	if err != nil {
		t.Fatalf("default decompress error: %v", err)
	}
	if !bytes.Equal(original, decompressed) {
		t.Error("default compression round-trip failed")
	}
}

func TestCacheTypeZeroValueIsCACHE_CONTROL_HEADERS(t *testing.T) {
	var ct CacheType
	if ct != CACHE_CONTROL_HEADERS {
		t.Errorf("zero value of CacheType should be CACHE_CONTROL_HEADERS, got %d", ct)
	}
}

func TestInitCache(t *testing.T) {
	resetGlobalCache()

	InitCache(IN_MEMORY, &CacheConfig{Compression: true, CompressionMethod: GZIP})

	store := getGlobalCacheStore()
	if _, ok := store.(*InMemoryCacheStore); !ok {
		t.Error("expected InMemoryCacheStore after InitCache with IN_MEMORY")
	}

	cacheType := getGlobalCacheType()
	if cacheType != IN_MEMORY {
		t.Errorf("expected IN_MEMORY, got %d", cacheType)
	}

	// Calling InitCache again should be a no-op (already initialized)
	InitCache(REDIS, nil)
	cacheType = getGlobalCacheType()
	if cacheType != IN_MEMORY {
		t.Error("InitCache should be a no-op after first call")
	}

	resetGlobalCache()
}

func TestGlobalCacheInitialization(t *testing.T) {
	resetGlobalCache()

	// Use InitCache directly instead of relying on file-based config
	InitCache(IN_MEMORY, nil)

	store := getGlobalCacheStore()
	if _, ok := store.(*InMemoryCacheStore); !ok {
		t.Error("expected InMemoryCacheStore for in_memory config")
	}

	cacheType := getGlobalCacheType()
	if cacheType != IN_MEMORY {
		t.Errorf("expected IN_MEMORY, got %d", cacheType)
	}

	resetGlobalCache()
}

func TestGlobalCacheDefaultsCACHE_CONTROL_HEADERS(t *testing.T) {
	// Without calling InitCache, lazy fallback should default to CACHE_CONTROL_HEADERS
	resetGlobalCache()

	store := getGlobalCacheStore()
	if _, ok := store.(*InMemoryCacheStore); !ok {
		t.Error("expected InMemoryCacheStore for default CACHE_CONTROL_HEADERS config")
	}

	cacheType := getGlobalCacheType()
	if cacheType != CACHE_CONTROL_HEADERS {
		t.Errorf("expected CACHE_CONTROL_HEADERS cache type, got %d", cacheType)
	}

	resetGlobalCache()
}

// --- Setup() tests ---

func TestSetupDevModeDefaults(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "dev")

	r := chi.NewRouter()
	Setup(r, AppConfig{
		CacheStrategy:         CACHE_CONTROL_HEADERS,
		LocalDevelopmentCache: IN_MEMORY,
	}, func(r chi.Router) {})

	cacheType := getGlobalCacheType()
	if cacheType != IN_MEMORY {
		t.Errorf("expected IN_MEMORY in dev mode, got %d", cacheType)
	}

	resetGlobalCache()
}

func TestSetupDevModeDefaultsZeroValue(t *testing.T) {
	// When LocalDevelopmentCache is zero value (CACHE_CONTROL_HEADERS), Setup should
	// override it to IN_MEMORY in dev mode.
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "dev")

	r := chi.NewRouter()
	Setup(r, AppConfig{
		CacheStrategy: CACHE_CONTROL_HEADERS,
		// LocalDevelopmentCache not set → zero value = CACHE_CONTROL_HEADERS
	}, func(r chi.Router) {})

	cacheType := getGlobalCacheType()
	if cacheType != IN_MEMORY {
		t.Errorf("expected IN_MEMORY when LocalDevelopmentCache is zero value in dev mode, got %d", cacheType)
	}

	resetGlobalCache()
}

func TestSetupProdModeDefaults(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "")

	r := chi.NewRouter()
	Setup(r, AppConfig{
		CacheStrategy:         CACHE_CONTROL_HEADERS,
		LocalDevelopmentCache: IN_MEMORY,
	}, func(r chi.Router) {})

	cacheType := getGlobalCacheType()
	if cacheType != CACHE_CONTROL_HEADERS {
		t.Errorf("expected CACHE_CONTROL_HEADERS in prod mode, got %d", cacheType)
	}

	resetGlobalCache()
}

func TestSetupExplicitDevOverride(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "dev")

	r := chi.NewRouter()
	Setup(r, AppConfig{
		CacheStrategy:         CACHE_CONTROL_HEADERS,
		LocalDevelopmentCache: REDIS,
	}, func(r chi.Router) {})

	// REDIS without config falls back to in-memory, but the type should still be REDIS
	cacheType := getGlobalCacheType()
	if cacheType != REDIS {
		t.Errorf("expected REDIS in dev mode with explicit override, got %d", cacheType)
	}

	resetGlobalCache()
}

func TestSetupDevServesPublicFolder(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "dev")

	r := chi.NewRouter()
	Setup(r, AppConfig{
		ServeStaticFiles: CDN,
	}, func(r chi.Router) {})

	// The /public/* route should be registered in dev mode
	req := httptest.NewRequest("GET", "/public/test.css", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// We expect a 404 (file doesn't exist) rather than 405 (method not allowed),
	// which proves the route IS registered.
	if rec.Code == http.StatusMethodNotAllowed {
		t.Error("expected /public/* route to be registered in dev mode")
	}

	resetGlobalCache()
}

func TestSetupProdNoPublicFolder(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "")

	r := chi.NewRouter()
	Setup(r, AppConfig{
		ServeStaticFiles: CDN,
	}, func(r chi.Router) {})

	// The /public/* route should NOT be registered in prod mode with CDN
	req := httptest.NewRequest("GET", "/public/test.css", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /public/* in prod mode with CDN, got %d", rec.Code)
	}

	resetGlobalCache()
}

func TestSetupDiskServesPublicFolder(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "")

	r := chi.NewRouter()
	Setup(r, AppConfig{
		ServeStaticFiles: DISK,
	}, func(r chi.Router) {})

	// The /public/* route should be registered even in prod mode with DISK
	req := httptest.NewRequest("GET", "/public/test.css", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// We expect a 404 (file doesn't exist) rather than 405 (method not allowed),
	// which proves the route IS registered.
	if rec.Code == http.StatusMethodNotAllowed {
		t.Error("expected /public/* route to be registered in prod mode with DISK")
	}

	resetGlobalCache()
}

// --- LRU MaxEntries tests ---

func TestInMemoryCacheStoreUnboundedByDefault(t *testing.T) {
	store := NewInMemoryCacheStore(nil)

	// Insert 2000 unique keys into an unbounded store.
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Set(key, []byte(key), 0)
	}

	// All 2000 should be retrievable.
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("key-%d", i)
		data, ok := store.Get(key)
		if !ok {
			t.Errorf("expected cache hit for %s (unbounded store)", key)
		}
		if string(data) != key {
			t.Errorf("expected %q, got %q", key, string(data))
		}
	}

	store.Close()
}

func TestInMemoryCacheStoreMaxEntriesLRU(t *testing.T) {
	store := NewInMemoryCacheStore(&CacheConfig{MaxEntries: 3})

	store.Set("a", []byte("a"), 0)
	store.Set("b", []byte("b"), 0)
	store.Set("c", []byte("c"), 0)
	store.Set("d", []byte("d"), 0) // should evict "a" (oldest)

	// "a" should be evicted
	_, ok := store.Get("a")
	if ok {
		t.Error("expected 'a' to be evicted")
	}

	// "b", "c", "d" should still be present
	for _, k := range []string{"b", "c", "d"} {
		data, ok := store.Get(k)
		if !ok {
			t.Errorf("expected %q to be present", k)
		} else if string(data) != k {
			t.Errorf("expected %q, got %q", k, string(data))
		}
	}

	store.Close()
}

func TestInMemoryCacheStoreLRURefreshOnGet(t *testing.T) {
	store := NewInMemoryCacheStore(&CacheConfig{MaxEntries: 3})

	store.Set("a", []byte("a"), 0)
	store.Set("b", []byte("b"), 0)
	store.Set("c", []byte("c"), 0)

	// Get "a" — promotes it to most-recently-used.
	store.Get("a")

	store.Set("d", []byte("d"), 0) // should evict "b" (now the least recently used)

	// "b" should be evicted
	_, ok := store.Get("b")
	if ok {
		t.Error("expected 'b' to be evicted")
	}

	// "a", "c", "d" should be present
	for _, k := range []string{"a", "c", "d"} {
		data, ok := store.Get(k)
		if !ok {
			t.Errorf("expected %q to be present", k)
		} else if string(data) != k {
			t.Errorf("expected %q, got %q", k, string(data))
		}
	}

	store.Close()
}

func TestInMemoryCacheStoreLRUUpdateExisting(t *testing.T) {
	store := NewInMemoryCacheStore(&CacheConfig{MaxEntries: 3})

	store.Set("a", []byte("a"), 0)
	store.Set("b", []byte("b"), 0)
	store.Set("c", []byte("c"), 0)

	// Update "a" — this refreshes its position to most-recently-used.
	store.Set("a", []byte("updated-a"), 0)

	store.Set("d", []byte("d"), 0) // should evict "b" (now the least recently used)

	// "b" should be evicted
	_, ok := store.Get("b")
	if ok {
		t.Error("expected 'b' to be evicted")
	}

	// "a" should have the new value
	data, ok := store.Get("a")
	if !ok {
		t.Error("expected 'a' to be present")
	} else if string(data) != "updated-a" {
		t.Errorf("expected 'updated-a', got %q", string(data))
	}

	// "c", "d" should be present
	for _, k := range []string{"c", "d"} {
		data, ok := store.Get(k)
		if !ok {
			t.Errorf("expected %q to be present", k)
		}
		_ = data
	}

	store.Close()
}

func TestInMemoryCacheStoreMaxEntriesZeroConfig(t *testing.T) {
	store := NewInMemoryCacheStore(&CacheConfig{MaxEntries: 0})

	// Insert 2000 unique keys with MaxEntries=0 (should behave as unbounded).
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("key-%d", i)
		store.Set(key, []byte(key), 0)
	}

	// All 2000 should be retrievable.
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("key-%d", i)
		data, ok := store.Get(key)
		if !ok {
			t.Errorf("expected cache hit for %s (MaxEntries=0 is unbounded)", key)
		}
		if string(data) != key {
			t.Errorf("expected %q, got %q", key, string(data))
		}
	}

	store.Close()
}

// TestInMemoryCacheStoreUnboundedSkipsLRUBookkeeping pins that the default,
// unbounded store allocates no LRU structures. Maintaining them would cost an
// element and an index entry per key, plus an exclusive lock on every read, to
// order a list nothing ever evicts from.
func TestInMemoryCacheStoreUnboundedSkipsLRUBookkeeping(t *testing.T) {
	unbounded := NewInMemoryCacheStore(&CacheConfig{})
	if unbounded.lru != nil || unbounded.lruIndex != nil {
		t.Error("unbounded store must not allocate LRU structures")
	}
	unbounded.Set("a", []byte("1"), 0)
	if got, ok := unbounded.Get("a"); !ok || string(got) != "1" {
		t.Errorf("unbounded Get = %q, %v; want \"1\", true", got, ok)
	}

	bounded := NewInMemoryCacheStore(&CacheConfig{MaxEntries: 2})
	if bounded.lru == nil || bounded.lruIndex == nil {
		t.Error("bounded store must allocate LRU structures")
	}
}

// TestInMemoryCacheStoreConcurrentReads exercises the shared-lock read path of
// the unbounded store. Run with -race, it fails if a reader mutates state.
func TestInMemoryCacheStoreConcurrentReads(t *testing.T) {
	store := NewInMemoryCacheStore(&CacheConfig{})
	for i := 0; i < 50; i++ {
		store.Set(strconv.Itoa(i), []byte(strconv.Itoa(i)), 0)
	}

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if got, ok := store.Get(strconv.Itoa(i % 50)); !ok || string(got) != strconv.Itoa(i%50) {
					t.Errorf("concurrent Get(%d) = %q, %v", i%50, got, ok)
					return
				}
			}
		}()
	}
	wg.Wait()
}
