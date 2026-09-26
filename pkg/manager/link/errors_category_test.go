package link

import "testing"

// Transient provider codes must never be permanent: fetchAndValidate memoises a
// permanent failure against the download URL, so the file would stay unreadable
// until the memo is cleared or the process restarts. Retryable and refetchable
// failures are refetched instead and never memoised.
//
// The second half guards against over-correcting: genuinely permanent codes must
// stay permanent, otherwise the caller would refetch forever on a dead file.
func TestTransientCodesAreRefetchable(t *testing.T) {
	transient := []string{"400", "404", "429", "500", "502", "503", "504", "read_pxy_timeout", "some_unrecognised_code"}
	for _, code := range transient {
		e := ErrorCodeToLinkError(code)
		if e.IsPermanent() {
			t.Errorf("code %q classified as permanent: the file would stay unreadable until restart", code)
		}
		if !e.ShouldRefetch() && !e.ShouldRetry() {
			t.Errorf("code %q is neither refetchable nor retryable, so the memoised failure is never cleared", code)
		}
	}

	permanent := []string{"401", "unauthorized", "link_not_found", "file_not_available"}
	for _, code := range permanent {
		e := ErrorCodeToLinkError(code)
		if !e.IsPermanent() {
			t.Errorf("code %q should remain permanent", code)
		}
		if e.ShouldRefetch() {
			t.Errorf("code %q should not trigger a refetch", code)
		}
	}
}
