package torbox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/customerror"
	"github.com/sirrobot01/decypharr/pkg/debrid/account"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
)

func linkTestTorbox(t *testing.T, handler http.HandlerFunc) *Torbox {
	t.Helper()
	config.SetConfigPath(t.TempDir())
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	tb := testTorbox(server.URL)
	tb.autoExpiresLinksAfter = 72 * time.Hour
	tb.accountsManager = account.NewManager(config.Debrid{Name: "torbox", DownloadAPIKeys: []string{"dl-token"}}, nil, zerolog.Nop())
	return tb
}

// Range requests must go to the CDN, not to requestdl: one API call per file
// until the resolved URL expires.
func TestDownloadLinkResolvesCDNURLOnce(t *testing.T) {
	var calls atomic.Int32
	tb := linkTestTorbox(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/torrents/requestdl" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		q := r.URL.Query()
		if q.Get("token") != "dl-token" || q.Get("torrent_id") != "7" || q.Get("file_id") != "3" {
			t.Errorf("requestdl query = %v, want the download token, torrent 7, file 3", q)
		}
		if q.Has("redirect") {
			t.Error("requestdl asked for a redirect; the JSON body carries the CDN URL")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"success":true,"detail":"Download link generated.","data":"https://cdn.example/dld/uuid?token=t1"}`)
	})

	file := &types.File{Id: "3", Name: "Release.mkv", Link: "torbox://7/3"}
	first, err := tb.GetDownloadLink(t.Context(), "7", file)
	if err != nil {
		t.Fatalf("GetDownloadLink() error = %v", err)
	}
	if first.DownloadLink != "https://cdn.example/dld/uuid?token=t1" {
		t.Fatalf("DownloadLink = %q, want the resolved CDN URL", first.DownloadLink)
	}
	if first.Token != "dl-token" {
		t.Fatalf("Token = %q, want the token of the account that cached the link", first.Token)
	}
	if lifetime := time.Until(first.ExpiresAt); lifetime <= 0 || lifetime > cdnLinkLifetime {
		t.Fatalf("link lifetime = %s, want at most %s even with auto_expire_links_after=72h", lifetime, cdnLinkLifetime)
	}

	second, err := tb.GetDownloadLink(t.Context(), "7", file)
	if err != nil {
		t.Fatalf("second GetDownloadLink() error = %v", err)
	}
	if second.DownloadLink != first.DownloadLink || calls.Load() != 1 {
		t.Fatalf("requestdl calls = %d, link %q; want one call and the cached URL reused", calls.Load(), second.DownloadLink)
	}
}

// A failed requestdl must surface as an error, never as a cached empty link.
func TestDownloadLinkRejectsFailedRequestdl(t *testing.T) {
	for name, respond := range map[string]func(http.ResponseWriter){
		"http error": func(w http.ResponseWriter) {
			http.Error(w, `{"success":false}`, http.StatusForbidden)
		},
		"unsuccessful body": func(w http.ResponseWriter) {
			_, _ = fmt.Fprint(w, `{"success":false,"error":"DOWNLOAD_NOT_FOUND","detail":"not found","data":null}`)
		},
		"empty link": func(w http.ResponseWriter) {
			_, _ = fmt.Fprint(w, `{"success":true,"data":""}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			tb := linkTestTorbox(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				respond(w)
			})
			file := &types.File{Id: "3", Name: "Release.mkv", Link: "torbox://7/3"}
			for range 2 {
				if dl, err := tb.GetDownloadLink(t.Context(), "7", file); err == nil {
					t.Fatalf("GetDownloadLink() = %+v, want an error", dl)
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("requestdl calls = %d, want 2 (a failure is not cached)", calls.Load())
			}
		})
	}
}

// Only a rejected API token is permanent: the DFS circuit breaker trips on it
// at once. 403 and 404 keep the link rules and are not permanent.
func TestRequestdlStatusClassification(t *testing.T) {
	for status, permanent := range map[int]bool{
		http.StatusUnauthorized: true,
		http.StatusForbidden:    false,
		http.StatusNotFound:     false,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			tb := linkTestTorbox(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"success":false}`, status)
			})
			_, err := tb.GetDownloadLink(t.Context(), "7", &types.File{Id: "3", Name: "Release.mkv", Link: "torbox://7/3"})
			if err == nil {
				t.Fatal("GetDownloadLink() succeeded, want an error")
			}
			if got := customerror.IsPermanentError(err); got != permanent {
				t.Fatalf("IsPermanentError(%v) = %v, want %v", err, got, permanent)
			}
		})
	}
}
