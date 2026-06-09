package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPrivateHTTPAdapterGenerate(t *testing.T) {
	var gotBody privateHTTPRequest
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("expected /v1/responses, got %s", request.URL.Path)
		}
		gotAuth = request.Header.Get("Authorization")

		if err := json.NewDecoder(request.Body).Decode(&gotBody); err != nil {
			t.Fatalf("expected valid upstream body: %v", err)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(privateHTTPResponse{
			ID:     "resp_upstream",
			Object: "response",
			Model:  "upstream-model",
			Output: []privateHTTPResponseItem{{Type: "output_text", Text: "Upstream: hello private"}},
			Usage: privateHTTPUsage{
				InputTokens:  12,
				OutputTokens: 4,
				TotalTokens:  16,
			},
			Runtime: privateHTTPRuntimeMetadata{
				Adapter:       "upstream",
				Deployment:    "private",
				MemoryApplied: true,
				Status:        "ready",
			},
		})
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL:         upstream.URL,
		PublicModelID:   privateEchoModelID,
		UpstreamModelID: "upstream-model",
		AuthToken:       "secret-token",
		Client:          upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	response, err := adapter.Generate(context.Background(), Request{
		Model: privateEchoModelID,
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello private"}}}},
		Memory: &MemoryRequest{
			Enabled: true,
			Scope:   "workspace:test",
		},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if gotBody.Model != "upstream-model" {
		t.Fatalf("expected upstream model id, got %#v", gotBody)
	}

	if gotAuth != "Bearer secret-token" {
		t.Fatalf("expected bearer auth header, got %q", gotAuth)
	}

	if response.Runtime.Adapter != "private-http" || response.Runtime.Deployment != "private" {
		t.Fatalf("unexpected runtime metadata: %#v", response.Runtime)
	}

	if response.Output[0].Text != "Upstream: hello private" {
		t.Fatalf("unexpected output payload: %#v", response.Output)
	}

	if response.Usage.TotalTokens != 16 {
		t.Fatalf("unexpected usage payload: %#v", response.Usage)
	}
}

func TestPrivateHTTPAdapterModelsUsesUpstreamDiscovery(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("expected /v1/models, got %s", request.URL.Path)
		}
		gotAuth = request.Header.Get("Authorization")
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(privateHTTPModelList{
			Object: "list",
			Data: []privateHTTPModel{
				{
					ID:           "upstream-private",
					Object:       "model",
					Provider:     "vllm",
					Deployment:   "private",
					Capabilities: []string{"text", "tools"},
					Status:       "warm",
				},
			},
		})
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL:         upstream.URL,
		PublicModelID:   privateEchoModelID,
		UpstreamModelID: "upstream-private",
		AuthToken:       "secret-token",
		Client:          upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	models := adapter.Models(context.Background())
	if len(models) != 1 {
		t.Fatalf("expected one discovered model, got %d", len(models))
	}

	if gotAuth != "Bearer secret-token" {
		t.Fatalf("expected bearer auth on discovery request, got %q", gotAuth)
	}

	model := models[0]
	if model.ID != privateEchoModelID || model.Provider != "private-http" || model.Status != "warm" {
		t.Fatalf("unexpected discovered model payload: %#v", model)
	}

	if len(model.Capabilities) != 2 || model.Capabilities[1] != "tools" {
		t.Fatalf("unexpected discovered capabilities: %#v", model.Capabilities)
	}

	if model.Metadata["discovery"] != "upstream" || model.Metadata["upstream_provider"] != "vllm" {
		t.Fatalf("unexpected discovered metadata: %#v", model.Metadata)
	}
}

func TestPrivateHTTPAdapterModelsFallsBackWhenDiscoveryFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, `{"error":{"code":"backend_unavailable","message":"no models"}}`, http.StatusBadGateway)
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL:         upstream.URL,
		PublicModelID:   privateEchoModelID,
		UpstreamModelID: "upstream-private",
		Client:          upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	models := adapter.Models(context.Background())
	if len(models) != 1 {
		t.Fatalf("expected one fallback model, got %d", len(models))
	}

	model := models[0]
	if model.Metadata["discovery"] != "fallback" || model.Metadata["upstream_model_id"] != "upstream-private" {
		t.Fatalf("unexpected fallback metadata: %#v", model.Metadata)
	}
}

func TestPrivateHTTPAdapterGenerateWithCustomAuthHeader(t *testing.T) {
	var gotHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotHeader = request.Header.Get("X-API-Key")
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(privateHTTPResponse{
			ID:     "resp_upstream",
			Object: "response",
			Model:  "upstream-model",
			Output: []privateHTTPResponseItem{{Type: "output_text", Text: "ok"}},
			Runtime: privateHTTPRuntimeMetadata{
				Status: "ready",
			},
		})
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL:         upstream.URL,
		PublicModelID:   privateEchoModelID,
		UpstreamModelID: "upstream-model",
		AuthHeader:      "X-API-Key",
		AuthToken:       "plain-key",
		Client:          upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	_, err = adapter.Generate(context.Background(), Request{
		Model: privateEchoModelID,
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello private"}}}},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if gotHeader != "plain-key" {
		t.Fatalf("expected custom auth header, got %q", gotHeader)
	}
}

func TestConfiguredRegistryUsesPrivateHTTPAdapter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "unused", http.StatusNotImplemented)
	}))
	defer upstream.Close()

	registry := ConfiguredRegistry(RegistryConfig{
		PrivateBaseURL: upstream.URL,
		HTTPClient:     upstream.Client(),
	})
	models := registry.Models(context.Background())

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	if models[1].Provider != "private-http" || models[1].ID != privateEchoModelID {
		t.Fatalf("expected configured private http model, got %#v", models[1])
	}

	if models[1].Metadata["discovery"] != "fallback" {
		t.Fatalf("expected fallback metadata, got %#v", models[1].Metadata)
	}
}
