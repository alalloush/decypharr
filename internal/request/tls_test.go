package request

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
)

// A server whose certificate does not verify never sees the request, so the
// provider's bearer token is not sent; an explicit opt-out still reaches it.
func TestClientVerifiesTLSCertificates(t *testing.T) {
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)

	var got atomic.Value
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("Authorization"))
	}))
	defer server.Close()

	get := func(opts ...ClientOption) error {
		opts = append(opts, WithMaxRetries(0), WithHeaders(map[string]string{"Authorization": "Bearer SECRET"}))
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := New(opts...).Do(req)
		if err == nil {
			DrainAndClose(resp.Body)
		}
		return err
	}

	err := get()
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); !ok {
		t.Fatalf("default client: error = %v, want a certificate verification error", err)
	}
	if got.Load() != nil {
		t.Fatal("default client sent the request to an unverified server")
	}

	if err := get(WithInsecureSkipVerify(true)); err != nil {
		t.Fatalf("insecure_skip_verify client: %v", err)
	}
	if got.Load() != "Bearer SECRET" {
		t.Fatalf("insecure_skip_verify client: server saw %v", got.Load())
	}
}
