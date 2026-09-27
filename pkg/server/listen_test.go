package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

// newListeningServer builds the real server for a bind address and port.
func newListeningServer(t *testing.T, bindAddress, port string) *Server {
	t.Helper()
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	if _, err := config.Update(func(c *config.Config) error {
		c.BindAddress, c.Port = bindAddress, port
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })
	return New(mgr)
}

// A bind failure used to be logged while the process ran on without its UI,
// API and WebDAV.
func TestStartFailsWhenThePortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	_, port, _ := net.SplitHostPort(taken.Addr().String())
	s := newListeningServer(t, "127.0.0.1", port)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = s.Start(ctx)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("Start with port %s taken = %v after the context ended (%v), want the bind error at once", port, err, ctx.Err())
	}
}

// expectClosed fails unless the server closes conn within limit.
func expectClosed(t *testing.T, conn net.Conn, limit time.Duration) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(limit))
	_, err := io.Copy(io.Discard, conn) // nil at EOF
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatalf("connection still open after %s", limit)
	}
}

// Without a header or idle timeout a client could hold a connection, and its
// goroutine, open forever. Neither limit may cut a long transfer.
func TestHTTPServerTimeouts(t *testing.T) {
	const headerTimeout, idle = 100 * time.Millisecond, 200 * time.Millisecond
	const chunks = 6 // sent headerTimeout apart: longer than both limits
	srv := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			for range chunks {
				_, _ = w.Write([]byte("chunk"))
				w.(http.Flusher).Flush()
				time.Sleep(headerTimeout)
			}
			return
		}
		n, _ := io.Copy(io.Discard, r.Body)
		_, _ = fmt.Fprint(w, n)
	}), headerTimeout, idle)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
	url := "http://" + listener.Addr().String()
	dial := func(t *testing.T) net.Conn {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}

	t.Run("headers never finished", func(t *testing.T) {
		conn := dial(t)
		_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\n")
		expectClosed(t, conn, 2*time.Second)
	})

	t.Run("idle kept-alive connection", func(t *testing.T) {
		conn := dial(t)
		_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\n\r\n")
		response, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.Close {
			t.Fatal("server did not keep the connection alive")
		}
		expectClosed(t, conn, 2*time.Second)
	})

	t.Run("long stream", func(t *testing.T) {
		response, err := http.Get(url + "/stream")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if want := strings.Repeat("chunk", chunks); err != nil || string(body) != want {
			t.Fatalf("stream = %q, %v; want %q", body, err, want)
		}
	})

	t.Run("slow upload", func(t *testing.T) {
		body, upload := io.Pipe()
		go func() {
			for range chunks {
				time.Sleep(headerTimeout)
				_, _ = upload.Write([]byte("chunk"))
			}
			_ = upload.Close()
		}()
		response, err := http.Post(url+"/upload", "application/octet-stream", body)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		received, err := io.ReadAll(response.Body)
		if want := fmt.Sprint(chunks * len("chunk")); err != nil || string(received) != want {
			t.Fatalf("server received %q bytes, %v; want %s", received, err, want)
		}
	})
}
