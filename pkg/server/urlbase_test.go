package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

// Behind a reverse proxy at /decypharr/, redirects went to the proxy's root
// (/setup, /register, /login, /), and the setup check matched its skip list
// against the full path, so /decypharr/setup and the wizard's assets were
// sent to the wizard again.
func TestRedirectsStayUnderTheURLBase(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	update := func(edit func(*config.Config)) {
		t.Helper()
		if _, err := config.Update(func(c *config.Config) error { edit(c); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	update(func(c *config.Config) { c.URLBase = "/decypharr/" })
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })
	s := New(mgr)

	var cookies []*http.Cookie
	expect := func(method, path, body string, status int, location string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if strings.HasPrefix(body, "{") {
			r.Header.Set("Content-Type", "application/json")
		} else if body != "" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, r)
		if w.Code != status || w.Header().Get("Location") != location {
			t.Errorf("%s %s = %d %q, want %d %q", method, path, w.Code, w.Header().Get("Location"), status, location)
		}
		return w
	}

	// Setup incomplete: pages go to the wizard, which loads with its assets.
	expect(http.MethodGet, "/decypharr/", "", http.StatusSeeOther, "/decypharr/setup")
	expect(http.MethodGet, "/decypharr/download", "", http.StatusSeeOther, "/decypharr/setup")
	expect(http.MethodGet, "/decypharr/setup", "", http.StatusOK, "")
	expect(http.MethodGet, "/decypharr/assets/js/setup.js", "", http.StatusOK, "")
	expect(http.MethodGet, "/decypharr/images/logo.png", "", http.StatusOK, "")
	expect(http.MethodGet, "/decypharr/version", "", http.StatusOK, "")

	// Setup complete, auth on, no credential yet: registration comes first.
	update(func(c *config.Config) {
		c.DownloadFolder = t.TempDir()
		c.Debrids = []config.Debrid{{Name: "realdebrid", APIKey: "key"}}
	})
	expect(http.MethodGet, "/decypharr/", "", http.StatusSeeOther, "/decypharr/register")
	expect(http.MethodGet, "/decypharr/login", "", http.StatusSeeOther, "/decypharr/register")
	expect(http.MethodGet, "/decypharr/setup", "", http.StatusSeeOther, "/decypharr/")
	expect(http.MethodPost, "/decypharr/register", "username=admin&password=secret&confirmPassword=secret", http.StatusSeeOther, "/decypharr/")

	// Registered: pages need a login, which lands back on the base.
	expect(http.MethodGet, "/decypharr/register", "", http.StatusSeeOther, "/decypharr/")
	expect(http.MethodGet, "/decypharr/download", "", http.StatusSeeOther, "/decypharr/login")
	cookies = expect(http.MethodPost, "/decypharr/login", `{"username":"admin","password":"secret"}`, http.StatusSeeOther, "/decypharr/").Result().Cookies()
	expect(http.MethodGet, "/decypharr/download", "", http.StatusOK, "")
}
