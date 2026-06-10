package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const privateDiscoveredModelPrefix = "orb/private/"

type PrivateHTTPAdapterConfig struct {
	BaseURL         string
	PublicModelID   string
	UpstreamModelID string
	AuthHeader      string
	AuthToken       string
	Client          *http.Client
}

type PrivateHTTPAdapter struct {
	baseURL         string
	publicModelID   string
	upstreamModelID string
	singleMapping   bool
	authHeader      string
	authToken       string
	client          *http.Client
}

type privateHTTPResolvedModel struct {
	Model           Model
	UpstreamModelID string
}

type privateHTTPRequest struct {
	Model    string                    `json:"model"`
	Input    []privateHTTPInputMessage `json:"input"`
	Memory   *privateHTTPMemory        `json:"memory,omitempty"`
	Stream   bool                      `json:"stream,omitempty"`
	Metadata map[string]any            `json:"metadata,omitempty"`
	Settings map[string]any            `json:"settings,omitempty"`
}

type privateHTTPInputMessage struct {
	Role    string                    `json:"role"`
	Content []privateHTTPInputContent `json:"content"`
}

type privateHTTPInputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type privateHTTPMemory struct {
	Enabled bool   `json:"enabled"`
	Scope   string `json:"scope,omitempty"`
}

type privateHTTPResponse struct {
	ID      string                     `json:"id"`
	Object  string                     `json:"object"`
	Model   string                     `json:"model"`
	Output  []privateHTTPResponseItem  `json:"output"`
	Usage   privateHTTPUsage           `json:"usage"`
	Runtime privateHTTPRuntimeMetadata `json:"runtime"`
}

type privateHTTPResponseItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type privateHTTPModelList struct {
	Object string             `json:"object"`
	Data   []privateHTTPModel `json:"data"`
}

type privateHTTPModel struct {
	ID           string   `json:"id"`
	Object       string   `json:"object"`
	Provider     string   `json:"provider"`
	Deployment   string   `json:"deployment"`
	Capabilities []string `json:"capabilities"`
	Status       string   `json:"status"`
}

type privateHTTPUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type privateHTTPRuntimeMetadata struct {
	Adapter       string `json:"adapter"`
	Deployment    string `json:"deployment"`
	MemoryApplied bool   `json:"memory_applied"`
	Status        string `json:"status"`
}

type privateHTTPErrorEnvelope struct {
	Error privateHTTPError `json:"error"`
}

type privateHTTPError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func NewPrivateHTTPAdapter(config PrivateHTTPAdapterConfig) (*PrivateHTTPAdapter, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("private http base url is required")
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid private http base url: %w", err)
	}

	publicModelID := strings.TrimSpace(config.PublicModelID)
	upstreamModelID := strings.TrimSpace(config.UpstreamModelID)
	singleMapping := publicModelID != "" || upstreamModelID != ""
	if singleMapping {
		if publicModelID == "" {
			publicModelID = privateEchoModelID
		}
		if upstreamModelID == "" {
			upstreamModelID = publicModelID
		}
	}

	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}

	return &PrivateHTTPAdapter{
		baseURL:         strings.TrimRight(parsedURL.String(), "/"),
		publicModelID:   publicModelID,
		upstreamModelID: upstreamModelID,
		singleMapping:   singleMapping,
		authHeader:      strings.TrimSpace(config.AuthHeader),
		authToken:       strings.TrimSpace(config.AuthToken),
		client:          client,
	}, nil
}

func (a *PrivateHTTPAdapter) Name() string {
	return "private-http"
}

func (a *PrivateHTTPAdapter) Models(ctx context.Context) []Model {
	if a.singleMapping {
		return []Model{a.discoverSingleModel(ctx)}
	}

	return a.discoverModels(ctx)
}

func (a *PrivateHTTPAdapter) discoverSingleModel(ctx context.Context) Model {
	fallback := a.fallbackSingleModel()

	models, err := a.fetchModels(ctx)
	if err != nil {
		return fallback
	}

	for _, model := range models {
		if strings.TrimSpace(model.ID) != a.upstreamModelID {
			continue
		}

		return a.runtimeModelFromUpstream(a.publicModelID, model)
	}

	return fallback
}

