package runtime

import (
	"context"
	"fmt"
	"net/http"
)

const echoModelID = "orb/example-text"

type EchoAdapter struct{}

func NewEchoAdapter() *EchoAdapter {
	return &EchoAdapter{}
}

func (a *EchoAdapter) Name() string {
	return "echo"
}

func (a *EchoAdapter) Models(context.Context) []Model {
	return []Model{
		{
			ID:           echoModelID,
			Object:       "model",
			Provider:     a.Name(),
			Deployment:   "local",
			Capabilities: []string{"text"},
			Status:       "ready",
		},
	}
}

func (a *EchoAdapter) Generate(_ context.Context, request Request) (Response, error) {
	if request.Model != echoModelID {
		return Response{}, &Error{
			Code:       "not_found",
			Message:    fmt.Sprintf("model %q is not available", request.Model),
			Details:    map[string]any{"model": request.Model},
			StatusCode: http.StatusNotFound,
		}
	}

	text := firstInputText(request.Input)
	if text == "" {
		text = "Echo adapter received the request, but no input_text content was found."
	} else {
		text = "Echo: " + text
	}

	return Response{
		Model: request.Model,
		Output: []OutputItem{
			{
				Type: "output_text",
				Text: text,
			},
		},
		Runtime: Runtime{
			Adapter:    a.Name(),
			Deployment: "local",
			Status:     "ready",
		},
	}, nil
}
