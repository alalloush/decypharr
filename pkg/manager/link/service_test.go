package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	debrid "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// cachingClient mimics a provider behind the account link cache: it calls the
// provider only when no link is cached, and DeleteLink drops the cached link.
type cachingClient struct {
	debrid.Client
	cdn     string
	fetches atomic.Int32
	cached  atomic.Pointer[types.DownloadLink]
}

func (c *cachingClient) GetDownloadLink(_ context.Context, _ string, file *types.File) (types.DownloadLink, error) {
	if dl := c.cached.Load(); dl != nil {
		return *dl, nil
	}
	n := c.fetches.Add(1)
	dl := types.DownloadLink{
		Debrid:       "torbox",
		Filename:     file.Name,
		Link:         file.Link,
		DownloadLink: fmt.Sprintf("%s/dld/%d", c.cdn, n),
	}
	c.cached.Store(&dl)
	return dl, nil
}

func (c *cachingClient) DeleteLink(types.DownloadLink) error {
	c.cached.Store(nil)
	return nil
}

// A file whose CDN keeps answering 404 costs one provider refetch per cooldown
// window, not one per read; the first successful validation lifts the cooldown.
func TestRefetchCooldownBoundsProviderCallsForPersistent404(t *testing.T) {
	var available atomic.Bool
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(cdn.Close)

	client := &cachingClient{cdn: cdn.URL}
	clients := xsync.NewMap[string, debrid.Client]()
	clients.Store("torbox", client)
	s := New(clients, nil, nil, nil, cdn.Client(), 0, zerolog.Nop())

	const filename = "Release.mkv"
	entry := &storage.Entry{
		InfoHash:       "hash",
		Name:           "Release",
		ActiveProvider: "torbox",
		Files:          map[string]*storage.File{filename: {Name: filename, Size: 1}},
		Providers: map[string]*storage.ProviderEntry{"torbox": {
			ID:    "7",
			Files: map[string]*storage.ProviderFile{filename: {Id: "3", Link: "torbox://7/3"}},
		}},
	}

	// First failure: one refetch, and the fresh link is handed out unvalidated.
	first, err := s.GetLink(t.Context(), entry, filename)
	if err != nil {
		t.Fatalf("first GetLink() error = %v, want the refetched link", err)
	}
	if got := client.fetches.Load(); got != 2 {
		t.Fatalf("provider fetches after first failure = %d, want 2 (initial + one refetch)", got)
	}

	// Inside the cooldown neither validation nor a mid-stream refresh may call
	// the provider again.
	for range 3 {
		if _, err := s.GetLink(t.Context(), entry, filename); !errors.Is(err, Err404) {
			t.Fatalf("GetLink() during cooldown error = %v, want the 404", err)
		}
	}
	if _, err := s.Refresh(t.Context(), entry, first); !errors.Is(err, ErrRefetchCooldown) {
		t.Fatalf("Refresh() during cooldown error = %v, want ErrRefetchCooldown", err)
	}
	if got := client.fetches.Load(); got != 2 {
		t.Fatalf("provider fetches during cooldown = %d, want still 2", got)
	}

	// Once the file validates again, the cooldown no longer applies.
	available.Store(true)
	if _, err := s.GetLink(t.Context(), entry, filename); err != nil {
		t.Fatalf("GetLink() after recovery error = %v", err)
	}
	if _, err := s.Refresh(t.Context(), entry, first); err != nil {
		t.Fatalf("Refresh() after recovery error = %v, want a refetch", err)
	}
	if got := client.fetches.Load(); got != 3 {
		t.Fatalf("provider fetches after recovery = %d, want 3", got)
	}
}
