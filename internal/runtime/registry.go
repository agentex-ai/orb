package runtime

import (
	"context"
	"log"
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

type RegistryConfig struct {
	OpenAIBaseURL          string
	OpenAIAPIKey           string
	OpenAIModelID          string
	OpenAIPublicModelID    string
	PrivateBaseURL         string
	PrivateModelID         string
	PrivateUpstreamModelID string
	PrivateAuthHeader      string
	PrivateAuthToken       string
	HTTPClient             *http.Client
}

func ConfiguredRegistry(config RegistryConfig) *Registry {
	adapters := []Adapter{
		NewEchoAdapter(),
	}

	if strings.TrimSpace(config.OpenAIAPIKey) != "" && strings.TrimSpace(config.OpenAIModelID) != "" {
		openAIAdapter, err := NewOpenAIAdapter(OpenAIAdapterConfig{
			BaseURL:       config.OpenAIBaseURL,
			APIKey:        config.OpenAIAPIKey,
			ModelID:       config.OpenAIModelID,
			PublicModelID: config.OpenAIPublicModelID,
			Client:        config.HTTPClient,
		})
		if err != nil {
			log.Printf("orb: invalid openai adapter config, skipping openai adapter: %v", err)
		} else {
			adapters = append(adapters, openAIAdapter)
		}
	}

	if strings.TrimSpace(config.PrivateBaseURL) == "" {
		adapters = append(adapters, NewPrivateEchoAdapter())
		return NewRegistry(adapters...)
	}

	privateAdapter, err := NewPrivateHTTPAdapter(PrivateHTTPAdapterConfig{
		BaseURL:         config.PrivateBaseURL,
		PublicModelID:   config.PrivateModelID,
		UpstreamModelID: config.PrivateUpstreamModelID,
		AuthHeader:      config.PrivateAuthHeader,
		AuthToken:       config.PrivateAuthToken,
		Client:          config.HTTPClient,
	})
	if err != nil {
		log.Printf("orb: invalid private http adapter config, falling back to bundled private adapter: %v", err)
		adapters = append(adapters, NewPrivateEchoAdapter())
		return NewRegistry(adapters...)
	}

	adapters = append(adapters, privateAdapter)
	return NewRegistry(adapters...)
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
