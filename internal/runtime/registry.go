package runtime

import (
	"context"
	"net/http"
	"strings"
)

type Registry struct {
	adapters []Adapter
}

func NewRegistry(adapters ...Adapter) *Registry {
	filtered := make([]Adapter, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		filtered = append(filtered, adapter)
	}

	return &Registry{adapters: filtered}
}

func DefaultRegistry() *Registry {
	return NewRegistry(
		NewEchoAdapter(),
		NewPrivateEchoAdapter(),
	)
}

func (r *Registry) Models(ctx context.Context) []Model {
	if r == nil {
		return nil
	}

	models := make([]Model, 0)
	seen := make(map[string]struct{})
	for _, adapter := range r.adapters {
		for _, model := range adapter.Models(ctx) {
			key := strings.TrimSpace(model.ID)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			models = append(models, model)
		}
	}

	return models
}

func (r *Registry) AdapterForModel(ctx context.Context, modelID string) (Adapter, Model, error) {
	if r == nil {
		return nil, Model{}, &Error{
			Code:       "backend_unavailable",
			Message:    "adapter registry is not configured",
			StatusCode: http.StatusBadGateway,
		}
	}

	target := strings.TrimSpace(modelID)
	for _, adapter := range r.adapters {
		for _, model := range adapter.Models(ctx) {
			if model.ID == target {
				return adapter, model, nil
			}
		}
	}

	return nil, Model{}, &Error{
		Code:       "not_found",
		Message:    "model " + `"` + modelID + `"` + " is not available",
		Details:    map[string]any{"model": modelID},
		StatusCode: http.StatusNotFound,
	}
}
