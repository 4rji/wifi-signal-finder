package serverapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestStaticHandlerServesRaspberryIndex(t *testing.T) {
	staticFS := http.FS(fstest.MapFS{
		"index.html": {Data: []byte("desktop")},
		"rb.html":    {Data: []byte("raspberry")},
		"rb.css":     {Data: []byte("css")},
	})
	handler := staticHandler(staticFS, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "raspberry" {
		t.Fatalf("body = %q, want raspberry index", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/rb.css", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "css" {
		t.Fatalf("asset body = %q, want css", got)
	}
}

func TestBrowserURLUsesLocalhostForWildcardBind(t *testing.T) {
	got := browserURL("0.0.0.0:8888")
	want := "http://127.0.0.1:8888/"
	if got != want {
		t.Fatalf("browserURL = %q, want %q", got, want)
	}
}
