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

	var response orb.Response
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

	var response orb.Response
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

func TestResponseByIDReturnsStoredResponse(t *testing.T) {
	handler := NewServer()

	createBody := []byte(`{
		"model":"orb/example-text",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello orb"}]}]
	}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(createBody))
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d", createRecorder.Code)
	}

	var created orb.Response
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("expected valid create JSON response: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/responses/"+created.ID, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected json content type, got %q", contentType)
	}

	var response orb.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.ID != created.ID || response.Model != "orb/example-text" {
		t.Fatalf("unexpected retrieved response payload: %#v", response)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "Echo: hello orb" {
		t.Fatalf("unexpected retrieved output payload: %#v", response.Output)
	}
}

func TestResponseByIDReturnsNotFoundForUnknownResponse(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), `"not_found"`) {
		t.Fatalf("expected not_found error, got %s", recorder.Body.String())
	}

	if !strings.Contains(recorder.Body.String(), `"persistence":"memory_only"`) {
		t.Fatalf("expected memory_only detail, got %s", recorder.Body.String())
	}
}

func TestResponseByIDRejectsBlankIdentifier(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/responses/%20", nil)
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), `"response_id is required"`) {
		t.Fatalf("expected response_id validation error, got %s", recorder.Body.String())
	}
}

func TestMemoryQueryReturnsScopedMatches(t *testing.T) {
	handler := NewServer()

	createBody := []byte(`{
		"model":"orb/example-text",
		"input":[{"role":"user","content":[{"type":"input_text","text":"deployment note alpha"}]}],
		"memory":{"enabled":true,"scope":"workspace:docs"}
	}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(createBody))
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d", createRecorder.Code)
	}

	queryBody := []byte(`{
		"scope":"workspace:docs",
		"query":"alpha",
		"limit":5
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/memory/query", bytes.NewReader(queryBody))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response MemoryQueryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.Object != "list" || len(response.Data) != 1 {
		t.Fatalf("unexpected memory query payload: %#v", response)
	}

	if response.Data[0].Scope != "workspace:docs" || response.Data[0].InputText != "deployment note alpha" {
		t.Fatalf("unexpected memory item: %#v", response.Data[0])
	}
}

func TestMemoryQueryRequiresScope(t *testing.T) {
	body := []byte(`{"query":"alpha"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/memory/query", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), `"scope is required"`) {
		t.Fatalf("expected scope validation error, got %s", recorder.Body.String())
	}
}

