package vfs

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/logger"
)

var errCDNDown = fmt.Errorf("cdn unavailable")

// A failed CDN read keeps its error for waiters and logs it at debug level;
// neither may carry the link's token, and the error still unwraps.
func TestDownloadErrorIsRedacted(t *testing.T) {
	previous := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previous) })
	var logs bytes.Buffer
	item := newTestItem(t, testMiB)
	item.logger = logger.NewRateLimitedLogger(logger.WithLogger(zerolog.New(&logs).Level(zerolog.DebugLevel))).Rate("item")
	dls := &Downloaders{item: item}

	cdnErr := &url.Error{Op: "Get", URL: "https://store-1.tb-cdn.st/zip/7?token=cdn-secret", Err: errCDNDown}
	dls.countErrors(0, fmt.Errorf("fetch https://store-1.tb-cdn.st/zip/8?token=cdn-secret failed: %w", cdnErr))

	if strings.Contains(logs.String(), "cdn-secret") || !strings.Contains(logs.String(), "download error") {
		t.Fatalf("log = %s", logs.String())
	}
	last := dls.getLastErr()
	if strings.Contains(last.Error(), "cdn-secret") {
		t.Fatalf("lastErr = %q", last)
	}
	if !errorsIs(last, errCDNDown) {
		t.Fatal("redacted lastErr no longer unwraps to the cause")
	}
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
