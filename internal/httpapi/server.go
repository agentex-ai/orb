package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	orb "github.com/agentex-ai/orb/internal/runtime"
)

type Model struct {
	ID           string   `json:"id"`
	Object       string   `json:"object"`
	Provider     string   `json:"provider"`
	Deployment   string   `json:"deployment"`
	Capabilities []string `json:"capabilities"`
	Status       string   `json:"status,omitempty"`
}

type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type ResponseRequest struct {
	Model    string                 `json:"model"`
	Input    []InputMessage         `json:"input"`
	Memory   *MemoryRequest         `json:"memory,omitempty"`
	Metadata map[string]any         `json:"metadata,omitempty"`
	Settings map[string]interface{} `json:"settings,omitempty"`
}

type InputMessage struct {
	Role    string         `json:"role"`
	Content []InputContent `json:"content"`
}

type InputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type MemoryRequest struct {
	Enabled bool   `json:"enabled"`
	Scope   string `json:"scope,omitempty"`
}

type ResponseEnvelope struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Model   string       `json:"model"`
	Output  []OutputItem `json:"output"`
	Usage   Usage        `json:"usage"`
	Runtime Runtime      `json:"runtime"`
}

type OutputItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type Runtime struct {
	Adapter       string `json:"adapter"`
	Deployment    string `json:"deployment"`
	MemoryApplied bool   `json:"memory_applied"`
	Status        string `json:"status"`
}

type ErrorEnvelope struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type server struct {
	service *orb.Service
}

func NewServer() http.Handler {
	return NewServerWithService(orb.NewService(orb.NewEchoAdapter()))
}

func NewServerWithService(service *orb.Service) http.Handler {
	if service == nil {
		service = orb.NewService(orb.NewEchoAdapter())
	}

	api := server{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", api.handleModels)
	mux.HandleFunc("POST /v1/responses", api.handleResponses)
	return mux
}

func (s server) handleModels(writer http.ResponseWriter, request *http.Request) {
	models := s.service.Models(request.Context())
	data := make([]Model, 0, len(models))
	for _, model := range models {
		data = append(data, Model{
			ID:           model.ID,
			Object:       model.Object,
			Provider:     model.Provider,
			Deployment:   model.Deployment,
			Capabilities: model.Capabilities,
			Status:       model.Status,
		})
	}

	writeJSON(writer, http.StatusOK, ModelList{
		Object: "list",
		Data:   data,
	})
}

func (s server) handleResponses(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	var payload ResponseRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "request body must be valid JSON",
		})
		return
	}

	response, err := s.service.CreateResponse(request.Context(), orb.Request{
		Model:    payload.Model,
		Input:    toRuntimeInput(payload.Input),
		Memory:   toRuntimeMemory(payload.Memory),
		Metadata: payload.Metadata,
		Settings: payload.Settings,
	})
	if err != nil {
		writeServiceError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, ResponseEnvelope{
		ID:     response.ID,
		Object: response.Object,
		Model:  response.Model,
		Output: toHTTPOutput(response.Output),
		Usage: Usage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
			TotalTokens:  response.Usage.TotalTokens,
		},
		Runtime: Runtime{
			Adapter:       response.Runtime.Adapter,
			Deployment:    response.Runtime.Deployment,
			MemoryApplied: response.Runtime.MemoryApplied,
			Status:        response.Runtime.Status,
		},
	})
}

func writeError(writer http.ResponseWriter, status int, apiError APIError) {
	writeJSON(writer, status, ErrorEnvelope{Error: apiError})
}

func writeServiceError(writer http.ResponseWriter, err error) {
	var apiErr *orb.Error
	if errors.As(err, &apiErr) {
		writeError(writer, apiErr.StatusCode, APIError{
			Code:    apiErr.Code,
			Message: apiErr.Message,
			Details: apiErr.Details,
		})
		return
	}

	writeError(writer, http.StatusInternalServerError, APIError{
		Code:    "internal_error",
		Message: "unexpected runtime error",
	})
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(payload); err != nil {
		http.Error(writer, `{"error":{"code":"internal_error","message":"failed to encode response"}}`, http.StatusInternalServerError)
	}
}

func toRuntimeInput(messages []InputMessage) []orb.InputMessage {
	result := make([]orb.InputMessage, 0, len(messages))
	for _, message := range messages {
		content := make([]orb.InputContent, 0, len(message.Content))
		for _, item := range message.Content {
			content = append(content, orb.InputContent{
				Type: item.Type,
				Text: item.Text,
			})
		}

		result = append(result, orb.InputMessage{
			Role:    message.Role,
			Content: content,
		})
	}

	return result
}

func toRuntimeMemory(memory *MemoryRequest) *orb.MemoryRequest {
	if memory == nil {
		return nil
	}

	return &orb.MemoryRequest{
		Enabled: memory.Enabled,
		Scope:   memory.Scope,
	}
}

func toHTTPOutput(items []orb.OutputItem) []OutputItem {
	output := make([]OutputItem, 0, len(items))
	for _, item := range items {
		output = append(output, OutputItem{
			Type: item.Type,
			Text: item.Text,
		})
	}

	return output
}
