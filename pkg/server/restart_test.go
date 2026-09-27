package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

// serveForStop runs handler behind s.trackInflight on a loopback listener.
func serveForStop(t *testing.T, s *Server, handler http.Handler) (*http.Server, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: s.trackInflight(handler)}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
	return srv, "http://" + listener.Addr().String()
}

func stopWithin(t *testing.T, s *Server, srv *http.Server, grace, handlerGrace, limit time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	done := make(chan struct{})
	go func() {
		_ = s.stop(srv, grace, handlerGrace)
		close(done)
	}()
	select {
	case <-done:
		return time.Since(start)
	case <-time.After(limit):
		t.Fatalf("stop still waiting after %s", limit)
		return 0
	}
}

// A restart cannot bind the next listener until the old server has stopped,
// and an open stream used to hold Shutdown for as long as the player read.
func TestStopClosesLongRequestsAfterGrace(t *testing.T) {
	s := &Server{logger: zerolog.Nop()}
	srv, url := serveForStop(t, s, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done() // streams until the connection goes
	}))
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	stopWithin(t, s, srv, 100*time.Millisecond, 5*time.Second, 3*time.Second)
	if n := s.inflight.Load(); n != 0 {
		t.Fatalf("%d handlers still running after stop", n)
	}
}

// A handler that ignores its connection must not hold the restart open
// either; stop gives up on it after handlerGrace.
func TestStopGivesUpOnHandlersThatIgnoreTheirConnection(t *testing.T) {
	s := &Server{logger: zerolog.Nop()}
	release := make(chan struct{})
	entered := make(chan struct{})
	srv, url := serveForStop(t, s, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	}))
	defer close(release)
	go func() {
		if response, err := http.Get(url); err == nil {
			_ = response.Body.Close()
		}
	}()
	<-entered

	took := stopWithin(t, s, srv, 50*time.Millisecond, 200*time.Millisecond, 3*time.Second)
	if took < 250*time.Millisecond {
		t.Fatalf("stop returned after %s, before the handler grace ran out", took)
	}
	if n := s.inflight.Load(); n != 1 {
		t.Fatalf("inflight = %d, want the stuck handler still counted", n)
	}
}

// The settings page reloads once /version names a different instance, so the
// marker must hold for one run and change with the next.
func TestVersionInstanceChangesAcrossRestarts(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })

	instance := func(s *Server) string {
		t.Helper()
		response := httptest.NewRecorder()
		s.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/version", nil))
		var body struct {
			Instance string `json:"instance"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK {
			t.Fatalf("GET /version = %d %s", response.Code, response.Body.String())
		}
		return body.Instance
	}
	first, restarted := New(mgr), New(mgr)
	if a, b := instance(first), instance(first); a == "" || a != b {
		t.Fatalf("instance within one run = %q then %q", a, b)
	}
	if instance(first) == instance(restarted) {
		t.Fatal("restarted server reports the previous instance")
	}
}
