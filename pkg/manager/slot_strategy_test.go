package manager

import (
	"fmt"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	debridCommon "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// fakeSlotClient implements the calls the slot strategy and the Fixer's
// re-insertion path make. Any other method hits the nil embedded interface
// and panics, so a new dependency fails loudly.
type fakeSlotClient struct {
	debridCommon.Client
	cfg       config.Debrid
	nextID    int
	submitted []string
	deleted   []string
}

func (f *fakeSlotClient) Config() config.Debrid { return f.cfg }

func (f *fakeSlotClient) SubmitMagnet(tr *types.Torrent) (*types.Torrent, error) {
	f.nextID++
	tr.Id = fmt.Sprintf("id-%d", f.nextID)
	tr.Debrid = f.cfg.Name
	f.submitted = append(f.submitted, tr.Id)
	return tr, nil
}

func (f *fakeSlotClient) CheckStatus(tr *types.Torrent) (*types.Torrent, error) {
	tr.Status = types.TorrentStatusDownloaded
	tr.Debrid = f.cfg.Name
	tr.Files = map[string]types.File{
		"movie.mkv": {Name: "movie.mkv", Link: "https://example.invalid/" + tr.Id, Size: 1024},
	}
	return tr, nil
}

func (f *fakeSlotClient) DeleteTorrent(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func newSlotStrategyManager(t *testing.T, strategy string) (*Manager, *fakeSlotClient, *storage.Storage) {
	t.Helper()
	config.SetConfigPath(t.TempDir())
	strg, err := storage.NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = strg.Close() })

	client := &fakeSlotClient{cfg: config.Debrid{Name: "alldebrid", Provider: "alldebrid", SlotStrategy: strategy}}
	m := &Manager{
		storage: strg,
		logger:  zerolog.Nop(),
		clients: xsync.NewMap[string, debridCommon.Client](),
		config:  &config.Config{},
	}
	m.clients.Store("alldebrid", client)
	return m, client, strg
}

func TestApplySlotStrategyFreesEachPlacementOnce(t *testing.T) {
	m, client, strg := newSlotStrategyManager(t, "remove_after_add")
	entry := &storage.Entry{InfoHash: "aaa1", Name: "test", Protocol: config.ProtocolTorrent}
	entry.AddTorrentProvider(&types.Torrent{Id: "111", Debrid: "alldebrid"})

	m.applySlotStrategy(entry)
	m.applySlotStrategy(entry)

	if len(client.deleted) != 1 || client.deleted[0] != "111" {
		t.Fatalf("deleted = %v, want [111] (freed once)", client.deleted)
	}
	saved, err := strg.Get(entry.InfoHash)
	if err != nil {
		t.Fatalf("storage.Get: %v", err)
	}
	if saved.Providers["alldebrid"].RemovedAt == nil {
		t.Fatal("RemovedAt not persisted")
	}
}

// Without remove_after_add nothing is deleted and nothing is written: the
// completion path behaves exactly as before the strategy existed.
func TestApplySlotStrategyWithoutStrategyIsInert(t *testing.T) {
	for _, strategy := range []string{"", "remove_oldest"} {
		m, client, strg := newSlotStrategyManager(t, strategy)
		entry := &storage.Entry{InfoHash: "bbb2", Name: "test", Protocol: config.ProtocolTorrent}
		entry.AddTorrentProvider(&types.Torrent{Id: "333", Debrid: "alldebrid"})

		m.applySlotStrategy(entry)

		if len(client.deleted) != 0 || entry.Providers["alldebrid"].RemovedAt != nil {
			t.Errorf("strategy %q: deleted = %v, RemovedAt = %v; want untouched", strategy, client.deleted, entry.Providers["alldebrid"].RemovedAt)
		}
		if _, err := strg.Get(entry.InfoHash); err == nil {
			t.Errorf("strategy %q: entry was persisted, want no write", strategy)
		}
	}
}

// A repair re-insertion replaces the placement (RemovedAt nil again), so
// MoveTorrent must free the slot again and persist that.
func TestMoveTorrentReappliesSlotStrategyAfterReinsertion(t *testing.T) {
	m, client, strg := newSlotStrategyManager(t, "remove_after_add")
	f := &Fixer{manager: m}
	entry := &storage.Entry{InfoHash: "aaa1", Name: "Test Movie", Protocol: config.ProtocolTorrent}

	for i := range 2 {
		// Keep MoveTorrent from also deleting the previous placement in a
		// goroutine, which would race the assertions below.
		entry.ActiveProvider = ""
		if ok, err := f.MoveTorrent(entry, "alldebrid", true); err != nil || !ok {
			t.Fatalf("MoveTorrent #%d = %v, %v", i+1, ok, err)
		}
		if len(client.deleted) != i+1 || client.deleted[i] != client.submitted[i] {
			t.Fatalf("after insertion #%d: deleted = %v, submitted = %v", i+1, client.deleted, client.submitted)
		}
	}
	saved, err := strg.Get(entry.InfoHash)
	if err != nil {
		t.Fatalf("storage.Get: %v", err)
	}
	if saved.Providers["alldebrid"].RemovedAt == nil {
		t.Fatal("RemovedAt not persisted after re-insertion")
	}
}

// The refresh must not drop a placement the strategy removed on purpose;
// otherwise the entry (and its symlinks' target) vanishes on the next sync.
func TestDetectTorrentChangesKeepsSlotFreedPlacement(t *testing.T) {
	m, _, strg := newSlotStrategyManager(t, "remove_after_add")
	freed := &storage.Entry{InfoHash: "aaa1", Name: "freed", Protocol: config.ProtocolTorrent}
	freed.AddTorrentProvider(&types.Torrent{Id: "111", Debrid: "alldebrid"})
	m.applySlotStrategy(freed)
	gone := &storage.Entry{InfoHash: "bbb2", Name: "gone", Protocol: config.ProtocolTorrent}
	gone.AddTorrentProvider(&types.Torrent{Id: "222", Debrid: "alldebrid"})
	if err := strg.AddOrUpdate(gone); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}

	_, updates, deletes, err := m.detectTorrentChanges("alldebrid", map[string]*types.Torrent{})
	if err != nil {
		t.Fatalf("detectTorrentChanges: %v", err)
	}
	if len(updates) != 0 || len(deletes) != 1 || deletes[0] != "bbb2" {
		t.Fatalf("updates = %d, deletes = %v; want only bbb2 deleted", len(updates), deletes)
	}
}
