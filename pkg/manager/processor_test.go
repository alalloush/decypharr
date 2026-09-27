package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/arr"
	debrid "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/notifications"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// newActionTestManager wires a Manager with real storage and queue, no mount
// and no provider clients, enough to run post-download actions end to end.
func newActionTestManager(t *testing.T) *Manager {
	t.Helper()
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	cfg := config.Get()
	cfg.Mount.MountPath = t.TempDir()
	cfg.SkipPreCache = true
	store, err := storage.NewStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	m := &Manager{
		storage: store, config: cfg, ctx: ctx, cancelDownloads: cancel,
		queue: newQueue(store, ""), arr: arr.New(), logger: zerolog.Nop(),
		clients: xsync.NewMap[string, debrid.Client](), processingEntries: xsync.NewMap[string, struct{}](),
		Notifications: notifications.New(&cfg.Notifications, zerolog.Nop()),
	}
	m.initEntryCache()
	m.downloader = NewDownloadManager(m)
	t.Cleanup(func() {
		_ = m.Stop()
		_ = store.Close()
	})
	return m
}

// newQueuedSymlinkEntry returns a symlink-action entry submitted to
// "provider" while the provider had not listed any files yet (an uncached or
// slot-queued grab).
func newQueuedSymlinkEntry(t *testing.T) *storage.Entry {
	t.Helper()
	entry := &storage.Entry{
		InfoHash: "0123456789012345678901234567890123456789", Name: "Show.S01E01",
		Protocol: config.ProtocolTorrent, State: storage.EntryStateDownloading,
		Status: types.TorrentStatusDownloading, Action: config.DownloadActionSymlink,
		SkipMultiSeason: true, SavePath: t.TempDir(),
		Files: map[string]*storage.File{}, Providers: map[string]*storage.ProviderEntry{},
	}
	applyDebridTorrentToEntry(entry, &types.Torrent{
		Id: "provider-id", Debrid: "provider", InfoHash: entry.InfoHash, Name: entry.Name,
		Status: types.TorrentStatusDownloading, Files: map[string]types.File{},
	})
	return entry
}

// completedTorrent is the provider's view once the download finished.
func completedTorrent(entry *storage.Entry, fileName string) *types.Torrent {
	return &types.Torrent{
		Id: "provider-id", Debrid: "provider", InfoHash: entry.InfoHash, Name: entry.Name,
		Status: types.TorrentStatusDownloaded, Progress: 100,
		Files: map[string]types.File{fileName: {Name: fileName, Id: "1", Size: 5, Link: "https://provider.invalid/1"}},
	}
}

// placeOnMount makes the file visible under the entry's mount folder.
func placeOnMount(t *testing.T, m *Manager, entry *storage.Entry, fileName string) string {
	t.Helper()
	mountPath := m.GetTorrentMountPath(entry)
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(mountPath, fileName)
	if err := os.WriteFile(target, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

// A grab that was still downloading when it was submitted has no file list.
// When the provider reports it finished, the files it lists must be linked;
// before, the action ran on the empty list and completed the entry with an
// empty folder for the *arr to import.
func TestQueuedDownloadLinksFilesListedAtCompletion(t *testing.T) {
	m := newActionTestManager(t)
	entry := newQueuedSymlinkEntry(t)
	const fileName = "Show.S01E01.mkv"
	m.clients.Store("provider", completedTorrentProvider{torrent: completedTorrent(entry, fileName)})
	if err := m.queue.Add(entry); err != nil {
		t.Fatal(err)
	}
	target := placeOnMount(t, m, entry, fileName)

	m.processQueuedTorrent(entry)

	saved, err := m.queue.GetTorrent(entry.InfoHash)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.IsComplete {
		t.Fatalf("entry not completed: state %q, error %q", saved.State, saved.LastError)
	}
	got, err := os.Readlink(filepath.Join(saved.DownloadPath(), fileName))
	if err != nil || got != target {
		t.Fatalf("symlink = %q, %v; want %q", got, err, target)
	}
	if placement := saved.GetActiveProvider(); placement == nil || placement.Files[fileName] == nil || placement.Files[fileName].Link == "" {
		t.Fatalf("provider file link not recorded: %#v", placement)
	}
}
