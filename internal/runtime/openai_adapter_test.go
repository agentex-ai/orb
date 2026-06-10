package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIAdapterModels(t *testing.T) {
	adapter, err := NewOpenAIAdapter(OpenAIAdapterConfig{
		APIKey:  "test-key",
		ModelID: "gpt-5-mini",
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	models := adapter.Models(context.Background())
	if len(models) != 1 {
		t.Fatalf("expected one model, got %#v", models)
	}

	model := models[0]
	if model.ID != "orb/openai/gpt-5-mini" || model.Provider != "openai" || model.Deployment != "hosted" {
		t.Fatalf("unexpected model payload: %#v", model)
	}

	if model.Metadata["upstream_model_id"] != "gpt-5-mini" {
		t.Fatalf("unexpected model metadata: %#v", model.Metadata)
	}
}

func TestOpenAIAdapterGenerate(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("expected /v1/responses, got %s", request.URL.Path)
		}

		gotAuth = request.Header.Get("Authorization")
		if err := json.NewDecoder(request.Body).Decode(&gotBody); err != nil {
			t.Fatalf("expected valid upstream body: %v", err)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id":     "resp_openai",
			"object": "response",
			"model":  "gpt-5-mini",
			"status": "completed",
			"output": []map[string]any{
				{
					"type": "message",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": "Hello from OpenAI",
						},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 4,
				"total_tokens":  14,
			},
		})
	}))
	defer upstream.Close()

	adapter, err := NewOpenAIAdapter(OpenAIAdapterConfig{
		BaseURL: upstream.URL + "/v1",
		APIKey:  "test-key",
		ModelID: "gpt-5-mini",
		Client:  upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	response, err := adapter.Generate(context.Background(), Request{
		Model: "orb/openai/gpt-5-mini",
		Input: []InputMessage{
			{
				Role: "user",
				Content: []InputContent{
					{Type: "input_text", Text: "hello"},
				},
			},
		},
		Metadata: map[string]any{
			"request_source": "test-suite",
		},
		Settings: map[string]any{
			"temperature": 0.2,
		},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Fatalf("expected bearer auth, got %q", gotAuth)
	}

	if gotBody["model"] != "gpt-5-mini" {
		t.Fatalf("unexpected request model payload: %#v", gotBody)
	}

	if gotBody["temperature"] != 0.2 {
		t.Fatalf("expected forwarded settings, got %#v", gotBody)
	}

	metadata, ok := gotBody["metadata"].(map[string]any)
	if !ok || metadata["request_source"] != "test-suite" {
		t.Fatalf("expected forwarded metadata, got %#v", gotBody["metadata"])
	}

	if response.Model != "orb/openai/gpt-5-mini" || response.Runtime.Adapter != "openai" || response.Runtime.Deployment != "hosted" {
		t.Fatalf("unexpected response payload: %#v", response)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "Hello from OpenAI" {
		t.Fatalf("unexpected output payload: %#v", response.Output)
	}

	if response.Usage.TotalTokens != 14 {
		t.Fatalf("unexpected usage payload: %#v", response.Usage)
	}
}

func TestOpenAIAdapterGenerateMapsProviderErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, `{"error":{"message":"bad request","type":"invalid_request_error","param":"input","code":"bad_input"}}`, http.StatusBadRequest)
	}))
	defer upstream.Close()

	adapter, err := NewOpenAIAdapter(OpenAIAdapterConfig{
		BaseURL: upstream.URL,
		APIKey:  "test-key",
		ModelID: "gpt-5-mini",
		Client:  upstream.Client(),
	})
	if err != nil {
		t.Fatalf("expected adapter config success, got %v", err)
	}

	_, err = adapter.Generate(context.Background(), Request{
		Model: "orb/openai/gpt-5-mini",
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello"}}}},
	})
	if err == nil {
		t.Fatal("expected provider error")
	}

	apiErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected runtime error, got %#v", err)
	}

	if apiErr.Code != "invalid_argument" || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected mapped error: %#v", apiErr)
	}

	if apiErr.Details["provider_type"] != "invalid_request_error" || apiErr.Details["provider_param"] != "input" {
		t.Fatalf("unexpected error details: %#v", apiErr.Details)
	}
}

func TestConfiguredRegistryAddsOpenAIAdapter(t *testing.T) {
	registry := ConfiguredRegistry(RegistryConfig{
		OpenAIAPIKey:  "test-key",
		OpenAIModelID: "gpt-5-mini",
	})

	models := registry.Models(context.Background())
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %#v", models)
	}

	if models[1].ID != "orb/openai/gpt-5-mini" || models[1].Provider != "openai" {
		t.Fatalf("unexpected openai model payload: %#v", models[1])
	}
}
