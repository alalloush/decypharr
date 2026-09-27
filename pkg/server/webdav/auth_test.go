package webdav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// newWebDAVServer serves the WebDAV routes, minus the readiness gate (the
// manager is never started), with use_auth on and a UI login set.
func newWebDAVServer(t *testing.T) (*httptest.Server, *manager.Manager) {
	t.Helper()
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	if _, err := config.Update(func(c *config.Config) error { return c.SetCredentials("admin", "secret") }); err != nil {
		t.Fatal(err)
	}

	m := manager.New()
	t.Cleanup(func() { _ = m.Stop() })
	entry := &storage.Entry{
		InfoHash: testInfohash,
		Name:     "Movie.2023",
		Files: map[string]*storage.File{
			"Movie.2023.mkv": {Name: "Movie.2023.mkv", Size: 1234, InfoHash: testInfohash},
		},
	}
	if err := m.Storage().AddOrUpdate(entry); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(NewHandler(m).routes())
	t.Cleanup(srv.Close)
	return srv, m
}

func do(t *testing.T, method, url string, prepare func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp
}

// WebDAV used to check credentials only when enable_webdav_auth was also
// set, which it was not by default: with use_auth on, anyone who could reach
// the port could list the library.
func TestWebDAVAuthFollowsUseAuth(t *testing.T) {
	srv, _ := newWebDAVServer(t)
	cfg := config.Get()
	token := cfg.GetAuth().APIToken
	if token == "" {
		t.Fatal("no API token")
	}

	for _, tc := range []struct {
		name    string
		prepare func(*http.Request)
		status  int
	}{
		{"no credentials", nil, http.StatusUnauthorized},
		{"wrong password", func(r *http.Request) { r.SetBasicAuth("admin", "wrong") }, http.StatusUnauthorized},
		{"unknown bearer token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, http.StatusUnauthorized},
		{"UI login", func(r *http.Request) { r.SetBasicAuth("admin", "secret") }, http.StatusMultiStatus},
		{"UI login again (cached)", func(r *http.Request) { r.SetBasicAuth("admin", "secret") }, http.StatusMultiStatus},
		{"API token as password", func(r *http.Request) { r.SetBasicAuth("any", token) }, http.StatusMultiStatus},
		{"API token as bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }, http.StatusMultiStatus},
		{"mount token", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+cfg.WebDAVMountToken()) }, http.StatusMultiStatus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, PROPFIND, srv.URL+"/", tc.prepare)
			if resp.StatusCode != tc.status {
				t.Fatalf("PROPFIND = %d, want %d", resp.StatusCode, tc.status)
			}
			if tc.status == http.StatusUnauthorized && resp.Header.Get("WWW-Authenticate") == "" {
				t.Fatal("401 without a Basic challenge")
			}
		})
	}

	// A new password makes the cached login stale.
	if _, err := config.Update(func(c *config.Config) error { return c.SetCredentials("admin", "changed") }); err != nil {
		t.Fatal(err)
	}
	if resp := do(t, PROPFIND, srv.URL+"/", func(r *http.Request) { r.SetBasicAuth("admin", "secret") }); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password after a change = %d, want 401", resp.StatusCode)
	}

	config.Get().UseAuth = false
	if resp := do(t, PROPFIND, srv.URL+"/", nil); resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND with use_auth off = %d, want 207", resp.StatusCode)
	}
}

// A wildcard Access-Control-Allow-Origin let any web page a LAN user opened
// read listings and send DELETEs.
func TestWebDAVSendsNoCORSHeaders(t *testing.T) {
	srv, _ := newWebDAVServer(t)
	for _, method := range []string{http.MethodOptions, PROPFIND} {
		resp := do(t, method, srv.URL+"/", func(r *http.Request) {
			r.Header.Set("Origin", "http://attacker.test")
			r.Header.Set("Access-Control-Request-Method", "DELETE")
			r.SetBasicAuth("admin", "secret")
		})
		for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
			if value := resp.Header.Get(header); value != "" {
				t.Errorf("%s response has %s: %s", method, header, value)
			}
		}
	}
}

// Deleting a torrent folder over WebDAV deletes the torrent from the debrid
// provider, so it takes webdav_allow_delete.
func TestWebDAVDeleteIsOptIn(t *testing.T) {
	srv, m := newWebDAVServer(t)
	folder := srv.URL + "/__all__/Movie.2023"
	login := func(r *http.Request) { r.SetBasicAuth("admin", "secret") }

	resp := do(t, http.MethodDelete, folder, login)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("DELETE = %d, want 403", resp.StatusCode)
	}
	if _, err := m.Storage().Get(testInfohash); err != nil {
		t.Fatalf("a refused DELETE removed the entry: %v", err)
	}
	if allow := do(t, http.MethodOptions, srv.URL+"/", login).Header.Get("Allow"); allow != "OPTIONS, GET, HEAD, PROPFIND" {
		t.Fatalf("read-only Allow = %q", allow)
	}

	// With the switch on, the DELETE reaches the manager (which fails here:
	// the test entry has no provider to delete from).
	config.Get().WebdavAllowDelete = true
	if resp := do(t, http.MethodDelete, folder, login); resp.StatusCode == http.StatusForbidden {
		t.Fatal("DELETE with webdav_allow_delete is still refused")
	}
}

// COPY and MOVE answered 500 (CopyEntry always failed), a status clients
// such as rclone retry. The tree mirrors the debrid accounts, so neither can
// work, with or without webdav_allow_delete: 405, and Allow never lists them.
func TestWebDAVRefusesCopyAndMove(t *testing.T) {
	srv, m := newWebDAVServer(t)
	for _, allowDelete := range []bool{false, true} {
		config.Get().WebdavAllowDelete = allowDelete
		for _, method := range []string{"COPY", "MOVE"} {
			for _, target := range []string{"/__all__/Movie.2023", "/__all__/Movie.2023/Movie.2023.mkv"} {
				resp := do(t, method, srv.URL+target, func(r *http.Request) {
					r.SetBasicAuth("admin", "secret")
					r.Header.Set("Destination", srv.URL+"/__all__/Renamed")
				})
				if allow := resp.Header.Get("Allow"); resp.StatusCode != http.StatusMethodNotAllowed || strings.Contains(allow, method) {
					t.Errorf("%s %s (webdav_allow_delete %t) = %d, Allow %q; want 405 without %s", method, target, allowDelete, resp.StatusCode, allow, method)
				}
			}
		}
	}
	if _, err := m.Storage().Get(testInfohash); err != nil {
		t.Fatalf("a refused MOVE removed the entry: %v", err)
	}
}
