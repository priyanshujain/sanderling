//go:build !withcompanion

package runnerassets

var embeddedArchive []byte

// IsPlaceholder reports whether the binary was built without the real runner
// archive embedded. Build with `make sanderling` (which passes
// -tags withcompanion) to embed the real runner test bundle.
func IsPlaceholder() bool { return true }
