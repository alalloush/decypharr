package webdav

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"

	"github.com/sirrobot01/decypharr/internal/config"
)

// authMiddleware asks for credentials whenever use_auth is on, like every
// other HTTP surface: the UI username and password, or the API token as a
// Basic password or bearer token. decypharr's own rclone mount presents
// config.WebDAVMountToken.
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the toggle live so a change applies without a restart.
		if cfg := config.Get(); cfg.UseAuth && !h.authorized(cfg, r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized reports whether r carries the UI login, the API token or the
// mount token.
func (h *Handler) authorized(cfg *config.Config, r *http.Request) bool {
	if username, password, ok := r.BasicAuth(); ok {
		return config.VerifyToken(password) || h.logins.verify(cfg, username, password)
	}
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		token, ok = strings.CutPrefix(header, "Token ")
	}
	if !ok || token == "" {
		return false
	}
	return config.VerifyToken(token) ||
		subtle.ConstantTimeCompare([]byte(token), []byte(cfg.WebDAVMountToken())) == 1
}

// loginCache remembers UI logins that passed bcrypt. WebDAV clients send Basic
// credentials with every request, and checking the bcrypt hash costs about
// 50 ms each time: a player's range reads and a library scan's PROPFINDs
// would all wait for it. Entries are keyed by a hash of the credentials and
// the auth version, which changes whenever the credentials or the API token
// do.
type loginCache struct {
	mu     sync.Mutex
	logins map[[sha256.Size]byte]struct{}
}

// maxCachedLogins bounds the cache; it is cleared when full.
const maxCachedLogins = 16

func (c *loginCache) verify(cfg *config.Config, username, password string) bool {
	auth := cfg.GetAuth()
	if auth == nil {
		return false
	}
	key := sha256.Sum256([]byte(auth.SessionVersion + "\x00" + username + "\x00" + password))
	c.mu.Lock()
	_, known := c.logins[key]
	c.mu.Unlock()
	if known {
		return true
	}
	if !config.VerifyAuth(username, password) {
		return false
	}
	c.mu.Lock()
	if c.logins == nil || len(c.logins) >= maxCachedLogins {
		c.logins = make(map[[sha256.Size]byte]struct{}, maxCachedLogins)
	}
	c.logins[key] = struct{}{}
	c.mu.Unlock()
	return true
}
