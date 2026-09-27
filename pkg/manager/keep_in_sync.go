package manager

import (
	"errors"
	"path/filepath"
	"slices"

	"github.com/sirrobot01/appendstore"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// keep_in_sync (per debrid) adopts completed torrents that are already on the
// provider but that decypharr did not add, typically torrents added through
// Debrid Media Manager. The torrent refresh already stores every provider
// torrent as an entry the mount serves; adoption adds the download-queue row
// those torrents lack, so they show up in the dashboard and the qBittorrent
// API as completed downloads in keepInSyncCategory.
//
// Adoption is bookkeeping only. It reuses what the refresh just listed and
// stored and makes no provider calls. It runs no post-download action:
// nothing is downloaded, symlinked or cache-warmed, and no notification is
// sent.
//
// The state lives in both stores, so it survives restarts:
//   - A stored entry with a category is owned, by an Arr import or by an
//     earlier adoption. Owned entries are never adopted, so deleting an
//     adopted row in the dashboard is final.
//   - The queue row carries keepInSyncTag. An Arr or API import of the same
//     hash replaces the row (see AddNewTorrent), and the row is dropped once
//     the torrent is gone from every provider.
//
// A hash held by several providers is a single entry, so it gets one row.
// keepInSyncMu stops concurrent provider refreshes from adopting it twice.
const (
	keepInSyncCategory = "other"
	keepInSyncTag      = "keep_in_sync"
)

// isKeepInSyncEntry reports whether keep_in_sync created this queue row.
func isKeepInSyncEntry(entry *storage.Entry) bool {
	return slices.Contains(entry.Tags, keepInSyncTag)
}

// keepInSync adopts the unowned, completed torrents from one provider's
// refresh. listed holds what the provider returned in that refresh.
func (m *Manager) keepInSync(provider string, listed map[string]*types.Torrent) {
	m.keepInSyncMu.Lock()
	defer m.keepInSyncMu.Unlock()

	// The metadata scan is in memory. After the first pass nearly every
	// listed torrent has a category, so few entries are read in full.
	var candidates []string
	if err := m.storage.ForEachMeta(func(meta *storage.EntryMetaInfo) error {
		if meta.Category != "" {
			return nil
		}
		if _, ok := listed[meta.InfoHash]; ok {
			candidates = append(candidates, meta.InfoHash)
		}
		return nil
	}); err != nil {
		m.logger.Error().Err(err).Str("debrid", provider).Msg("keep_in_sync: failed to scan entries")
		return
	}

	adopted := 0
	for _, infohash := range candidates {
		ok, err := m.adoptEntry(provider, infohash)
		if err != nil {
			m.logger.Error().Err(err).Str("debrid", provider).Str("infohash", infohash).Msg("keep_in_sync: failed to adopt torrent")
			continue
		}
		if ok {
			adopted++
		}
	}
	if adopted > 0 {
		m.logger.Info().Str("debrid", provider).Int("count", adopted).Msgf("Adopted provider torrents into category %q", keepInSyncCategory)
	}
}

// adoptEntry adopts one stored entry and reports whether it added a queue row.
func (m *Manager) adoptEntry(provider, infohash string) (bool, error) {
	entry, err := m.storage.Get(infohash)
	if errors.Is(err, appendstore.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if entry.Category != "" || !keepInSyncAdoptable(entry, provider) {
		return false, nil
	}

	queued, err := m.queue.GetTorrent(infohash)
	switch {
	case err == nil && !isKeepInSyncEntry(queued):
		// An Arr or API import of this hash is in flight. Its completion
		// gives the entry a category.
		return false, nil
	case err == nil:
		// Adopted earlier, but the category never reached the entry: a
		// crash between the two writes below, or a concurrent refresh
		// wrote back an older copy.
		entry.Category = queued.Category
		entry.SavePath = queued.SavePath
		return false, m.storage.AddOrUpdate(entry)
	case !errors.Is(err, appendstore.ErrKeyNotFound):
		return false, err
	}

	row := *entry
	row.Category = keepInSyncCategory
	row.SavePath = filepath.Join(m.config.DownloadFolder, keepInSyncCategory)
	row.Action = config.DownloadActionNone
	row.Status = types.TorrentStatusDownloaded
	row.Tags = append(slices.Clone(entry.Tags), keepInSyncTag)
	row.MarkAsCompleted(row.DownloadPath())
	// Update, not Add: the row keeps the provider's add time. The row goes
	// first because the next pass repairs a missing category (see above),
	// while a category without a row would read as a deleted adoption.
	if err := m.queue.Update(&row); err != nil {
		return false, err
	}
	entry.Category = row.Category
	entry.SavePath = row.SavePath
	return true, m.storage.AddOrUpdate(entry)
}

// keepInSyncAdoptable reports whether entry is a finished torrent on provider
// that the mount can serve.
func keepInSyncAdoptable(entry *storage.Entry, provider string) bool {
	if entry.IsNZB() || entry.Bad {
		return false
	}
	placement := entry.Providers[provider]
	if placement == nil || placement.Status != types.TorrentStatusDownloaded || len(placement.Files) == 0 {
		return false
	}
	return entry.Validate() == nil
}

// dropKeepInSyncEntry removes the adopted queue row of a torrent that is gone
// from every provider. Rows that belong to an Arr stay; the Arr removes them.
func (m *Manager) dropKeepInSyncEntry(infohash string) {
	m.keepInSyncMu.Lock()
	defer m.keepInSyncMu.Unlock()

	queued, err := m.queue.GetTorrent(infohash)
	if err != nil || !isKeepInSyncEntry(queued) {
		return
	}
	if err := m.queue.Delete(infohash, false, nil); err != nil {
		m.logger.Error().Err(err).Str("infohash", infohash).Msg("keep_in_sync: failed to remove the row of a deleted torrent")
	}
}
