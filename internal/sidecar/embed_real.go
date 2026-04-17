//go:build withsidecar

package sidecar

import _ "embed"

//go:embed assets/sidecar-all.jar
var embeddedJAR []byte

// IsPlaceholder reports whether the binary was built without the real
// sidecar JAR embedded. -tags withsidecar builds always return false.
func IsPlaceholder() bool { return false }
