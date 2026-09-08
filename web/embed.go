// Package web embeds the built React admin SPA.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the built SPA rooted at the dist directory.
func Assets() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist is always embedded at build time
	}
	return sub
}
