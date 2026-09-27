package stats

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

// The server builds its Collector before it listens, so a snapshot taken in
// New (a profile request to every provider) held the UI, the qBittorrent API
// and WebDAV back for as long as a provider took to answer.
func TestNewDoesNotWaitForProviders(t *testing.T) {
	var profileRequests atomic.Int32
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/api/user/me" {
			profileRequests.Add(1)
		}
		<-release
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(provider.Close)
	var releaseOnce sync.Once
	releaseProvider := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseProvider) // before provider.Close, which waits for handlers

	config.Reset()
	t.Cleanup(config.Reset)
	dir := t.TempDir()
	config.SetConfigPath(dir)
	seed := `{"download_folder": "` + dir + `", "debrids": [{"name": "torbox", "provider": "torbox",
		"api_key": "key", "api_host": "` + provider.URL + `/v1"}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })

	created := make(chan *Collector, 1)
	go func() { created <- New(mgr) }()
	var c *Collector
	select {
	case c = <-created:
	case <-time.After(2 * time.Second):
		releaseProvider()
		<-created
		t.Fatal("New waited for the provider to answer")
	}

	c.Start(t.Context())
	t.Cleanup(c.Stop)
	served := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		c.Handler()(w, httptest.NewRequest(http.MethodGet, "/debug/stats", nil))
		served <- w
	}()

	// The first snapshot is being taken in the background: the provider has
	// the profile request and the stats request waits for the snapshot.
	deadline := time.Now().Add(5 * time.Second)
	for profileRequests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if profileRequests.Load() == 0 {
		t.Fatal("Start did not take a snapshot")
	}
	select {
	case <-served:
		t.Fatal("stats were served before the first snapshot")
	case <-time.After(100 * time.Millisecond):
	}

	releaseProvider()
	select {
	case w := <-served:
		if w.Code != http.StatusOK || c.Snapshot() == nil {
			t.Fatalf("stats = %d, snapshot %v", w.Code, c.Snapshot())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no snapshot after the provider answered")
	}
}
