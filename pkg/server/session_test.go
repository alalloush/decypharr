package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/sirrobot01/decypharr/internal/config"
)

func TestCredentialChangesInvalidateBrowserSessions(t *testing.T) {
	for _, change := range []string{"password", "token", "mode"} {
		t.Run(change, func(t *testing.T) {
			config.Reset()
			config.SetConfigPath(t.TempDir())
			t.Cleanup(config.Reset)
			cfg := config.Get()
			cfg.UseAuth = true
			body := `{"username":"admin","password":"old-password"}`
			if change == "token" {
				if err := cfg.SaveAuth(&config.Auth{TokenOnly: true, APIToken: "old-token"}); err != nil {
					t.Fatal(err)
				}
				body = `{"password":"old-token"}`
			} else if err := cfg.SetCredentials("admin", "old-password"); err != nil {
				t.Fatal(err)
			}
			s := &Server{cookie: sessions.NewCookieStore([]byte(cfg.SecretKey()))}
			login := httptest.NewRecorder()
			s.LoginHandler(login, httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body)))
			if login.Code != http.StatusSeeOther {
				t.Fatalf("login status = %d: %s", login.Code, login.Body.String())
			}
			cookies := login.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("login set %d cookies, want 1", len(cookies))
			}
			handler := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			request.AddCookie(cookies[0])
			before := httptest.NewRecorder()
			handler.ServeHTTP(before, request)
			if before.Code != http.StatusNoContent {
				t.Fatalf("fresh session status = %d", before.Code)
			}
			switch change {
			case "password":
				if err := cfg.SetCredentials("admin", "new-password"); err != nil {
					t.Fatal(err)
				}
			case "token":
				if _, err := s.refreshAPIToken(); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := cfg.SaveAuth(&config.Auth{TokenOnly: true, APIToken: "new-token"}); err != nil {
					t.Fatal(err)
				}
			}
			after := httptest.NewRecorder()
			handler.ServeHTTP(after, request)
			if after.Code != http.StatusUnauthorized {
				t.Fatalf("old session status = %d, want 401", after.Code)
			}
		})
	}
}

// The session cookie had no Secure flag, so behind a TLS-terminating proxy
// (Pangolin, Traefik) a browser would also send it over plain HTTP.
func TestSessionCookieIsSecureOverHTTPS(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	cfg := config.Get()
	if err := cfg.SetCredentials("admin", "secret"); err != nil {
		t.Fatal(err)
	}
	s := &Server{cookie: newCookieStore(cfg.SecretKey())}
	for _, tc := range []struct {
		name, target, forwardedProto string
		secure                       bool
	}{
		{"plain HTTP", "http://decypharr.lan/login", "", false},
		{"TLS", "https://decypharr.example/login", "", true},
		{"TLS-terminating proxy", "http://decypharr.lan/login", "https", true},
		{"proxy chain", "http://decypharr.lan/login", "HTTPS, http", true},
		{"proxy over plain HTTP", "http://decypharr.lan/login", "http", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(`{"username":"admin","password":"secret"}`))
			if tc.forwardedProto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.forwardedProto)
			}
			w := httptest.NewRecorder()
			s.LoginHandler(w, r)
			cookies := w.Result().Cookies()
			if w.Code != http.StatusSeeOther || len(cookies) != 1 {
				t.Fatalf("login = %d with %d cookies: %s", w.Code, len(cookies), w.Body.String())
			}
			if cookies[0].Secure != tc.secure {
				t.Fatalf("session cookie Secure = %t, want %t", cookies[0].Secure, tc.secure)
			}
		})
	}
}
