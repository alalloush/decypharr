package types

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// ProfileCache holds a provider's account profile for a fixed time. Callers
// that miss the cache together share one fetch, and each caller gets its own
// copy, so nobody can change the cached profile. The zero value is ready to
// use.
type ProfileCache struct {
	mu      sync.Mutex
	profile *Profile
	fetched time.Time
	flight  singleflight.Group
}

// Get returns the cached profile while it is younger than ttl. Otherwise it
// calls fetch, once for all concurrent callers, and caches the result. A
// failed fetch is not cached.
func (c *ProfileCache) Get(ttl time.Duration, fetch func() (*Profile, error)) (*Profile, error) {
	if profile, ok := c.fresh(ttl); ok {
		return profile, nil
	}
	v, err, _ := c.flight.Do("profile", func() (any, error) {
		// A flight that finished after this caller's miss has filled the
		// cache already.
		if profile, ok := c.fresh(ttl); ok {
			return profile, nil
		}
		profile, err := fetch()
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.profile, c.fetched = profile, time.Now()
		c.mu.Unlock()
		return profile, nil
	})
	if err != nil {
		return nil, err
	}
	profile := *v.(*Profile)
	return &profile, nil
}

func (c *ProfileCache) fresh(ttl time.Duration) (*Profile, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.profile == nil || time.Since(c.fetched) >= ttl {
		return nil, false
	}
	profile := *c.profile
	return &profile, true
}