func TestRunsReturnsEchoPayload(t *testing.T) {
	body := []byte(`{
		"model":"orb/example-text",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello run"}]}],
		"memory":{"enabled":true,"scope":"workspace:runs"}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response orb.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.Object != "response" || response.Model != "orb/example-text" {
		t.Fatalf("unexpected run response payload: %#v", response)
	}

	if response.Runtime.Adapter != "echo" || !response.Runtime.MemoryApplied {
		t.Fatalf("unexpected run runtime metadata: %#v", response.Runtime)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "Echo: hello run" {
		t.Fatalf("unexpected run output payload: %#v", response.Output)
	}
}

func TestRunsStreamsOpenAIAdapterEvents(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("expected /v1/responses, got %s", request.URL.Path)
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_run_stream"}}`,
			"",
			"event: response.output_text.delta",
			`data: {"type":"response.output_text.delta","delta":"hello run"}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"id":"resp_run_stream","status":"completed"}}`,
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	service := orb.NewService(orb.ConfiguredRegistry(orb.RegistryConfig{
		OpenAIBaseURL: upstream.URL + "/v1",
		OpenAIAPIKey:  "test-key",
		OpenAIModelID: "gpt-5-mini",
		HTTPClient:    upstream.Client(),
	}))

	body := []byte(`{
		"model":"orb/openai/gpt-5-mini",
		"stream":true,
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello run stream"}]}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServerWithService(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("expected sse content type, got %q", contentType)
	}

	responseBody := recorder.Body.String()
	if !strings.Contains(responseBody, "event: response.output_text.delta") {
		t.Fatalf("expected response.output_text.delta event, got %s", responseBody)
	}

	if !strings.Contains(responseBody, `"delta":"hello run"`) {
		t.Fatalf("expected run delta payload, got %s", responseBody)
	}
}

func TestModelsIncludesConfiguredOpenAIModel(t *testing.T) {
	service := orb.NewService(orb.ConfiguredRegistry(orb.RegistryConfig{
		OpenAIAPIKey:  "test-key",
		OpenAIModelID: "gpt-5-mini",
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

	if response.Data[1].ID != "orb/openai/gpt-5-mini" || response.Data[1].Provider != "openai" || response.Data[1].Deployment != "hosted" {
		t.Fatalf("unexpected openai model payload: %#v", response.Data[1])
	}
}

func TestResponsesRoutesToOpenAIAdapter(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("expected /v1/responses, got %s", request.URL.Path)
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
							"text": "hosted route ok",
						},
					},
				},
			},
			"usage": map[string]any{
				"input_tokens":  8,
				"output_tokens": 3,
				"total_tokens":  11,
			},
		})
	}))
	defer upstream.Close()

	service := orb.NewService(orb.ConfiguredRegistry(orb.RegistryConfig{
		OpenAIBaseURL: upstream.URL + "/v1",
		OpenAIAPIKey:  "test-key",
		OpenAIModelID: "gpt-5-mini",
		HTTPClient:    upstream.Client(),
	}))

	body := []byte(`{
		"model":"orb/openai/gpt-5-mini",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello hosted orb"}]}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServerWithService(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response orb.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.Model != "orb/openai/gpt-5-mini" || response.Runtime.Adapter != "openai" || response.Runtime.Deployment != "hosted" {
		t.Fatalf("unexpected hosted response payload: %#v", response)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "hosted route ok" {
		t.Fatalf("unexpected hosted output payload: %#v", response.Output)
	}
}

func TestResponsesStreamsOpenAIAdapterEvents(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("expected /v1/responses, got %s", request.URL.Path)
		}

		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_stream"}}`,
			"",
			"event: response.output_text.delta",
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			"",
			"event: response.completed",
			`data: {"type":"response.completed","response":{"id":"resp_stream","status":"completed"}}`,
			"",
		}, "\n")))
	}))
	defer upstream.Close()

	service := orb.NewService(orb.ConfiguredRegistry(orb.RegistryConfig{
		OpenAIBaseURL: upstream.URL + "/v1",
		OpenAIAPIKey:  "test-key",
		OpenAIModelID: "gpt-5-mini",
		HTTPClient:    upstream.Client(),
	}))

	body := []byte(`{
		"model":"orb/openai/gpt-5-mini",
		"stream":true,
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello hosted orb"}]}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServerWithService(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("expected sse content type, got %q", contentType)
	}

	responseBody := recorder.Body.String()
	if !strings.Contains(responseBody, "event: response.created") {
		t.Fatalf("expected response.created event, got %s", responseBody)
	}

	if !strings.Contains(responseBody, "event: response.output_text.delta") {
		t.Fatalf("expected response.output_text.delta event, got %s", responseBody)
	}

	if !strings.Contains(responseBody, `"delta":"hello"`) {
		t.Fatalf("expected delta payload, got %s", responseBody)
	}
}

func TestResponsesStreamingReturnsSSEErrorForUnsupportedModel(t *testing.T) {
	body := []byte(`{
		"model":"orb/example-text",
		"stream":true,
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello orb"}]}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("expected sse content type, got %q", contentType)
	}

	if !strings.Contains(recorder.Body.String(), "event: error") || !strings.Contains(recorder.Body.String(), `"streaming is not supported`) {
		t.Fatalf("expected streaming error event, got %s", recorder.Body.String())
	}
}

