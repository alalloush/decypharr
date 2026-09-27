package customerror

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"syscall"
)

// Classification uses types only. Error text is never parsed: messages carry
// provider bodies, file names and byte offsets, so a stream truncated at
// offset 2147404800 would contain "404" and "Gone Girl" would contain "gone".
// Errors with no typed signal are neither retriable nor permanent. The DFS
// downloader counts them towards its circuit breaker without retrying them
// locally.

// retryDecider is implemented by errors that carry an explicit retry decision:
// customerror.Error, link.Error and nntp.Error.
type retryDecider interface {
	error
	IsRetryable() bool
}

type permanenceDecider interface {
	error
	IsPermanent() bool
}

// IsRetriableError reports whether err is known to be transient, so repeating
// the operation can succeed. Only these count:
//   - an explicit decision carried by a typed error in the chain;
//   - a context deadline, an unexpected EOF or a closed pipe or connection;
//   - a network or transport failure the standard library reports as a type or
//     an errno.
func IsRetriableError(err error) bool {
	if err == nil {
		return false
	}

	if r, ok := errors.AsType[retryDecider](err); ok {
		return r.IsRetryable()
	}

	if IsPermanentError(err) {
		return false
	}

	switch {
	case errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, io.ErrUnexpectedEOF),
		// The remote end closed the pipe or connection mid-transfer; retry as
		// for EPIPE.
		errors.Is(err, io.ErrClosedPipe),
		errors.Is(err, net.ErrClosed):
		return true
	}

	// *net.OpError covers dial, read and write failures, and the TLS alerts
	// crypto/tls reports as local or remote errors. *net.DNSError covers
	// resolver failures.
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return true
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return true
	}

	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH)
}

// IsPermanentError reports whether retrying err cannot help. Only these count:
//   - an explicit decision carried by a typed error;
//   - a missing file.
//
// HTTP statuses count only once they are typed: the link package classifies
// CDN and validation statuses, and providers mark a rejected API token (401)
// permanent. That matches the link rules: 400, 404, 410, 5xx and unknown codes
// are not permanent there either (upstream #369, #381, #402).
func IsPermanentError(err error) bool {
	if err == nil {
		return false
	}

	if p, ok := errors.AsType[permanenceDecider](err); ok {
		return p.IsPermanent()
	}
	if r, ok := errors.AsType[retryDecider](err); ok {
		return !r.IsRetryable()
	}

	return errors.Is(err, fs.ErrNotExist)
}
