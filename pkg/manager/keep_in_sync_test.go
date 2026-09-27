package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/utils"
	"github.com/sirrobot01/decypharr/pkg/arr"
	debrid "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashC = "cccccccccccccccccccccccccccccccccccccccc"
	hashD = "dddddddddddddddddddddddddddddddddddddddd"

	fakeFileSize = 1 << 30
	fakeAdded    = "2026-09-01T00:00:00Z"
)

// libraryTorrent is a finished torrent in a fake provider account. TorBox ids
// must be numeric.
type libraryTorrent struct {
	id, hash, name string
}

// fakeDebridAPI holds one provider account and counts the requests the
// refresh makes against it.
type fakeDebridAPI struct {
	mu       sync.Mutex
	library  []libraryTorrent
	requests map[string]int
}

func (f *fakeDebridAPI) setLibrary(torrents ...libraryTorrent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.library = torrents
}

// serve counts a request to endpoint and returns the library it sees.
func (f *fakeDebridAPI) serve(endpoint string) []libraryTorrent {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests == nil {
		f.requests = make(map[string]int)
	}
	f.requests[endpoint]++
	return slices.Clone(f.library)
}

// takeRequests returns the requests counted since the previous call.
func (f *fakeDebridAPI) takeRequests() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	requests := f.requests
	f.requests = nil
	return requests
}

