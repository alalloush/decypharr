// Package throttle builds the request limiters of the debrid API clients.
//
// Every call one debrid entry makes to its provider's API (listing, info,
// submission, repair probes and link requests on every download key) draws
// from one budget. The budget is the entry's rate_limit, capped just under
// the provider's documented limit for Real-Debrid and TorBox, so the call
// paths cannot add up to more than the provider allows.
package throttle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/ratelimit"

	"github.com/sirrobot01/decypharr/internal/config"
)

// Limit is a request ceiling: at most Count requests start in any window of
// length Per.
type Limit struct {
	Count int
	Per   time.Duration
}

var units = []struct {
	name    string
	aliases []string
	per     time.Duration
}{
	{"second", []string{"second", "sec"}, time.Second},
	{"minute", []string{"minute", "min"}, time.Minute},
	{"hour", []string{"hour", "hr"}, time.Hour},
	{"day", []string{"day", "d"}, 24 * time.Hour},
}

// Parse reads a rate such as "250/minute" or "10/second". The unit is
// second (sec), minute (min), hour (hr) or day (d), singular or plural.
func Parse(s string) (Limit, bool) {
	count, unit, ok := strings.Cut(s, "/")
	if !ok {
		return Limit{}, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(count))
	if err != nil || n <= 0 {
		return Limit{}, false
	}
	unit = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(unit)), "s")
	for _, u := range units {
		for _, alias := range u.aliases {
			if unit == alias {
				return Limit{Count: n, Per: u.per}, true
			}
		}
	}
	return Limit{}, false
}

func (l Limit) String() string {
	for _, u := range units {
		if l.Per == u.per {
			return fmt.Sprintf("%d/%s", l.Count, u.name)
		}
	}
	return fmt.Sprintf("%d/%s", l.Count, l.Per)
}

// slower reports whether l allows fewer requests per unit of time than o.
func (l Limit) slower(o Limit) bool {
	return float64(l.Count)*float64(o.Per) < float64(o.Count)*float64(l.Per)
}

// newLimiter returns a limiter that lets at most l.Count requests start in
// any window of l.Per. After an idle spell a tenth of Count may start at
// once, the rest are spaced evenly. A limiter paced at r per window with a
// burst of b lets r+b requests start in a window that opens after an idle
// spell, so the pace is Count minus the burst. The gap is rounded up to the
// nanosecond: rounded down, r gaps would end just inside the window and let
// one more in. A nil clk is the real clock.
func newLimiter(l Limit, clk ratelimit.Clock) ratelimit.Limiter {
	burst := l.Count / 10
	pace := time.Duration(l.Count - burst)
	gap := (l.Per + pace - 1) / pace
	return ratelimit.New(int(pace), options(clk, ratelimit.Per(gap*pace), ratelimit.WithSlack(burst))...)
}

// newPaced returns a limiter without a burst that spaces requests evenly at
// share times l's rate.
func newPaced(l Limit, share float64, clk ratelimit.Clock) ratelimit.Limiter {
	per := time.Duration(float64(l.Per) / share)
	return ratelimit.New(l.Count, options(clk, ratelimit.Per(per), ratelimit.WithoutSlack)...)
}

func options(clk ratelimit.Clock, opts ...ratelimit.Option) []ratelimit.Option {
	if clk != nil {
		opts = append(opts, ratelimit.WithClock(clk))
	}
	return opts
}

// chain takes a permit from each limiter in turn, so a request waits for the
// strictest of them and counts against all of them.
type chain []ratelimit.Limiter

func (c chain) Take() time.Time {
	var t time.Time
	for _, l := range c {
		t = l.Take()
	}
	return t
}

func newChain(limiters ...ratelimit.Limiter) ratelimit.Limiter {
	var c chain
	for _, l := range limiters {
		if l != nil {
			c = append(c, l)
		}
	}
	switch len(c) {
	case 0:
		return nil
	case 1:
		return c[0]
	}
	return c
}

type providerLimit struct {
	// documented is the provider's published limit.
	documented Limit
	// limit is what decypharr uses: 4% under documented, because requests
	// that leave evenly can arrive bunched.
	limit Limit
	// perKey is set when the provider counts each API key separately.
	perKey bool
}

var providerLimits = map[string]providerLimit{
	// "The API is limited to 250 requests per minute", without saying per
	// key or per address (https://api.real-debrid.com/), so every
	// Real-Debrid client in the process shares one budget: every entry,
	// download key and repair probe.
	"realdebrid": {
		documented: Limit{Count: 250, Per: time.Minute},
		limit:      Limit{Count: 240, Per: time.Minute},
	},
	// 300 per minute per API key
	// (https://support.torbox.app/en/articles/13726368-api-rate-limits).
	"torbox": {
		documented: Limit{Count: 300, Per: time.Minute},
		limit:      Limit{Count: 288, Per: time.Minute},
		perKey:     true,
	},
}

// defaultLimit is the budget of a provider without a documented limit whose
// entry sets no rate_limit.
var defaultLimit = func() Limit {
	l, ok := Parse(config.DefaultRateLimit)
	if !ok {
		panic("throttle: config.DefaultRateLimit is not a rate: " + config.DefaultRateLimit)
	}
	return l
}()

// backgroundShare is the part of the budget that listing, info and repair
// calls may use on their own. They are paced evenly at this share, so a
// refresh with 50 workers cannot queue ahead of a stream's link request;
// link requests and TorBox submissions can use the whole budget.
const backgroundShare = 0.8

var (
	sharedMu      sync.Mutex
	shared        = map[string]ratelimit.Limiter{}
	sharedWindows = map[string]*Window{}
)

