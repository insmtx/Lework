// Package webui embeds the prebuilt control-plane browser application.
//
// The directory is populated by `make frontend-control-plane` before a release
// build. A tracked .gitkeep keeps `go test ./...` working on a clean checkout;
// without an index the handler serves a small build-instructions page instead
// of a blank 404.
package webui

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

const placeholderHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Lework control plane</title>
<style>body{font:15px/1.6 ui-sans-serif,system-ui,sans-serif;max-width:46rem;margin:10vh auto;padding:0 1.5rem;color:#172033}
code{background:#eef2f7;border-radius:4px;padding:.1rem .35rem}pre{background:#0b1120;color:#dbeafe;padding:1rem;border-radius:8px;overflow:auto}</style>
</head>
<body>
<h1>Lework control plane</h1>
<p>The browser interface has not been built into this binary yet.</p>
<p>Run <code>make frontend-control-plane</code> and rebuild, or use the release image.</p>
<pre>make frontend-control-plane
go build -o ./bundles/lework ./backend/cmd/lework/</pre>
<p>The API is already running; <code>GET /healthz</code> and <code>GET /api/nodes</code> are available.</p>
</body>
</html>`

// Handler serves the embedded export. It is safe to call when dist contains
// only the tracked .gitkeep placeholder.
func Handler() http.Handler {
	root, err := fs.Sub(embedded, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "webui: embedded assets are unavailable", http.StatusInternalServerError)
		})
	}
	return handlerFor(root)
}

// handlerFor is split from Handler so tests can exercise both a clean checkout
// (no index) and a release build without changing the embedded FS.
func handlerFor(root fs.FS) http.Handler {
	index := false
	if _, statErr := fs.Stat(root, "index.html"); statErr == nil {
		index = true
	}
	files := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeNotFound(w)
			return
		}
		if !index {
			if r.URL.Path != "/" && r.URL.Path != "/index.html" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(placeholderHTML))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": "not_found", "message": "not found"})
}
