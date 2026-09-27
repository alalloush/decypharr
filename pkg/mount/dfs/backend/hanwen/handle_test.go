//go:build linux || (darwin && amd64)

package hanwen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	internalconfig "github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
	mountconfig "github.com/sirrobot01/decypharr/pkg/mount/dfs/config"
	"github.com/sirrobot01/decypharr/pkg/mount/dfs/vfs"
	"github.com/sirrobot01/decypharr/pkg/mount/dfs/vfs/ranges"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

const (
	cachedEntryName = "Release"
	cachedFileName  = "movie.mkv"
)

// stallingOrigin is a DFS backend whose upstream never delivers. Like the
// manager's session, opening is lazy; reads block until the downloader
// cancels them.
type stallingOrigin struct{ entry *storage.Entry }

func (o stallingOrigin) GetEntryByName(string, string) (*storage.Entry, error) {
	return o.entry, nil
}
func (stallingOrigin) TrackStream(*storage.Entry, string, string) string { return "stream" }
func (stallingOrigin) UntrackStream(string)                              {}
func (stallingOrigin) OpenStreamUntrackedForCache(ctx context.Context, _ *storage.Entry, _ string, _ int64) (manager.StreamReader, error) {
	return stallingStream{ctx}, nil
}

type stallingStream struct{ ctx context.Context }

func (s stallingStream) Read([]byte) (int, error) {
	<-s.ctx.Done()
	return 0, s.ctx.Err()
}
func (stallingStream) Seek(off int64, _ int) (int64, error) { return off, nil }
func (stallingStream) Close() error                         { return nil }
func (stallingStream) Size() int64                          { return 0 }
func (stallingStream) Prime() error                         { return nil }

// openCachedFile opens a DFS file of size bytes whose cache metadata says
// [0, cached) is on disk. After the item opens, its data file is cut to
// onDisk bytes, which is what a cache file truncated under an open item looks
// like.
func openCachedFile(t *testing.T, size, cached, onDisk int64) *vfs.StreamingFile {
	t.Helper()
	// The cache's loggers and buffer pool read the global config.
	internalconfig.SetConfigPath(t.TempDir())
	internalconfig.Reset()
	t.Cleanup(internalconfig.Reset)
	cacheDir := t.TempDir()
	entryDir := filepath.Join(cacheDir, cachedEntryName)
	dataPath := filepath.Join(entryDir, cachedFileName)
	if err := os.MkdirAll(entryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*31 + 7)
	}
	if err := os.WriteFile(dataPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if cached > 0 {
		now := time.Now()
		meta, err := json.Marshal(vfs.ItemInfo{Size: size, Rs: ranges.Ranges{{Pos: 0, Size: cached}}, ModTime: now, ATime: now})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dataPath+".json", meta, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := mountconfig.DefaultFuseConfig()
	cfg.CacheDir = cacheDir
	cfg.CacheCleanupInterval = time.Hour
	entry := &storage.Entry{
		Name:     cachedEntryName,
		InfoHash: "hash",
		Files:    map[string]*storage.File{cachedFileName: {Name: cachedFileName, Size: size}},
	}
	cache, err := vfs.NewCache(context.Background(), stallingOrigin{entry: entry}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	item, err := cache.GetItem(cachedEntryName, cachedFileName, size)
	if err != nil {
		t.Fatal(err)
	}
	// The buffer extends its data file to the full size when it opens it.
	if err := os.Truncate(dataPath, onDisk); err != nil {
		t.Fatal(err)
	}
	file := vfs.NewStreamingFile(item)
	if file == nil {
		t.Fatal("NewStreamingFile returned nil")
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

// A read that cannot be completed must fail. A short successful read is what
// the kernel and players take for the end of the file, so playback or a scan
// would stop early without an error.
func TestHandleReadNeverEndsFileEarly(t *testing.T) {
	const size = int64(2 << 20)

	t.Run("cache file shorter than its ranges", func(t *testing.T) {
		fh := &Handle{streamFile: openCachedFile(t, size, size, 1<<20+64<<10)}
		res, errno := fh.Read(context.Background(), make([]byte, 128<<10), 1<<20)
		if errno != syscall.EIO {
			t.Fatalf("Read() = %d bytes, errno %v; want EIO", resultSize(res), errno)
		}
	})

	t.Run("interrupted while downloading", func(t *testing.T) {
		fh := &Handle{streamFile: openCachedFile(t, size, 0, size)}
		interrupt := make(chan struct{})
		close(interrupt)
		res, errno := fh.Read(&fuse.Context{Cancel: interrupt}, make([]byte, 128<<10), 0)
		if errno != syscall.EINTR {
			t.Fatalf("Read() = %d bytes, errno %v; want EINTR", resultSize(res), errno)
		}
	})

	t.Run("read across the end of the file", func(t *testing.T) {
		fh := &Handle{streamFile: openCachedFile(t, size, size, size)}
		res, errno := fh.Read(context.Background(), make([]byte, 4096), size-100)
		if errno != 0 || resultSize(res) != 100 {
			t.Fatalf("Read() = %d bytes, errno %v; want the last 100 bytes", resultSize(res), errno)
		}
	})
}

func resultSize(res fuse.ReadResult) int {
	if res == nil {
		return 0
	}
	return res.Size()
}
