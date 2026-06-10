package runtime

import (
	"context"
	"testing"
)

func TestServiceModels(t *testing.T) {
	service := NewService(DefaultRegistry())

	models := service.Models(context.Background())
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	if models[0].ID != echoModelID || models[0].Provider != "echo" {
		t.Fatalf("unexpected model payload: %#v", models[0])
	}

	if models[1].ID != privateEchoModelID || models[1].Provider != "private-echo" {
		t.Fatalf("unexpected second model payload: %#v", models[1])
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
		NewPrivateEchoAdapter(),
	)
	service := NewService(registry)

	response, err := service.CreateResponse(context.Background(), Request{
		Model: privateEchoModelID,
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

func TestServiceGetResponseReturnsStoredResponse(t *testing.T) {
	service := NewService(DefaultRegistry())

	created, err := service.CreateResponse(context.Background(), Request{
		Model: "orb/example-text",
		Input: []InputMessage{{Role: "user", Content: []InputContent{{Type: "input_text", Text: "hello orb"}}}},
	})
	if err != nil {
		t.Fatalf("expected create success, got %v", err)
	}

	stored, err := service.GetResponse(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("expected retrieval success, got %v", err)
	}

	if stored.ID != created.ID || stored.Model != created.Model {
		t.Fatalf("unexpected stored response payload: %#v", stored)
	}

	if len(stored.Output) != 1 || stored.Output[0].Text != "Echo: hello orb" {
		t.Fatalf("unexpected stored output payload: %#v", stored.Output)
	}
}

func TestServiceGetResponseReturnsNotFoundForUnknownResponse(t *testing.T) {
	service := NewService(DefaultRegistry())

	_, err := service.GetResponse(context.Background(), "resp_missing")
	if err == nil {
		t.Fatal("expected not found error")
	}

	apiErr, ok := err.(*Error)
	if !ok || apiErr.Code != "not_found" {
		t.Fatalf("unexpected error: %#v", err)
	}

	if apiErr.Details["persistence"] != "memory_only" {
		t.Fatalf("expected memory_only detail, got %#v", apiErr.Details)
	}
}
