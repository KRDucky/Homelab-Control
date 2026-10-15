// Package webui embeds the built frontend (frontend/app/dist is copied into
// ./dist by `make frontend`). When only the placeholder is present the server
// reports that the UI is not built.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded UI, or nil if the UI has not been built.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
