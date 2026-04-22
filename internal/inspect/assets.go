package inspect

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the embedded SPA bundle rooted at the dist directory.
// In Stage 2 this contains a stub index.html. Stage 4 wires the real
// bundle in via Makefile (copy inspect-ui/dist -> internal/inspect/dist).
func Assets() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("inspect: dist embed missing: " + err.Error())
	}
	return sub
}
