package request

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirrobot01/decypharr/internal/config"
)

func TestRetryPolicyPreservesUnlistedProviderStatus(t *testing.T) {
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "provider response", true: "configured retry"}[explicit], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(509)
				_, _ = io.WriteString(w, `{"error":"active_downloads_limit"}`)
			}))
			defer server.Close()
			opts := []ClientOption{WithMaxRetries(0)}
			if explicit {
				opts = append(opts, WithRetryableStatus(509))
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := New(opts...).Do(req)
			if calls.Load() != 1 {
				t.Fatalf("calls = %d", calls.Load())
			}
			if explicit {
				if err == nil {
					DrainAndClose(resp.Body)
					t.Fatal("configured retry did not reach its limit")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer DrainAndClose(resp.Body)
			body, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != 509 || string(body) != `{"error":"active_downloads_limit"}` {
				t.Fatalf("response = %d %q, error = %v", resp.StatusCode, body, err)
			}
		})
	}
}

type countingLimiter struct{ takes atomic.Int32 }

func (l *countingLimiter) Take() time.Time {
	l.takes.Add(1)
	return time.Now()
}

// A provider counts every attempt against its rate limit, 429s included, so
// each retry must wait for a permit of its own.
func TestRetriesTakeARatePermitEach(t *testing.T) {
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	limiter := &countingLimiter{}
	client := New(WithMaxRetries(3), WithRateLimiter(limiter))
	client.client.RetryWaitMin, client.client.RetryWaitMax = time.Millisecond, time.Millisecond
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	DrainAndClose(resp.Body)
	if calls.Load() != 3 || limiter.takes.Load() != 3 {
		t.Fatalf("%d attempts took %d permits, want 3 and 3", calls.Load(), limiter.takes.Load())
	}
}
