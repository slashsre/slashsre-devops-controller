package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed ui-dist/*
var uiDist embed.FS

// Prefixes that belong to the backend and must never fall back to index.html.
var backendPrefixes = []string{"/api", "/swagger", "/metrics", "/healthz", "/readyz", "/version", "/webhooks", "/flux", "/alertmanager"}

// spaHandler serves the embedded single page application.
type spaHandler struct {
	staticFS  fs.FS
	files     http.Handler
	indexHTML []byte
}

func newSPAHandler() (*spaHandler, error) {
	staticFS, err := fs.Sub(uiDist, "ui-dist")
	if err != nil {
		return nil, err
	}
	indexHTML, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		return nil, err
	}
	return &spaHandler{staticFS: staticFS, files: http.FileServer(http.FS(staticFS)), indexHTML: indexHTML}, nil
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := h.staticFS.Open(path); err == nil {
		_ = f.Close()
		h.files.ServeHTTP(w, r)
		return
	}
	for _, p := range backendPrefixes {
		if strings.HasPrefix(r.URL.Path, p) {
			http.NotFound(w, r)
			return
		}
	}
	// Unknown path: let the client-side router handle it.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(h.indexHTML)
}
