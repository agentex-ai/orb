package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	if len(response.Data) != 1 || response.Data[0].ID != "orb/example-text" {
		t.Fatalf("unexpected model list: %#v", response.Data)
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

func TestResponsesReturnsStubPayload(t *testing.T) {
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

	if response.Runtime.Adapter != "stub" || !response.Runtime.MemoryApplied {
		t.Fatalf("unexpected runtime metadata: %#v", response.Runtime)
	}

	if len(response.Output) != 1 || !strings.Contains(response.Output[0].Text, "hello orb") {
		t.Fatalf("unexpected output payload: %#v", response.Output)
	}
}
