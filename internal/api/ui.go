package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves static assets from the embedded SPA and falls back to
// index.html for client-side routes (any path without a file extension).
func spaHandler(assets fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(assets))

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			serveIndex(w, r, assets)
			return
		}
		if _, err := fs.Stat(assets, path); err != nil {
			// Unknown path with no extension = client route: serve index.
			if !strings.Contains(path, ".") {
				serveIndex(w, r, assets)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}