func (a *PrivateHTTPAdapter) discoverModels(ctx context.Context) []Model {
	models, err := a.fetchModels(ctx)
	if err != nil {
		return nil
	}

	discovered := make([]Model, 0, len(models))
	seen := make(map[string]struct{})
	for _, model := range models {
		resolved, ok := a.resolvedModelFromUpstream(model)
		if !ok {
			continue
		}
		if _, exists := seen[resolved.Model.ID]; exists {
			continue
		}
		seen[resolved.Model.ID] = struct{}{}
		discovered = append(discovered, resolved.Model)
	}

	return discovered
}

func (a *PrivateHTTPAdapter) fallbackSingleModel() Model {
	return Model{
		ID:           a.publicModelID,
		Object:       "model",
		Provider:     a.Name(),
		Deployment:   "private",
		Capabilities: []string{"text"},
		Status:       "ready",
		Metadata: map[string]any{
			"discovery":         "fallback",
			"upstream_model_id": a.upstreamModelID,
		},
	}
}

func (a *PrivateHTTPAdapter) fetchModels(ctx context.Context) ([]privateHTTPModel, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	a.applyAuthHeader(httpRequest)

	httpResponse, err := a.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode >= http.StatusBadRequest {
		body, readErr := io.ReadAll(httpResponse.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, toPrivateHTTPError(httpResponse.StatusCode, body)
	}

	var response privateHTTPModelList
	if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response.Data, nil
}

func (a *PrivateHTTPAdapter) Generate(ctx context.Context, request Request) (Response, error) {
	targetModel, err := a.resolveTargetModel(ctx, request.Model)
	if err != nil {
		return Response{}, err
	}

	payload, err := json.Marshal(privateHTTPRequest{
		Model:    targetModel.UpstreamModelID,
		Input:    toPrivateHTTPInput(request.Input),
		Memory:   toPrivateHTTPMemory(request.Memory),
		Metadata: request.Metadata,
		Settings: request.Settings,
	})
	if err != nil {
		return Response{}, &Error{
			Code:       "internal_error",
			Message:    "failed to encode private adapter request",
			StatusCode: http.StatusInternalServerError,
		}
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/responses", bytes.NewReader(payload))
	if err != nil {
		return Response{}, &Error{
			Code:       "backend_unavailable",
			Message:    "failed to create private adapter request",
			StatusCode: http.StatusBadGateway,
		}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	a.applyAuthHeader(httpRequest)

	httpResponse, err := a.client.Do(httpRequest)
	if err != nil {
		return Response{}, &Error{
			Code:       "backend_unavailable",
			Message:    "private adapter request failed",
			Details:    map[string]any{"error": err.Error()},
			StatusCode: http.StatusBadGateway,
		}
	}
	defer httpResponse.Body.Close()

	body, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return Response{}, &Error{
			Code:       "backend_unavailable",
			Message:    "failed to read private adapter response",
			StatusCode: http.StatusBadGateway,
		}
	}

	if httpResponse.StatusCode >= http.StatusBadRequest {
		return Response{}, toPrivateHTTPError(httpResponse.StatusCode, body)
	}

	var upstream privateHTTPResponse
	if err := json.Unmarshal(body, &upstream); err != nil {
		return Response{}, &Error{
			Code:       "backend_unavailable",
			Message:    "private adapter returned invalid JSON",
			Details:    map[string]any{"error": err.Error()},
			StatusCode: http.StatusBadGateway,
		}
	}

	return Response{
		ID:     upstream.ID,
		Object: upstream.Object,
		Model:  targetModel.Model.ID,
		Output: toRuntimeOutput(upstream.Output),
		Usage: Usage{
			InputTokens:  upstream.Usage.InputTokens,
			OutputTokens: upstream.Usage.OutputTokens,
			TotalTokens:  upstream.Usage.TotalTokens,
		},
		Runtime: Runtime{
			Adapter:       a.Name(),
			Deployment:    "private",
			MemoryApplied: upstream.Runtime.MemoryApplied,
			Status:        strings.TrimSpace(upstream.Runtime.Status),
		},
	}, nil
}

func (a *PrivateHTTPAdapter) GenerateStream(ctx context.Context, request Request, emit func(StreamEvent) error) error {
	targetModel, err := a.resolveTargetModel(ctx, request.Model)
	if err != nil {
		return err
	}
	if emit == nil {
		return &Error{
			Code:       "internal_error",
			Message:    "stream emitter is required",
			StatusCode: http.StatusInternalServerError,
		}
	}

	payload, err := json.Marshal(privateHTTPRequest{
		Model:    targetModel.UpstreamModelID,
		Input:    toPrivateHTTPInput(request.Input),
		Memory:   toPrivateHTTPMemory(request.Memory),
		Stream:   true,
		Metadata: request.Metadata,
		Settings: request.Settings,
	})
	if err != nil {
		return &Error{
			Code:       "internal_error",
			Message:    "failed to encode private adapter request",
			StatusCode: http.StatusInternalServerError,
		}
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/responses", bytes.NewReader(payload))
	if err != nil {
		return &Error{
			Code:       "backend_unavailable",
			Message:    "failed to create private adapter request",
			StatusCode: http.StatusBadGateway,
		}
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	a.applyAuthHeader(httpRequest)

	httpResponse, err := a.client.Do(httpRequest)
	if err != nil {
		return &Error{
			Code:       "backend_unavailable",
			Message:    "private adapter request failed",
			Details:    map[string]any{"error": err.Error()},
			StatusCode: http.StatusBadGateway,
		}
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode >= http.StatusBadRequest {
		body, readErr := io.ReadAll(httpResponse.Body)
		if readErr != nil {
			return &Error{
				Code:       "backend_unavailable",
				Message:    "failed to read private adapter response",
				StatusCode: http.StatusBadGateway,
			}
		}
		return toPrivateHTTPError(httpResponse.StatusCode, body)
	}

	return streamSSEEvents(httpResponse.Body, "failed to read private streaming response", emit)
}

func (a *PrivateHTTPAdapter) resolveTargetModel(ctx context.Context, publicModelID string) (privateHTTPResolvedModel, error) {
	target := strings.TrimSpace(publicModelID)
	if a.singleMapping {
		if a.publicModelID != target {
			return privateHTTPResolvedModel{}, modelNotFoundError(publicModelID)
		}
		return privateHTTPResolvedModel{
			Model:           a.fallbackSingleModel(),
			UpstreamModelID: a.upstreamModelID,
		}, nil
	}

	models, err := a.fetchModels(ctx)
	if err != nil {
		return privateHTTPResolvedModel{}, toPrivateHTTPDiscoveryError(err)
	}

	for _, model := range models {
		resolved, ok := a.resolvedModelFromUpstream(model)
		if !ok {
			continue
		}
		if resolved.Model.ID == target {
			return resolved, nil
		}
	}

	return privateHTTPResolvedModel{}, modelNotFoundError(publicModelID)
}

func (a *PrivateHTTPAdapter) resolvedModelFromUpstream(model privateHTTPModel) (privateHTTPResolvedModel, bool) {
	upstreamModelID := strings.TrimSpace(model.ID)
	if upstreamModelID == "" {
		return privateHTTPResolvedModel{}, false
	}

	publicModelID := publicModelIDForUpstream(upstreamModelID)
	if publicModelID == "" {
		return privateHTTPResolvedModel{}, false
	}

	return privateHTTPResolvedModel{
		Model:           a.runtimeModelFromUpstream(publicModelID, model),
		UpstreamModelID: upstreamModelID,
	}, true
}

func (a *PrivateHTTPAdapter) runtimeModelFromUpstream(publicModelID string, model privateHTTPModel) Model {
	capabilities := model.Capabilities
	if len(capabilities) == 0 {
		capabilities = []string{"text"}
	}

	metadata := map[string]any{
		"discovery":         "upstream",
		"upstream_model_id": model.ID,
	}
	if provider := strings.TrimSpace(model.Provider); provider != "" {
		metadata["upstream_provider"] = provider
	}
	if status := strings.TrimSpace(model.Status); status != "" {
		metadata["upstream_status"] = status
	}
	if deployment := strings.TrimSpace(model.Deployment); deployment != "" {
		metadata["upstream_deployment"] = deployment
	}

	return Model{
		ID:           publicModelID,
		Object:       defaultString(model.Object, "model"),
		Provider:     a.Name(),
		Deployment:   "private",
		Capabilities: capabilities,
		Status:       defaultString(model.Status, "ready"),
		Metadata:     metadata,
	}
}

func (a *PrivateHTTPAdapter) applyAuthHeader(request *http.Request) {
	token := strings.TrimSpace(a.authToken)
	if token == "" {
		return
	}

	headerName := defaultString(a.authHeader, "Authorization")
	headerValue := token
	if strings.EqualFold(headerName, "Authorization") && !strings.Contains(token, " ") {
		headerValue = "Bearer " + token
	}

	request.Header.Set(headerName, headerValue)
}

func toPrivateHTTPInput(messages []InputMessage) []privateHTTPInputMessage {
	result := make([]privateHTTPInputMessage, 0, len(messages))
	for _, message := range messages {
		content := make([]privateHTTPInputContent, 0, len(message.Content))
		for _, item := range message.Content {
			content = append(content, privateHTTPInputContent{
				Type: item.Type,
				Text: item.Text,
			})
		}
		result = append(result, privateHTTPInputMessage{
			Role:    message.Role,
			Content: content,
		})
	}
	return result
}

func toPrivateHTTPMemory(memory *MemoryRequest) *privateHTTPMemory {
	if memory == nil {
		return nil
	}
	return &privateHTTPMemory{
		Enabled: memory.Enabled,
		Scope:   memory.Scope,
	}
}

func toRuntimeOutput(items []privateHTTPResponseItem) []OutputItem {
	output := make([]OutputItem, 0, len(items))
	for _, item := range items {
		output = append(output, OutputItem{
			Type: item.Type,
			Text: item.Text,
		})
	}
	return output
}

func toPrivateHTTPError(statusCode int, body []byte) error {
	var envelope privateHTTPErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Code != "" {
		return &Error{
			Code:       envelope.Error.Code,
			Message:    envelope.Error.Message,
			Details:    envelope.Error.Details,
			StatusCode: statusCode,
		}
	}

	return &Error{
		Code:       "backend_unavailable",
		Message:    "private adapter request failed",
		Details:    map[string]any{"status_code": statusCode},
		StatusCode: http.StatusBadGateway,
	}
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func publicModelIDForUpstream(upstreamModelID string) string {
	modelID := strings.TrimSpace(upstreamModelID)
	modelID = strings.TrimLeft(modelID, "/")
	if modelID == "" {
		return ""
	}
	if strings.HasPrefix(modelID, privateDiscoveredModelPrefix) {
		return modelID
	}
	if strings.HasPrefix(modelID, "orb/") {
		return privateDiscoveredModelPrefix + strings.TrimPrefix(modelID, "orb/")
	}
	return privateDiscoveredModelPrefix + modelID
}

func modelNotFoundError(modelID string) error {
	return &Error{
		Code:       "not_found",
		Message:    "model " + `"` + modelID + `"` + " is not available",
		Details:    map[string]any{"model": modelID},
		StatusCode: http.StatusNotFound,
	}
}

func toPrivateHTTPDiscoveryError(err error) error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	return &Error{
		Code:       "backend_unavailable",
		Message:    "private model discovery failed",
		Details:    map[string]any{"error": err.Error()},
		StatusCode: http.StatusBadGateway,
	}
}
