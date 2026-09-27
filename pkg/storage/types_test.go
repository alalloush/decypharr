package storage

import (
	"os"
	"strings"
	"testing"
)

func TestGetFirstFileReturnsOnlyActiveFiles(t *testing.T) {
	active := &File{Name: "active"}
	item := &EntryItem{Files: map[string]*File{"deleted": {Deleted: true}, "nil": nil}}
	if file, err := item.GetFirstFile(); file != nil || err == nil {
		t.Fatalf("no active files: got %v, %v", file, err)
	}
	item.Files["active"] = active
	if file, err := item.GetFirstFile(); file != active || err != nil {
		t.Fatalf("active file: got %v, %v", file, err)
	}
}

// A release name in a two-byte script easily passes the 255-byte NAME_MAX,
// and the symlink folder under the category must still be creatable.
func TestDownloadPathFitsNameMax(t *testing.T) {
	entry := &Entry{
		SavePath: t.TempDir(),
		Name:     strings.Repeat("Перси Джексон и Олимпийцы - ", 6) + "Сезон 2 - Серии 1-4 из 8 [2025, WEB-DL 1080p].mkv",
	}
	if err := os.MkdirAll(entry.DownloadPath(), 0o755); err != nil {
		t.Fatal(err)
	}
}
