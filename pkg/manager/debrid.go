package manager

import (
	"errors"
	"slices"

	"github.com/sirrobot01/decypharr/internal/config"
	debrid "github.com/sirrobot01/decypharr/pkg/debrid/common"
	"github.com/sirrobot01/decypharr/pkg/debrid/providers/alldebrid"
	"github.com/sirrobot01/decypharr/pkg/debrid/providers/debridlink"
	"github.com/sirrobot01/decypharr/pkg/debrid/providers/premiumize"
	"github.com/sirrobot01/decypharr/pkg/debrid/providers/realdebrid"
	"github.com/sirrobot01/decypharr/pkg/debrid/providers/torbox"
	"github.com/sirrobot01/decypharr/pkg/debrid/throttle"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

var (
	ErrUnsupportedDebridProvider = errors.New("unsupported debrid provider")
)

func (m *Manager) ProviderClient(name string) debrid.Client {
	client, ok := m.clients.Load(name)
	if !ok {
		return nil
	}
	return client
}

func (m *Manager) initDebridClients() {
	cfg := config.Get()
	order := make([]string, 0, len(cfg.Debrids))
	for _, dc := range config.DebridsByPriority(cfg.Debrids) {
		client, err := m.createClient(dc)
		if err != nil {
			m.logger.Error().Err(err).Str("debrid", dc.Name).Msg("Failed to create debrid client")
			continue
		}
		m.clients.Store(dc.Name, client)
		if !slices.Contains(order, dc.Name) {
			order = append(order, dc.Name)
		}
	}
	m.debridOrder = order
}

// createClient creates a debrid client based on configuration
func (m *Manager) createClient(dc config.Debrid) (debrid.Client, error) {
	var client debrid.Client
	var err error

	// Every call path of the entry shares the budget ForDebrid builds.
	switch dc.Provider {
	case "realdebrid":
		client, err = realdebrid.New(dc, throttle.ForDebrid(dc, m.logger))
	case "alldebrid":
		client, err = alldebrid.New(dc, throttle.ForDebrid(dc, m.logger))
	case "torbox":
		client, err = torbox.New(dc, throttle.ForDebrid(dc, m.logger))
	case "debridlink":
		client, err = debridlink.New(dc, throttle.ForDebrid(dc, m.logger))
	case "premiumize":
		client, err = premiumize.New(dc, throttle.ForDebrid(dc, m.logger))
	default:
		return nil, ErrUnsupportedDebridProvider
	}

	if err != nil {
		return nil, err
	}

	return client, nil
}

// FilterDebrid returns the clients that match filter in submission order:
// ascending priority, then config order (config.DebridsByPriority). Walking
// m.clients instead would follow the map's per-process random seed.
func (m *Manager) FilterDebrid(filter func(debrid.Client) bool) []debrid.Client {
	var filtered []debrid.Client
	for _, name := range m.debridOrder {
		client, ok := m.clients.Load(name)
		if ok && client != nil && filter(client) {
			filtered = append(filtered, client)
		}
	}
	return filtered
}

func (m *Manager) GetIngests() ([]types.IngestData, error) {
	// Use streaming to avoid loading all torrents into memory
	var ingests []types.IngestData
	err := m.storage.ForEach(func(torrent *storage.Entry) error {
		ingests = append(ingests, types.IngestData{
			Debrid: torrent.ActiveProvider,
			Name:   torrent.OriginalFilename,
			Hash:   torrent.InfoHash,
			Size:   torrent.Bytes,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ingests, nil
}

func (m *Manager) GetIngestsByDebrid(debridName string) ([]types.IngestData, error) {
	// Use streaming to avoid loading all torrents into memory
	var ingests []types.IngestData
	err := m.storage.ForEach(func(torrent *storage.Entry) error {
		if !torrent.HasProvider(debridName) {
			return nil
		}
		ingests = append(ingests, types.IngestData{
			Debrid: torrent.ActiveProvider,
			Name:   torrent.OriginalFilename,
			Hash:   torrent.InfoHash,
			Size:   torrent.Bytes,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ingests, nil
}
