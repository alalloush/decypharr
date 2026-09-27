package cgofuse

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	internalconfig "github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
	mountconfig "github.com/sirrobot01/decypharr/pkg/mount/dfs/config"
	"github.com/sirrobot01/decypharr/pkg/mount/dfs/vfs"
	"github.com/sirrobot01/decypharr/pkg/mount/dfs/vfs/ranges"
	"github.com/sirrobot01/decypharr/pkg/storage"
	"github.com/winfsp/cgofuse/fuse"
)

// A read that returns some bytes and then fails must fail as a whole. The
// kernel and players take a short successful read for the end of the file.
func TestReadFailsInsteadOfReturningShortRead(t *testing.T) {
	const (
		size      = int64(2 << 20)
		entryName = "Release"
		fileName  = "movie.mkv"
		off       = int64(1 << 20)
	)
	internalconfig.SetConfigPath(t.TempDir())
	internalconfig.Reset()
	mgr := manager.New()
	t.Cleanup(func() {
		if err := mgr.Stop(); err != nil {
			t.Errorf("stop manager: %v", err)
		}
		internalconfig.Reset()
	})
	entry := &storage.Entry{
		Protocol: internalconfig.ProtocolTorrent,
		InfoHash: "hash",
		Name:     entryName,
		Size:     size,
		Files:    map[string]*storage.File{fileName: {Name: fileName, Size: size, InfoHash: "hash"}},
	}
	if err := mgr.Storage().AddOrUpdate(entry); err != nil {
		t.Fatal(err)
	}
	info, err := mgr.GetTorrentFile(entryName, fileName)
	if err != nil {
		t.Fatal(err)
	}

	// Cache metadata claims the whole file is on disk.
	cacheDir := t.TempDir()
	dataPath := filepath.Join(cacheDir, entryName, fileName)
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	meta, err := json.Marshal(vfs.ItemInfo{Size: size, Rs: ranges.Ranges{{Pos: 0, Size: size}}, ModTime: now, ATime: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath+".json", meta, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := mountconfig.DefaultFuseConfig()
	cfg.CacheDir = cacheDir
	cfg.CacheCleanupInterval = time.Hour
	cache, err := vfs.NewCache(context.Background(), mgr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	item, err := cache.GetItem(entryName, fileName, size)
	if err != nil {
		t.Fatal(err)
	}
	// The data file now ends 64 KiB into the read, so the cache returns 64 KiB
	// and io.ErrUnexpectedEOF.
	if err := os.Truncate(dataPath, off+64<<10); err != nil {
		t.Fatal(err)
	}
	reader := vfs.NewStreamingFile(item)
	if reader == nil {
		t.Fatal("NewStreamingFile returned nil")
	}
	t.Cleanup(func() { _ = reader.Close() })

	fs := &FS{handles: NewHandleManager(), logger: zerolog.Nop()}
	fh := fs.handles.Create(info, reader)
	if got := fs.Read("/"+entryName+"/"+fileName, make([]byte, 128<<10), off, fh); got != -fuse.EIO {
		t.Fatalf("Read() = %d, want -EIO (%d)", got, -fuse.EIO)
	}
}
