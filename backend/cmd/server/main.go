package main

import (
	"log"
	nethttp "net/http"

	apphttp "github.com/radiation/coyote-ci/backend/internal/http"
	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	postgresrepo "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func main() {
	cfg := config.Load()

	db, err := platformdb.Open(cfg.DatabaseURL())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("error closing database: %v", err)
		}
	}()

	buildRepo := postgresrepo.NewBuildRepository(db)
	buildService := service.NewBuildService(buildRepo)
	buildHandler := handler.NewBuildHandler(buildService)

	router := apphttp.NewRouter(buildHandler)

	addr := ":" + cfg.AppPort
	log.Printf("starting server on %s", addr)

	if err := nethttp.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
