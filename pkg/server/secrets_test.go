package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

// seedSecrets writes a config with every config.SecretFields property set to
// a distinct value and loads it. It returns the value stored in each field,
// keyed "Type.field" ("Type.field[i]" for list secrets).
func seedSecrets(t *testing.T) map[string]string {
	t.Helper()
	config.Reset()
	t.Cleanup(config.Reset)
	dir := t.TempDir()
	config.SetConfigPath(dir)

	seed := config.Config{
		DownloadFolder: dir,
		Debrids:        []config.Debrid{{Name: "rd", Provider: "realdebrid", RcUrl: "http://rclone:5572"}},
		Arrs:           []config.Arr{{Name: "sonarr", Host: "http://sonarr:8989"}},
		Usenet:         config.Usenet{Providers: []config.UsenetProvider{{Host: "news.test", Port: 563, Username: "user", MaxConnections: 5}}},
		Mount:          config.Mount{ExternalRclone: config.ExternalRclone{RCUrl: "http://rclone:5572"}},
	}
	want := map[string]string{}
	eachSecret(reflect.ValueOf(&seed).Elem(), func(typeName, field string, v reflect.Value) {
		value := "secret-" + typeName + "-" + field
		if v.Kind() == reflect.Slice {
			v.Set(reflect.ValueOf([]string{value + "-0", value + "-1"}))
			want[typeName+"."+field+"[0]"] = value + "-0"
			want[typeName+"."+field+"[1]"] = value + "-1"
			return
		}
		v.SetString(value)
		want[typeName+"."+field] = value
	})
	// Every declared secret must be reachable from the seed, or the tests
	// below would not cover it.
	for typeName, fields := range config.SecretFields {
		for _, field := range fields {
			if _, ok := want[typeName+"."+field]; !ok && want[typeName+"."+field+"[0]"] == "" {
				t.Fatalf("the seed config has no %s.%s; add a %s to it", typeName, field, typeName)
			}
		}
	}

	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.Get().SetupComplete(); err != nil {
		t.Fatalf("seed config is incomplete: %v", err)
	}
	return want
}

// storedSecrets returns the value of every secret in c, keyed like seedSecrets.
func storedSecrets(c *config.Config) map[string]string {
	got := map[string]string{}
	eachSecret(reflect.ValueOf(c).Elem(), func(typeName, field string, v reflect.Value) {
		if v.Kind() == reflect.Slice {
			for i := range v.Len() {
				got[fmt.Sprintf("%s.%s[%d]", typeName, field, i)] = v.Index(i).String()
			}
			return
		}
		got[typeName+"."+field] = v.String()
	})
	return got
}

func newConfigTestServer(t *testing.T) *Server {
	t.Helper()
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })
	return &Server{manager: mgr}
}

func getConfig(t *testing.T, s *Server) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleGetConfig(w, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d: %s", w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func postConfig(s *Server, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.handleUpdateConfig(w, httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body)))
	return w
}

func TestGetConfigRedactsEverySecret(t *testing.T) {
	seeded := seedSecrets(t)
	body := getConfig(t, newConfigTestServer(t))

	for name, value := range seeded {
		if strings.Contains(string(body), value) {
			t.Errorf("GET /api/config returns %s", name)
		}
	}
	var got config.Config
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	for name, value := range storedSecrets(&got) {
		// session_secret is left out of the response entirely.
		if value != secretPlaceholder && name != "Config.session_secret" {
			t.Errorf("%s = %q, want the placeholder", name, value)
		}
	}
	if config.Get().Debrids[0].APIKey != seeded["Debrid.api_key"] {
		t.Fatal("GET changed the live config")
	}
}

// The settings page posts back what it loaded, placeholders included.
func TestSaveOfRedactedConfigKeepsSecrets(t *testing.T) {
	seeded := seedSecrets(t)
	s := newConfigTestServer(t)

	if w := postConfig(s, string(getConfig(t, s))); w.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", w.Code, w.Body.String())
	}
	if got := storedSecrets(config.Get()); !reflect.DeepEqual(got, seeded) {
		t.Fatalf("secrets after the round trip:\n got %v\nwant %v", got, seeded)
	}
}

