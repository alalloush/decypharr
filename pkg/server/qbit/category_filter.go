package qbit

import (
	"fmt"
	"strings"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// categoryHashFilter never interprets absent hashes as a request for all rows.
// qBittorrent clients may use either repeated fields or a pipe-delimited field.
// Only torrent entries match: the queue also holds SABnzbd downloads, which the
// qBittorrent API does not manage.
func categoryHashFilter(values []string) (func(*storage.Entry) bool, error) {
	hashes := make(map[string]bool)
	for _, value := range values {
		for _, hash := range strings.Split(value, "|") {
			hash = strings.ToLower(strings.TrimSpace(hash))
			if hash != "" {
				hashes[hash] = true
			}
		}
	}
	if len(hashes) == 0 {
		return nil, fmt.Errorf("explicit hashes required for category changes")
	}
	if hashes["all"] {
		if len(hashes) != 1 {
			return nil, fmt.Errorf("all cannot be mixed with individual hashes")
		}
		return func(entry *storage.Entry) bool {
			return entry.Protocol == config.ProtocolTorrent
		}, nil
	}
	return func(entry *storage.Entry) bool {
		return entry.Protocol == config.ProtocolTorrent && hashes[strings.ToLower(entry.InfoHash)]
	}, nil
}
