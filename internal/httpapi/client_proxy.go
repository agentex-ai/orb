package httpapi

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type clientProxyConfig struct {
	ActiveProfile string               `json:"active_profile"`
	Profiles      []clientProxyProfile `json:"profiles"`
}

type clientProxyProfile struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
}

type clientProxyProfileDisplay struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	IsActive bool   `json:"is_active"`
}

type clientProxyIntegration struct {
	configPath string
}

func newClientProxyIntegration(configPath string) *clientProxyIntegration {
	return &clientProxyIntegration{configPath: strings.TrimSpace(configPath)}
}

func (c *clientProxyIntegration) path() string {
	if c == nil {
		return ""
	}
	if c.configPath != "" {
		return c.configPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".orb", "client-proxy.json")
}

func (c *clientProxyIntegration) read() (*clientProxyConfig, error) {
	path := c.path()
	if path == "" {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &clientProxyConfig{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return &clientProxyConfig{}, nil
	}
	var config clientProxyConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c *clientProxyIntegration) write(config *clientProxyConfig) error {
	path := c.path()
	if path == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (c *clientProxyIntegration) displays() ([]clientProxyProfileDisplay, error) {
	config, err := c.read()
	if err != nil {
		return nil, err
	}
	items := make([]clientProxyProfileDisplay, 0, len(config.Profiles))
	for _, profile := range config.Profiles {
		items = append(items, clientProxyProfileDisplay{
			Name:     profile.Name,
			Provider: profile.Provider,
			Model:    profile.Model,
			APIKey:   maskAPIKey(profile.APIKey),
			BaseURL:  profile.BaseURL,
			IsActive: profile.Name == config.ActiveProfile,
		})
	}
	return items, nil
}

func (c *clientProxyIntegration) activate(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return os.ErrNotExist
	}
	config, err := c.read()
	if err != nil {
		return err
	}
	for _, profile := range config.Profiles {
		if profile.Name == name {
			config.ActiveProfile = name
			return c.write(config)
		}
	}
	return os.ErrNotExist
}

func (c *clientProxyIntegration) upsertProxy(profile clientProxyProfile) error {
	config, err := c.read()
	if err != nil {
		return err
	}
	for i := range config.Profiles {
		if config.Profiles[i].Name == profile.Name {
			config.Profiles[i] = profile
			config.ActiveProfile = profile.Name
			return c.write(config)
		}
	}
	config.Profiles = append(config.Profiles, profile)
	config.ActiveProfile = profile.Name
	return c.write(config)
}

func (s server) handleClientProxyProfiles(writer http.ResponseWriter, request *http.Request) {
	profiles, err := s.clientProxy.displays()
	if err != nil {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "client_proxy_unavailable",
			Message: err.Error(),
		})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"object": "list",
		"path":   s.clientProxy.path(),
		"data":   profiles,
	})
}

func (s server) handleClientProxyActivate(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, APIError{Code: "invalid_argument", Message: "request body must be valid JSON"})
		return
	}
	if err := s.clientProxy.activate(payload.Name); err != nil {
		writeError(writer, http.StatusNotFound, APIError{Code: "client_proxy_profile_not_found", Message: "client proxy profile is not available"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"active_profile": strings.TrimSpace(payload.Name)})
}

func (s server) handleClientProxyProxy(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	var payload struct {
		Name    string `json:"name"`
		Model   string `json:"model"`
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, APIError{Code: "invalid_argument", Message: "request body must be valid JSON"})
		return
	}
	baseURL := cmp.Or(strings.TrimRight(strings.TrimSpace(payload.BaseURL), "/"), s.publicBaseURL, requestHostBaseURL(request))
	profile := clientProxyProfile{
		Name:     cmp.Or(strings.TrimSpace(payload.Name), "orb-api-proxy"),
		Provider: "anthropic",
		Model:    cmp.Or(strings.TrimSpace(payload.Model), "orb/example-text"),
		APIKey:   cmp.Or(strings.TrimSpace(payload.APIKey), "orb"),
		BaseURL:  baseURL,
	}
	if err := s.clientProxy.upsertProxy(profile); err != nil {
		writeError(writer, http.StatusBadRequest, APIError{Code: "client_proxy_unavailable", Message: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"active_profile": profile.Name,
		"base_url":       baseURL,
		"path":           s.clientProxy.path(),
	})
}

func requestHostBaseURL(request *http.Request) string {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}
	host := strings.TrimSpace(request.Host)
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func maskAPIKey(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}
	if len(apiKey) <= 8 {
		return "****"
	}
	return apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
}
