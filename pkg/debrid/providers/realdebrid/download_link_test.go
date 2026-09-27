package realdebrid

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/internal/customerror"
	"github.com/sirrobot01/decypharr/pkg/debrid/account"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
)

// Only a rejected API token is permanent: the DFS circuit breaker trips on it
// at once. Other failures keep the link rules, where 403 and 404 are not
// permanent, so they count towards the breaker one at a time.
func TestUnrestrictFailureClassification(t *testing.T) {
	for name, tc := range map[string]struct {
		status    int
		body      string
		permanent bool
	}{
		"bad token":         {http.StatusUnauthorized, `{"error":"bad_token","error_code":8}`, true},
		"permission denied": {http.StatusForbidden, `{"error":"permission_denied","error_code":9}`, false},
		"unknown resource":  {http.StatusNotFound, `{"error":"unknown_ressource","error_code":7}`, false},
		"no download link":  {http.StatusOK, `{"download":""}`, true},
	} {
		t.Run(name, func(t *testing.T) {
			config.SetConfigPath(t.TempDir())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(server.Close)
			dc := config.Debrid{Name: "realdebrid", DownloadAPIKeys: []string{"dl-token"}}
			rd := &RealDebrid{
				Host:            server.URL,
				config:          dc,
				logger:          zerolog.Nop(),
				accountsManager: account.NewManager(dc, nil, zerolog.Nop()),
			}

			_, err := rd.GetDownloadLink(t.Context(), "id", &types.File{Name: "Release.mkv", Link: "https://real-debrid.com/d/ABCDEF"})
			if err == nil {
				t.Fatal("GetDownloadLink() succeeded, want an error")
			}
			if got := customerror.IsPermanentError(err); got != tc.permanent {
				t.Fatalf("IsPermanentError(%v) = %v, want %v", err, got, tc.permanent)
			}
		})
	}
}
