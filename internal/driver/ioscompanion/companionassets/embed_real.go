//go:build withcompanion

package companionassets

import _ "embed"

//go:embed assets/companion-1.1.8.tar.gz
var embeddedArchive []byte

// IsPlaceholder reports whether the binary was built without the real
// companion archive embedded. -tags withcompanion builds always return false.
func IsPlaceholder() bool { return false }
