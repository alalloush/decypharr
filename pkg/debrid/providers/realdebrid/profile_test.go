package realdebrid

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/debrid/throttle"
)

// New fetches the profile in the background while the stats page and slot
// checks call GetProfile. Concurrent callers on a cold cache must share one
// /user request, and the cache must be safe to read while it is filled.
func TestGetProfileConcurrentCallersShareOneRequest(t *testing.T) {
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		// Answer slowly, so every caller asks while the first request is
		// still in flight.
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":7,"username":"al","type":"premium"}`)
	}))
	t.Cleanup(server.Close)

	rd, err := New(config.Debrid{Name: "rd", Provider: "realdebrid", APIKey: "key", APIHost: server.URL}, throttle.Lanes{})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			profile, err := rd.GetProfile()
			if err != nil {
				t.Errorf("GetProfile: %v", err)
				return
			}
			if profile.Username != "al" || profile.Name != "rd" {
				t.Errorf("profile = %+v", profile)
			}
		})
	}
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Fatalf("/user requests = %d, want 1 shared by New and every concurrent caller", got)
	}
	if _, err := rd.GetProfile(); err != nil || requests.Load() != 1 {
		t.Fatalf("cached GetProfile: err %v, /user requests %d, want 1", err, requests.Load())
	}
}
