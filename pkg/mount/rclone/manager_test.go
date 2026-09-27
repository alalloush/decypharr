package rclone

import (
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
)

// With an IPv6 bind address the mount pointed rclone at http://::1:8282/,
// which is not a URL it can dial.
func TestWebDAVURLForAnIPv6BindAddress(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	if _, err := config.Update(func(c *config.Config) error {
		c.BindAddress, c.Port, c.URLBase = "::1", "8282", "/decypharr/"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil)
	if m == nil {
		t.Fatal("NewManager = nil: the WebDAV URL did not parse")
	}
	t.Cleanup(m.cancel)
	if want := "http://[::1]:8282/decypharr/webdav/"; m.webdavURL != want {
		t.Fatalf("WebDAV URL = %q, want %q", m.webdavURL, want)
	}
}
