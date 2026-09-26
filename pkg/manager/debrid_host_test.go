package manager

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
)

// requestLog records what reached a test server.
type requestLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *requestLog) add(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
}

func (l *requestLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// TestDebridAPIHost checks that debrids[].api_host redirects every provider's
// API traffic, and that without it traffic still targets the public API.
func TestDebridAPIHost(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	config.Get().Retries = 0

	defaultHosts := map[string]string{
		"realdebrid": "api.real-debrid.com:443",
		"torbox":     "api.torbox.app:443",
		"alldebrid":  "api.alldebrid.com:443",
		"debridlink": "debrid-link.com:443",
		"premiumize": "www.premiumize.me:443",
	}
	for provider, defaultHost := range defaultHosts {
		t.Run(provider, func(t *testing.T) {
			debridConfig := config.Debrid{
				Name:            provider,
				Provider:        provider,
				APIKey:          "token",
				DownloadAPIKeys: []string{"token"},
			}

			t.Run("configured host", func(t *testing.T) {
				var seen requestLog
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					seen.add(r.URL.Path)
					http.Error(w, "fake provider", http.StatusServiceUnavailable)
				}))
				t.Cleanup(server.Close)

				dc := debridConfig
				dc.APIHost = server.URL + "/fake/api/"
				client, err := (&Manager{}).createClient(dc)
				if err != nil {
					t.Fatalf("createClient: %v", err)
				}
				_, _ = client.GetProfile()

				paths := seen.snapshot()
				if len(paths) == 0 {
					t.Fatal("no request reached the configured API host")
				}
				for _, path := range paths {
					if !strings.HasPrefix(path, "/fake/api/") || strings.HasPrefix(path, "/fake/api//") {
						t.Errorf("request path %q is not under the configured base /fake/api", path)
					}
				}
			})

			t.Run("default host", func(t *testing.T) {
				// An HTTP proxy sees the CONNECT target of every HTTPS request,
				// so the public host is observed without leaving the machine.
				var seen requestLog
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					seen.add(r.Method + " " + r.Host)
					http.Error(w, "no egress in tests", http.StatusForbidden)
				}))
				t.Cleanup(proxy.Close)

				dc := debridConfig
				dc.Proxy = proxy.URL
				client, err := (&Manager{}).createClient(dc)
				if err != nil {
					t.Fatalf("createClient: %v", err)
				}
				_, _ = client.GetProfile()

				targets := seen.snapshot()
				if len(targets) == 0 {
					t.Fatal("no request reached the proxy")
				}
				for _, target := range targets {
					if target != http.MethodConnect+" "+defaultHost {
						t.Errorf("proxied request %q, want CONNECT %s", target, defaultHost)
					}
				}
			})
		})
	}
}
