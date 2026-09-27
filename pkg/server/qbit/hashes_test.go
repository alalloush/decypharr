package qbit

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

func postForm(q *QBit, path string, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	q.Routes().ServeHTTP(response, request)
	return response
}

// The Web API joins hashes with "|" in a single field; matched whole, the
// joined value selected nothing.
func TestPipeDelimitedHashesSelectEachTorrent(t *testing.T) {
	q := newAuthenticationTestQBit(t)
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	queueTorrents(t, q,
		&storage.Entry{InfoHash: a, Category: "sonarr", Protocol: config.ProtocolTorrent},
		&storage.Entry{InfoHash: b, Category: "sonarr", Protocol: config.ProtocolTorrent},
		&storage.Entry{InfoHash: c, Category: "sonarr", Protocol: config.ProtocolTorrent},
	)

	if response := postForm(q, "/torrents/delete", url.Values{"hashes": {a + "|" + b}}); response.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", response.Code, response.Body.String())
	}
	for hash, want := range map[string]bool{a: false, b: false, c: true} {
		if _, err := q.manager.Queue().GetTorrent(hash); (err == nil) != want {
			t.Errorf("%s queued = %v, want %v", hash, err == nil, want)
		}
	}
}

// Without hashes the queue filter matches every torrent, so a tag request
// that left them out used to tag or untag the whole queue.
func TestTagChangesRequireHashes(t *testing.T) {
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	for _, tc := range []struct {
		name   string
		hashes []string // form values; nil sends no hashes field
		status int
		tagged []string
	}{
		{"missing hashes", nil, http.StatusBadRequest, nil},
		{"empty hashes", []string{""}, http.StatusBadRequest, nil},
		{"pipe-delimited hashes", []string{a + "|" + b}, http.StatusOK, []string{a, b}},
		{"all", []string{"all"}, http.StatusOK, []string{a, b, c}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := newAuthenticationTestQBit(t)
			queueTorrents(t, q,
				&storage.Entry{InfoHash: a, Protocol: config.ProtocolTorrent, Tags: []string{"old"}},
				&storage.Entry{InfoHash: b, Protocol: config.ProtocolTorrent, Tags: []string{"old"}},
				&storage.Entry{InfoHash: c, Protocol: config.ProtocolTorrent, Tags: []string{"old"}},
			)
			for _, change := range []struct{ path, tag string }{{"/torrents/addTags", "new"}, {"/torrents/removeTags", "old"}} {
				form := url.Values{"tags": {change.tag}}
				for _, hash := range tc.hashes {
					form.Add("hashes", hash)
				}
				if response := postForm(q, change.path, form); response.Code != tc.status {
					t.Fatalf("%s = %d, want %d: %s", change.path, response.Code, tc.status, response.Body.String())
				}
			}
			for _, hash := range []string{a, b, c} {
				entry, err := q.manager.Queue().GetTorrent(hash)
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"old"}
				if slices.Contains(tc.tagged, hash) {
					want = []string{"new"}
				}
				if !slices.Equal(entry.Tags, want) {
					t.Errorf("%s tags = %v, want %v", hash, entry.Tags, want)
				}
			}
		})
	}
}
