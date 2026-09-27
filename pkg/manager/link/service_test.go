package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/customerror"
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
	s := New(clients, nil, nil, nil, func(string) *http.Client { return cdn.Client() }, 0, zerolog.Nop())

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

// gatedClient is a provider whose link fetches block until the test opens
// the gate or the fetch's own context ends.
type gatedClient struct {
	debrid.Client
	cdn       string
	started   chan struct{}
	gate      chan struct{}
	openOnce  sync.Once
	fetches   atomic.Int32
	panicking bool
}

func newGatedService(t *testing.T) (*Service, *gatedClient, *storage.Entry) {
	t.Helper()
	cdn := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(cdn.Close)
	client := &gatedClient{cdn: cdn.URL, started: make(chan struct{}, 8), gate: make(chan struct{})}
	t.Cleanup(client.open) // never leave a fetch parked past the test
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
	return s, client, entry
}

func (c *gatedClient) open() { c.openOnce.Do(func() { close(c.gate) }) }

func (c *gatedClient) GetDownloadLink(ctx context.Context, _ string, file *types.File) (types.DownloadLink, error) {
	n := c.fetches.Add(1)
	if c.panicking {
		panic("provider bug")
	}
	c.started <- struct{}{}
	select {
	case <-c.gate:
	case <-ctx.Done():
		return types.DownloadLink{}, ctx.Err()
	}
	return types.DownloadLink{
		Debrid:       "torbox",
		Filename:     file.Name,
		Link:         file.Link,
		DownloadLink: fmt.Sprintf("%s/dld/%d", c.cdn, n),
	}, nil
}

func (c *gatedClient) DeleteLink(types.DownloadLink) error { return nil }

type linkResult struct {
	link types.DownloadLink
	err  error
}

func getLinkAsync(ctx context.Context, s *Service, entry *storage.Entry) <-chan linkResult {
	done := make(chan linkResult, 1)
	go func() {
		dl, err := s.GetLink(ctx, entry, "Release.mkv")
		done <- linkResult{dl, err}
	}()
	return done
}

func awaitLink(t *testing.T, done <-chan linkResult, what string) linkResult {
	t.Helper()
	select {
	case res := <-done:
		return res
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", what)
		return linkResult{}
	}
}

// The caller that starts a shared fetch may give up (a closed WebDAV request,
// an aborted repair). The DFS downloaders waiting on the same file must still
// get the link rather than that caller's context error.
func TestSharedLinkFetchOutlivesTheCallerThatStartedIt(t *testing.T) {
	s, client, entry := newGatedService(t)

	firstCtx, cancelFirst := context.WithCancel(t.Context())
	first := getLinkAsync(firstCtx, s, entry)
	<-client.started
	second := getLinkAsync(t.Context(), s, entry)
	time.Sleep(50 * time.Millisecond) // let the second caller join the fetch

	cancelFirst()
	if res := awaitLink(t, first, "cancelled caller"); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("cancelled caller got error %v, want context.Canceled", res.err)
	}
	client.open()
	res := awaitLink(t, second, "waiting caller")
	if res.err != nil || res.link.DownloadLink == "" {
		t.Fatalf("waiting caller got error %v, link %q; want the link", res.err, res.link.DownloadLink)
	}
}

// Each caller stops waiting when its own context ends, even while the shared
// fetch is still running for others.
func TestLinkCallerHonoursItsOwnDeadline(t *testing.T) {
	s, client, entry := newGatedService(t)

	first := getLinkAsync(t.Context(), s, entry)
	<-client.started
	shortCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if res := awaitLink(t, getLinkAsync(shortCtx, s, entry), "caller with a deadline"); !errors.Is(res.err, context.DeadlineExceeded) {
		t.Fatalf("caller with a deadline got error %v, want context.DeadlineExceeded", res.err)
	}

	client.open()
	if res := awaitLink(t, first, "first caller"); res.err != nil {
		t.Fatalf("first caller got %v, want the link", res.err)
	}
}

// A detached fetch that hangs must not hold the file forever: the service's
// own timeout ends it, and the next caller starts a fresh fetch.
func TestHungLinkFetchReleasesTheFile(t *testing.T) {
	s, client, entry := newGatedService(t)
	s.fetchTimeout = 100 * time.Millisecond

	if res := awaitLink(t, getLinkAsync(t.Context(), s, entry), "hung fetch"); !errors.Is(res.err, context.DeadlineExceeded) {
		t.Fatalf("hung fetch returned error %v, want context.DeadlineExceeded", res.err)
	}
	client.open()
	if res := awaitLink(t, getLinkAsync(t.Context(), s, entry), "next caller"); res.err != nil {
		t.Fatalf("next caller got %v, want the link", res.err)
	}
	if got := client.fetches.Load(); got != 2 {
		t.Fatalf("provider fetches = %d, want 2", got)
	}
}

// A bug in a provider fails the link fetch; it must not crash the process,
// which would also take the DFS mount down.
func TestLinkFetchPanicBecomesError(t *testing.T) {
	s, client, entry := newGatedService(t)
	client.panicking = true

	res := awaitLink(t, getLinkAsync(t.Context(), s, entry), "panicking fetch")
	if !customerror.IsPanicError(res.err) {
		t.Fatalf("panicking fetch returned error %v, want a panic error", res.err)
	}
}
