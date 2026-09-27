package qbit

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// setCategory used to leave its filter nil, so moving one torrent to another
// category (an arr's post-import category) rewrote every queued entry.
func TestSetCategoryOnlyChangesSelectedTorrents(t *testing.T) {
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	const nzb = "nzb-download"
	untouched := map[string]string{a: "sonarr", b: "sonarr", c: "sonarr", nzb: "sonarr"}

	for _, tc := range []struct {
		name   string
		hashes []string // form values; nil sends no hashes field
		status int
		want   map[string]string
	}{
		{"one hash", []string{strings.ToUpper(a)}, http.StatusOK, map[string]string{a: "imported", b: "sonarr", c: "sonarr", nzb: "sonarr"}},
		{"pipe-delimited hashes", []string{a + "|" + b}, http.StatusOK, map[string]string{a: "imported", b: "imported", c: "sonarr", nzb: "sonarr"}},
		{"repeated hashes fields", []string{a, b}, http.StatusOK, map[string]string{a: "imported", b: "imported", c: "sonarr", nzb: "sonarr"}},
		{"all selects every torrent but no NZB", []string{"all"}, http.StatusOK, map[string]string{a: "imported", b: "imported", c: "imported", nzb: "sonarr"}},
		{"missing hashes", nil, http.StatusBadRequest, untouched},
		{"all mixed with a hash", []string{"all|" + a}, http.StatusBadRequest, untouched},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := newAuthenticationTestQBit(t)
			queueTorrents(t, q,
				&storage.Entry{InfoHash: a, Category: "sonarr", Protocol: config.ProtocolTorrent},
				&storage.Entry{InfoHash: b, Category: "sonarr", Protocol: config.ProtocolTorrent},
				&storage.Entry{InfoHash: c, Category: "sonarr", Protocol: config.ProtocolTorrent},
				&storage.Entry{InfoHash: nzb, Category: "sonarr", Protocol: config.ProtocolNZB},
			)

			form := url.Values{"category": {"imported"}}
			for _, hash := range tc.hashes {
				form.Add("hashes", hash)
			}
			request := httptest.NewRequest(http.MethodPost, "/torrents/setCategory", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			q.Routes().ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}

			for hash, want := range tc.want {
				entry, err := q.manager.Queue().GetTorrent(hash)
				if err != nil {
					t.Fatal(err)
				}
				if entry.Category != want {
					t.Errorf("%s category = %q, want %q", hash, entry.Category, want)
				}
			}
		})
	}
}
