package runtime

import (
	"context"
	"testing"
)

func TestServiceModels(t *testing.T) {
	service := NewService(DefaultRegistry())

	models := service.Models(context.Background())
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	if models[0].ID != echoModelID || models[0].Provider != "echo" {
		t.Fatalf("unexpected model payload: %#v", models[0])
	}
}

func TestServiceCreateResponseRequiresModel(t *testing.T) {
	service := NewService(DefaultRegistry())

	_, err := service.CreateResponse(context.Background(), Request{
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello"}}}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	apiErr, ok := err.(*Error)
	if !ok || apiErr.Code != "invalid_argument" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestServiceCreateResponseUsesEchoAdapter(t *testing.T) {
	service := NewService(DefaultRegistry())

	response, err := service.CreateResponse(context.Background(), Request{
		Model: "orb/example-text",
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello orb"}}}},
		Memory: &MemoryRequest{
			Enabled: true,
			Scope:   "workspace:test",
		},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if response.Runtime.Adapter != "echo" || !response.Runtime.MemoryApplied {
		t.Fatalf("unexpected runtime metadata: %#v", response.Runtime)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "Echo: hello orb" {
		t.Fatalf("unexpected output payload: %#v", response.Output)
	}
}

func TestServiceCreateResponseUnknownModel(t *testing.T) {
	service := NewService(DefaultRegistry())

	_, err := service.CreateResponse(context.Background(), Request{
		Model: "orb/unknown",
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello"}}}},
	})
	if err == nil {
		t.Fatal("expected not found error")
	}

	apiErr, ok := err.(*Error)
	if !ok || apiErr.Code != "not_found" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestServiceRoutesToRegisteredAdapterByModel(t *testing.T) {
	registry := NewRegistry(
		NewEchoAdapter(),
		NewStaticEchoAdapter("private-echo", Model{
			ID:           "orb/private-example-text",
			Object:       "model",
			Provider:     "private-echo",
			Deployment:   "private",
			Capabilities: []string{"text"},
			Status:       "ready",
		}, "Private Echo: "),
	)
	service := NewService(registry)

	response, err := service.CreateResponse(context.Background(), Request{
		Model: "orb/private-example-text",
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "private hello"}}}},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if response.Runtime.Adapter != "private-echo" || response.Runtime.Deployment != "private" {
		t.Fatalf("unexpected routed runtime metadata: %#v", response.Runtime)
	}

	if len(response.Output) != 1 || response.Output[0].Text != "Private Echo: private hello" {
		t.Fatalf("unexpected routed output payload: %#v", response.Output)
	}
}
