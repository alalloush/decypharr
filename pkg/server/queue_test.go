package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
	"github.com/sirrobot01/decypharr/pkg/server/qbit"
	"github.com/sirrobot01/decypharr/pkg/server/sabnzbd"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

func TestQueueReadFailuresReachHTTPClients(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	config.Get().UseAuth = false
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })
	server := &Server{manager: mgr}
	qbitRoutes := qbit.New(mgr).Routes()
	sabRoutes := sabnzbd.New(mgr).Routes()
	if err := mgr.Storage().Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		handler    http.Handler
	}{
		{"web queue", "/api/torrents", http.HandlerFunc(server.handleGetTorrents)},
		{"qbit queue", "/torrents/info", qbitRoutes},
		{"sab queue", "/api/?mode=queue", sabRoutes},
		{"sab history", "/api/?mode=history", sabRoutes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			tc.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500: %s", response.Code, response.Body.String())
			}
		})
	}
}

// "all" as every torrent is a qBittorrent API sentinel, resolved by the qBit
// handlers. The web UI's bulk delete takes a literal hash list, and can also
// remove each entry from the debrid provider, so "all" must stay literal here.
func TestBulkDeleteTreatsAllAsALiteralHash(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })
	server := &Server{manager: mgr}
	for _, hash := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		if err := mgr.Queue().Add(&storage.Entry{InfoHash: hash, Name: hash, SavePath: t.TempDir(), Protocol: config.ProtocolTorrent}); err != nil {
			t.Fatal(err)
		}
	}

	response := httptest.NewRecorder()
	server.handleDeleteTorrents(response, httptest.NewRequest(http.MethodDelete, "/api/torrents?hashes=all", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	remaining, err := mgr.Queue().ListFilter("", config.ProtocolAll, "", nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("hashes=all deleted %d of 2 queued entries", 2-len(remaining))
	}
}
