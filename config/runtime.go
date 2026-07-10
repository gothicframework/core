package config

// This file holds the runtime routing configuration a project declares inside its
// gothic.config.go Config (the Runtime field). It lives in this lightweight,
// user-facing package so the config can be written with gothic.* types and read
// directly by the generated main.go (same package). pkg/helpers/routes re-exports
// these as aliases, so the framework's routing code keeps using its own names.

// CacheType selects a cache backend.
type CacheType int

const (
	// CACHE_CONTROL_HEADERS is the default — production behavior emitting
	// Cache-Control headers for a CDN rather than storing responses.
	CACHE_CONTROL_HEADERS CacheType = iota
	// IN_MEMORY caches responses in a Go in-memory map.
	IN_MEMORY
	// REDIS caches responses in Redis (see CacheConfig.RedisURL).
	REDIS
	// LOCAL_FILES caches responses on the local filesystem.
	LOCAL_FILES
)

// StaticFilesMode controls when /public/* assets are served from local disk.
type StaticFilesMode int

const (
	// HOT_RELOAD_ONLY (default) serves /public/* from disk only during development;
	// in production CloudFront serves them from S3.
	HOT_RELOAD_ONLY StaticFilesMode = iota
	// ALL_ENVS serves /public/* from disk in every environment (the public folder
	// must ship alongside the server binary).
	ALL_ENVS
)

// CompressionMethod selects a compression algorithm for cached/served payloads.
type CompressionMethod int

const (
	// GZIP is the default compression method.
	GZIP CompressionMethod = iota
	// BROTLI uses Brotli compression.
	BROTLI
)

// RuntimeConfig is the runtime routing/caching configuration for the server. Its
// zero value is the sensible default (CACHE_CONTROL_HEADERS in production,
// in-memory in dev, static files served only under hot reload), so a project may
// omit the Runtime field entirely.
type RuntimeConfig struct {
	// CacheStrategy selects the production cache backend.
	CacheStrategy CacheType

	// LocalDevelopmentCache selects the dev (hot-reload) cache backend.
	LocalDevelopmentCache CacheType

	// ServeStaticFiles controls when /public/* is served from disk.
	ServeStaticFiles StaticFilesMode

	// CacheConfig provides backend-specific settings (Redis URL, file path, ...).
	CacheConfig *CacheConfig
}

// CacheConfig carries backend-specific cache settings.
type CacheConfig struct {
	RedisURL          string
	RedisPassword     string
	RedisTLS          bool
	CacheFilesPath    string
	Compression       bool
	CompressionMethod CompressionMethod
}
