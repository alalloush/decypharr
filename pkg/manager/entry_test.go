package manager

import (
	"path/filepath"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// TestEntryListingsSkipInternalMetadata pins that the storage's internal
// migration record, which has no name, never reaches a directory listing. An
// empty directory entry fails the whole FUSE readdir with an I/O error.
func TestEntryListingsSkipInternalMetadata(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	store, err := storage.NewStorage(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.AddOrUpdate(&storage.Entry{InfoHash: "abc123", Name: "Example", Protocol: config.ProtocolTorrent}); err != nil {
		t.Fatalf("AddOrUpdate: %v", err)
	}
	if err := store.SaveMigrationStatus(&storage.SystemMigrationStatus{}); err != nil {
		t.Fatalf("SaveMigrationStatus: %v", err)
	}
	m := &Manager{storage: store}
	everything := config.VirtualFolder{Name: "Everything"}

	_, all := m.getEntryChildren(EntryAllFolder)
	listings := map[string][]FileInfo{
		EntryAllFolder:  all,
		everything.Name: m.getVirtualFolderChildren(mustCompileVirtualFolders(t, everything), everything.Name),
	}
	for folder, infos := range listings {
		if len(infos) != 1 || infos[0].name != "Example" {
			t.Errorf("%s listing = %+v, want only Example", folder, infos)
		}
	}
	if total, _, err := m.PreviewVirtualFolder(everything, 5); err != nil || total != 1 {
		t.Errorf("PreviewVirtualFolder total = %d, err = %v; want 1 entry", total, err)
	}
}

func TestIsListableEntryMetaSkipsInternalAndInvalidNames(t *testing.T) {
	tests := []struct {
		name string
		meta *storage.EntryMetaInfo
		want bool
	}{
		{
			name: "valid entry",
			meta: &storage.EntryMetaInfo{InfoHash: "abc123", Name: "Example"},
			want: true,
		},
		{
			name: "migration status",
			meta: &storage.EntryMetaInfo{InfoHash: "__migration_status__", Name: ""},
			want: false,
		},
		{
			name: "empty name",
			meta: &storage.EntryMetaInfo{InfoHash: "abc123", Name: ""},
			want: false,
		},
		{
			name: "dot name",
			meta: &storage.EntryMetaInfo{InfoHash: "abc123", Name: "."},
			want: false,
		},
		{
			name: "slash name",
			meta: &storage.EntryMetaInfo{InfoHash: "abc123", Name: "bad/name"},
			want: false,
		},
		{
			name: "nul name",
			meta: &storage.EntryMetaInfo{InfoHash: "abc123", Name: "bad\x00name"},
			want: false,
		},
		{
			name: "nil meta",
			meta: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isListableEntryMeta(tt.meta); got != tt.want {
				t.Fatalf("isListableEntryMeta() = %v, want %v", got, tt.want)
			}
		})
	}
}
