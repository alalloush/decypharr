package server

import (
	"crypto/rand"
	"fmt"
	"net/http"
)

// pageCSP is the Content-Security-Policy of every rendered page. Scripts run
// only from /assets or from an inline block that carries the page's nonce, so
// markup that reaches a page through a torrent name or a crafted link cannot
// run code: inline event handlers, javascript: URLs and injected <script>
// tags are all refused. Styles still allow inline style attributes, which the
// templates and the generated markup use; with img-src and font-src limited
// to this origin, an injected style cannot load anything from elsewhere.
// frame-ancestors is left out so dashboards such as Organizr can still frame
// the UI.
const pageCSP = "default-src 'self'; script-src 'self' 'nonce-%s'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; " +
	"connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'"

// render executes a page template with a fresh CSP nonce. Templates mark
// their inline scripts with nonce="{{.CSPNonce}}".
func (s *Server) render(w http.ResponseWriter, name string, data map[string]any) error {
	nonce := rand.Text()
	w.Header().Set("Content-Security-Policy", fmt.Sprintf(pageCSP, nonce))
	data["CSPNonce"] = nonce
	return s.templates.ExecuteTemplate(w, name, data)
}
