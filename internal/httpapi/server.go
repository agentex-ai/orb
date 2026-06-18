package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	orb "github.com/agentex-ai/orb/internal/runtime"
)

type ModelList struct {
	Object string      `json:"object"`
	Data   []orb.Model `json:"data"`
}

type MemoryQueryResponse struct {
	Object string             `json:"object"`
	Data   []orb.MemoryResult `json:"data"`
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
	service       *orb.Service
	harness       *harnessRegistry
	clientProxy   *clientProxyIntegration
	publicBaseURL string
}

type ServerConfig struct {
	Service               *orb.Service
	ClientProxyConfigPath string
	PublicBaseURL         string
}

func NewServer() http.Handler {
	return NewServerWithService(nil)
}

func NewServerWithService(service *orb.Service) http.Handler {
	return NewServerWithConfig(ServerConfig{
		Service:               service,
		ClientProxyConfigPath: os.Getenv("ORB_CLIENT_PROXY_CONFIG"),
		PublicBaseURL:         os.Getenv("ORB_PUBLIC_BASE_URL"),
	})
}

func NewServerWithConfig(config ServerConfig) http.Handler {
	service := config.Service
	if service == nil {
		service = orb.NewService(orb.DefaultRegistry())
	}

	api := server{
		service:       service,
		harness:       newHarnessRegistry(),
		clientProxy:   newClientProxyIntegration(config.ClientProxyConfigPath),
		publicBaseURL: strings.TrimRight(strings.TrimSpace(config.PublicBaseURL), "/"),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", api.handleModels)
	mux.HandleFunc("POST /v1/responses", api.handleExecution)
	mux.HandleFunc("GET /v1/responses/{response_id}", api.handleResponseByID)
	mux.HandleFunc("POST /v1/messages", api.handleAnthropicMessages)
	mux.HandleFunc("POST /v1/memory/query", api.handleMemoryQuery)
	mux.HandleFunc("POST /v1/runs", api.handleExecution)
	mux.HandleFunc("GET /api/v1/client-proxy/profiles", api.handleClientProxyProfiles)
	mux.HandleFunc("POST /api/v1/client-proxy/activate", api.handleClientProxyActivate)
	mux.HandleFunc("POST /api/v1/client-proxy/proxy", api.handleClientProxyProxy)
	mux.HandleFunc("GET /api/v1/harness/bundles", api.handleHarnessBundles)
	mux.HandleFunc("POST /api/v1/harness/experiments", api.handleHarnessCreateExperiment)
	mux.HandleFunc("GET /api/v1/harness/experiments", api.handleHarnessListExperiments)
	mux.HandleFunc("GET /api/v1/harness/experiments/{experiment_id}/artifacts/{artifact}", api.handleHarnessExperimentArtifact)
	mux.HandleFunc("GET /api/v1/harness/experiments/{experiment_id}", api.handleHarnessExperiment)
	return mux
}

func (s server) handleModels(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, ModelList{
		Object: "list",
		Data:   s.service.Models(request.Context()),
	})
}

func (s server) handleExecution(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	var payload orb.Request
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "request body must be valid JSON",
		})
		return
	}

	if payload.Stream {
		flusher, ok := writer.(http.Flusher)
		if !ok {
			writeError(writer, http.StatusInternalServerError, APIError{
				Code:    "internal_error",
				Message: "streaming is not supported by this server",
			})
			return
		}

		writeSSEHeaders(writer)
		err := s.service.StreamResponse(request.Context(), payload, func(event orb.StreamEvent) error {
			if err := writeSSEEvent(writer, event); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		})
		if err != nil {
			writeStreamError(writer, err)
			flusher.Flush()
		}
		return
	}

	response, err := s.service.CreateResponse(request.Context(), payload)
	if err != nil {
		writeServiceError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, response)
}

func (s server) handleResponseByID(writer http.ResponseWriter, request *http.Request) {
	responseID := strings.TrimSpace(request.PathValue("response_id"))
	if responseID == "" {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "response_id is required",
			Details: map[string]any{"field": "response_id"},
		})
		return
	}

	response, err := s.service.GetResponse(request.Context(), responseID)
	if err != nil {
		writeServiceError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, response)
}

func (s server) handleMemoryQuery(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	var payload orb.MemoryQuery
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "request body must be valid JSON",
		})
		return
	}

	results, err := s.service.QueryMemory(request.Context(), payload)
	if err != nil {
		writeServiceError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, MemoryQueryResponse{
		Object: "list",
		Data:   results,
	})
}

func writeSSEHeaders(writer http.ResponseWriter) {
	headers := writer.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
}

func writeSSEEvent(writer http.ResponseWriter, event orb.StreamEvent) error {
	payload, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "event: %s\n", strings.TrimSpace(event.Type)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "data: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

func writeStreamError(writer http.ResponseWriter, err error) {
	var apiErr *orb.Error
	if errors.As(err, &apiErr) {
		_ = writeSSEEvent(writer, orb.StreamEvent{
			Type: "error",
			Data: APIError{
				Code:    apiErr.Code,
				Message: apiErr.Message,
				Details: apiErr.Details,
			},
		})
		return
	}

	_ = writeSSEEvent(writer, orb.StreamEvent{
		Type: "error",
		Data: APIError{
			Code:    "internal_error",
			Message: "unexpected runtime error",
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