func TestResponsesStreamsPrivateHTTPAdapterEvents(t *testing.T) {
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
						"capabilities": []string{"text"},
						"status":       "warm",
					},
				},
			})
		case "/v1/responses":
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
		"stream":true,
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello private orb"}]}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServerWithService(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("expected sse content type, got %q", contentType)
	}

	responseBody := recorder.Body.String()
	if !strings.Contains(responseBody, "event: response.output_text.delta") {
		t.Fatalf("expected response.output_text.delta event, got %s", responseBody)
	}

	if !strings.Contains(responseBody, `"delta":"private hello"`) {
		t.Fatalf("expected private delta payload, got %s", responseBody)
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

	var response orb.Response
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

func TestHarnessBundles(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/harness/bundles", nil)
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response HarnessBundleList
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if response.Object != "list" {
		t.Fatalf("expected list object, got %q", response.Object)
	}

	if len(response.Data) != 5 {
		t.Fatalf("expected 5 harness bundles, got %#v", response.Data)
	}

	if response.Data[0].ID != "core/exact_math" || !response.Data[0].DefaultEnabled {
		t.Fatalf("unexpected first bundle payload: %#v", response.Data[0])
	}

	if response.Data[3].ID != "memory/scope_recall" || response.Data[3].Category != "memory" {
		t.Fatalf("unexpected memory bundle payload: %#v", response.Data[3])
	}
}

func TestHarnessExperimentsCreateListAndFetch(t *testing.T) {
	handler := NewServer()
	body := []byte(`{
		"experiment_id":"exp_private_memory_001",
		"user_objective":{"primary":"balanced"},
		"bundles":["core/exact_math","memory/scope_recall"],
		"search_space":{"models":{"ids":["orb/example-text","orb/private-example-text"]},"memory":{"enabled":[false,true]}}
	}`)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/harness/experiments", bytes.NewReader(body))
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", createRecorder.Code)
	}

	var created HarnessExperimentDetail
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if created.ExperimentID != "exp_private_memory_001" || created.Object != "harness.experiment" {
		t.Fatalf("unexpected create payload: %#v", created)
	}

	if created.State != "queued" || created.Objective != "balanced" || created.Progress != 0 {
		t.Fatalf("unexpected create state: %#v", created)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/harness/experiments", nil)
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d", listRecorder.Code)
	}

	var listed HarnessExperimentList
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("expected valid list JSON response: %v", err)
	}

	if listed.Object != "list" || len(listed.Data) != 1 || listed.Data[0].ExperimentID != "exp_private_memory_001" {
		t.Fatalf("unexpected experiment list payload: %#v", listed)
	}

	if listed.Data[0].State != "completed" || listed.Data[0].Progress != 100 {
		t.Fatalf("expected completed list state, got %#v", listed.Data[0])
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/harness/experiments/exp_private_memory_001", nil)
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getRequest)

	if getRecorder.Code != http.StatusOK {
		t.Fatalf("expected get status 200, got %d", getRecorder.Code)
	}

	var fetched HarnessExperimentDetail
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("expected valid get JSON response: %v", err)
	}

	if fetched.ExperimentID != created.ExperimentID || fetched.State != "completed" {
		t.Fatalf("unexpected fetched experiment payload: %#v", fetched)
	}

	if fetched.Summary["mode"] != "runner" || fetched.Summary["total_candidates"].(float64) != 4 {
		t.Fatalf("unexpected summary payload: %#v", fetched.Summary)
	}

	if fetched.Summary["strict_promoted"].(float64) != 2 || fetched.Summary["rejected"].(float64) != 2 {
		t.Fatalf("unexpected promotion summary: %#v", fetched.Summary)
	}

	if len(fetched.Results) != 4 || fetched.Results[0]["promotion"] != "strict" {
		t.Fatalf("unexpected results payload: %#v", fetched.Results)
	}

	if fetched.Results[0]["mode"] == "stub" {
		t.Fatalf("expected real runner result, got %#v", fetched.Results[0])
	}

	if fetched.Artifacts["report_path"] != "/api/v1/harness/experiments/exp_private_memory_001/artifacts/report" {
		t.Fatalf("unexpected artifact paths: %#v", fetched.Artifacts)
	}
}

func TestHarnessExperimentSummaryArtifactReturnsJSON(t *testing.T) {
	handler := NewServer()
	body := []byte(`{
		"experiment_id":"exp_summary_001",
		"user_objective":{"primary":"balanced"},
		"bundles":["core/exact_math"],
		"search_space":{"models":{"ids":["orb/example-text","orb/private-example-text"]}}
	}`)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/harness/experiments", bytes.NewReader(body))
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected create status 202, got %d", createRecorder.Code)
	}

	artifactRequest := httptest.NewRequest(http.MethodGet, "/api/v1/harness/experiments/exp_summary_001/artifacts/summary", nil)
	artifactRecorder := httptest.NewRecorder()
	handler.ServeHTTP(artifactRecorder, artifactRequest)

	if artifactRecorder.Code != http.StatusOK {
		t.Fatalf("expected artifact status 200, got %d", artifactRecorder.Code)
	}

	if contentType := artifactRecorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected json content type, got %q", contentType)
	}

	var summary map[string]any
	if err := json.Unmarshal(artifactRecorder.Body.Bytes(), &summary); err != nil {
		t.Fatalf("expected valid summary JSON: %v", err)
	}

	if summary["object"] != "harness.summary" || summary["mode"] != "runner" {
		t.Fatalf("unexpected summary artifact payload: %#v", summary)
	}

	if summary["total_candidates"].(float64) != 2 {
		t.Fatalf("unexpected total candidate count: %#v", summary)
	}
}

