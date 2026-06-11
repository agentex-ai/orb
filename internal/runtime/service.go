package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
)

type Service struct {
	registry    *Registry
	responsesMu sync.RWMutex
	responses   map[string]Response
	memoriesMu  sync.RWMutex
	memories    []MemoryResult
}

type Adapter interface {
	Name() string
	Models(context.Context) []Model
	Generate(context.Context, Request) (Response, error)
}

type StreamingAdapter interface {
	GenerateStream(context.Context, Request, func(StreamEvent) error) error
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
	Stream   bool
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

type MemoryQuery struct {
	Scope string
	Query string
	Limit int
}

type MemoryResult struct {
	ID         string
	Object     string
	Scope      string
	ResponseID string
	Model      string
	InputText  string
	OutputText string
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

type StreamEvent struct {
	Type string
	Data any
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

	return &Service{
		registry:  registry,
		responses: make(map[string]Response),
	}
}

func (s *Service) Models(ctx context.Context) []Model {
	models := s.registry.Models(ctx)
	return cloneModels(models)
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

	s.storeResponse(response)
	s.storeMemory(request, response)

	return response, nil
}

func (s *Service) StreamResponse(ctx context.Context, request Request, emit func(StreamEvent) error) error {
	if err := validateRequest(request); err != nil {
		return err
	}

	adapter, _, err := s.registry.AdapterForModel(ctx, request.Model)
	if err != nil {
		return err
	}

	streamingAdapter, ok := adapter.(StreamingAdapter)
	if !ok {
		return &Error{
			Code:       "invalid_argument",
			Message:    "streaming is not supported for model " + `"` + request.Model + `"`,
			Details:    map[string]any{"model": request.Model},
			StatusCode: http.StatusBadRequest,
		}
	}

	return normalizeError(streamingAdapter.GenerateStream(ctx, request, emit))
}

func (s *Service) GetResponse(_ context.Context, responseID string) (Response, error) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return Response{}, &Error{
			Code:       "invalid_argument",
			Message:    "response_id is required",
			Details:    map[string]any{"field": "response_id"},
			StatusCode: http.StatusBadRequest,
		}
	}

	s.responsesMu.RLock()
	response, ok := s.responses[responseID]
	s.responsesMu.RUnlock()
	if !ok {
		return Response{}, &Error{
			Code:    "not_found",
			Message: "response " + `"` + responseID + `"` + " is not available in the current runtime",
			Details: map[string]any{
				"response_id": responseID,
				"persistence": "memory_only",
			},
			StatusCode: http.StatusNotFound,
		}
	}

	return cloneResponse(response), nil
}

func (s *Service) QueryMemory(_ context.Context, query MemoryQuery) ([]MemoryResult, error) {
	scope := strings.TrimSpace(query.Scope)
	if scope == "" {
		return nil, &Error{
			Code:       "invalid_argument",
			Message:    "scope is required",
			Details:    map[string]any{"field": "scope"},
			StatusCode: http.StatusBadRequest,
		}
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}

	needle := strings.ToLower(strings.TrimSpace(query.Query))

	s.memoriesMu.RLock()
	defer s.memoriesMu.RUnlock()

	results := make([]MemoryResult, 0, min(limit, len(s.memories)))
	for i := len(s.memories) - 1; i >= 0 && len(results) < limit; i-- {
		item := s.memories[i]
		if item.Scope != scope {
			continue
		}

		if needle != "" {
			haystack := strings.ToLower(item.InputText + "\n" + item.OutputText)
			if !strings.Contains(haystack, needle) {
				continue
			}
		}

		results = append(results, cloneMemoryResult(item))
	}

	return results, nil
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

func (s *Service) storeResponse(response Response) {
	if s == nil || strings.TrimSpace(response.ID) == "" {
		return
	}

	s.responsesMu.Lock()
	s.responses[response.ID] = cloneResponse(response)
	s.responsesMu.Unlock()
}

func (s *Service) storeMemory(request Request, response Response) {
	if s == nil || request.Memory == nil || !request.Memory.Enabled {
		return
	}

	scope := strings.TrimSpace(request.Memory.Scope)
	if scope == "" {
		return
	}

	item := MemoryResult{
		ID:         "mem_" + response.ID,
		Object:     "memory_entry",
		Scope:      scope,
		ResponseID: response.ID,
		Model:      response.Model,
		InputText:  joinedInputText(request.Input),
		OutputText: joinedOutputText(response.Output),
	}

	s.memoriesMu.Lock()
	s.memories = append(s.memories, item)
	s.memoriesMu.Unlock()
}

func cloneModels(models []Model) []Model {
	if len(models) == 0 {
		return nil
	}

	cloned := make([]Model, 0, len(models))
	for _, model := range models {
		next := model
		if len(model.Capabilities) > 0 {
			next.Capabilities = append([]string(nil), model.Capabilities...)
		}
		if len(model.Metadata) > 0 {
			next.Metadata = cloneMetadata(model.Metadata)
		}
		cloned = append(cloned, next)
	}

	return cloned
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}

	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		switch typed := value.(type) {
		case []string:
			cloned[key] = append([]string(nil), typed...)
		case map[string]any:
			cloned[key] = cloneMetadata(typed)
		default:
			cloned[key] = typed
		}
	}

	return cloned
}

func cloneResponse(response Response) Response {
	cloned := response
	if len(response.Output) > 0 {
		cloned.Output = append([]OutputItem(nil), response.Output...)
	}
	return cloned
}

func cloneMemoryResult(item MemoryResult) MemoryResult {
	return item
}

func joinedInputText(messages []InputMessage) string {
	parts := make([]string, 0)
	for _, message := range messages {
		for _, content := range message.Content {
			if content.Type != "input_text" {
				continue
			}
			text := strings.TrimSpace(content.Text)
			if text == "" {
				continue
			}
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n")
}

func joinedOutputText(items []OutputItem) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}

	return strings.Join(parts, "\n")
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
