package customerror_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/sirrobot01/decypharr/internal/customerror"
	"github.com/sirrobot01/decypharr/pkg/manager/link"
)

func TestRetryClassificationUsesWrappedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name             string
		err              error
		retry, permanent bool
	}{
		{"refetchable 404", link.NewRefetchableError(link.Err404, "404"), true, false},
		{"retryable forbidden", link.NewRetryableError(errors.New("forbidden"), "403"), true, false},
		{"permanent timeout", link.NewPermanentError(errors.New("i/o timeout"), ""), false, true},
		{"custom permanent", customerror.NewPermanentError(errors.New("broken pipe")), false, true},
		{"custom retryable", customerror.NewSilentError(errors.New("not found")).Retryable(), true, false},
		{"typed timeout", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, true, false},
		{"connection reset", &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}, true, false},
		{"dns failure", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "cdn.example", IsNotFound: true}}, true, false},
		{"deadline", context.DeadlineExceeded, true, false},
		{"cancelled", context.Canceled, false, false},
		{"missing file", fs.ErrNotExist, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertClassification(t, tc.err, tc.retry, tc.permanent)
		})
	}
}

// Error text is not an error class. A byte offset, a provider message or a
// release name must not turn a transient failure permanent, which would fast-trip
// the DFS circuit breaker and lock the file for two minutes.
func TestRetryClassificationIgnoresMessageText(t *testing.T) {
	for _, tc := range []struct {
		name             string
		err              error
		retry, permanent bool
	}{
		// Truncations at offsets whose decimal form contains a 4xx status.
		{"truncated at 2147404800", truncated(2147404800), true, false},
		{"truncated at 1401028608", truncated(1401028608), true, false},
		{"truncated at 3402629120", truncated(3402629120), true, false},
		{"truncated at 1403125760", truncated(1403125760), true, false},
		{"truncated at 4104126464", truncated(4104126464), true, false},
		{"reset while reading Gone Girl", fmt.Errorf("read Gone.Girl.2014.mkv: %w",
			&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}), true, false},
		// Untyped status text stays unclassified: the link rules make 404
		// refetchable and 5xx retryable only once they are typed.
		{"untyped 404 text", errors.New("torbox requestdl: HTTP 404"), false, false},
		{"untyped forbidden text", errors.New("forbidden: payment required"), false, false},
		{"untyped timeout text", errors.New("i/o timeout"), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertClassification(t, tc.err, tc.retry, tc.permanent)
		})
	}
}

func truncated(off int64) error {
	return fmt.Errorf("stream ended at offset %d before requested range %d-%d: %w",
		off, off, off+1<<20, io.ErrUnexpectedEOF)
}

func assertClassification(t *testing.T, err error, retry, permanent bool) {
	t.Helper()
	for _, err := range []error{err, fmt.Errorf("fetch: %w", err), errors.Join(errors.New("context"), err)} {
		if got := customerror.IsRetriableError(err); got != retry {
			t.Errorf("IsRetriableError(%v) = %v, want %v", err, got, retry)
		}
		if got := customerror.IsPermanentError(err); got != permanent {
			t.Errorf("IsPermanentError(%v) = %v, want %v", err, got, permanent)
		}
	}
}
