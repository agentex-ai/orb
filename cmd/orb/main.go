package main

import (
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/agentex-ai/orb/internal/httpapi"
)

func main() {
	addr := os.Getenv("ORB_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server := &http.Server{
		Addr:    addr,
		Handler: httpapi.NewServer(),
	}

	log.Printf("orb listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
