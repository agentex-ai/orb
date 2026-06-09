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
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("expected /v1/responses, got %s", request.URL.Path)
		}

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
}
