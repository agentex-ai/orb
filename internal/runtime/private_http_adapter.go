package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

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
	authHeader      string
	authToken       string
	client          *http.Client
}

type privateHTTPRequest struct {
	Model    string                    `json:"model"`
	Input    []privateHTTPInputMessage `json:"input"`
	Memory   *privateHTTPMemory        `json:"memory,omitempty"`
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
	if publicModelID == "" {
		publicModelID = privateEchoModelID
	}

	upstreamModelID := strings.TrimSpace(config.UpstreamModelID)
	if upstreamModelID == "" {
		upstreamModelID = publicModelID
	}

	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}

	return &PrivateHTTPAdapter{
		baseURL:         strings.TrimRight(parsedURL.String(), "/"),
		publicModelID:   publicModelID,
		upstreamModelID: upstreamModelID,
		authHeader:      strings.TrimSpace(config.AuthHeader),
		authToken:       strings.TrimSpace(config.AuthToken),
		client:          client,
	}, nil
}

func (a *PrivateHTTPAdapter) Name() string {
	return "private-http"
}

func (a *PrivateHTTPAdapter) Models(context.Context) []Model {
	return []Model{
		{
			ID:           a.publicModelID,
			Object:       "model",
			Provider:     a.Name(),
			Deployment:   "private",
			Capabilities: []string{"text"},
			Status:       "ready",
		},
	}
}

func (a *PrivateHTTPAdapter) Generate(ctx context.Context, request Request) (Response, error) {
	payload, err := json.Marshal(privateHTTPRequest{
		Model:    a.upstreamModelID,
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
		Model:  a.publicModelID,
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
			Status:        defaultString(upstream.Runtime.Status, "ready"),
		},
	}, nil
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
