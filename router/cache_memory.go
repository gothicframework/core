package helpers

import (
	"container/list"
	"sync"
	"time"
)

type memoryCacheEntry struct {
	data      []byte
	expiresAt time.Time
	hasTTL    bool
}

// InMemoryCacheStore implements the CacheStore interface with an in-memory map
// and optional least-recently-used eviction bounded by MaxEntries.
//
// The LRU bookkeeping exists only when a bound is configured. Unbounded is the
// default, and there a read takes the shared lock and touches no list: recording
// recency would cost an exclusive lock on every Get plus a list element and an
// index entry per key, to order a list nothing ever evicts from.
type InMemoryCacheStore struct {
	mu         sync.RWMutex
	entries    map[string]memoryCacheEntry
	lru        *list.List
	lruIndex   map[string]*list.Element
	maxEntries int
	config     *CacheConfig
}

func NewInMemoryCacheStore(config *CacheConfig) *InMemoryCacheStore {
	maxEntries := 0
	if config != nil {
		maxEntries = config.MaxEntries
	}
	s := &InMemoryCacheStore{
		entries:    make(map[string]memoryCacheEntry),
		maxEntries: maxEntries,
		config:     config,
	}
	if s.bounded() {
		s.lru = list.New()
		s.lruIndex = make(map[string]*list.Element)
	}
	return s
}

// bounded reports whether eviction is configured. Everything LRU-related is
// skipped when it is not.
func (s *InMemoryCacheStore) bounded() bool { return s.maxEntries > 0 }

func (s *InMemoryCacheStore) Get(key string) ([]byte, bool) {
	if s.bounded() {
		return s.getBounded(key)
	}

	s.mu.RLock()
	entry, exists := s.entries[key]
	s.mu.RUnlock()

	if !exists {
		return nil, false
	}
	if entry.hasTTL && time.Now().After(entry.expiresAt) {
		s.mu.Lock()
		delete(s.entries, key)
		s.mu.Unlock()
		return nil, false
	}
	return s.decode(entry.data)
}

// getBounded takes the exclusive lock because a hit promotes the key in the LRU
// list, which is a write.
func (s *InMemoryCacheStore) getBounded(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.entries[key]
	if !exists {
		return nil, false
	}

	if entry.hasTTL && time.Now().After(entry.expiresAt) {
		delete(s.entries, key)
		if elem, ok := s.lruIndex[key]; ok {
			s.lru.Remove(elem)
			delete(s.lruIndex, key)
		}
		return nil, false
	}

	if elem, ok := s.lruIndex[key]; ok {
		s.lru.MoveToFront(elem)
	}

	return s.decode(entry.data)
}

// decode reverses the compression applied by Set, if any.
func (s *InMemoryCacheStore) decode(data []byte) ([]byte, bool) {
	if !s.compressionEnabled() {
		return data, true
	}
	decompressed, err := decompressData(data, s.compressionMethod())
	if err != nil {
		return nil, false
	}
	return decompressed, true
}

func (s *InMemoryCacheStore) Set(key string, value []byte, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := value
	if s.compressionEnabled() {
		compressed, err := compressData(value, s.compressionMethod())
		if err == nil {
			data = compressed
		}
	}

	entry := memoryCacheEntry{data: data}
	if ttl > 0 {
		entry.hasTTL = true
		entry.expiresAt = time.Now().Add(ttl)
	}

	_, exists := s.entries[key]
	s.entries[key] = entry

	if !s.bounded() {
		return
	}

	if exists {
		if elem, ok := s.lruIndex[key]; ok {
			s.lru.MoveToFront(elem)
		}
		return
	}

	s.lruIndex[key] = s.lru.PushFront(key)

	// Evict the least-recently-used entry when the insert took us over the bound.
	if s.lru.Len() > s.maxEntries {
		if back := s.lru.Back(); back != nil {
			evictKey := back.Value.(string)
			s.lru.Remove(back)
			delete(s.lruIndex, evictKey)
			delete(s.entries, evictKey)
		}
	}
}

func (s *InMemoryCacheStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]memoryCacheEntry)
	if s.bounded() {
		s.lruIndex = make(map[string]*list.Element)
		s.lru.Init()
	}
	return nil
}

func (s *InMemoryCacheStore) Close() error {
	return s.Flush()
}

func (s *InMemoryCacheStore) compressionEnabled() bool {
	return s.config != nil && s.config.Compression
}

func (s *InMemoryCacheStore) compressionMethod() CompressionMethod {
	if s.config != nil {
		return s.config.CompressionMethod
	}
	return GZIP
}
