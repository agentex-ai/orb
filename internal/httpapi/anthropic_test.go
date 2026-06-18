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

func TestAnthropicMessagesReturnsClaudeCompatibleResponse(t *testing.T) {
	body := []byte(`{
		"model":"orb/example-text",
		"system":"stay brief",
		"messages":[{"role":"user","content":[{"type":"text","text":"hello anthropic"}]}],
		"max_tokens":64
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServer().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response anthropicResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}
	if response.Type != "message" || response.Role != "assistant" || response.Model != "orb/example-text" {
		t.Fatalf("unexpected response envelope: %#v", response)
	}
	if len(response.Content) != 1 || !strings.Contains(response.Content[0].Text, "hello anthropic") {
		t.Fatalf("unexpected content: %#v", response.Content)
	}
}

func TestAnthropicMessagesStreamsTextDeltas(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("expected /v1/responses, got %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(strings.Join([]string{
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
		"messages":[{"role":"user","content":"hello stream"}]
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	NewServerWithService(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("expected sse content type, got %q", contentType)
	}
	responseBody := recorder.Body.String()
	if !strings.Contains(responseBody, "event: message_start") || !strings.Contains(responseBody, "event: content_block_delta") || !strings.Contains(responseBody, `"text":"hello"`) {
		t.Fatalf("expected anthropic stream events, got %s", responseBody)
	}
}