func TestHarnessExperimentReportArtifactReturnsMarkdown(t *testing.T) {
	handler := NewServer()
	body := []byte(`{
		"experiment_id":"exp_report_001",
		"user_objective":{"primary":"balanced"},
		"bundles":["core/exact_math","memory/scope_recall"]
	}`)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/harness/experiments", bytes.NewReader(body))
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected create status 202, got %d", createRecorder.Code)
	}

	reportRequest := httptest.NewRequest(http.MethodGet, "/api/v1/harness/experiments/exp_report_001/artifacts/report", nil)
	reportRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reportRecorder, reportRequest)

	if reportRecorder.Code != http.StatusOK {
		t.Fatalf("expected report status 200, got %d", reportRecorder.Code)
	}

	if contentType := reportRecorder.Header().Get("Content-Type"); contentType != "text/markdown; charset=utf-8" {
		t.Fatalf("expected markdown content type, got %q", contentType)
	}

	bodyText := reportRecorder.Body.String()
	if !strings.Contains(bodyText, "# Harness Report") || !strings.Contains(bodyText, "exp_report_001") {
		t.Fatalf("unexpected report artifact body: %s", bodyText)
	}
}

func TestHarnessExperimentsRejectDuplicateExperimentID(t *testing.T) {
	handler := NewServer()
	body := []byte(`{
		"experiment_id":"exp_duplicate_001",
		"user_objective":{"primary":"balanced"},
		"bundles":["core/exact_math"]
	}`)

	firstRequest := httptest.NewRequest(http.MethodPost, "/api/v1/harness/experiments", bytes.NewReader(body))
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, firstRequest)

	if firstRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected first status 202, got %d", firstRecorder.Code)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/api/v1/harness/experiments", bytes.NewReader(body))
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)

	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf("expected duplicate status 409, got %d", secondRecorder.Code)
	}

	if !strings.Contains(secondRecorder.Body.String(), `"already_exists"`) {
		t.Fatalf("expected already_exists error, got %s", secondRecorder.Body.String())
	}
}

func TestHarnessExperimentsValidateRequiredFields(t *testing.T) {
	body := []byte(`{"experiment_id":"exp_invalid_001","bundles":["core/exact_math"]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/harness/experiments", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), `"user_objective.primary is required"`) {
		t.Fatalf("expected objective validation error, got %s", recorder.Body.String())
	}
}

func TestHarnessExperimentNotFound(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/harness/experiments/exp_missing", nil)
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}

	if !strings.Contains(recorder.Body.String(), `"experiment_not_found"`) {
		t.Fatalf("expected experiment_not_found error, got %s", recorder.Body.String())
	}
}

func TestHarnessExperimentUnknownArtifactReturnsNotFound(t *testing.T) {
	handler := NewServer()
	body := []byte(`{
		"experiment_id":"exp_unknown_artifact_001",
		"user_objective":{"primary":"balanced"},
		"bundles":["core/exact_math"]
	}`)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/harness/experiments", bytes.NewReader(body))
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected create status 202, got %d", createRecorder.Code)
	}

	artifactRequest := httptest.NewRequest(http.MethodGet, "/api/v1/harness/experiments/exp_unknown_artifact_001/artifacts/not-real", nil)
	artifactRecorder := httptest.NewRecorder()
	handler.ServeHTTP(artifactRecorder, artifactRequest)

	if artifactRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected artifact status 404, got %d", artifactRecorder.Code)
	}

	if !strings.Contains(artifactRecorder.Body.String(), `"artifact_not_found"`) {
		t.Fatalf("expected artifact_not_found error, got %s", artifactRecorder.Body.String())
	}
}
