package httpapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"

	orb "github.com/agentex-ai/orb/internal/runtime"
)

type anthropicMessageRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	System        any                `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Metadata      map[string]any     `json:"metadata,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Model        string                  `json:"model"`
	Content      []anthropicContentBlock `json:"content"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        anthropicUsage          `json:"usage"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (s server) handleAnthropicMessages(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	var payload anthropicMessageRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, APIError{
			Code:    "invalid_argument",
			Message: "request body must be valid JSON",
		})
		return
	}

	runtimeRequest := payload.runtimeRequest()
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
		started := false
		err := s.service.StreamResponse(request.Context(), runtimeRequest, func(event orb.StreamEvent) error {
			if event.Type == "error" {
				return writeSSEEvent(writer, event)
			}
			delta := responseOutputDelta(event.Data)
			if delta == "" {
				return nil
			}
			if !started {
				if err := writeAnthropicStreamStart(writer, runtimeRequest.Model); err != nil {
					return err
				}
				started = true
			}
			if err := writeSSEEvent(writer, orb.StreamEvent{
				Type: "content_block_delta",
				Data: map[string]any{
					"type":  "content_block_delta",
					"index": 0,
					"delta": map[string]any{
						"type": "text_delta",
						"text": delta,
					},
				},
			}); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		})
		if err != nil {
			writeStreamError(writer, err)
			flusher.Flush()
			return
		}

		if !started {
			_ = writeAnthropicStreamStart(writer, runtimeRequest.Model)
		}
		_ = writeAnthropicStreamStop(writer)
		flusher.Flush()
		return
	}

	response, err := s.service.CreateResponse(request.Context(), runtimeRequest)
	if err != nil {
		writeServiceError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, anthropicResponse{
		ID:    response.ID,
		Type:  "message",
		Role:  "assistant",
		Model: response.Model,
		Content: []anthropicContentBlock{
			{Type: "text", Text: joinedOutputText(response.Output)},
		},
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
		},
	})
}

func writeAnthropicStreamStart(writer http.ResponseWriter, model string) error {
	if err := writeSSEEvent(writer, orb.StreamEvent{
		Type: "message_start",
		Data: map[string]any{
			"type": "message_start",
			"message": anthropicResponse{
				ID:      "msg_orb_stream",
				Type:    "message",
				Role:    "assistant",
				Model:   model,
				Content: []anthropicContentBlock{},
			},
		},
	}); err != nil {
		return err
	}
	return writeSSEEvent(writer, orb.StreamEvent{
		Type: "content_block_start",
		Data: map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": anthropicContentBlock{Type: "text", Text: ""},
		},
	})
}

func writeAnthropicStreamStop(writer http.ResponseWriter) error {
	if err := writeSSEEvent(writer, orb.StreamEvent{Type: "content_block_stop", Data: map[string]any{"type": "content_block_stop", "index": 0}}); err != nil {
		return err
	}
	if err := writeSSEEvent(writer, orb.StreamEvent{Type: "message_delta", Data: map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": anthropicUsage{}}}); err != nil {
		return err
	}
	return writeSSEEvent(writer, orb.StreamEvent{Type: "message_stop", Data: map[string]any{"type": "message_stop"}})
}

func (r anthropicMessageRequest) runtimeRequest() orb.Request {
	input := make([]orb.InputMessage, 0, len(r.Messages))
	for _, message := range r.Messages {
		if text := anthropicText(message.Content); text != "" {
			input = append(input, orb.InputMessage{
				Role:    strings.TrimSpace(message.Role),
				Content: []orb.InputContent{{Type: "input_text", Text: text}},
			})
		}
	}

	settings := map[string]any{}
	if r.MaxTokens > 0 {
		settings["max_output_tokens"] = r.MaxTokens
	}
	if r.Temperature != nil {
		settings["temperature"] = *r.Temperature
	}
	if r.TopP != nil {
		settings["top_p"] = *r.TopP
	}
	if len(r.StopSequences) > 0 {
		settings["stop"] = r.StopSequences
	}

	metadata := r.Metadata
	if systemText := anthropicText(r.System); systemText != "" {
		metadata = maps.Clone(metadata)
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["anthropic_system"] = systemText
	}

	return orb.Request{
		Model:    strings.TrimSpace(r.Model),
		Input:    input,
		Stream:   r.Stream,
		Metadata: metadata,
		Settings: settings,
	}
}

func anthropicText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := anthropicTextBlock(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func anthropicTextBlock(value any) string {
	block, ok := value.(map[string]any)
	if !ok {
		return anthropicText(value)
	}
	switch strings.TrimSpace(fmt.Sprint(block["type"])) {
	case "text":
		return strings.TrimSpace(fmt.Sprint(block["text"]))
	case "tool_result":
		// ponytail: tool calls are flattened to text until Orb grows native tool IO.
		return anthropicText(block["content"])
	default:
		return ""
	}
}

func responseOutputDelta(data any) string {
	raw, ok := data.(json.RawMessage)
	if !ok {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	delta, _ := payload["delta"].(string)
	return delta
}

func joinedOutputText(items []orb.OutputItem) string {
	var parts []string
	for _, item := range items {
		if text := strings.TrimSpace(item.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}
