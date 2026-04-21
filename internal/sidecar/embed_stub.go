//go:build !withsidecar

package sidecar

var embeddedJAR []byte

// IsPlaceholder reports whether the binary was built without the real
// sidecar JAR embedded. Build with `make sanderling` (which passes
// -tags withsidecar) to embed the real fat JAR.
func IsPlaceholder() bool { return true }
