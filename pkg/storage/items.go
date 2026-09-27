package storage

import (
	"errors"
	"fmt"
	"github.com/sirrobot01/appendstore"
	"google.golang.org/protobuf/proto"
	"slices"
)

// GetEntryItems returns all entry item names
func (s *Storage) GetEntryItems() map[string]struct{} {
	items := make(map[string]struct{})
	_ = s.entryItems.ForEachMetadata(func(key string, meta *appendstore.Metadata) error {
		items[key] = struct{}{}
		return nil
	})
	return items
}

// UpdateEntryItem updates an entry item from an entry
func (s *Storage) UpdateEntryItem(entry *Entry) error {
	return s.updateEntryItem(entry)
}

func (s *Storage) UpdateItem(item *EntryItem) error {
	var oldFingerprint string
	existing, err := s.GetEntryItem(item.Name)
	if err != nil && !errors.Is(err, appendstore.ErrKeyNotFound) {
		return fmt.Errorf("read name index %q: %w", item.Name, err)
	}
	oldFingerprint = EntryItemRepairFingerprint(existing)

	pb := EntryItemToProto(item)
	data, err := proto.Marshal(pb)
	if err != nil {
		return fmt.Errorf("encode name index %q: %w", item.Name, err)
	}
	if oldFingerprint != EntryItemRepairFingerprint(item) {
		if err := s.MarkEntryDirty(item.Name, "", "entry_item_changed"); err != nil {
			return err
		}
	}
	if err := s.entryItems.Put(item.Name, data, nil); err != nil {
		return fmt.Errorf("save name index %q: %w", item.Name, err)
	}
	return nil
}

// GetEntryItem retrieves an entry item by name
func (s *Storage) GetEntryItem(name string) (*EntryItem, error) {
	data, err := s.entryItems.Get(name)
	if err != nil {
		return nil, err
	}

	var pb EntryItemProto
	if err := proto.Unmarshal(data, &pb); err != nil {
		return nil, err
	}
	return ProtoToEntryItem(&pb), nil
}

// ForEachEntryItem iterates over entry items
func (s *Storage) ForEachEntryItem(fn func(*EntryItem) error) error {
	return s.entryItems.ForEach(func(key string, value []byte) error {
		var pb EntryItemProto
		if proto.Unmarshal(value, &pb) != nil {
			return nil
		}
		return fn(ProtoToEntryItem(&pb))
	})
}

// updateEntryItem updates the name index.
func (s *Storage) updateEntryItem(entry *Entry) error {
	name := entry.GetFolder()
	if name == "" {
		return nil
	}
	item, err := s.GetEntryItem(name)
	if err != nil && !errors.Is(err, appendstore.ErrKeyNotFound) {
		return fmt.Errorf("read name index %q: %w", name, err)
	}
	oldFingerprint := EntryItemRepairFingerprint(item)
	if item == nil {
		item = &EntryItem{Name: name, Files: make(map[string]*File)}
	}
	for fileName, file := range entry.Files {
		mergeFileIntoItem(item, fileName, file)
	}
	item.Size = item.GetSize()
	data, err := proto.Marshal(EntryItemToProto(item))
	if err != nil {
		return fmt.Errorf("encode name index %q: %w", name, err)
	}
	if oldFingerprint != EntryItemRepairFingerprint(item) {
		if err := s.MarkEntryDirty(name, entry.Protocol, "entry_item_changed"); err != nil {
			return err
		}
	}
	if err := s.entryItems.Put(name, data, nil); err != nil {
		return fmt.Errorf("save name index %q: %w", name, err)
	}
	return nil
}

