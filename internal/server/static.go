package server

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:static/dist
var embeddedStaticFS embed.FS

// StaticHandler serves the embedded SPA with clean index.html fallback for client routes.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(embeddedStaticFS, "static/dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cleaned := path.Clean("/" + r.URL.Path)
		if strings.HasPrefix(cleaned, "/api/") || cleaned == "/api" {
			http.NotFound(w, r)
			return
		}
		target := strings.TrimPrefix(cleaned, "/")
		if target == "" {
			target = "index.html"
		}
		if info, statErr := fs.Stat(sub, target); statErr == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback to index.html
		indexBytes, readErr := fs.ReadFile(sub, "index.html")
		if readErr != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexBytes)
	})
}
