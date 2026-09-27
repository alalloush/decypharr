package manager

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/sirrobot01/decypharr/internal/config"
	debrid "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// lateFilesProvider lists no files for the first emptyPolls GetTorrent calls,
// the way Premiumize answers right after an instantly cached grab finishes.
type lateFilesProvider struct {
	debrid.Client
	torrent    func() *types.Torrent
	emptyPolls int32
	calls      atomic.Int32
}

func (p *lateFilesProvider) GetTorrent(string) (*types.Torrent, error) {
	t := p.torrent()
	if p.calls.Add(1) <= p.emptyPolls {
		t.Files = map[string]types.File{}
	}
	return t, nil
}

// Config satisfies the post-action slot-strategy lookup; no strategy is set.
func (p *lateFilesProvider) Config() config.Debrid { return config.Debrid{} }

// runFinishedWithoutFiles submits a grab the provider reports finished with
// an empty file list and runs the post-download action to its end.
func runFinishedWithoutFiles(t *testing.T, emptyPolls int32) (*Manager, *storage.Entry, *lateFilesProvider, string) {
	t.Helper()
	const fileName = "Show.S01E01.mkv"
	m := newActionTestManager(t)
	entry := newQueuedSymlinkEntry(t)
	snapshot := *entry
	provider := &lateFilesProvider{
		torrent:    func() *types.Torrent { return completedTorrent(&snapshot, fileName) },
		emptyPolls: emptyPolls,
	}
	m.clients.Store("provider", provider)
	if err := m.queue.Add(entry); err != nil {
		t.Fatal(err)
	}
	target := placeOnMount(t, m, entry, fileName)

	finished := completedTorrent(entry, fileName)
	finished.Files = map[string]types.File{}
	m.processNewTorrent(entry, finished)
	m.downloadTasks.Wait()
	return m, entry, provider, target
}

func TestFinishedTorrentWithoutFilesIsRefreshed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, entry, provider, target := runFinishedWithoutFiles(t, 2)

		saved, err := m.queue.GetTorrent(entry.InfoHash)
		if err != nil {
			t.Fatal(err)
		}
		if !saved.IsComplete {
			t.Fatalf("entry not completed: state %q, error %q", saved.State, saved.LastError)
		}
		got, err := os.Readlink(filepath.Join(saved.DownloadPath(), "Show.S01E01.mkv"))
		if err != nil || got != target {
			t.Fatalf("symlink = %q, %v; want %q", got, err, target)
		}
		if calls := provider.calls.Load(); calls != 3 {
			t.Fatalf("provider polled %d times, want 3", calls)
		}
		// The mount lists what storage holds, so the files must be saved there.
		stored, err := m.storage.Get(entry.InfoHash)
		if err != nil || len(stored.GetActiveFiles()) != 1 {
			t.Fatalf("stored entry files = %v, %v", stored, err)
		}
	})
}

// Files that never show up fail the entry instead of completing it empty.
func TestFinishedTorrentThatNeverListsFilesFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, entry, provider, _ := runFinishedWithoutFiles(t, 1<<30)

		saved, err := m.queue.GetTorrent(entry.InfoHash)
		if err != nil {
			t.Fatal(err)
		}
		if saved.IsComplete || saved.State != storage.EntryStateError {
			t.Fatalf("entry = complete %v, state %q; want an error", saved.IsComplete, saved.State)
		}
		if calls := provider.calls.Load(); calls != noFilesRetryAttempts {
			t.Fatalf("provider polled %d times, want %d", calls, noFilesRetryAttempts)
		}
	})
}
