package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerPlaceholderWithoutIndex(t *testing.T) {
	handler := handlerFor(fstest.MapFS{"dist/.gitkeep": &fstest.MapFile{Data: []byte{}}})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "make frontend-control-plane") {
		t.Fatalf("placeholder body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content type = %q", got)
	}
}

func TestHandlerServesIndexAndAssets(t *testing.T) {
	root := fstest.MapFS{
		"index.html":                &fstest.MapFile{Data: []byte("<html>app</html>")},
		"_next/static/chunk.js":     &fstest.MapFile{Data: []byte("console.log(1)")},
		"_next/static/chunk.js.map": &fstest.MapFile{Data: []byte("{}")},
	}
	handler := handlerFor(root)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("index status = %d body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index cache = %q, want no-cache", got)
	}

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/_next/static/chunk.js", nil))
	if asset.Code != http.StatusOK {
		t.Fatalf("asset status = %d", asset.Code)
	}
	if got := asset.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("asset cache = %q, want immutable", got)
	}
}

func TestHandlerRejectsUnknownAPIAsJSON(t *testing.T) {
	handler := handlerFor(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q", got)
	}
}