// keyFor names the budget of token at dc's provider and API host. The
// token is hashed so the registry holds no credentials.
func keyFor(dc config.Debrid, token string) string {
	sum := sha256.Sum256([]byte(token))
	return dc.Provider + "|" + dc.APIHost + "|" + hex.EncodeToString(sum[:8])
}

// sharedLimiter returns the process-wide limiter for key, so clients built by
// different entries, or again after a config reload, draw from one budget.
func sharedLimiter(key string, l Limit, clk ratelimit.Clock) ratelimit.Limiter {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if limiter, ok := shared[key]; ok {
		return limiter
	}
	limiter := newLimiter(l, clk)
	shared[key] = limiter
	return limiter
}

// Lanes are the limiters of one debrid entry's call paths. They all draw
// from the entry's budget.
type Lanes struct {
	// API carries listing, info and account calls on the main key.
	API ratelimit.Limiter
	// Repair carries repair probes; repair_rate_limit caps it further.
	Repair ratelimit.Limiter
	// Submit carries TorBox's torrent creation, which must not wait behind
	// list refreshes; download_rate_limit caps it further.
	Submit ratelimit.Limiter
	// Download returns the limiter of the download-key account for token;
	// download_rate_limit caps all download keys together.
	Download func(token string) ratelimit.Limiter
}

// ForDebrid builds the lanes of dc and logs the budget they share.
func ForDebrid(dc config.Debrid, log zerolog.Logger) Lanes {
	return forDebrid(dc, log, nil)
}

func forDebrid(dc config.Debrid, log zerolog.Logger, clk ratelimit.Clock) Lanes {
	log = log.With().Str("debrid", dc.Name).Logger()
	configured, set := Parse(dc.RateLimit)
	if dc.RateLimit != "" && !set {
		log.Warn().Str("rate_limit", dc.RateLimit).Msg("rate_limit is not a rate such as 250/minute; ignoring it")
	}

	// entry is this entry's own limiter, needed unless the provider-wide
	// limiter is the stricter one.
	var budget Limit
	var entry ratelimit.Limiter
	provider, documented := providerLimits[dc.Provider]
	switch {
	case !documented:
		budget = defaultLimit
		if set {
			budget = configured
		}
		entry = newLimiter(budget, clk)
	case set && configured.slower(provider.limit):
		budget = configured
		entry = newLimiter(budget, clk)
	default:
		budget = provider.limit
		if set && provider.documented.slower(configured) {
			log.Warn().Str("rate_limit", dc.RateLimit).
				Msgf("rate_limit is above the %s documented by %s; using %s", provider.documented, dc.Provider, budget)
		}
	}

	// Real-Debrid shares its limit across the process, TorBox per API key.
	providerLimiter := func(token string) ratelimit.Limiter {
		if !documented {
			return nil
		}
		key := dc.Provider + "|" + dc.APIHost
		if provider.perKey {
			key = keyFor(dc, token)
		}
		return sharedLimiter(key, provider.limit, clk)
	}

	var repairOnly, downloadOnly ratelimit.Limiter
	if l, ok := Parse(dc.RepairRateLimit); ok {
		repairOnly = newLimiter(l, clk)
	} else if dc.RepairRateLimit != "" {
		log.Warn().Str("repair_rate_limit", dc.RepairRateLimit).Msg("repair_rate_limit is not a rate such as 60/minute; ignoring it")
	}
	if l, ok := Parse(dc.DownloadRateLimit); ok {
		downloadOnly = newLimiter(l, clk)
	} else if dc.DownloadRateLimit != "" {
		log.Warn().Str("download_rate_limit", dc.DownloadRateLimit).Msg("download_rate_limit is not a rate such as 60/minute; ignoring it")
	}

	background := newPaced(budget, backgroundShare, clk)
	main := providerLimiter(dc.APIKey)
	event := log.Info().Str("budget", budget.String())
	if dc.RateLimit != "" {
		event = event.Str("rate_limit", dc.RateLimit)
	}
	event.Msg("API rate limit shared by API, repair and download-key calls")
	return Lanes{
		API:    newChain(background, entry, main),
		Repair: newChain(repairOnly, background, entry, main),
		Submit: newChain(downloadOnly, entry, main),
		Download: func(token string) ratelimit.Limiter {
			return newChain(downloadOnly, entry, providerLimiter(token))
		},
	}
}

// Window counts events over a sliding window, for a limit that is better
// refused than waited out, such as TorBox's 60 uncached adds per hour.
type Window struct {
	mu    sync.Mutex
	limit Limit
	times []time.Time
}

func NewWindow(l Limit) *Window {
	return &Window{limit: l}
}

// SharedWindow returns the process-wide Window called name for dc's API
// key, so a config reload or a second entry with the same key continues the
// count.
func SharedWindow(dc config.Debrid, name string, l Limit) *Window {
	key := name + "|" + keyFor(dc, dc.APIKey)
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if w, ok := sharedWindows[key]; ok {
		return w
	}
	w := NewWindow(l)
	sharedWindows[key] = w
	return w
}

// Allow records an event at now and reports true, unless l.Count events
// were recorded in the window before now; then it records nothing. A nil
// Window allows everything.
func (w *Window) Allow(now time.Time) bool {
	if w == nil {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := now.Add(-w.limit.Per)
	expired := 0
	for expired < len(w.times) && !w.times[expired].After(cutoff) {
		expired++
	}
	w.times = w.times[expired:]
	if len(w.times) >= w.limit.Count {
		return false
	}
	w.times = append(w.times, now)
	return true
}
