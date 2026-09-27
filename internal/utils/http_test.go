package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// API responses go to programs; indenting them only adds bytes, which adds up
// for the queue and browse listings the UI polls.
func TestJSONResponseIsCompact(t *testing.T) {
	w := httptest.NewRecorder()
	JSONResponse(w, map[string]any{"hashes": []string{"a", "b"}, "total": 2}, http.StatusCreated)

	if w.Code != http.StatusCreated || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, content type %q", w.Code, w.Header().Get("Content-Type"))
	}
	if got, want := w.Body.String(), `{"hashes":["a","b"],"total":2}`+"\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
