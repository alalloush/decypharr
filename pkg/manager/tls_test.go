package manager

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/sirrobot01/decypharr/internal/config"
	debrid "github.com/sirrobot01/decypharr/pkg/debrid/common"
)

// Every provider verifies its API server's certificate, so the API key never
// reaches a server that fails verification; insecure_skip_verify on the
// debrid lets the request through.
func TestProvidersVerifyTLS(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	config.Get().Retries = 0

	for _, provider := range []string{"realdebrid", "torbox", "alldebrid", "debridlink", "premiumize"} {
		t.Run(provider, func(t *testing.T) {
			var seen requestLog
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen.add(r.URL.Path)
				http.Error(w, "fake provider", http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)

			dc := config.Debrid{
				Name:            provider,
				Provider:        provider,
				APIKey:          "token",
				DownloadAPIKeys: []string{"token"},
				APIHost:         server.URL,
			}
			client, err := (&Manager{}).createClient(dc)
			if err != nil {
				t.Fatalf("createClient: %v", err)
			}
			_, _ = client.GetProfile()
			if n := len(seen.snapshot()); n != 0 {
				t.Fatalf("%d requests reached a server whose certificate does not verify", n)
			}

			dc.InsecureSkipVerify = true
			client, err = (&Manager{}).createClient(dc)
			if err != nil {
				t.Fatalf("createClient: %v", err)
			}
			_, _ = client.GetProfile()
			if len(seen.snapshot()) == 0 {
				t.Fatal("insecure_skip_verify: no request reached the server")
			}
		})
	}
}

// Download links are fetched with certificate verification unless the link's
// debrid set insecure_skip_verify.
func TestStreamClientVerifiesTLSPerDebrid(t *testing.T) {
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(cdn.Close)

	clients := xsync.NewMap[string, debrid.Client]()
	clients.Store("verified", cachedFallback{cfg: config.Debrid{Name: "verified"}})
	clients.Store("optout", cachedFallback{cfg: config.Debrid{Name: "optout", InsecureSkipVerify: true}})
	m := &Manager{clients: clients, streamClient: newStreamClient(false), insecureStreamClient: newStreamClient(true)}

	for _, tc := range []struct {
		debrid   string
		verifies bool
	}{
		{"verified", true},
		{"unknown", true},
		{"optout", false},
	} {
		resp, err := m.streamClientFor(tc.debrid).Get(cdn.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
		_, certErr := errors.AsType[*tls.CertificateVerificationError](err)
		if tc.verifies && !certErr {
			t.Errorf("%s: error = %v, want a certificate verification error", tc.debrid, err)
		}
		if !tc.verifies && err != nil {
			t.Errorf("%s: insecure_skip_verify link failed: %v", tc.debrid, err)
		}
	}
}
