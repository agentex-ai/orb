package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orb "github.com/agentex-ai/orb/internal/runtime"
)

func TestModels(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response ModelList
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.Object != "list" {
		t.Fatalf("expected list object, got %q", response.Object)
	}

	if len(response.Data) != 2 || response.Data[0].ID != "orb/example-text" {
		t.Fatalf("unexpected model list: %#v", response.Data)
	}

	if response.Data[0].Provider != "echo" {
		t.Fatalf("expected echo provider, got %#v", response.Data[0])
	}

	if response.Data[1].ID != "orb/private-example-text" || response.Data[1].Provider != "private-echo" || response.Data[1].Deployment != "private" {
		t.Fatalf("expected private echo model, got %#v", response.Data[1])
	}

	if response.Data[1].Metadata != nil {
		t.Fatalf("did not expect metadata for bundled private echo model, got %#v", response.Data[1].Metadata)
	}
}

func TestResponsesRequiresModel(t *testing.T) {
	body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), `"model is required"`) {
		t.Fatalf("expected model validation error, got %s", recorder.Body.String())
	}
}

func TestResponsesReturnsEchoPayload(t *testing.T) {
	body := []byte(`{
		"model":"orb/example-text",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello orb"}]}],
		"memory":{"enabled":true,"scope":"workspace:test"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response ResponseEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.Object != "response" {
		t.Fatalf("expected response object, got %q", response.Object)
	}

	if response.Runtime.Adapter != "echo" || !response.Runtime.MemoryApplied {
		t.Fatalf("unexpected runtime metadata: %#v", response.Runtime)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "Echo: hello orb" {
		t.Fatalf("unexpected output payload: %#v", response.Output)
	}
}

func TestResponsesUnknownModel(t *testing.T) {
	body := []byte(`{
		"model":"orb/unknown",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello orb"}]}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), `"not_found"`) {
		t.Fatalf("expected not found error, got %s", recorder.Body.String())
	}
}

func TestResponsesReturnsPrivateEchoPayload(t *testing.T) {
	body := []byte(`{
		"model":"orb/private-example-text",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello private orb"}]}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response ResponseEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.Runtime.Adapter != "private-echo" || response.Runtime.Deployment != "private" {
		t.Fatalf("unexpected private runtime metadata: %#v", response.Runtime)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "Private Echo: hello private orb" {
		t.Fatalf("unexpected private output payload: %#v", response.Output)
	}
}

func TestModelsIncludesPrivateHTTPDiscoveryMetadata(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("expected /v1/models, got %s", request.URL.Path)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":           "upstream-private",
					"object":       "model",
					"provider":     "vllm",
					"deployment":   "private",
					"capabilities": []string{"text", "tools"},
					"status":       "warm",
				},
			},
		})
	}))
	defer upstream.Close()

	service := orb.NewService(orb.ConfiguredRegistry(orb.RegistryConfig{
		PrivateBaseURL:         upstream.URL,
		PrivateUpstreamModelID: "upstream-private",
		HTTPClient:             upstream.Client(),
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recorder := httptest.NewRecorder()

	NewServerWithService(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response ModelList
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if len(response.Data) != 2 {
		t.Fatalf("expected 2 models, got %#v", response.Data)
	}

	privateModel := response.Data[1]
	if privateModel.Provider != "private-http" || privateModel.Status != "warm" {
		t.Fatalf("unexpected private http model payload: %#v", privateModel)
	}

	if privateModel.Metadata["discovery"] != "upstream" || privateModel.Metadata["upstream_provider"] != "vllm" {
		t.Fatalf("expected upstream discovery metadata, got %#v", privateModel.Metadata)
	}
}

func TestModelsIncludesMultipleDiscoveredPrivateModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("expected /v1/models, got %s", request.URL.Path)
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":           "qwen3-32b",
					"object":       "model",
					"provider":     "vllm",
					"deployment":   "private",
					"capabilities": []string{"text"},
					"status":       "warm",
				},
				{
					"id":           "llama3-70b",
					"object":       "model",
					"provider":     "sglang",
					"deployment":   "private",
					"capabilities": []string{"text", "tools"},
					"status":       "ready",
				},
			},
		})
	}))
	defer upstream.Close()

	service := orb.NewService(orb.ConfiguredRegistry(orb.RegistryConfig{
		PrivateBaseURL: upstream.URL,
		HTTPClient:     upstream.Client(),
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recorder := httptest.NewRecorder()

	NewServerWithService(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response ModelList
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if len(response.Data) != 3 {
		t.Fatalf("expected 3 models, got %#v", response.Data)
	}

	if response.Data[1].ID != "orb/private/qwen3-32b" || response.Data[2].ID != "orb/private/llama3-70b" {
		t.Fatalf("unexpected discovered model ids: %#v", response.Data)
	}
}

func TestResponsesRoutesToDiscoveredPrivateHTTPModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"object": "list",
				"data": []map[string]any{
					{
						"id":           "qwen3-32b",
						"object":       "model",
						"provider":     "vllm",
						"deployment":   "private",
						"capabilities": []string{"text", "tools"},
						"status":       "warm",
					},
				},
			})
		case "/v1/responses":
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"id":     "resp_private",
				"object": "response",
				"model":  "qwen3-32b",
				"output": []map[string]any{
					{
						"type": "output_text",
						"text": "private runtime ok",
					},
				},
				"runtime": map[string]any{
					"status": "warm",
				},
			})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer upstream.Close()

	service := orb.NewService(orb.ConfiguredRegistry(orb.RegistryConfig{
		PrivateBaseURL: upstream.URL,
		HTTPClient:     upstream.Client(),
	}))

	body := []byte(`{
		"model":"orb/private/qwen3-32b",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello private orb"}]}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServerWithService(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response ResponseEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.Model != "orb/private/qwen3-32b" || response.Runtime.Adapter != "private-http" {
		t.Fatalf("unexpected discovered private response payload: %#v", response)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "private runtime ok" {
		t.Fatalf("unexpected discovered private output payload: %#v", response.Output)
	}
}
