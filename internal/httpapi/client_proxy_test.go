package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClientProxyWritesActiveProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	handler := NewServerWithConfig(ServerConfig{
		ClientProxyConfigPath: configPath,
		PublicBaseURL:         "http://127.0.0.1:8080",
	})

	body := []byte(`{"name":"orb-local","model":"orb/example-text","api_key":"secret-token"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/client-proxy/proxy", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var config clientProxyConfig
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("expected valid client proxy JSON: %v", err)
	}

	if config.ActiveProfile != "orb-local" || len(config.Profiles) != 1 {
		t.Fatalf("unexpected config: %#v", config)
	}
	profile := config.Profiles[0]
	if profile.Provider != "anthropic" || profile.Model != "orb/example-text" || profile.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected proxy profile: %#v", profile)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/client-proxy/profiles", nil)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listRecorder.Code)
	}
	if !bytes.Contains(listRecorder.Body.Bytes(), []byte(`"api_key":"secr...oken"`)) {
		t.Fatalf("expected masked API key, got %s", listRecorder.Body.String())
	}
}
