package torbox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirrobot01/decypharr/internal/config"
)

// GetAvailableSlots and the usenet listing read the profile concurrently.
// Callers on a cold cache must share one /user/me request, and the cache
// must be safe to read while it is filled.
func TestGetProfileConcurrentCallersShareOneRequest(t *testing.T) {
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/me" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"success":true,"data":{"id":7,"email":"al@example.test","plan":2}}`)
	}))
	t.Cleanup(server.Close)

	tb := testTorbox(server.URL)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			slots, err := tb.GetAvailableSlots()
			if err != nil {
				t.Errorf("GetAvailableSlots: %v", err)
				return
			}
			if slots != planSlots["pro"] {
				t.Errorf("slots = %d, want %d for the pro plan", slots, planSlots["pro"])
			}
		})
	}
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Fatalf("/api/user/me requests = %d, want 1 shared by every concurrent caller", got)
	}
}
