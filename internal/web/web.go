// Package web serves the embedded, offline frontend.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed frontend/dist
var assets embed.FS

func Handler() http.Handler {
	files, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		panic(err)
	}
	server := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		server.ServeHTTP(w, r)
	})
}
