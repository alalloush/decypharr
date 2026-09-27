package manager

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/request"
)

// busiestWindow returns the most arrivals in any window [t, t+window).
func busiestWindow(times []time.Time, window time.Duration) int {
	times = slices.Clone(times)
	slices.SortFunc(times, time.Time.Compare)
	most, start := 0, 0
	for end := range times {
		for !times[start].Add(window).After(times[end]) {
			start++
		}
		most = max(most, end-start+1)
	}
	return most
}

// hammerRealDebrid keeps a Real-Debrid entry's API, repair and two
// download-key clients busy for d against a fake API, and returns the arrival
// time of every request.
func hammerRealDebrid(t *testing.T, rateLimit string, d time.Duration) []time.Time {
	t.Helper()
	var (
		mu       sync.Mutex
		arrivals []time.Time
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/torrents/activeCount"):
			_, _ = fmt.Fprint(w, `{"nb":0,"limit":10}`)
		case strings.HasSuffix(r.URL.Path, "/user"):
			_, _ = fmt.Fprint(w, `{"id":1,"username":"al"}`)
		default:
			_, _ = fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(server.Close)

	client, err := (&Manager{}).createClient(config.Debrid{
		Name:            "rd",
		Provider:        "realdebrid",
		APIKey:          "main-key",
		DownloadAPIKeys: []string{"main-key", "extra-key"},
		RateLimit:       rateLimit,
		APIHost:         server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), d)
	defer cancel()
	var wg sync.WaitGroup
	hammer := func(call func()) {
		wg.Go(func() {
			for ctx.Err() == nil {
				call()
			}
		})
	}
	hammer(func() { _, _ = client.GetAvailableSlots() })
	hammer(func() { _ = client.CheckFile(ctx, "hash", "https://real-debrid.com/d/ABC") })
	for _, acc := range client.AccountManager().All() {
		hammer(func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/downloads", nil)
			if err != nil {
				return
			}
			if resp, err := acc.Client().Do(req); err == nil {
				request.DrainAndClose(resp.Body)
			}
		})
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(arrivals)
}

// The API, repair and download-key clients of a Real-Debrid entry draw from
// one budget: rate_limit when it is set, and a limit under Real-Debrid's 250
// requests per minute when it is not. They used to get one limiter each
// from rate_limit, and none at all without it.
func TestRealDebridCallPathsShareOneBudget(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	config.Get().Retries = 0

	t.Run("rate_limit", func(t *testing.T) {
		arrivals := hammerRealDebrid(t, "2/second", 2*time.Second)
		// Two per second, 500ms apart. A third can arrive at the far edge
		// of a window when the first one is late, for example because it
		// opened the connection.
		if got := busiestWindow(arrivals, time.Second); got > 3 {
			t.Fatalf("busiest second had %d requests, want rate_limit's 2 (one more at a window edge)", got)
		}
	})

	t.Run("no rate_limit", func(t *testing.T) {
		arrivals := hammerRealDebrid(t, "", 2*time.Second)
		// Real-Debrid's budget is 240 per minute, a burst of 24 and then
		// 216 per minute: at most 24 + 216/60 = 27.6 requests in a second.
		if got := busiestWindow(arrivals, time.Second); got > 28 {
			t.Fatalf("busiest second had %d requests, want at most 28 under the 240/minute budget", got)
		}
	})
}