func TestSaveResolvesPlaceholdersAndOmittedSecrets(t *testing.T) {
	seeded := seedSecrets(t)
	s := newConfigTestServer(t)

	// rc_pass and the Arr token are left out of their entries; the second
	// download key is new; the SMB password is cleared.
	w := postConfig(s, `{
		"debrids": [{"name": "rd", "provider": "realdebrid", "rc_url": "http://rclone:5572",
			"api_key": "********", "download_api_keys": ["********", "new-download-key"]}],
		"arrs": [{"name": "sonarr", "host": "http://sonarr:8989"}],
		"usenet": {"providers": [{"host": "news.test", "port": 563, "username": "user", "password": "********"}]},
		"notifications": {"webhook_url": "********"},
		"mount": {"external_rclone": {"rc_url": "http://rclone:5572", "rc_password": "********"}},
		"smb": {"password": ""}
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", w.Code, w.Body.String())
	}

	want := map[string]string{}
	for name, value := range seeded {
		want[name] = value
	}
	want["Debrid.download_api_keys[1]"] = "new-download-key"
	want["SMB.password"] = ""
	if got := storedSecrets(config.Get()); !reflect.DeepEqual(got, want) {
		t.Fatalf("stored secrets:\n got %v\nwant %v", got, want)
	}

	// An explicit [] clears the download keys; Save then falls back to the
	// API key, as for any debrid without download keys.
	w = postConfig(s, `{"debrids": [{"name": "rd", "provider": "realdebrid", "rc_url": "http://rclone:5572",
		"api_key": "********", "download_api_keys": []}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST = %d: %s", w.Code, w.Body.String())
	}
	if keys := config.Get().Debrids[0].DownloadAPIKeys; !reflect.DeepEqual(keys, []string{seeded["Debrid.api_key"]}) {
		t.Fatalf("download keys after clearing = %q", keys)
	}
}

// A kept secret must not follow its entry to a new host: a client that can
// save settings but not read them could otherwise collect the stored keys.
func TestSaveRejectsKeptSecretsForANewDestination(t *testing.T) {
	for name, body := range map[string]string{
		"debrid api_host, placeholder key": `{"debrids": [{"name": "rd", "provider": "realdebrid", "api_host": "https://attacker.test",
			"api_key": "********", "download_api_keys": []}]}`,
		"debrid proxy, omitted key": `{"debrids": [{"name": "rd", "provider": "realdebrid", "proxy": "http://attacker.test:3128",
			"download_api_keys": []}]}`,
		"debrid api_host, new key, kept download keys": `{"debrids": [{"name": "rd", "provider": "realdebrid",
			"api_host": "https://attacker.test", "api_key": "new-key"}]}`,
		"debrid rc_url, kept rc_pass": `{"debrids": [{"name": "rd", "provider": "realdebrid", "rc_url": "http://attacker.test",
			"api_key": "********", "download_api_keys": ["********", "********"]}]}`,
		"arr host":                         `{"arrs": [{"name": "sonarr", "host": "http://attacker.test", "token": "********"}]}`,
		"rclone rc_url":                    `{"mount": {"external_rclone": {"rc_url": "http://attacker.test"}}}`,
		"usenet host":                      `{"usenet": {"providers": [{"host": "attacker.test", "port": 563, "username": "user", "password": "********"}]}}`,
		"renamed debrid":                   `{"debrids": [{"name": "other", "provider": "realdebrid", "api_key": "********"}]}`,
		"placeholder with no stored entry": `{"arrs": [{"name": "new", "host": "http://new.test", "token": "********"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			seeded := seedSecrets(t)
			s := newConfigTestServer(t)
			if w := postConfig(s, body); w.Code != http.StatusBadRequest {
				t.Fatalf("POST = %d: %s, want 400", w.Code, w.Body.String())
			}
			if got := storedSecrets(config.Get()); !reflect.DeepEqual(got, seeded) {
				t.Fatalf("a rejected save changed the secrets: %v", got)
			}
		})
	}

	// Sending the secret again is how an entry moves. Clearing rc_url sends
	// the omitted rc_pass nowhere, so it is kept.
	seeded := seedSecrets(t)
	s := newConfigTestServer(t)
	w := postConfig(s, `{"debrids": [{"name": "rd", "provider": "realdebrid", "api_host": "https://mirror.test",
		"api_key": "new-key", "download_api_keys": []}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("POST with a new key = %d: %s", w.Code, w.Body.String())
	}
	if d := config.Get().Debrids[0]; d.APIKey != "new-key" || d.RcPass != seeded["Debrid.rc_pass"] {
		t.Fatalf("moved debrid = %+v", d)
	}
}

// An unfinished usenet provider sends the user back to the wizard, which
// pre-fills the stored debrid key from GET /api/config and so posts the
// placeholder when the user keeps it.
func TestSetupWizardKeepsTheRedactedAPIKey(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)
	dir := t.TempDir()
	config.SetConfigPath(dir)
	seed := `{"debrids": [{"name": "realdebrid", "provider": "realdebrid", "api_key": "stored-key"}],
		"usenet": {"providers": [{"host": "news.test"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if config.Get().SetupComplete() == nil {
		t.Fatal("seed config should need the wizard")
	}
	s := newConfigTestServer(t)

	w := httptest.NewRecorder()
	s.setupCompleteHandler(w, httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(`{
		"auth": {"skip_auth": true},
		"debrid": {"provider": "realdebrid", "api_key": "********"},
		"download": {"download_folder": "`+filepath.Join(dir, "downloads")+`"},
		"mount": {"mount_type": "none"}
	}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("setup = %d: %s", w.Code, w.Body.String())
	}
	if d := config.Get().Debrids[0]; d.APIKey != "stored-key" || !reflect.DeepEqual(d.DownloadAPIKeys, []string{"stored-key"}) {
		t.Fatalf("debrid after setup = %+v", d)
	}
}
