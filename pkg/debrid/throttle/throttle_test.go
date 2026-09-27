package throttle

import (
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/ratelimit"

	"github.com/sirrobot01/decypharr/internal/config"
)

// fakeClock is a clock for one goroutine: Sleep moves time forward.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) {
	if d > 0 {
		c.now = c.now.Add(d)
	}
}

// busiestWindow returns the most permits issued in any window [t, t+window).
func busiestWindow(times []time.Time, window time.Duration) int {
	most, start := 0, 0
	for end := range times {
		for !times[start].Add(window).After(times[end]) {
			start++
		}
		most = max(most, end-start+1)
	}
	return most
}

// drain takes permits from the limiters in turn, as callers that always have
// work would, for span of fake time. Every tenth of the span it idles for ten
// minutes, so bursts refill. It returns each limiter's permit times.
func drain(clk *fakeClock, span time.Duration, limiters []ratelimit.Limiter) [][]time.Time {
	times := make([][]time.Time, len(limiters))
	end := clk.now.Add(span)
	nextIdle := clk.now.Add(span / 10)
	for i := 0; clk.now.Before(end); i++ {
		if !clk.now.Before(nextIdle) {
			clk.now = clk.now.Add(10 * time.Minute)
			end = end.Add(10 * time.Minute)
			nextIdle = clk.now.Add(span / 10)
		}
		n := i % len(limiters)
		limiters[n].Take()
		times[n] = append(times[n], clk.now)
	}
	return times
}

func merge(groups ...[]time.Time) []time.Time {
	all := slices.Concat(groups...)
	slices.SortFunc(all, time.Time.Compare)
	return all
}

// Real-Debrid allows 250 requests per minute. Whatever the entry's
// rate_limit says, the API, repair and every download key of every
// Real-Debrid entry together stay under that in any 60-second window.
func TestRealDebridStaysUnderDocumentedLimit(t *testing.T) {
	for _, rateLimit := range []string{"", "250/minute", "1000/minute", "not a rate"} {
		t.Run(rateLimit, func(t *testing.T) {
			clk := &fakeClock{now: time.Unix(1_000_000, 0)}
			host := "https://rd.test/" + t.Name()
			var limiters []ratelimit.Limiter
			for _, name := range []string{"rd-main", "rd-second"} {
				lanes := forDebrid(config.Debrid{
					Name:            name,
					Provider:        "realdebrid",
					APIKey:          name + "-key",
					DownloadAPIKeys: []string{name + "-key", name + "-extra"},
					RateLimit:       rateLimit,
					APIHost:         host,
				}, zerolog.Nop(), clk)
				limiters = append(limiters, lanes.API, lanes.Repair, lanes.Download(name+"-key"), lanes.Download(name+"-extra"))
			}

			all := merge(drain(clk, time.Hour, limiters)...)
			if got := busiestWindow(all, time.Minute); got > 240 {
				t.Fatalf("busiest minute had %d requests, want at most 240, under Real-Debrid's 250", got)
			}
			// The budget is still usable: 240 per minute less the burst.
			if perMinute := float64(len(all)) / time.Hour.Minutes(); perMinute < 200 {
				t.Fatalf("%.0f requests per busy minute, want the budget used (about 216)", perMinute)
			}
		})
	}
}

// TorBox allows 300 requests per minute per API key. Calls on the main key
// share one budget; a second download key has its own.
func TestTorBoxStaysUnderDocumentedLimitPerKey(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_000_000, 0)}
	lanes := forDebrid(config.Debrid{
		Name:            "tb",
		Provider:        "torbox",
		APIKey:          "main-key",
		DownloadAPIKeys: []string{"main-key", "other-key"},
		RateLimit:       "300/minute",
		APIHost:         "https://tb.test/" + t.Name(),
	}, zerolog.Nop(), clk)

	other := lanes.Download("other-key")
	times := drain(clk, time.Hour, []ratelimit.Limiter{lanes.API, lanes.Submit, lanes.Download("main-key"), other})
	for key, permits := range map[string][]time.Time{
		"main-key":  merge(times[0], times[1], times[2]),
		"other-key": times[3],
	} {
		if got := busiestWindow(permits, time.Minute); got > 288 {
			t.Errorf("%s: busiest minute had %d requests, want at most 288, under TorBox's 300", key, got)
		}
	}

	// The main key's budget is spent, so its next permit lies ahead. The
	// other key has used a quarter of its own and must not wait.
	before := clk.now
	other.Take()
	if waited := clk.now.Sub(before); waited > 0 {
		t.Fatalf("other-key waited %v behind main-key's traffic; TorBox counts each API key separately", waited)
	}
}

// For a provider without a documented limit, rate_limit is the ceiling of all
// lanes together, and the per-lane limits only lower it.
func TestRateLimitIsSharedByAllLanes(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_000_000, 0)}
	lanes := forDebrid(config.Debrid{
		Name:              "ad",
		Provider:          "alldebrid",
		APIKey:            "key",
		DownloadAPIKeys:   []string{"key", "extra"},
		RateLimit:         "100/minute",
		RepairRateLimit:   "20/minute",
		DownloadRateLimit: "100/minute",
	}, zerolog.Nop(), clk)

	times := drain(clk, time.Hour, []ratelimit.Limiter{lanes.API, lanes.Repair, lanes.Download("key"), lanes.Download("extra")})
	if got := busiestWindow(merge(times...), time.Minute); got > 100 {
		t.Fatalf("busiest minute had %d requests, want at most rate_limit's 100", got)
	}
	// A lane limit sits in front of the shared ones, whose waits can shift
	// its permits, so check its average rate.
	if perMinute := float64(len(times[1])) / time.Hour.Minutes(); perMinute > 20 {
		t.Fatalf("%.1f repair requests per busy minute, want at most repair_rate_limit's 20", perMinute)
	}
}

func TestParse(t *testing.T) {
	for in, want := range map[string]Limit{
		"250/minute":  {250, time.Minute},
		" 10 / Secs ": {10, time.Second},
		"60/hr":       {60, time.Hour},
		"1000/d":      {1000, 24 * time.Hour},
	} {
		if got, ok := Parse(in); !ok || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "250", "0/minute", "-1/minute", "ten/minute", "5/fortnight"} {
		if got, ok := Parse(in); ok {
			t.Errorf("Parse(%q) = %v, want an error", in, got)
		}
	}
}
