//go:build !withcompanion

package companionassets

var embeddedArchive []byte

// IsPlaceholder reports whether the binary was built without the real
// companion archive embedded. Build with `make sanderling` (which passes
// -tags withcompanion) to embed the real companion bundle.
func IsPlaceholder() bool { return true }
