package server

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var (
	nonceSource   = regexp.MustCompile(`script-src 'self' 'nonce-([A-Za-z0-9]+)';`)
	scriptTag     = regexp.MustCompile(`<script\b([^>]*)>`)
	scriptBlock   = regexp.MustCompile(`(?s)<script\b.*?</script>`)
	inlineHandler = regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`)
)

// Under the page policy an inline script runs only with the page's nonce, and
// an inline event handler never runs.
func TestPagesRunUnderTheContentSecurityPolicy(t *testing.T) {
	s := newTestServer(t)
	pages := []struct{ layout, page string }{{"setup_layout", "setup"}}
	for _, page := range []string{"index", "browse", "download", "repair", "reacquire", "stats", "config", "login", "register"} {
		pages = append(pages, struct{ layout, page string }{"layout", page})
	}
	seen := map[string]bool{}
	for _, p := range pages {
		w := httptest.NewRecorder()
		if err := s.render(w, p.layout, map[string]any{"URLBase": "/", "Page": p.page, "Title": p.page}); err != nil {
			t.Fatalf("%s: %v", p.page, err)
		}
		policy := w.Header().Get("Content-Security-Policy")
		match := nonceSource.FindStringSubmatch(policy)
		if match == nil {
			t.Fatalf("%s: policy %q does not limit scripts to 'self' and a nonce", p.page, policy)
		}
		nonce := match[1]
		if seen[nonce] {
			t.Fatalf("%s: nonce %s reused", p.page, nonce)
		}
		seen[nonce] = true

		body := w.Body.String()
		for _, tag := range scriptTag.FindAllStringSubmatch(body, -1) {
			if !strings.Contains(tag[1], "src=") && !strings.Contains(tag[1], `nonce="`+nonce+`"`) {
				t.Errorf("%s: inline script %s would be blocked", p.page, tag[0])
			}
		}
		if handler := inlineHandler.FindString(scriptBlock.ReplaceAllString(body, "")); handler != "" {
			t.Errorf("%s: inline event handler would be blocked: %s", p.page, handler)
		}
	}
}
