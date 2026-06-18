package main

import (
	"cmp"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/agentex-ai/orb/internal/httpapi"
	"github.com/agentex-ai/orb/internal/runtime"
)

func main() {
	server := &http.Server{
		Addr: cmp.Or(os.Getenv("ORB_ADDR"), ":8080"),
		Handler: httpapi.NewServerWithConfig(httpapi.ServerConfig{
			Service: runtime.NewService(runtime.ConfiguredRegistry(runtime.RegistryConfig{
				OpenAIBaseURL:          os.Getenv("ORB_OPENAI_BASE_URL"),
				OpenAIAPIKey:           os.Getenv("ORB_OPENAI_API_KEY"),
				OpenAIModelID:          os.Getenv("ORB_OPENAI_MODEL_ID"),
				OpenAIPublicModelID:    os.Getenv("ORB_OPENAI_PUBLIC_MODEL_ID"),
				PrivateBaseURL:         os.Getenv("ORB_PRIVATE_BASE_URL"),
				PrivateModelID:         os.Getenv("ORB_PRIVATE_MODEL_ID"),
				PrivateUpstreamModelID: os.Getenv("ORB_PRIVATE_UPSTREAM_MODEL"),
				PrivateAuthHeader:      os.Getenv("ORB_PRIVATE_AUTH_HEADER"),
				PrivateAuthToken:       os.Getenv("ORB_PRIVATE_AUTH_TOKEN"),
			})),
			ClientProxyConfigPath: os.Getenv("ORB_CLIENT_PROXY_CONFIG"),
			PublicBaseURL:         os.Getenv("ORB_PUBLIC_BASE_URL"),
		}),
	}

	log.Printf("orb listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
