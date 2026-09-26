package torbox

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/customerror"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
)

const testHash = "0123456789abcdef0123456789abcdef01234567"

// The point of the check is to refuse cheaply *before* createtorrent, and to stay
// out of the way whenever the answer is not conclusive: "the probe failed" is not
// "not cached".
func TestSubmitMagnetSkipsCreateTorrentOnlyWhenKnownUncached(t *testing.T) {
	config.SetConfigPath(t.TempDir())

	cases := []struct {
		name             string
		hash             string
		checkcached      func(w http.ResponseWriter)
		downloadUncached bool
		wantProbe        bool
		wantCreateCalled bool
		wantNotCached    bool
	}{
		{
			name: "known uncached: refuse without calling createtorrent",
			hash: testHash,
			checkcached: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
			},
			wantProbe: true, wantCreateCalled: false, wantNotCached: true,
		},
		{
			name: "probe failed: fall through to createtorrent as before",
			hash: testHash,
			checkcached: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantProbe: true, wantCreateCalled: true,
		},
		{
			name: "cached: proceed to createtorrent",
			hash: testHash,
			checkcached: func(w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":true,"data":{"` + testHash +
					`":{"name":"x","size":123,"hash":"` + testHash + `"}}}`))
			},
			wantProbe: true, wantCreateCalled: true,
		},
		{
			name:             "download_uncached: never probe, never refuse",
			hash:             testHash,
			downloadUncached: true,
			wantProbe:        false, wantCreateCalled: true,
		},
		{
			name:      "empty hash is unknown, not negative",
			hash:      "",
			wantProbe: false, wantCreateCalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var probed, createCalled bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/torrents/checkcached":
					probed = true
					if tc.checkcached == nil {
						w.WriteHeader(http.StatusTeapot)
						return
					}
					tc.checkcached(w)
				case "/api/torrents/createtorrent":
					createCalled = true
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"success":true,"data":{"torrent_id":1,"hash":"` +
						testHash + `"}}`))
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			t.Cleanup(server.Close)

			_, err := testTorbox(server.URL).SubmitMagnet(&types.Torrent{
				InfoHash:         tc.hash,
				Name:             "some.release",
				DownloadUncached: tc.downloadUncached,
				Magnet:           &utils.Magnet{Link: "magnet:?xt=urn:btih:" + testHash},
			})

			if probed != tc.wantProbe {
				t.Errorf("checkcached called = %v, want %v", probed, tc.wantProbe)
			}
			if createCalled != tc.wantCreateCalled {
				t.Errorf("createtorrent called = %v, want %v", createCalled, tc.wantCreateCalled)
			}
			if tc.wantNotCached {
				if !errors.Is(err, customerror.TorrentNotCachedError) {
					t.Errorf("SubmitMagnet() error = %v, want TorrentNotCachedError", err)
				}
			} else if err != nil {
				t.Errorf("SubmitMagnet() error = %v, want nil", err)
			}
		})
	}
}
