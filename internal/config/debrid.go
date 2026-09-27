package config

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type Debrid struct {
	Provider         string   `json:"provider,omitempty"` // realdebrid, alldebrid, debridlink, torbox, premiumize
	Name             string   `json:"name,omitempty"`
	APIKey           string   `json:"api_key,omitempty"`
	DownloadAPIKeys  []string `json:"download_api_keys,omitempty"`
	DownloadUncached bool     `json:"download_uncached,omitempty"`
	// RateLimit caps all of this entry's API calls together (main API,
	// repair and every download key), e.g. 200/minute or 10/second: at most
	// that many start in any minute or second. Real-Debrid stays at or under
	// 240/minute and TorBox at or under 288/minute per API key whatever it
	// says. Empty means those limits, or 250/minute for other providers.
	RateLimit string `json:"rate_limit,omitempty"`
	// RepairRateLimit also caps repair probes, which still count against
	// RateLimit.
	RepairRateLimit string `json:"repair_rate_limit,omitempty"`
	// DownloadRateLimit also caps download-link calls on all download keys
	// together, and TorBox submissions. They still count against RateLimit.
	DownloadRateLimit            string `json:"download_rate_limit,omitempty"`
	Proxy                        string `json:"proxy,omitempty"`
	UnpackRar                    bool   `json:"unpack_rar,omitempty"`
	MinimumFreeSlot              int    `json:"minimum_free_slot,omitempty"` // Minimum active pots to use this debrid
	Priority                     int    `json:"priority,omitempty"`          // Submission order, lower first; 0 means config position (index+1)
	Limit                        int    `json:"limit,omitempty"`             // Maximum number of total torrents
	TorrentsRefreshInterval      string `json:"torrents_refresh_interval,omitempty"`
	DownloadLinksRefreshInterval string `json:"download_links_refresh_interval,omitempty"`
	AutoExpireLinksAfter         string `json:"auto_expire_links_after,omitempty"`
	UserAgent                    string `json:"user_agent,omitempty"`
	// SlotStrategy frees AllDebrid magnet slots: "remove_after_add" deletes a
	// magnet once its download completes, "remove_oldest" deletes the oldest
	// magnet on the account before a submit at the limit. Empty keeps them.
	SlotStrategy string `json:"slot_strategy,omitempty"`
	// APIHost overrides the provider's API base URL: scheme, host and version
	// path, e.g. https://api.real-debrid.com/rest/1.0. Empty uses the
	// provider's public API. Meant for tests and fake providers.
	APIHost string `json:"api_host,omitempty"`
	// KeepInSync adopts completed torrents that are already on this provider
	// but were added outside decypharr (for example through Debrid Media
	// Manager) as completed downloads in the "other" category. Adopted
	// entries are served from the mount; nothing is downloaded or linked.
	KeepInSync bool `json:"keep_in_sync,omitempty"`
	// InsecureSkipVerify turns off TLS certificate verification for this
	// provider's API and download (CDN) connections. Certificates are
	// verified by default; set this only for a provider whose certificate
	// cannot be verified.
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`

	// Folder
	Folder        string `json:"folder,omitempty"`          // Deprecated. Use Mount MountPath instead.
	FolderNaming  string `json:"folder_naming,omitempty"`   // Deprecated. Use global setting instead.
	RcUrl         string `json:"rc_url,omitempty"`          // Deprecated. Use global setting instead.
	RcUser        string `json:"rc_user,omitempty"`         // Deprecated. Use global setting instead.
	RcPass        string `json:"rc_pass,omitempty"`         // Deprecated. Use global setting instead.
	RcRefreshDirs string `json:"rc_refresh_dirs,omitempty"` // Deprecated. Use global setting instead.

	// Directories
	Directories map[string]WebdavDirectories `json:"directories,omitempty"` // Deprecated. Use global setting instead.
}

// APIBaseURL returns the configured API host without trailing slashes, or
// defaultHost when none is configured.
func (d Debrid) APIBaseURL(defaultHost string) string {
	if host := strings.TrimRight(d.APIHost, "/"); host != "" {
		return host
	}
	return defaultHost
}

// DebridsByPriority returns a copy of debrids in submission order: ascending
// Priority, ties kept in config order. An unset (0) Priority counts as the
// provider's config position, as setDefaults would make it; debrids added
// only through env overrides have not been through setDefaults.
func DebridsByPriority(debrids []Debrid) []Debrid {
	ordered := slices.Clone(debrids)
	for i := range ordered {
		if ordered[i].Priority == 0 {
			ordered[i].Priority = i + 1
		}
	}
	slices.SortStableFunc(ordered, func(a, b Debrid) int {
		return cmp.Compare(a.Priority, b.Priority)
	})
	return ordered
}

func (c *Config) updateDebrid(index int, d Debrid) Debrid {
	if d.Provider == "" {
		d.Provider = d.Name
	}

	var downloadKeys []string

	if len(d.DownloadAPIKeys) > 0 {
		downloadKeys = d.DownloadAPIKeys
	} else {
		// If no download API keys are specified, use the main API key
		downloadKeys = []string{d.APIKey}
	}
	d.DownloadAPIKeys = downloadKeys

	if d.TorrentsRefreshInterval == "" {
		d.TorrentsRefreshInterval = DefaultTorrentsRefreshInterval
	}
	if d.DownloadLinksRefreshInterval == "" {
		d.DownloadLinksRefreshInterval = DefaultDownloadsRefreshInterval
	}
	if d.AutoExpireLinksAfter == "" {
		d.AutoExpireLinksAfter = DefaultAutoExpireLinksAfter
	}
	if d.Priority == 0 {
		d.Priority = index + 1 // Default priority based on order
	}

	return d
}

func validateDebrids(debrids []Debrid) error {
	if len(debrids) == 0 {
		return nil
	}

	for _, debrid := range debrids {
		// Basic field validation
		if debrid.APIKey == "" {
			return errors.New("debrid api key is required")
		}
		if debrid.SlotStrategy != "" {
			if cmp.Or(debrid.Provider, debrid.Name) != "alldebrid" {
				return fmt.Errorf("slot_strategy is only supported for alldebrid provider")
			}
			if debrid.SlotStrategy != "remove_after_add" && debrid.SlotStrategy != "remove_oldest" {
				return fmt.Errorf("invalid slot_strategy: %s (must be 'remove_after_add' or 'remove_oldest')", debrid.SlotStrategy)
			}
		}
	}

	return nil
}

func (c *Config) applyDebridEnvVars() {
	// Debrid providers array
	for i := range 10 { // Support up to 10 debrid providers
		prefix := fmt.Sprintf("DEBRIDS__%d__", i)
		if val := getEnv(prefix + "NAME"); val != "" {
			// Ensure array is large enough
			if i >= len(c.Debrids) {
				c.Debrids = append(c.Debrids, make([]Debrid, i-len(c.Debrids)+1)...)
			}
			c.Debrids[i].Name = val

			// Set other debrid fields
			if apiKey := getEnv(prefix + "API_KEY"); apiKey != "" {
				c.Debrids[i].APIKey = apiKey
			}
			if folder := getEnv(prefix + "FOLDER"); folder != "" {
				c.Debrids[i].Folder = folder
			}
			if provider := getEnv(prefix + "PROVIDER"); provider != "" {
				c.Debrids[i].Provider = provider
			}
			if proxy := getEnv(prefix + "PROXY"); proxy != "" {
				c.Debrids[i].Proxy = proxy
			}
			if insecure := getEnv(prefix + "INSECURE_SKIP_VERIFY"); insecure != "" {
				c.Debrids[i].InsecureSkipVerify = parseBool(insecure)
			}
			if apiHost := getEnv(prefix + "API_HOST"); apiHost != "" {
				c.Debrids[i].APIHost = apiHost
			}
			if priority := getEnv(prefix + "PRIORITY"); priority != "" {
				if v, err := strconv.Atoi(priority); err == nil {
					c.Debrids[i].Priority = v
				}
			}
			if keepInSync := getEnv(prefix + "KEEP_IN_SYNC"); keepInSync != "" {
				c.Debrids[i].KeepInSync = parseBool(keepInSync)
			}
		}
	}
}
