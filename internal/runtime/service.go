package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

type Service struct {
	registry *Registry
}

type Adapter interface {
	Name() string
	Models(context.Context) []Model
	Generate(context.Context, Request) (Response, error)
}

type Model struct {
	ID           string
	Object       string
	Provider     string
	Deployment   string
	Capabilities []string
	Status       string
	Metadata     map[string]any
}

type Request struct {
	Model    string
	Input    []InputMessage
	Memory   *MemoryRequest
	Metadata map[string]any
	Settings map[string]any
}

type InputMessage struct {
	Role    string
	Content []InputContent
}

type InputContent struct {
	Type string
	Text string
}

type MemoryRequest struct {
	Enabled bool
	Scope   string
}

type Response struct {
	ID      string
	Object  string
	Model   string
	Output  []OutputItem
	Usage   Usage
	Runtime Runtime
}

type OutputItem struct {
	Type string
	Text string
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type Runtime struct {
	Adapter       string
	Deployment    string
	MemoryApplied bool
	Status        string
}

type Error struct {
	Code       string
	Message    string
	Details    map[string]any
	StatusCode int
}

func (e *Error) Error() string {
	return e.Message
}

func NewService(registry *Registry) *Service {
	if registry == nil {
		registry = DefaultRegistry()
	}

	return &Service{registry: registry}
}

func (s *Service) Models(ctx context.Context) []Model {
	models := s.registry.Models(ctx)
	cloned := make([]Model, len(models))
	copy(cloned, models)
	return cloned
}

func (s *Service) CreateResponse(ctx context.Context, request Request) (Response, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}

	adapter, model, err := s.registry.AdapterForModel(ctx, request.Model)
	if err != nil {
		return Response{}, err
	}

	response, err := adapter.Generate(ctx, request)
	if err != nil {
		return Response{}, normalizeError(err)
	}

	if response.ID == "" {
		response.ID = newResponseID()
	}
	if response.Object == "" {
		response.Object = "response"
	}
	if response.Model == "" {
		response.Model = model.ID
	}
	if response.Runtime.Adapter == "" {
		response.Runtime.Adapter = adapter.Name()
	}
	if response.Runtime.Deployment == "" {
		response.Runtime.Deployment = model.Deployment
	}
	response.Runtime.MemoryApplied = request.Memory != nil && request.Memory.Enabled
	if response.Runtime.Status == "" {
		response.Runtime.Status = model.Status
	}

	return response, nil
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.Model) == "" {
		return &Error{
			Code:       "invalid_argument",
			Message:    "model is required",
			Details:    map[string]any{"field": "model"},
			StatusCode: http.StatusBadRequest,
		}
	}

	if len(request.Input) == 0 {
		return &Error{
			Code:       "invalid_argument",
			Message:    "input is required",
			Details:    map[string]any{"field": "input"},
			StatusCode: http.StatusBadRequest,
		}
	}

	return nil
}

func normalizeError(err error) error {
	apiErr, ok := err.(*Error)
	if ok {
		if apiErr.StatusCode == 0 {
			apiErr.StatusCode = statusCodeFor(apiErr.Code)
		}
		return apiErr
	}

	return &Error{
		Code:       "backend_unavailable",
		Message:    "adapter execution failed",
		StatusCode: http.StatusBadGateway,
	}
}

func statusCodeFor(code string) int {
	switch code {
	case "invalid_argument":
		return http.StatusBadRequest
	case "not_found":
		return http.StatusNotFound
	case "unauthorized":
		return http.StatusUnauthorized
	case "forbidden":
		return http.StatusForbidden
	case "rate_limited":
		return http.StatusTooManyRequests
	case "backend_unavailable":
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func firstInputText(messages []InputMessage) string {
	for _, message := range messages {
		for _, content := range message.Content {
			if content.Type != "input_text" {
				continue
			}

			text := strings.TrimSpace(content.Text)
			if text == "" {
				continue
			}

			if len(text) > 120 {
				return text[:120] + "..."
			}

			return text
		}
	}

	return ""
}

func newResponseID() string {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "resp_fallback"
	}

	return "resp_" + hex.EncodeToString(randomBytes)
}
