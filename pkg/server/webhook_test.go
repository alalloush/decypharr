package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

const testAPIToken = "test-api-token"

// A targetless payload: valid JSON, correct topic, no media id. This is the
// shape that used to fall through to a full repair sweep.
const targetlessPayload = `{"topic":"tautulli","fix":true}`

// TestTautulliWebhookRequiresAPIToken pins that the webhook sits behind the
// API authentication used by every other mutating endpoint. A request with
// valid credentials reaches the handler, where the targetless payload is
// rejected with 400 instead of starting a sweep.
func TestTautulliWebhookRequiresAPIToken(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	cfg := config.Get()
	cfg.UseAuth = true
	if err := cfg.SaveAuth(&config.Auth{TokenOnly: true, APIToken: testAPIToken}); err != nil {
		t.Fatal(err)
	}
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })
	router := New(mgr).router

	for _, tc := range []struct {
		name          string
		authorization string
		want          int
	}{
		{name: "no credentials", want: http.StatusUnauthorized},
		{name: "wrong token", authorization: "Bearer not-the-token", want: http.StatusUnauthorized},
		{name: "valid token", authorization: "Bearer " + testAPIToken, want: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/webhooks/tautulli", strings.NewReader(targetlessPayload))
			req.Header.Set("Content-Type", "application/json")
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%q", recorder.Code, tc.want, recorder.Body.String())
			}
		})
	}
}

// TestTautulliWebhookRejectsTargetlessPayload pins that a payload carrying no
// media id is a client error, not an instruction to sweep the whole library.
// The server has no manager on purpose: reaching s.manager.Repair() panics,
// and that is exactly the path that used to start the sweep.
func TestTautulliWebhookRejectsTargetlessPayload(t *testing.T) {
	s := &Server{logger: zerolog.Nop()}
	for _, body := range []string{
		`{"topic":"tautulli","fix":true}`,
		`{"topic":"tautulli","fix":false}`,
		`{"topic":"tautulli","arr":"radarr","fix":true}`,
		`{"topic":"tautulli","media_id":"   ","fix":true}`,
	} {
		t.Run(body, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			s.handleTautulli(recorder, httptest.NewRequest(http.MethodPost, "/webhooks/tautulli", strings.NewReader(body)))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", recorder.Code, recorder.Body.String())
			}
		})
	}
}
