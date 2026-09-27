package request

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// These tests pin DrainAndClose's own contract, not the standard library's
// handling of a bare Close. Go 1.27 changed that: the HTTP/1 transport now
// drains up to 256 KiB (or 50 ms) of an unread body itself on Close, so
// whether a bare Close reuses the connection depends on the toolchain.

// trackingBody records how much of the body was consumed and whether it was
// closed.
type trackingBody struct {
	r      io.Reader
	read   int64
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.read += int64(n)
	return n, err
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

// DrainAndClose reads a remainder up to maxDrain to EOF, never more than
// maxDrain of a larger one, and closes the body either way. The bound is what
// keeps it safe to defer on an unexpectedly large response.
func TestDrainAndCloseReadsAtMostMaxDrain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remainder int
		wantRead  int64
	}{
		{"under the limit", 32 << 10, 32 << 10},
		{"exactly the limit", maxDrain, maxDrain},
		{"past the limit", 4 * maxDrain, maxDrain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackingBody{r: strings.NewReader(strings.Repeat("p", tc.remainder))}
			DrainAndClose(body)
			if body.read != tc.wantRead || !body.closed {
				t.Fatalf("read %d bytes, closed %v; want %d bytes, closed", body.read, body.closed, tc.wantRead)
			}
		})
	}
}

// After DrainAndClose consumes a remainder under maxDrain, the connection goes
// back to the idle pool: 20 requests share one connection.
func TestDrainAndCloseReusesConnection(t *testing.T) {
	var conns atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ok":true}`+"\n"+strings.Repeat("p", 32<<10))
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	c := srv.Client()
	for range 20 {
		resp, err := c.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		var out struct{ OK bool }
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		DrainAndClose(resp.Body)
	}
	if got := conns.Load(); got != 1 {
		t.Fatalf("expected a single reused connection, got %d", got)
	}
}
