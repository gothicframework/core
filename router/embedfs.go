package helpers

import "io/fs"

// embeddedPublicFS backs /public/* when ServeStaticFiles == EMBEDDED. It is set
// once at process start (before main) by the generated root embed file, which
// registers the //go:embed public FS. It stays nil when EMBEDDED is not in use.
var embeddedPublicFS fs.FS

// SetEmbeddedPublicFS registers the embed.FS backing /public/* when
// ServeStaticFiles == EMBEDDED. fsys MUST already be rooted at the public dir
// (fs.Sub'd) so its top-level entries are the files inside public/. The generated
// root embed file calls this from init(), before main().
func SetEmbeddedPublicFS(fsys fs.FS) { embeddedPublicFS = fsys }
