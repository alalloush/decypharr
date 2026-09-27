package torbox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
)

const usenetDone = `{"id":2515089,"hash":"19028D8F31E388975068B9F11CAB4476","name":"Show.S01E01.1080p-GRP","size":300,"progress":1,"download_state":"completed","download_finished":true,"download_present":true,"created_at":"2026-09-23T09:03:01Z","files":[{"id":0,"name":"Show.S01E01.1080p-GRP/ebfd37cc.mkv","absolute_path":"/downloads/x/Show.S01E01.1080p-GRP/ebfd37cc.mkv","size":300}]}`
const usenetFailed = `{"id":2515090,"hash":"aa","name":"Broken","download_state":"failed (Aborted)","download_finished":true,"download_present":false,"created_at":"2026-09-23T09:03:01Z","files":[]}`

type usenetRequests struct {
	listings atomic.Int32 // /api/usenet/mylist
	creates  atomic.Int32 // /api/torrents/createtorrent
}

// usenetTorbox serves one finished and one failed usenet item to an account on
// plan (a GetProfile type such as "pro").
func usenetTorbox(t *testing.T, failUsenet bool, plan string) (*Torbox, *usenetRequests) {
	t.Helper()
	config.SetConfigPath(t.TempDir())
	var reqs usenetRequests
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/usenet/mylist") {
			reqs.listings.Add(1)
		}
		if r.URL.Path == "/api/torrents/createtorrent" {
			reqs.creates.Add(1)
		}
		off := r.URL.Query().Get("offset")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/usenet/mylist") && failUsenet:
			http.Error(w, "boom", http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/api/usenet/mylist") && r.URL.Query().Get("id") != "":
			_, _ = fmt.Fprint(w, `{"success":true,"data":`+usenetDone+`}`)
		case strings.HasPrefix(r.URL.Path, "/api/usenet/mylist") && off == "0":
			_, _ = fmt.Fprint(w, `{"success":true,"data":[`+usenetDone+`,`+usenetFailed+`]}`)
		default:
			_, _ = fmt.Fprint(w, `{"success":true,"data":[]}`)
		}
	}))
	t.Cleanup(srv.Close)
	tb := testTorbox(srv.URL)
	seedProfile(t, tb, &types.Profile{Type: plan})
	return tb, &reqs
}

func TestGetTorrentsIncludesFinishedUsenet(t *testing.T) {
	tb, _ := usenetTorbox(t, false, usenetPlan)
	got, err := tb.GetTorrents()
	if err != nil {
		t.Fatalf("GetTorrents() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1 (failed usenet job must be skipped)", len(got))
	}
	u := got[0]
	if u.Id != "u2515089" || u.InfoHash != "19028d8f31e388975068b9f11cab4476" || u.OriginalFilename != "Show.S01E01.1080p-GRP" {
		t.Fatalf("unexpected usenet item: id=%s hash=%s folder=%s", u.Id, u.InfoHash, u.OriginalFilename)
	}
	f, ok := u.Files["ebfd37cc.mkv"]
	if !ok || f.Link != "torbox://u2515089/0" {
		t.Fatalf("file link = %#v", u.Files)
	}
}

func TestGetTorrentsFailsWholeRefreshOnUsenetError(t *testing.T) {
	tb, _ := usenetTorbox(t, true, usenetPlan)
	got, err := tb.GetTorrents()
	if err == nil || got != nil {
		t.Fatalf("want error and nil list on usenet failure, got %v / %d items", err, len(got))
	}
}

// Accounts without usenet must keep refreshing torrents whatever
// /usenet/mylist would answer them, so they never call it.
func TestGetTorrentsSkipsUsenetWithoutUsenetPlan(t *testing.T) {
	tb, reqs := usenetTorbox(t, true, "standard")
	if _, err := tb.GetTorrents(); err != nil {
		t.Fatalf("GetTorrents() error = %v, want the torrent refresh unaffected", err)
	}
	if n := reqs.listings.Load(); n != 0 {
		t.Fatalf("usenet listing requests = %d, want none on a plan without usenet", n)
	}
}

func TestUsenetSafetyGuards(t *testing.T) {
	tb, reqs := usenetTorbox(t, false, usenetPlan)
	if _, err := tb.GetTorrents(); err != nil {
		t.Fatal(err)
	}
	if err := tb.DeleteTorrent("u2515089"); err != nil {
		t.Fatalf("usenet delete must be a no-op, got %v", err)
	}
	_, err := tb.SubmitMagnet(&types.Torrent{Name: "x", InfoHash: "19028D8F31E388975068B9F11CAB4476", DownloadUncached: true, Magnet: &utilsMagnet})
	if err == nil || reqs.creates.Load() != 0 {
		t.Fatalf("SubmitMagnet must refuse a known usenet hash before createtorrent: err=%v, createtorrent calls=%d", err, reqs.creates.Load())
	}
	if err := tb.CheckFile(t.Context(), "", "torbox://u2515089/0"); err != nil {
		t.Fatalf("present usenet item should pass CheckFile, got %v", err)
	}
	tr, err := tb.GetTorrent("u2515089")
	if err != nil || tr.Id != "u2515089" {
		t.Fatalf("GetTorrent(u…) = %v, %v", tr, err)
	}
}

// Usenet files resolve through /usenet/requestdl with the bare usenet id.
func TestUsenetDownloadLinkUsesUsenetRequestdl(t *testing.T) {
	tb := linkTestTorbox(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/usenet/requestdl" || q.Get("usenet_id") != "2515089" || q.Get("file_id") != "0" || q.Has("torrent_id") {
			t.Errorf("request = %s?%s, want /api/usenet/requestdl for usenet 2515089 file 0", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"success":true,"data":"https://cdn.example/dld/usenet"}`)
	})
	dl, err := tb.GetDownloadLink(t.Context(), "u2515089", &types.File{Id: "0", Name: "ebfd37cc.mkv", Link: "torbox://u2515089/0"})
	if err != nil || dl.DownloadLink != "https://cdn.example/dld/usenet" {
		t.Fatalf("GetDownloadLink(u…) = %q, %v; want the usenet CDN URL", dl.DownloadLink, err)
	}
}

func TestUsenetID(t *testing.T) {
	for in, want := range map[string]bool{"u123": true, "123": false, "u": false, "uabc": false} {
		if _, ok := usenetID(in); ok != want {
			t.Errorf("usenetID(%q) = %v, want %v", in, ok, want)
		}
	}
}

var utilsMagnet = utils.Magnet{Link: "magnet:?xt=urn:btih:19028d8f31e388975068b9f11cab4476"}
