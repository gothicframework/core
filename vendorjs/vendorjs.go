// Package vendorjs owns the third-party client scripts Gothic used to load from a
// public CDN (unpkg): HTMX and the amz-content-sha256 HTMX extension. They are
// embedded here and served from the framework's own /_gothic/ route (via
// runtimeassets) instead of a render-blocking cross-origin <script>, so the
// browser never pays a third-party DNS/TLS/connection cost on the critical path
// and the scripts inherit the framework's immutable cache + on-the-wire
// compression.
//
// Each asset exposes a content hash (Version) used as the ?v= cache-buster in the
// URL the layout references, matching the convention used by gothiccore/corewasm.
// Bumping the pinned upstream file (recopying the min.js) changes the bytes →
// changes the hash → busts the browser cache automatically.
//
// It is a LEAF package (no internal deps) so both the runtime-asset registry and
// the components layer can import it without forming a cycle.
package vendorjs

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// Emitted basenames served under /_gothic/.
const (
	HtmxFileName   = "htmx.min.js"
	AmzExtFileName = "amz-content-sha256.min.js"
)

// htmxJS is HTMX (pinned upstream; see HtmxVersion for the cache-buster). Served
// verbatim from /_gothic/htmx.min.js.
//
//go:embed htmx.min.js
var htmxJS []byte

// amzExtJS is the amz-content-sha256 HTMX extension. Served from
// /_gothic/amz-content-sha256.min.js.
//
//go:embed amz-content-sha256.min.js
var amzExtJS []byte

// hash16 returns the first 16 hex chars of sha256(b) — the content cache-buster,
// matching gothiccore/corewasm/runtimeassets.
func hash16(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

var htmxHash = hash16(htmxJS)
var amzExtHash = hash16(amzExtJS)

// HtmxJS returns the embedded HTMX bytes. Served from /_gothic/.
func HtmxJS() []byte { return htmxJS }

// AmzExtJS returns the embedded amz-content-sha256 extension bytes. Served from /_gothic/.
func AmzExtJS() []byte { return amzExtJS }

// HtmxVersion returns HTMX's content hash — the ?v= cache-buster the layout uses.
func HtmxVersion() string { return htmxHash }

// AmzExtVersion returns the amz extension's content hash — its ?v= cache-buster.
func AmzExtVersion() string { return amzExtHash }
