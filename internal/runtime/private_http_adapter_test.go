package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type modelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

func TestPrivateHTTPAdapterGenerate(t *testing.T) {
	var gotBody Request
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
		_ = json.NewEncoder(writer).Encode(Response{
			ID:     "resp_upstream",
			Object: "response",
			Model:  "upstream-model",
			Output: []OutputItem{{Type: "output_text", Text: "Upstream: hello private"}},
			Usage: Usage{
				InputTokens:  12,
				OutputTokens: 4,
				TotalTokens:  16,
			},
			Runtime: Runtime{
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

func TestPrivateHTTPAdapterGenerateUsesDiscoveredModelRoute(t *testing.T) {
	var gotBody Request
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(struct {
				Object string  `json:"object"`
				Data   []Model `json:"data"`
			}{
				Object: "list",
				Data: []Model{
					{
						ID:           "qwen3-32b",
						Object:       "model",
						Provider:     "vllm",
						Deployment:   "private",
						Capabilities: []string{"text", "tools"},
						Status:       "warm",
					},
				},
			})
		case "/v1/responses":
			if err := json.NewDecoder(request.Body).Decode(&gotBody); err != nil {
				t.Fatalf("expected valid upstream body: %v", err)
			}

			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(Response{
				ID:     "resp_multi",
				Object: "response",
				Model:  "qwen3-32b",
				Output: []OutputItem{{Type: "output_text", Text: "upstream multi ok"}},
				Runtime: Runtime{
					Status: "warm",
				},
			})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL: upstream.URL,
		Client:  upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	response, err := adapter.Generate(context.Background(), Request{
		Model: "orb/private/qwen3-32b",
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello private"}}}},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if gotBody.Model != "qwen3-32b" {
		t.Fatalf("expected discovered upstream model id, got %#v", gotBody)
	}

	if response.Model != "orb/private/qwen3-32b" || response.Runtime.Adapter != "private-http" {
		t.Fatalf("unexpected routed response payload: %#v", response)
	}
}

func TestPrivateHTTPAdapterGenerateStream(t *testing.T) {
	var gotAccept string
	var gotBody Request

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("expected /v1/responses, got %s", request.URL.Path)
		}

		gotAccept = request.Header.Get("Accept")
		if err := json.NewDecoder(request.Body).Decode(&gotBody); err != nil {
			t.Fatalf("expected valid upstream body: %v", err)
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_private_stream"}}`,
			"",
			"event: response.output_text.delta",
			`data: {"type":"response.output_text.delta","delta":"private hello"}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"id":"resp_private_stream","status":"completed"}}`,
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL:         upstream.URL,
		PublicModelID:   privateEchoModelID,
		UpstreamModelID: "upstream-model",
		Client:          upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	events := make([]StreamEvent, 0)
	err = adapter.GenerateStream(context.Background(), Request{
		Model:  privateEchoModelID,
		Stream: true,
		Input:  []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello private"}}}},
	}, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("expected stream success, got %v", err)
	}

	if gotAccept != "text/event-stream" {
		t.Fatalf("expected SSE accept header, got %q", gotAccept)
	}

	if gotBody.Model != "upstream-model" || !gotBody.Stream {
		t.Fatalf("expected streamed upstream request, got %#v", gotBody)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 stream events, got %#v", events)
	}

	if events[1].Type != "response.output_text.delta" {
		t.Fatalf("unexpected stream events: %#v", events)
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
		_ = json.NewEncoder(writer).Encode(modelList{
			Object: "list",
			Data: []Model{
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

func TestPrivateHTTPAdapterModelsDiscoversMultipleUpstreamModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("expected /v1/models, got %s", request.URL.Path)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(modelList{
			Object: "list",
			Data: []Model{
				{
					ID:           "qwen3-32b",
					Object:       "model",
					Provider:     "vllm",
					Deployment:   "private",
					Capabilities: []string{"text"},
					Status:       "warm",
				},
				{
					ID:           "orb/custom-tools",
					Object:       "model",
					Provider:     "sglang",
					Deployment:   "private",
					Capabilities: []string{"text", "tools"},
					Status:       "ready",
				},
			},
		})
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL: upstream.URL,
		Client:  upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	models := adapter.Models(context.Background())
	if len(models) != 2 {
		t.Fatalf("expected two discovered models, got %#v", models)
	}

	if models[0].ID != "orb/private/qwen3-32b" || models[0].Metadata["upstream_model_id"] != "qwen3-32b" {
		t.Fatalf("unexpected first discovered model: %#v", models[0])
	}

	if models[1].ID != "orb/private/custom-tools" || models[1].Metadata["upstream_provider"] != "sglang" {
		t.Fatalf("unexpected second discovered model: %#v", models[1])
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

func TestPrivateHTTPAdapterModelsReturnsNoModelsWhenAutoDiscoveryFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, `{"error":{"code":"backend_unavailable","message":"no models"}}`, http.StatusBadGateway)
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL: upstream.URL,
		Client:  upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	models := adapter.Models(context.Background())
	if len(models) != 0 {
		t.Fatalf("expected no auto-discovered models on discovery failure, got %#v", models)
	}
}

func TestPrivateHTTPAdapterGenerateWithCustomAuthHeader(t *testing.T) {
	var gotHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotHeader = request.Header.Get("X-API-Key")
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(Response{
			ID:     "resp_upstream",
			Object: "response",
			Model:  "upstream-model",
			Output: []OutputItem{{Type: "output_text", Text: "ok"}},
			Runtime: Runtime{
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

func TestPrivateHTTPAdapterGenerateReturnsNotFoundForUnknownDiscoveredModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("expected /v1/models, got %s", request.URL.Path)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(modelList{
			Object: "list",
			Data: []Model{
				{ID: "qwen3-32b", Object: "model", Provider: "vllm", Deployment: "private", Status: "ready"},
			},
		})
	}))
	defer upstream.Close()

	adapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL: upstream.URL,
		Client:  upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	_, err = adapter.Generate(context.Background(), Request{
		Model: "orb/private/unknown",
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello"}}}},
	})
	if err == nil {
		t.Fatal("expected not found error")
	}

	apiErr, ok := err.(*Error)
	if !ok || apiErr.Code != "not_found" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestConfiguredRegistryUsesPrivateHTTPAdapterSingleModelOverride(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "unused", http.StatusNotImplemented)
	}))
	defer upstream.Close()

	registry := ConfiguredRegistry(RegistryConfig{
		PrivateBaseURL: upstream.URL,
		PrivateModelID: privateEchoModelID,
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

func TestConfiguredRegistryUsesPrivateHTTPAdapterDiscoveredModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("expected /v1/models, got %s", request.URL.Path)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(modelList{
			Object: "list",
			Data: []Model{
				{ID: "qwen3-32b", Object: "model", Provider: "vllm", Deployment: "private", Status: "warm"},
				{ID: "llama3-70b", Object: "model", Provider: "vllm", Deployment: "private", Status: "ready"},
			},
		})
	}))
	defer upstream.Close()

	registry := ConfiguredRegistry(RegistryConfig{
		PrivateBaseURL: upstream.URL,
		HTTPClient:     upstream.Client(),
	})
	models := registry.Models(context.Background())

	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %#v", models)
	}

	if models[1].ID != "orb/private/qwen3-32b" || models[2].ID != "orb/private/llama3-70b" {
		t.Fatalf("unexpected discovered registry models: %#v", models)
	}
}
