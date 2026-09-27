package qbit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

func queueTorrents(t *testing.T, q *QBit, entries ...*storage.Entry) {
	t.Helper()
	for _, entry := range entries {
		if err := q.manager.Queue().Add(entry); err != nil {
			t.Fatalf("queue %s: %v", entry.InfoHash, err)
		}
	}
}

// qBittorrent uses "all" as a sentinel meaning "no filtering" for the `filter`
// parameter, and a lone "all" selects every torrent for `hashes`. Matched
// literally, each returned an empty list: no entry has the state "all", and no
// torrent has the infohash "all".
func TestTorrentsInfoTreatsAllAsUnfiltered(t *testing.T) {
	q := newAuthenticationTestQBit(t)
	radarr := strings.Repeat("a", 40)
	sonarr := strings.Repeat("b", 40)
	queueTorrents(t, q,
		&storage.Entry{InfoHash: radarr, Category: "radarr", Protocol: config.ProtocolTorrent, State: storage.EntryStateDownloading},
		&storage.Entry{InfoHash: sonarr, Category: "sonarr", Protocol: config.ProtocolTorrent, State: storage.EntryStatePausedUP},
	)
	routes := q.Routes()

	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"filter all", "filter=all", []string{radarr, sonarr}},
		{"filter all is case-insensitive", "filter=ALL", []string{radarr, sonarr}},
		{"a real state still filters", "filter=downloading", []string{radarr}},
		{"hashes all", "hashes=all", []string{radarr, sonarr}},
		{"hashes all keeps the category filter", "hashes=all&category=radarr", []string{radarr}},
		{"a real hash still filters", "hashes=" + sonarr, []string{sonarr}},
		{"all inside a hash list is literal", "hashes=all&hashes=" + radarr, []string{radarr}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/torrents/info?"+tc.query, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			var torrents []Torrent
			if err := json.Unmarshal(response.Body.Bytes(), &torrents); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got := make([]string, 0, len(torrents))
			for _, torrent := range torrents {
				got = append(got, torrent.Hash)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("hashes = %v, want %v", got, tc.want)
			}
		})
	}
}
