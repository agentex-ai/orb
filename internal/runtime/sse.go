package runtime

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func streamSSEEvents(body io.Reader, readErrorMessage string, emit func(StreamEvent) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	var dataLines []string

	flushEvent := func() error {
		if strings.TrimSpace(eventType) == "" && len(dataLines) == 0 {
			return nil
		}

		data := strings.Join(dataLines, "\n")
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			eventType = "message"
		}

		data = strings.TrimSpace(data)
		if data == "" {
			data = "{}"
		}
		if data == "[DONE]" {
			eventType = "done"
			data = `{}`
		}

		raw := json.RawMessage(data)
		if !json.Valid(raw) {
			raw = json.RawMessage(`{"raw":` + strconv.Quote(data) + `}`)
		}

		if err := emit(StreamEvent{
			Type: eventType,
			Data: raw,
		}); err != nil {
			return err
		}

		eventType = ""
		dataLines = nil
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if err := scanner.Err(); err != nil {
		return &Error{
			Code:       "backend_unavailable",
			Message:    readErrorMessage,
			Details:    map[string]any{"error": err.Error()},
			StatusCode: http.StatusBadGateway,
		}
	}

	return flushEvent()
}