// serveRealDebrid fakes the Real-Debrid endpoints the refresh uses: the
// /torrents list, which has no files, and /torrents/info, which has them.
func serveRealDebrid(t *testing.T, api *fakeDebridAPI) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/torrents":
			library := api.serve("list")
			list := make([]map[string]any, 0, len(library))
			// The client pages with offset until a page comes back empty.
			if r.URL.Query().Get("offset") == "" {
				for _, lt := range library {
					list = append(list, map[string]any{
						"id": lt.id, "filename": lt.name, "hash": lt.hash, "bytes": fakeFileSize,
						"progress": 100, "status": "downloaded", "added": fakeAdded, "ended": fakeAdded,
						"links": []string{"https://real-debrid.invalid/d/" + lt.id},
					})
				}
			}
			_ = json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(r.URL.Path, "/torrents/info/"):
			id := strings.TrimPrefix(r.URL.Path, "/torrents/info/")
			for _, lt := range api.serve("info") {
				if lt.id != id {
					continue
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": lt.id, "filename": lt.name, "original_filename": lt.name, "hash": lt.hash,
					"bytes": fakeFileSize, "progress": 100, "status": "downloaded", "added": fakeAdded,
					"files": []map[string]any{{"id": 1, "path": "/" + lt.name + "/video.mkv", "bytes": fakeFileSize, "selected": 1}},
					"links": []string{"https://real-debrid.invalid/d/" + lt.id},
				})
				return
			}
			http.Error(w, `{"error":"unknown_ressource","error_code":7}`, http.StatusNotFound)
		default:
			// The client fetches its profile in the background; the refresh
			// does not need it.
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// serveTorBox fakes the TorBox endpoints the refresh uses. The account is on
// a plan without usenet, and /torrents/mylist already carries the files.
func serveTorBox(t *testing.T, api *fakeDebridAPI) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/me":
			_, _ = fmt.Fprint(w, `{"success":true,"data":{"id":1,"email":"user@example.invalid","plan":1}}`)
		case "/api/torrents/mylist":
			library := api.serve("list")
			items := make([]map[string]any, 0, len(library))
			if r.URL.Query().Get("offset") == "0" {
				for _, lt := range library {
					id, err := strconv.Atoi(lt.id)
					if err != nil {
						t.Errorf("TorBox id %q is not numeric", lt.id)
					}
					items = append(items, map[string]any{
						"id": id, "hash": lt.hash, "name": lt.name, "size": fakeFileSize, "progress": 1,
						"download_state": "cached", "download_finished": true, "download_present": true,
						"created_at": fakeAdded,
						"files":      []map[string]any{{"id": 0, "name": lt.name + "/video.mkv", "absolute_path": "/" + lt.name + "/video.mkv", "size": fakeFileSize}},
					})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": items})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func useTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	config.Reset()
	config.SetConfigPath(dir)
	t.Cleanup(config.Reset)
	return dir
}

// openManager opens the store in dir and builds real provider clients for
// debrids. The returned func closes the store, as a restart would.
func openManager(t *testing.T, dir string, debrids ...config.Debrid) (*Manager, func()) {
	t.Helper()
	store, err := storage.NewStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	closeStore := func() { once.Do(func() { _ = store.Close() }) }
	t.Cleanup(closeStore)
	m := &Manager{
		storage: store,
		queue:   newQueue(store, ""),
		clients: xsync.NewMap[string, debrid.Client](),
		logger:  zerolog.Nop(),
		config:  config.Get(),
	}
	for _, dc := range debrids {
		setClient(t, m, dc)
	}
	return m, closeStore
}

func setClient(t *testing.T, m *Manager, dc config.Debrid) {
	t.Helper()
	client, err := m.createClient(dc)
	if err != nil {
		t.Fatal(err)
	}
	m.clients.Store(dc.Name, client)
	m.debridOrder = append(m.debridOrder, dc.Name)
}

func refreshProvider(m *Manager, name string) error {
	return m.refreshTorrents(context.Background(), name, m.ProviderClient(name))
}

func mustRefresh(t *testing.T, m *Manager, name string) {
	t.Helper()
	if err := refreshProvider(m, name); err != nil {
		t.Fatalf("refresh %s: %v", name, err)
	}
}

func queuedRows(t *testing.T, m *Manager) map[string]*storage.Entry {
	t.Helper()
	rows, err := m.queue.ListFilter("", config.ProtocolAll, "", nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	byHash := make(map[string]*storage.Entry, len(rows))
	for _, row := range rows {
		byHash[row.InfoHash] = row
	}
	return byHash
}

func checkAdopted(t *testing.T, m *Manager, rows map[string]*storage.Entry, hash string) {
	t.Helper()
	row := rows[hash]
	if row == nil {
		t.Fatalf("%s has no queue row", hash)
	}
	if row.Category != keepInSyncCategory || !isKeepInSyncEntry(row) || row.State != storage.EntryStatePausedUP ||
		row.CompletedAt == nil || row.Action != config.DownloadActionNone || row.Status != types.TorrentStatusDownloaded {
		t.Fatalf("row for %s is not a completed adoption: category=%q tags=%v state=%q action=%q status=%q completed=%v",
			hash, row.Category, row.Tags, row.State, row.Action, row.Status, row.CompletedAt)
	}
	stored, err := m.storage.Get(hash)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Category != keepInSyncCategory {
		t.Fatalf("stored entry %s has category %q, want %q", hash, stored.Category, keepInSyncCategory)
	}
}

func TestKeepInSyncAdoptsProviderTorrentsOnce(t *testing.T) {
	configDir := useTestConfig(t)
	rd := &fakeDebridAPI{}
	rd.setLibrary(
		libraryTorrent{id: "RDA", hash: hashA, name: "Movie.A.2024.1080p"},
		libraryTorrent{id: "RDB", hash: hashB, name: "Movie.B.2024.1080p"},
	)
	rdURL := serveRealDebrid(t, rd)

	// A config that does not mention keep_in_sync, loaded the normal way.
	configJSON := fmt.Sprintf(`{"debrids":[{"name":"realdebrid","provider":"realdebrid","api_key":"token","api_host":%q}]}`, rdURL)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Get()
	cfg.Retries = 0
	dc := cfg.Debrids[0]

	dbDir := filepath.Join(t.TempDir(), "db")
	m, restart := openManager(t, dbDir, dc)
	mustRefresh(t, m, "realdebrid")
	if count, _ := m.storage.Count(); count != 2 {
		t.Fatalf("stored %d entries, want both provider torrents", count)
	}
	if rows := queuedRows(t, m); len(rows) != 0 {
		t.Fatalf("keep_in_sync is off by default, but the queue has %d rows", len(rows))
	}
	if got := rd.takeRequests()["info"]; got != 2 {
		t.Fatalf("first refresh made %d /torrents/info calls, want one per new torrent", got)
	}
	restart()

	// Turning keep_in_sync on (debrid settings apply after a restart) adopts
	// the stored torrents without asking the provider about any of them.
	dc.KeepInSync = true
	m, restart = openManager(t, dbDir, dc)
	mustRefresh(t, m, "realdebrid")
	if got := rd.takeRequests()["info"]; got != 0 {
		t.Fatalf("adoption made %d /torrents/info calls, want none", got)
	}
	rows := queuedRows(t, m)
	checkAdopted(t, m, rows, hashA)
	checkAdopted(t, m, rows, hashB)

	// Later passes leave adopted rows alone, and deleting a row in the
	// dashboard is final, also across a restart.
	if err := m.queue.Delete(hashB, false, nil); err != nil {
		t.Fatal(err)
	}
	relabelled := rows[hashA]
	relabelled.Category = "movies"
	if err := m.queue.Update(relabelled); err != nil {
		t.Fatal(err)
	}
	mustRefresh(t, m, "realdebrid")
	restart()
	m, _ = openManager(t, dbDir, dc)
	mustRefresh(t, m, "realdebrid")
	rows = queuedRows(t, m)
	if len(rows) != 1 || rows[hashA] == nil || rows[hashA].Category != "movies" {
		t.Fatalf("rows after more passes and a restart = %v, want only %s, still in movies", slices.Collect(maps.Keys(rows)), hashA)
	}
	if got := rd.takeRequests()["info"]; got != 0 {
		t.Fatalf("later refreshes made %d /torrents/info calls, want none", got)
	}
}

func TestKeepInSyncLeavesArrTorrentsAlone(t *testing.T) {
	useTestConfig(t)
	config.Get().Retries = 0
	rd := &fakeDebridAPI{}
	rd.setLibrary(
		libraryTorrent{id: "RDA", hash: hashA, name: "Movie.A.2024.1080p"},
		libraryTorrent{id: "RDC", hash: hashC, name: "Show.C.S01.1080p"},
		libraryTorrent{id: "RDD", hash: hashD, name: "Show.D.S01.1080p"},
	)
	dc := config.Debrid{Name: "realdebrid", Provider: "realdebrid", APIKey: "token", APIHost: serveRealDebrid(t, rd)}
	m, _ := openManager(t, filepath.Join(t.TempDir(), "db"), dc)
	mustRefresh(t, m, "realdebrid")

	// C was imported by Sonarr, which then removed it from the queue: the
	// stored entry keeps Sonarr's category. D is a Sonarr import in flight.
	finished, err := m.storage.Get(hashC)
	if err != nil {
		t.Fatal(err)
	}
	finished.Category = "sonarr"
	if err := m.storage.AddOrUpdate(finished); err != nil {
		t.Fatal(err)
	}
	inFlight := &storage.Entry{
		InfoHash: hashD, Name: "Show.D.S01.1080p", Protocol: config.ProtocolTorrent, Category: "sonarr",
		State: storage.EntryStateDownloading, Status: types.TorrentStatusDownloading, Action: config.DownloadActionSymlink,
	}
	if err := m.queue.Add(inFlight); err != nil {
		t.Fatal(err)
	}

	dc.KeepInSync = true
	setClient(t, m, dc)
	mustRefresh(t, m, "realdebrid")
	rows := queuedRows(t, m)
	checkAdopted(t, m, rows, hashA)
	if rows[hashC] != nil {
		t.Fatal("adopted a torrent that Sonarr imported")
	}
	if row := rows[hashD]; row == nil || row.Category != "sonarr" || row.State != storage.EntryStateDownloading || isKeepInSyncEntry(row) {
		t.Fatalf("in-flight Sonarr row changed: %+v", row)
	}

	// Once a torrent is gone from the provider, its adopted row goes too.
	// Sonarr's row stays for Sonarr to remove.
	rd.setLibrary(libraryTorrent{id: "RDC", hash: hashC, name: "Show.C.S01.1080p"})
	mustRefresh(t, m, "realdebrid")
	rows = queuedRows(t, m)
	if rows[hashA] != nil {
		t.Fatal("adopted row outlived its torrent")
	}
	if rows[hashD] == nil {
		t.Fatal("removed Sonarr's row")
	}
}

// TestKeepInSyncAdoptsSharedHashOnce covers upstream #287: a hash held by both
// Real-Debrid and TorBox is one entry and gets one row.
func TestKeepInSyncAdoptsSharedHashOnce(t *testing.T) {
	useTestConfig(t)
	config.Get().Retries = 0
	rd, tb := &fakeDebridAPI{}, &fakeDebridAPI{}
	rd.setLibrary(
		libraryTorrent{id: "RDA", hash: hashA, name: "Movie.A.2024.1080p"},
		libraryTorrent{id: "RDB", hash: hashB, name: "Movie.B.2024.1080p"},
	)
	tb.setLibrary(
		libraryTorrent{id: "11", hash: hashA, name: "Movie.A.2024.1080p"},
		libraryTorrent{id: "12", hash: hashC, name: "Show.C.S01.1080p"},
	)
	rdConfig := config.Debrid{Name: "realdebrid", Provider: "realdebrid", APIKey: "token", APIHost: serveRealDebrid(t, rd)}
	tbConfig := config.Debrid{Name: "torbox", Provider: "torbox", APIKey: "token", APIHost: serveTorBox(t, tb)}

	t.Run("both providers, concurrent refreshes", func(t *testing.T) {
		rdOn, tbOn := rdConfig, tbConfig
		rdOn.KeepInSync, tbOn.KeepInSync = true, true
		m, _ := openManager(t, filepath.Join(t.TempDir(), "db"), rdOn, tbOn)
		// The startup sync refreshes every provider at once.
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, name := range []string{"realdebrid", "torbox"} {
			wg.Go(func() { errs <- refreshProvider(m, name) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		rows := queuedRows(t, m)
		if len(rows) != 3 {
			t.Fatalf("queue has %d rows, want one per hash (3)", len(rows))
		}
		for _, hash := range []string{hashA, hashB, hashC} {
			checkAdopted(t, m, rows, hash)
		}
	})

	t.Run("only one provider opted in", func(t *testing.T) {
		tbOn := tbConfig
		tbOn.KeepInSync = true
		m, _ := openManager(t, filepath.Join(t.TempDir(), "db"), rdConfig, tbOn)
		mustRefresh(t, m, "realdebrid")
		mustRefresh(t, m, "torbox")
		rows := queuedRows(t, m)
		if len(rows) != 2 || rows[hashB] != nil {
			t.Fatalf("rows = %v, want the TorBox torrents %s and %s only", slices.Collect(maps.Keys(rows)), hashA, hashC)
		}
		checkAdopted(t, m, rows, hashA)
		checkAdopted(t, m, rows, hashC)
		shared, err := m.storage.Get(hashA)
		if err != nil {
			t.Fatal(err)
		}
		if !shared.HasProvider("realdebrid") || !shared.HasProvider("torbox") {
			t.Fatalf("shared entry placements = %v, want both providers kept", slices.Collect(maps.Keys(shared.Providers)))
		}
	})
}

// importProvider accepts every magnet as an instantly cached torrent.
type importProvider struct {
	debrid.Client
	submissions atomic.Int32
}

func (p *importProvider) Config() config.Debrid {
	return config.Debrid{Name: "realdebrid", Provider: "realdebrid"}
}

func (p *importProvider) Logger() zerolog.Logger { return zerolog.Nop() }

func (p *importProvider) SubmitMagnet(torrent *types.Torrent) (*types.Torrent, error) {
	p.submissions.Add(1)
	torrent.Id = "added-" + torrent.InfoHash[:4]
	torrent.Debrid = "realdebrid"
	torrent.Status = types.TorrentStatusDownloaded
	return torrent, nil
}

func (p *importProvider) CheckStatus(torrent *types.Torrent) (*types.Torrent, error) {
	return torrent, nil
}

// An Arr grabbing a hash that keep_in_sync adopted must get it under its own
// category; an existing row that an Arr owns stays an idempotent success.
func TestAddNewTorrentReplacesAdoptedRow(t *testing.T) {
	useTestConfig(t)
	store, err := storage.NewStorage(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := &importProvider{}
	m := &Manager{
		storage:            store,
		queue:              newQueue(store, ""),
		clients:            xsync.NewMap[string, debrid.Client](),
		logger:             zerolog.Nop(),
		torrentSubmissions: newTorrentSubmissionGate(torrentSubmissionDedupWindow),
	}
	m.clients.Store("realdebrid", provider)
	m.debridOrder = []string{"realdebrid"}
	m.jobQueue = NewJobQueue(t.Context(), 1, func(context.Context, *Job) {})
	t.Cleanup(m.jobQueue.Close)

	adopted := &storage.Entry{
		InfoHash: hashA, Name: "Movie.A.2024.1080p", Protocol: config.ProtocolTorrent, Category: keepInSyncCategory,
		Tags: []string{keepInSyncTag}, State: storage.EntryStatePausedUP, Action: config.DownloadActionNone,
	}
	owned := &storage.Entry{
		InfoHash: hashB, Name: "Movie.B.2024.1080p", Protocol: config.ProtocolTorrent, Category: "radarr",
		State: storage.EntryStatePausedUP, Action: config.DownloadActionSymlink,
	}
	for _, row := range []*storage.Entry{adopted, owned} {
		if err := m.queue.Update(row); err != nil {
			t.Fatal(err)
		}
	}

	grab := func(hash, name, category string) {
		t.Helper()
		magnet := &utils.Magnet{InfoHash: hash, Name: name, Link: "magnet:?xt=urn:btih:" + hash}
		req := NewTorrentRequest("", t.TempDir(), magnet, arr.Arr{Name: category}, config.DownloadActionSymlink, nil, "", ImportTypeQBit, false)
		if err := m.AddNewTorrent(t.Context(), req); err != nil {
			t.Fatalf("AddNewTorrent(%s): %v", hash, err)
		}
	}

	grab(hashA, "Movie.A.2024.1080p", "radarr")
	if got := provider.submissions.Load(); got != 1 {
		t.Fatalf("submissions = %d, want the adopted hash submitted for Radarr", got)
	}
	row, err := m.queue.GetTorrent(hashA)
	if err != nil {
		t.Fatal(err)
	}
	if row.Category != "radarr" || isKeepInSyncEntry(row) {
		t.Fatalf("row after the grab: category=%q tags=%v, want Radarr's import", row.Category, row.Tags)
	}

	grab(hashB, "Movie.B.2024.1080p", "sonarr")
	if got := provider.submissions.Load(); got != 1 {
		t.Fatalf("submissions = %d, want no submission for a hash an Arr already owns", got)
	}
	if row, err := m.queue.GetTorrent(hashB); err != nil || row.Category != "radarr" {
		t.Fatalf("owned row changed: %+v, %v", row, err)
	}
}
