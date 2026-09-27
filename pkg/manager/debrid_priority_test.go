package manager

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/utils"
	debrid "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
)

func newPriorityTestManager(t *testing.T, debrids []config.Debrid) *Manager {
	t.Helper()
	cfg := config.Get()
	cfg.Debrids = debrids
	m := &Manager{clients: xsync.NewMap[string, debrid.Client](), logger: zerolog.Nop()}
	m.initDebridClients()
	return m
}

func clientNames(clients []debrid.Client) []string {
	names := make([]string, 0, len(clients))
	for _, c := range clients {
		names = append(names, c.Config().Name)
	}
	return names
}

// Each manager gets a fresh client map, so a fresh hash seed. The order must
// not depend on it: ascending priority, unset (0) meaning config position,
// ties in config order.
func TestFilterDebridOrderIsPriorityThenConfigOrder(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)

	debrids := []config.Debrid{
		{Name: "a", Provider: "torbox", APIKey: "k"},                // unset: position 1
		{Name: "b", Provider: "torbox", APIKey: "k", Priority: 2},   //
		{Name: "c", Provider: "torbox", APIKey: "k", Priority: 1},   // ties a, after it
		{Name: "d", Provider: "torbox", APIKey: "k"},                // unset: position 4
		{Name: "e", Provider: "torbox", APIKey: "k", Priority: -1},  // first
		{Name: "f", Provider: "torbox", APIKey: "k", Priority: 2},   // ties b, after it
		{Name: "g", Provider: "torbox", APIKey: "k", Priority: 100}, // last
	}
	want := []string{"e", "a", "c", "b", "f", "d", "g"}

	all := func(debrid.Client) bool { return true }
	for range 50 {
		got := clientNames(newPriorityTestManager(t, debrids).FilterDebrid(all))
		if !slices.Equal(got, want) {
			t.Fatalf("FilterDebrid order = %v, want %v", got, want)
		}
	}
}

// cachedFallback stands in for the second provider: its submit succeeds and
// the torrent is already downloaded.
type cachedFallback struct {
	debrid.Client
	cfg    config.Debrid
	record func(string)
}

func (c cachedFallback) Config() config.Debrid  { return c.cfg }
func (c cachedFallback) Logger() zerolog.Logger { return zerolog.Nop() }

func (c cachedFallback) SubmitMagnet(tr *types.Torrent) (*types.Torrent, error) {
	c.record(c.cfg.Name + " submit")
	return &types.Torrent{Id: "rd-1", InfoHash: tr.InfoHash, Name: tr.Name, Debrid: c.cfg.Name}, nil
}

func (c cachedFallback) CheckStatus(tr *types.Torrent) (*types.Torrent, error) {
	tr.Status = types.TorrentStatusDownloaded
	return tr, nil
}

// With download_uncached=false, TorBox asks checkcached before createtorrent
// (#370). A miss on the higher-priority TorBox must fall through to the next
// provider instead of failing the grab.
func TestSendToDebridFallsThroughUncachedTorboxByPriority(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)

	const hash = "0123456789abcdef0123456789abcdef01234567"
	var (
		mu    sync.Mutex
		calls []string
	)
	record := func(call string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/torrents/checkcached":
			record("torbox checkcached")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
		case "/api/torrents/createtorrent":
			record("torbox createtorrent")
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	// realdebrid is listed first but has the lower priority. Its client is
	// built as TorBox against the test server (realdebrid.New fetches the
	// profile in the background) and then replaced by the fake.
	rd := config.Debrid{Name: "realdebrid", Provider: "torbox", APIKey: "k", APIHost: server.URL, Priority: 2}
	tb := config.Debrid{Name: "torbox", Provider: "torbox", APIKey: "k", APIHost: server.URL, Priority: 1}
	m := newPriorityTestManager(t, []config.Debrid{rd, tb})
	m.clients.Store(rd.Name, cachedFallback{cfg: rd, record: record})

	got, err := m.SendToDebrid(t.Context(), &ImportRequest{
		Magnet: &utils.Magnet{InfoHash: hash, Name: "some.release", Link: "magnet:?xt=urn:btih:" + hash},
	})
	if err != nil {
		t.Fatalf("SendToDebrid() error = %v, want fall-through to realdebrid", err)
	}
	if got.Debrid != "realdebrid" {
		t.Errorf("torrent placed on %q, want realdebrid", got.Debrid)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"torbox checkcached", "realdebrid submit"}; !slices.Equal(calls, want) {
		t.Errorf("provider calls = %v, want %v", calls, want)
	}
}
