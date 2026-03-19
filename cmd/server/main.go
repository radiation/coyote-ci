package main

import (
	"log"
	nethttp "net/http"

	apphttp "github.com/radiation/coyote-ci/internal/http"
	"github.com/radiation/coyote-ci/internal/http/handler"
	"github.com/radiation/coyote-ci/internal/repository/memory"
	"github.com/radiation/coyote-ci/internal/service"
)

func main() {
	buildRepo := memory.NewBuildRepository()
	buildService := service.NewBuildService(buildRepo)
	buildHandler := handler.NewBuildHandler(buildService)

	router := apphttp.NewRouter(buildHandler)

	addr := ":8080"
	log.Printf("starting server on %s", addr)

	if err := nethttp.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
