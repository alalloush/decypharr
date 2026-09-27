package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/arr"
	"github.com/sirrobot01/decypharr/pkg/manager"
)

func TestMergeConfigUpdatePreservesOmittedFields(t *testing.T) {
	current := config.Config{
		Port:     "9000",
		LogLevel: "info",
		Debrids: []config.Debrid{{
			Name:   "realdebrid",
			APIKey: "secret",
		}},
		Mount: config.Mount{
			Type:      config.MountTypeDFS,
			MountPath: "/mnt/decypharr",
		},
		Notifications: config.Notifications{
			Enabled:    true,
			WebhookURL: "https://example.com/webhook",
		},
	}

	merged, err := mergeConfigUpdate(&current, strings.NewReader(`{"log_level":"debug"}`))
	if err != nil {
		t.Fatalf("merge config update: %v", err)
	}

	if merged.LogLevel != "debug" {
		t.Fatalf("expected updated log level, got %q", merged.LogLevel)
	}
	if merged.Port != current.Port {
		t.Fatalf("expected port %q to be preserved, got %q", current.Port, merged.Port)
	}
	if !reflect.DeepEqual(merged.Debrids, current.Debrids) {
		t.Fatalf("expected debrid config to be preserved, got %#v", merged.Debrids)
	}
	if !reflect.DeepEqual(merged.Mount, current.Mount) {
		t.Fatalf("expected mount config to be preserved, got %#v", merged.Mount)
	}
	if !reflect.DeepEqual(merged.Notifications, current.Notifications) {
		t.Fatalf("expected notification config to be preserved, got %#v", merged.Notifications)
	}
}

func TestMergeConfigUpdateMergesNestedObjects(t *testing.T) {
	current := config.Config{
		Mount: config.Mount{
			Type:      config.MountTypeRclone,
			MountPath: "/mnt/decypharr",
		},
	}

	merged, err := mergeConfigUpdate(&current, strings.NewReader(`{"mount":{"type":"dfs"}}`))
	if err != nil {
		t.Fatalf("merge config update: %v", err)
	}

	if merged.Mount.Type != config.MountTypeDFS {
		t.Fatalf("expected mount type %q, got %q", config.MountTypeDFS, merged.Mount.Type)
	}
	if merged.Mount.MountPath != current.Mount.MountPath {
		t.Fatalf("expected mount path %q to be preserved, got %q", current.Mount.MountPath, merged.Mount.MountPath)
	}
}

func TestMergeConfigUpdateAllowsExplicitClear(t *testing.T) {
	current := config.Config{Debrids: []config.Debrid{{Name: "realdebrid", APIKey: "secret"}}}

	merged, err := mergeConfigUpdate(&current, strings.NewReader(`{"debrids":[]}`))
	if err != nil {
		t.Fatalf("merge config update: %v", err)
	}

	if len(merged.Debrids) != 0 {
		t.Fatalf("expected debrid config to be cleared, got %#v", merged.Debrids)
	}
}

// A posted list is the complete list. Decoding onto the current config merged
// lists by index, so an entry kept the unposted fields of whichever entry held
// its position before, and an entry could never drop a field by omitting it.
func TestMergeConfigUpdateReplacesListsWhole(t *testing.T) {
	uncached := true
	current := config.Config{
		Debrids: []config.Debrid{
			{Name: "realdebrid", Provider: "realdebrid", APIKey: "rd-key", DownloadAPIKeys: []string{"rd-download-key"}, RateLimit: "250/minute"},
			{Name: "torbox", Provider: "torbox", APIKey: "tb-key"},
		},
		Arrs: []config.Arr{
			{Name: "sonarr", Host: "http://sonarr:8989", Token: "sonarr-token", DownloadUncached: &uncached},
			{Name: "radarr", Host: "http://radarr:7878", Token: "radarr-token", DownloadUncached: &uncached},
		},
	}

	// The settings page removed the first provider and the first Arr, and
	// returned Radarr's download_uncached to the default by omitting it.
	merged, err := mergeConfigUpdate(&current, strings.NewReader(`{
		"debrids": [{"name": "torbox", "provider": "torbox", "api_key": "tb-key"}],
		"arrs": [{"name": "radarr", "host": "http://radarr:7878", "token": "radarr-token"}]
	}`))
	if err != nil {
		t.Fatalf("merge config update: %v", err)
	}

	wantDebrids := []config.Debrid{{Name: "torbox", Provider: "torbox", APIKey: "tb-key"}}
	if !reflect.DeepEqual(merged.Debrids, wantDebrids) {
		t.Fatalf("debrids = %#v, want %#v", merged.Debrids, wantDebrids)
	}
	wantArrs := []config.Arr{{Name: "radarr", Host: "http://radarr:7878", Token: "radarr-token"}}
	if !reflect.DeepEqual(merged.Arrs, wantArrs) {
		t.Fatalf("arrs = %#v, want %#v", merged.Arrs, wantArrs)
	}
}

