package webdav

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/customerror"
	"github.com/sirrobot01/decypharr/internal/logger"
)

// Streaming errors can quote a CDN link; the WebDAV client and the error log
// must see it without its token.
func TestWriteStreamErrorRedactsLinks(t *testing.T) {
	cause := errors.New(`Get "https://store-1.tb-cdn.st/zip/7?token=cdn-secret": connection reset by peer`)
	for name, err := range map[string]error{
		"typed":   customerror.NewError(cause, http.StatusBadGateway, "cdn", false, false),
		"generic": cause,
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			h := &Handler{logger: logger.NewRateLimitedLogger(logger.WithLogger(zerolog.New(&logs)))}
			recorder := httptest.NewRecorder()
			h.writeStreamError("movie/file.mkv", err, recorder)

			if strings.Contains(recorder.Body.String(), "cdn-secret") || strings.Contains(logs.String(), "cdn-secret") {
				t.Fatalf("token leaked: body %q, log %q", recorder.Body.String(), logs.String())
			}
			if name == "typed" && (recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "token=REDACTED")) {
				t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
			}
		})
	}
}
