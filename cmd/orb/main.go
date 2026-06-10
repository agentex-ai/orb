package main

import (
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/agentex-ai/orb/internal/httpapi"
	"github.com/agentex-ai/orb/internal/runtime"
)

func main() {
	addr := os.Getenv("ORB_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server := &http.Server{
		Addr: addr,
		Handler: httpapi.NewServerWithService(runtime.NewService(runtime.ConfiguredRegistry(runtime.RegistryConfig{
			OpenAIBaseURL:          os.Getenv("ORB_OPENAI_BASE_URL"),
			OpenAIAPIKey:           os.Getenv("ORB_OPENAI_API_KEY"),
			OpenAIModelID:          os.Getenv("ORB_OPENAI_MODEL_ID"),
			OpenAIPublicModelID:    os.Getenv("ORB_OPENAI_PUBLIC_MODEL_ID"),
			PrivateBaseURL:         os.Getenv("ORB_PRIVATE_BASE_URL"),
			PrivateModelID:         os.Getenv("ORB_PRIVATE_MODEL_ID"),
			PrivateUpstreamModelID: os.Getenv("ORB_PRIVATE_UPSTREAM_MODEL"),
			PrivateAuthHeader:      os.Getenv("ORB_PRIVATE_AUTH_HEADER"),
			PrivateAuthToken:       os.Getenv("ORB_PRIVATE_AUTH_TOKEN"),
		}))),
	}

	log.Printf("orb listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