func TestMergeConfigUpdateNullResetsAField(t *testing.T) {
	current := config.Config{Notifications: config.Notifications{Enabled: true, WebhookURL: "https://example.com/webhook"}}

	merged, err := mergeConfigUpdate(&current, strings.NewReader(`{"notifications":{"webhook_url":null}}`))
	if err != nil {
		t.Fatalf("merge config update: %v", err)
	}
	if merged.Notifications.WebhookURL != "" || !merged.Notifications.Enabled {
		t.Fatalf("notifications = %#v, want webhook cleared and enabled kept", merged.Notifications)
	}
}

func TestMergeConfigUpdateRejectsNonObjectBodies(t *testing.T) {
	current := config.Config{LogLevel: "info"}
	for _, body := range []string{`null`, `[]`, `"debug"`, `{"log_level":"debug"} {}`} {
		if merged, err := mergeConfigUpdate(&current, strings.NewReader(body)); err == nil {
			t.Errorf("body %s accepted: log_level = %q", body, merged.LogLevel)
		}
	}
}

func TestConfigHandlersUseSnapshots(t *testing.T) {
	config.Reset()
	config.SetConfigPath(t.TempDir())
	t.Cleanup(config.Reset)
	before := config.Get()
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })
	mgr.Arr().AddOrUpdate(arr.Arr{Name: "manual", Host: "http://example.test", Token: "token", Source: arr.SourceManual})
	server := &Server{manager: mgr}
	response := httptest.NewRecorder()
	server.handleGetConfig(response, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET status=%d", response.Code)
	}
	if len(before.Arrs) != 0 {
		t.Fatal("GET changed the current snapshot")
	}
	response = httptest.NewRecorder()
	server.handleUpdateConfig(response, httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"app_url":"https://new.example.test"}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", response.Code, response.Body.String())
	}
	if before.AppURL == "https://new.example.test" {
		t.Fatal("POST changed the previous snapshot")
	}
	if config.Get().AppURL != "https://new.example.test" {
		t.Fatal("POST did not publish the update")
	}
	var result struct {
		Restarted bool `json:"restarted"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Restarted {
		t.Fatal("live URL update restarted services")
	}
}

// The #343 incident end to end: a POST that sends only the field it changes
// must not wipe the configured providers, api keys included, from disk.
func TestUpdateConfigPartialPostKeepsSavedSections(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)
	dir := t.TempDir()
	config.SetConfigPath(dir)
	configFile := filepath.Join(dir, "config.json")
	seed := `{
		"log_level": "info",
		"download_folder": "/downloads",
		"debrids": [
			{"name": "realdebrid", "provider": "realdebrid", "api_key": "rd-key"},
			{"name": "torbox", "provider": "torbox", "api_key": "tb-key"}
		],
		"arrs": [{"name": "radarr", "host": "http://radarr:7878", "token": "radarr-token"}]
	}`
	if err := os.WriteFile(configFile, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg := config.Get(); len(cfg.Debrids) != 2 {
		t.Fatalf("seed config did not load: %#v", cfg.Debrids)
	}
	mgr := manager.New()
	t.Cleanup(func() { _ = mgr.Stop() })
	server := &Server{manager: mgr}

	response := httptest.NewRecorder()
	server.handleUpdateConfig(response, httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"log_level":"debug"}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	var saved config.Config
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.LogLevel != "debug" {
		t.Fatalf("log_level = %q, want debug", saved.LogLevel)
	}
	if len(saved.Debrids) != 2 || saved.Debrids[0].APIKey != "rd-key" || saved.Debrids[1].APIKey != "tb-key" {
		t.Fatalf("saved debrids = %#v", saved.Debrids)
	}
	if len(saved.Arrs) != 1 || saved.Arrs[0].Token != "radarr-token" || saved.DownloadFolder != "/downloads" {
		t.Fatalf("saved arrs = %#v, download_folder = %q", saved.Arrs, saved.DownloadFolder)
	}
}
