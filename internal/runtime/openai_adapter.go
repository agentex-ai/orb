package runtime

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	openAIDefaultBaseURL     = "https://api.openai.com/v1"
	openAIDefaultModelPrefix = "orb/openai/"
)

type OpenAIAdapterConfig struct {
	BaseURL       string
	APIKey        string
	ModelID       string
	PublicModelID string
	Client        *http.Client
}

type OpenAIAdapter struct {
	baseURL       string
	apiKey        string
	modelID       string
	publicModelID string
	client        *http.Client
}

type openAIRequestInputItem struct {
	Type    string                      `json:"type"`
	Role    string                      `json:"role"`
	Content []openAIRequestInputContent `json:"content"`
}

type openAIRequestInputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type openAIResponse struct {
	ID     string             `json:"id"`
	Object string             `json:"object"`
	Model  string             `json:"model"`
	Status string             `json:"status"`
	Output []openAIOutputItem `json:"output"`
	Usage  openAIUsage        `json:"usage"`
}

type openAIOutputItem struct {
	Type    string                `json:"type"`
	Text    string                `json:"text,omitempty"`
	Refusal string                `json:"refusal,omitempty"`
	Content []openAIOutputContent `json:"content,omitempty"`
}

type openAIOutputContent struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

type openAIUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type openAIErrorEnvelope struct {
	Error openAIError `json:"error"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    any    `json:"code"`
}

func NewOpenAIAdapter(config OpenAIAdapterConfig) (*OpenAIAdapter, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openai api key is required")
	}

	modelID := strings.TrimSpace(config.ModelID)
	if modelID == "" {
		return nil, fmt.Errorf("openai model id is required")
	}

	baseURL := strings.TrimSpace(config.BaseURL)
	if baseURL == "" {
		baseURL = openAIDefaultBaseURL
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid openai base url: %w", err)
	}

	publicModelID := strings.TrimSpace(config.PublicModelID)
	if publicModelID == "" {
		publicModelID = publicModelIDForOpenAIModel(modelID)
	}

	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}

	return &OpenAIAdapter{
		baseURL:       strings.TrimRight(parsedURL.String(), "/"),
		apiKey:        apiKey,
		modelID:       modelID,
		publicModelID: publicModelID,
		client:        client,
	}, nil
}

func (a *OpenAIAdapter) Name() string {
	return "openai"
}

func (a *OpenAIAdapter) Models(context.Context) []Model {
	return []Model{
		{
			ID:           a.publicModelID,
			Object:       "model",
			Provider:     a.Name(),
			Deployment:   "hosted",
			Capabilities: []string{"text"},
			Status:       "ready",
			Metadata: map[string]any{
				"upstream_model_id": a.modelID,
			},
		},
	}
}

func (a *OpenAIAdapter) Generate(ctx context.Context, request Request) (Response, error) {
	if strings.TrimSpace(request.Model) != a.publicModelID {
		return Response{}, modelNotFoundError(request.Model)
	}

	payload := map[string]any{
		"model": a.modelID,
		"input": toOpenAIInput(request.Input),
	}
	if metadata := toOpenAIMetadata(request.Metadata); len(metadata) > 0 {
		payload["metadata"] = metadata
	}
	mergeOpenAISettings(payload, request.Settings)

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, &Error{
			Code:       "internal_error",
			Message:    "failed to encode openai adapter request",
			StatusCode: http.StatusInternalServerError,
		}
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return Response{}, &Error{
			Code:       "backend_unavailable",
			Message:    "failed to create openai adapter request",
			StatusCode: http.StatusBadGateway,
		}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := a.client.Do(httpRequest)
	if err != nil {
		return Response{}, &Error{
			Code:       "backend_unavailable",
			Message:    "openai adapter request failed",
			Details:    map[string]any{"error": err.Error()},
			StatusCode: http.StatusBadGateway,
		}
	}
	defer httpResponse.Body.Close()

	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return Response{}, &Error{
			Code:       "backend_unavailable",
			Message:    "failed to read openai adapter response",
			StatusCode: http.StatusBadGateway,
		}
	}

	if httpResponse.StatusCode >= http.StatusBadRequest {
		return Response{}, toOpenAIError(httpResponse.StatusCode, responseBody)
	}

	var upstream openAIResponse
	if err := json.Unmarshal(responseBody, &upstream); err != nil {
		return Response{}, &Error{
			Code:       "backend_unavailable",
			Message:    "openai adapter returned invalid JSON",
			Details:    map[string]any{"error": err.Error()},
			StatusCode: http.StatusBadGateway,
		}
	}

	return Response{
		ID:     upstream.ID,
		Object: upstream.Object,
		Model:  a.publicModelID,
		Output: toRuntimeOutputFromOpenAI(upstream.Output),
		Usage: Usage{
			InputTokens:  upstream.Usage.InputTokens,
			OutputTokens: upstream.Usage.OutputTokens,
			TotalTokens:  upstream.Usage.TotalTokens,
		},
		Runtime: Runtime{
			Adapter:    a.Name(),
			Deployment: "hosted",
			Status:     cmp.Or(strings.TrimSpace(upstream.Status), "completed"),
		},
	}, nil
}

func toOpenAIInput(messages []InputMessage) []openAIRequestInputItem {
	result := make([]openAIRequestInputItem, 0, len(messages))
	for _, message := range messages {
		content := make([]openAIRequestInputContent, 0, len(message.Content))
		for _, item := range message.Content {
			if item.Type != "input_text" {
				continue
			}
			content = append(content, openAIRequestInputContent{
				Type: "input_text",
				Text: item.Text,
			})
		}
		if len(content) == 0 {
			continue
		}
		result = append(result, openAIRequestInputItem{
			Type:    "message",
			Role:    cmp.Or(strings.TrimSpace(message.Role), "user"),
			Content: content,
		})
	}
	return result
}

func mergeOpenAISettings(payload map[string]any, settings map[string]any) {
	for key, value := range settings {
		switch key {
		case "", "model", "input", "metadata", "stream":
			continue
		default:
			payload[key] = value
		}
	}
}

func (a *OpenAIAdapter) GenerateStream(ctx context.Context, request Request, emit func(StreamEvent) error) error {
	if strings.TrimSpace(request.Model) != a.publicModelID {
		return modelNotFoundError(request.Model)
	}
	if emit == nil {
		return &Error{
			Code:       "internal_error",
			Message:    "stream emitter is required",
			StatusCode: http.StatusInternalServerError,
		}
	}

	payload := map[string]any{
		"model":  a.modelID,
		"input":  toOpenAIInput(request.Input),
		"stream": true,
	}
	if metadata := toOpenAIMetadata(request.Metadata); len(metadata) > 0 {
		payload["metadata"] = metadata
	}
	mergeOpenAISettings(payload, request.Settings)

	body, err := json.Marshal(payload)
	if err != nil {
		return &Error{
			Code:       "internal_error",
			Message:    "failed to encode openai adapter request",
			StatusCode: http.StatusInternalServerError,
		}
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return &Error{
			Code:       "backend_unavailable",
			Message:    "failed to create openai adapter request",
			StatusCode: http.StatusBadGateway,
		}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")

	httpResponse, err := a.client.Do(httpRequest)
	if err != nil {
		return &Error{
			Code:       "backend_unavailable",
			Message:    "openai adapter request failed",
			Details:    map[string]any{"error": err.Error()},
			StatusCode: http.StatusBadGateway,
		}
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode >= http.StatusBadRequest {
		responseBody, readErr := io.ReadAll(httpResponse.Body)
		if readErr != nil {
			return &Error{
				Code:       "backend_unavailable",
				Message:    "failed to read openai adapter response",
				StatusCode: http.StatusBadGateway,
			}
		}
		return toOpenAIError(httpResponse.StatusCode, responseBody)
	}

	return streamSSEEvents(httpResponse.Body, "failed to read openai streaming response", emit)
}

func toRuntimeOutputFromOpenAI(items []openAIOutputItem) []OutputItem {
	output := make([]OutputItem, 0)
	for _, item := range items {
		if item.Type == "output_text" && strings.TrimSpace(item.Text) != "" {
			output = append(output, OutputItem{
				Type: "output_text",
				Text: item.Text,
			})
		}
		if item.Type == "refusal" && strings.TrimSpace(item.Refusal) != "" {
			output = append(output, OutputItem{
				Type: "output_text",
				Text: item.Refusal,
			})
		}

		for _, content := range item.Content {
			switch content.Type {
			case "output_text":
				if strings.TrimSpace(content.Text) == "" {
					continue
				}
				output = append(output, OutputItem{
					Type: "output_text",
					Text: content.Text,
				})
			case "refusal":
				if strings.TrimSpace(content.Refusal) == "" {
					continue
				}
				output = append(output, OutputItem{
					Type: "output_text",
					Text: content.Refusal,
				})
			}
		}
	}
	return output
}

func toOpenAIMetadata(metadata map[string]any) map[string]string {
	if len(metadata) == 0 {
		return nil
	}

	result := make(map[string]string, len(metadata))
	for key, value := range metadata {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" || value == nil {
			continue
		}
		result[trimmedKey] = fmt.Sprint(value)
	}

	return result
}

func toOpenAIError(statusCode int, body []byte) error {
	var envelope openAIErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		details := map[string]any{}
		if envelope.Error.Type != "" {
			details["provider_type"] = envelope.Error.Type
		}
		if envelope.Error.Param != "" {
			details["provider_param"] = envelope.Error.Param
		}
		if envelope.Error.Code != nil {
			details["provider_code"] = envelope.Error.Code
		}

		return &Error{
			Code:       openAIErrorCodeForStatus(statusCode),
			Message:    envelope.Error.Message,
			Details:    details,
			StatusCode: statusCode,
		}
	}

	return &Error{
		Code:       openAIErrorCodeForStatus(statusCode),
		Message:    "openai adapter request failed",
		Details:    map[string]any{"status_code": statusCode},
		StatusCode: statusCode,
	}
}

func openAIErrorCodeForStatus(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_argument"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusTooManyRequests:
		return "rate_limited"
	}
	return "backend_unavailable"
}

func publicModelIDForOpenAIModel(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if strings.HasPrefix(modelID, openAIDefaultModelPrefix) {
		return modelID
	}
	return openAIDefaultModelPrefix + modelID
}
