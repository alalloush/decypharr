package arr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// manualImportFiles runs ManualImport against a fake Arr that offers one
// candidate and returns the files the import command submitted, decoded as
// generic JSON so absent keys stay visible.
func manualImportFiles(t *testing.T, kind Type, candidate string) []map[string]any {
	t.Helper()
	var submitted []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/manualimport":
			if got := r.URL.Query().Get("downloadId"); got != "download-1" {
				t.Errorf("downloadId = %q", got)
			}
			_, _ = fmt.Fprintf(w, "[%s]", candidate)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read command: %v", err)
			}
			submitted = body
			_, _ = fmt.Fprint(w, `{"id":1,"name":"ManualImport","status":"queued"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := testService(Arr{Host: server.URL, Token: "secret", Type: kind})
	if err := s.ManualImport(t.Context(), "arr", "download-1"); err != nil {
		t.Fatal(err)
	}
	var command struct {
		Name  string           `json:"name"`
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(submitted, &command); err != nil {
		t.Fatalf("decode command %s: %v", submitted, err)
	}
	if command.Name != "ManualImport" || len(command.Files) != 1 {
		t.Fatalf("command = %s", submitted)
	}
	return command.Files
}

// Radarr imports a file into the movie named by movieId; without it the
// command references movie 0 and the import fails.
func TestManualImportSendsRadarrMovieID(t *testing.T) {
	files := manualImportFiles(t, Radarr, `{"path":"/downloads/radarr/Movie (2020)/movie.mkv","folderName":"Movie (2020)","movie":{"id":42,"title":"Movie"}}`)
	if got := files[0]["movieId"]; got != float64(42) {
		t.Fatalf("movieId = %v, want 42", got)
	}
}

func TestManualImportOmitsMovieIDForSonarr(t *testing.T) {
	files := manualImportFiles(t, Sonarr, `{"path":"/downloads/sonarr/Show/S01E01.mkv","folderName":"Show","series":{"id":7},"seasonNumber":1,"episodes":[{"id":100}]}`)
	if _, ok := files[0]["movieId"]; ok {
		t.Fatalf("Sonarr import carries a movieId: %v", files[0])
	}
	if files[0]["seriesId"] != float64(7) {
		t.Fatalf("seriesId = %v, want 7", files[0]["seriesId"])
	}
}
