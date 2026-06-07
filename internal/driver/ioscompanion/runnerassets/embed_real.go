//go:build withcompanion

package runnerassets

import _ "embed"

//go:embed assets/runner-1.0.0.tar.gz
var embeddedArchive []byte

// IsPlaceholder reports whether the binary was built without the real runner
// archive embedded. -tags withcompanion builds always return false.
func IsPlaceholder() bool { return false }