// removeFromEntryItem removes an entry from the name index.
//
// Several entries can share one folder name. A provider that re-keys the same
// release produces exactly that: a grab stores the entry under the magnet
// infohash, while a later sync of the same cloud transfer stores it under a
// different key (Premiumize has no infohash in transfer/list, so sync derives a
// synthetic one). Both entries render the same folder, and the name index holds
// one file record per filename, tagged with whichever entry wrote it last.
//
// Deleting one of those entries must therefore not take the folder down with
// it: after dropping the dying entry's own file records, the index is rebuilt
// from the entries that are still live, and it is only removed when no entry
// maps to the folder any more. Without the rebuild the folder stays listed
// (listings come from entry metadata) while serving no files at all.
func (s *Storage) removeFromEntryItem(entry *Entry) error {
	name := entry.GetFolder()
	if name == "" {
		return nil
	}
	item, err := s.GetEntryItem(name)
	if errors.Is(err, appendstore.ErrKeyNotFound) {
		return s.DeleteEntryHealth(name)
	}
	if err != nil {
		return fmt.Errorf("read name index %q before deletion: %w", name, err)
	}
	for fileName := range entry.Files {
		if f, exists := item.Files[fileName]; exists && f.InfoHash == entry.InfoHash {
			delete(item.Files, fileName)
		}
	}
	s.rebuildEntryItemFiles(item, name, entry.InfoHash)
	if len(item.Files) == 0 {
		if err := s.DeleteEntryHealth(name); err != nil {
			return err
		}
		if err := s.entryItems.Delete(name); err != nil {
			return fmt.Errorf("delete name index %q: %w", name, err)
		}
		return nil
	}
	item.Size = item.GetSize()
	return s.UpdateItem(item)
}

// mergeFileIntoItem adds a file record to the name index, keeping the newest
// record when several entries hold the same filename. Same rule as
// updateEntryItem, so a rebuild lands on the same result as a normal write.
func mergeFileIntoItem(item *EntryItem, fileName string, file *File) {
	if item.Files == nil {
		item.Files = make(map[string]*File)
	}
	existing, ok := item.Files[fileName]
	if !ok || file.AddedOn.After(existing.AddedOn) ||
		(file.AddedOn.Equal(existing.AddedOn) && file.Size != existing.Size) {
		item.Files[fileName] = file
	}
}

// entriesByFolder returns the infohashes of every live entry that renders the
// given folder name, skipping skipInfoHash. The folder name is an indexed
// attribute, so this is a map lookup: no disk reads and no library scan, which
// keeps a deletion O(1) however large the library is.
func (s *Storage) entriesByFolder(name, skipInfoHash string) []string {
	return slices.DeleteFunc(s.entries.KeysBy(attributeName, name), func(infoHash string) bool {
		return infoHash == skipInfoHash
	})
}

// rebuildEntryItemFiles merges back the files of every live entry that shares
// the folder name, so an index left short by a deletion still serves the
// folders its remaining entries provide. An entry that cannot be read is
// skipped: it is either being deleted concurrently or already unreadable, and
// either way it has nothing to serve.
func (s *Storage) rebuildEntryItemFiles(item *EntryItem, name, skipInfoHash string) {
	for _, infoHash := range s.entriesByFolder(name, skipInfoHash) {
		entry, err := s.Get(infoHash)
		if err != nil {
			continue
		}
		for fileName, file := range entry.Files {
			mergeFileIntoItem(item, fileName, file)
		}
	}
}

// ReconcileEntryItems rebuilds name index records that went missing while the
// entry behind them stayed live. Such a folder is listed but serves nothing,
// and nothing repairs it on its own, because the index is only written when an
// entry is written. Returns how many folders were rebuilt.
func (s *Storage) ReconcileEntryItems() (int, error) {
	rebuilt := 0
	for _, name := range s.entries.AttributeValues(attributeName) {
		if name == "" || s.entryItems.Exists(name) {
			continue
		}
		item := &EntryItem{Name: name, Files: make(map[string]*File)}
		s.rebuildEntryItemFiles(item, name, "")
		if len(item.Files) == 0 {
			continue
		}
		item.Size = item.GetSize()
		if err := s.UpdateItem(item); err != nil {
			return rebuilt, fmt.Errorf("rebuild name index %q: %w", name, err)
		}
		rebuilt++
	}
	return rebuilt, nil
}
