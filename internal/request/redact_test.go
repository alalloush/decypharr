package request

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
)

func TestRedactURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://api.torbox.app/v1/api/torrents/requestdl?file_id=3&token=dl-token&torrent_id=7",
			"https://api.torbox.app/v1/api/torrents/requestdl?file_id=3&token=REDACTED&torrent_id=7"},
		{"http://h/x?ApiKey=a&api_key=b&KEY=c&password=d&name=e",
			"http://h/x?ApiKey=REDACTED&api_key=REDACTED&KEY=REDACTED&password=REDACTED&name=e"},
		{"https://user:pass@h/x", "https://REDACTED@h/x"},
		{"https://h/d/ABC/file.mkv?limit=1000", "https://h/d/ABC/file.mkv?limit=1000"},
		{"https://h/x?tokens=1&monkey=2", "https://h/x?tokens=1&monkey=2"},
	} {
		if got := RedactURL(tc.in); got != tc.want {
			t.Errorf("RedactURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A request that runs out of retries reports its URL, but TorBox's requestdl
// carries the API token in the query: the error must not.
func TestGiveUpErrorRedactsCredentials(t *testing.T) {
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)

	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unavailable.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := "http://user:hunter2@" + ln.Addr().String()
	_ = ln.Close()

	for name, base := range map[string]string{"retryable status": unavailable.URL, "transport error": refused} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/requestdl?file_id=3&token=dl-token", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = New(WithMaxRetries(0)).Do(req)
			if err == nil {
				t.Fatal("expected an error")
			}
			msg := err.Error()
			if strings.Contains(msg, "dl-token") || strings.Contains(msg, "hunter2") || !strings.Contains(msg, "file_id=3&token=REDACTED") {
				t.Fatalf("error = %q", msg)
			}
			if urlErr, ok := errorsAsURL(err); ok && strings.Contains(urlErr.URL, "dl-token") {
				t.Fatalf("wrapped url.Error still holds the token: %q", urlErr.URL)
			}
		})
	}
}

func errorsAsURL(err error) (*url.Error, bool) {
	for err != nil {
		if urlErr, ok := err.(*url.Error); ok {
			return urlErr, true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil, false
		}
		err = unwrapper.Unwrap()
	}
	return nil, false
}
