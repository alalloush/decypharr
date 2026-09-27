package types

import (
	"errors"
	"testing"
	"time"
)

func TestProfileCacheExpiresAndSkipsFailures(t *testing.T) {
	var cache ProfileCache
	fetches := 0
	fail := false
	fetch := func() (*Profile, error) {
		fetches++
		if fail {
			return nil, errors.New("provider down")
		}
		return &Profile{Name: "rd", Username: "al"}, nil
	}

	first, err := cache.Get(time.Hour, fetch)
	if err != nil {
		t.Fatal(err)
	}
	first.Username = "changed by a caller"
	second, err := cache.Get(time.Hour, fetch)
	if err != nil || fetches != 1 {
		t.Fatalf("fresh Get: err %v, fetches %d, want 1", err, fetches)
	}
	if second.Username != "al" {
		t.Fatalf("cached username = %q, want al: a caller changed the cached profile", second.Username)
	}

	// A zero TTL makes the cached profile stale.
	fail = true
	if _, err := cache.Get(0, fetch); err == nil || fetches != 2 {
		t.Fatalf("stale Get with failing fetch: err %v, fetches %d, want error and 2", err, fetches)
	}
	fail = false
	if _, err := cache.Get(0, fetch); err != nil || fetches != 3 {
		t.Fatalf("Get after a failed fetch: err %v, fetches %d, want 3", err, fetches)
	}
}
