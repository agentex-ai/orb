package runtime

import (
	"context"
)

const echoModelID = "orb/example-text"
const privateEchoModelID = "orb/private-example-text"

type EchoAdapter struct {
	name       string
	model      Model
	textPrefix string
}

func NewEchoAdapter() *EchoAdapter {
	return &EchoAdapter{
		name: "echo",
		model: Model{
			ID:           echoModelID,
			Object:       "model",
			Provider:     "echo",
			Deployment:   "local",
			Capabilities: []string{"text"},
			Status:       "ready",
		},
		textPrefix: "Echo: ",
	}
}

func NewPrivateEchoAdapter() *EchoAdapter {
	return &EchoAdapter{
		name: "private-echo",
		model: Model{
			ID:           privateEchoModelID,
			Object:       "model",
			Provider:     "private-echo",
			Deployment:   "private",
			Capabilities: []string{"text"},
			Status:       "ready",
		},
		textPrefix: "Private Echo: ",
	}
}

func (a *EchoAdapter) Name() string {
	return a.name
}

func (a *EchoAdapter) Models(context.Context) []Model {
	return []Model{a.model}
}

func (a *EchoAdapter) Generate(_ context.Context, request Request) (Response, error) {
	text := firstInputText(request.Input)
	if text == "" {
		text = "Echo adapter received the request, but no input_text content was found."
	} else {
		text = a.textPrefix + text
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
			Deployment: a.model.Deployment,
			Status:     a.model.Status,
		},
	}, nil
}
