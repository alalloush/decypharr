package request

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// redacted replaces a credential in a URL.
const redacted = "REDACTED"

// sensitiveParams are query parameters whose values are credentials, matched
// case-insensitively. TorBox's requestdl takes token=, AllDebrid and the Arr
// APIs take apikey=.
var sensitiveParams = map[string]struct{}{
	"token":    {},
	"apikey":   {},
	"api_key":  {},
	"key":      {},
	"password": {},
}

// urlPattern finds absolute URLs in free text, such as an error message. It
// stops at whitespace and at the quotes that url.Error puts around its URL.
var urlPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)

// RedactURL returns rawURL with its credentials replaced: the userinfo, and
// the values of the token, apikey, api_key, key and password query
// parameters. Anything else, including parameter order, is kept. If rawURL
// does not parse, its whole query is replaced.
func RedactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		if base, _, found := strings.Cut(rawURL, "?"); found {
			return base + "?" + redacted
		}
		return rawURL
	}
	changed := false
	if u.User != nil {
		u.User = url.User(redacted)
		changed = true
	}
	if query, ok := redactQuery(u.RawQuery); ok {
		u.RawQuery = query
		changed = true
	}
	if !changed {
		return rawURL
	}
	return u.String()
}

func redactQuery(rawQuery string) (string, bool) {
	if rawQuery == "" {
		return rawQuery, false
	}
	params := strings.Split(rawQuery, "&")
	changed := false
	for i, param := range params {
		name, value, found := strings.Cut(param, "=")
		if !found || value == "" || value == redacted {
			continue
		}
		key, err := url.QueryUnescape(name)
		if err != nil {
			key = name
		}
		if _, sensitive := sensitiveParams[strings.ToLower(key)]; sensitive {
			params[i] = name + "=" + redacted
			changed = true
		}
	}
	return strings.Join(params, "&"), changed
}

// redactText applies RedactURL to every URL in s. Punctuation that closes a
// sentence or a list is kept outside the URL.
func redactText(s string) string {
	return urlPattern.ReplaceAllStringFunc(s, func(match string) string {
		trimmed := strings.TrimRight(match, ".,:;)]}")
		return RedactURL(trimmed) + match[len(trimmed):]
	})
}

// RedactError removes credentials from the URLs err reports. Every
// *url.Error in the chain has its URL redacted in place. If the message still
// carries a credential, for example a URL formatted into it with %s, the
// result wraps err: Error() is redacted, and errors.Is and errors.As still
// see the original chain.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	redactURLErrors(err)
	msg := err.Error()
	if clean := redactText(msg); clean != msg {
		return &redactedError{err: err, msg: clean}
	}
	return err
}

func redactURLErrors(err error) {
	for err != nil {
		if urlErr, ok := err.(*url.Error); ok {
			urlErr.URL = RedactURL(urlErr.URL)
		}
		switch wrapped := err.(type) {
		case interface{ Unwrap() []error }:
			for _, inner := range wrapped.Unwrap() {
				redactURLErrors(inner)
			}
			return
		case interface{ Unwrap() error }:
			err = wrapped.Unwrap()
		default:
			return
		}
	}
}

type redactedError struct {
	err error
	msg string
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// retryError is returned when the retry loop gives up. It has
// retryablehttp's wording, but the URL is redacted: retryablehttp's own error
// keeps the query, which is where TorBox's requestdl carries the API token.
type retryError struct {
	method   string
	url      string
	attempts int
	err      error
}

func (e *retryError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("%s %s giving up after %d attempt(s)", e.method, e.url, e.attempts)
	}
	return fmt.Sprintf("%s %s giving up after %d attempt(s): %v", e.method, e.url, e.attempts, e.err)
}

func (e *retryError) Unwrap() error { return e.err }

// giveUp is the retry client's ErrorHandler. The handler does not see the
// request, so Do fills in the method and URL.
func giveUp(resp *http.Response, err error, attempts int) (*http.Response, error) {
	if resp != nil {
		DrainAndClose(resp.Body)
	}
	return nil, &retryError{attempts: attempts, err: RedactError(err)}
}

// nameRequest completes a retryError from giveUp with the request it failed.
func nameRequest(err error, req *http.Request) error {
	if retryErr, ok := errors.AsType[*retryError](err); ok {
		retryErr.method = req.Method
		retryErr.url = RedactURL(req.URL.String())
	}
	return err
}
