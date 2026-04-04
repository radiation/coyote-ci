package main

import (
	"log"
	nethttp "net/http"

	docs "github.com/radiation/coyote-ci/backend/docs"
	"github.com/radiation/coyote-ci/backend/internal/artifact"
	apphttp "github.com/radiation/coyote-ci/backend/internal/http"
	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/service"
	"github.com/radiation/coyote-ci/backend/internal/source"
	httpSwagger "github.com/swaggo/http-swagger"
)

// @title Coyote CI API
// @version 0.1
// @description HTTP API for Coyote CI control-plane workflows.
// @BasePath /
// @schemes http

func main() {
	cfg := config.Load()
	docs.SwaggerInfo.BasePath = "/"

	db, err := platformdb.Open(cfg.DatabaseURL())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("error closing database: %v", err)
		}
	}()

	buildRepo := repositorypostgres.NewBuildRepository(db)
	executionJobRepo := repositorypostgres.NewExecutionJobRepository(db)
	executionJobOutputRepo := repositorypostgres.NewExecutionJobOutputRepository(db)
	jobRepo := repositorypostgres.NewJobRepository(db)
	webhookDeliveryRepo := repositorypostgres.NewWebhookDeliveryRepository(db)
	artifactRepo := repositorypostgres.NewArtifactRepository(db)
	artifactStore := artifact.NewFilesystemStore(cfg.ArtifactStorageRoot)
	logSink := logs.NewPostgresSink(db)
	buildService := service.NewBuildService(buildRepo, nil, logSink)
	buildService.SetExecutionJobRepository(executionJobRepo)
	buildService.SetExecutionJobOutputRepository(executionJobOutputRepo)
	buildService.SetRepoFetcher(source.NewGitFetcher())
	buildService.SetArtifactPersistence(artifactRepo, artifactStore, cfg.ExecutionWorkspaceRoot)
	jobService := service.NewJobService(jobRepo, buildService)
	webhookService := service.NewWebhookIngressService(webhookDeliveryRepo, jobService)
	buildHandler := handler.NewBuildHandler(buildService)
	jobHandler := handler.NewJobHandler(jobService)
	eventHandler := handler.NewEventHandler(jobService, webhookService, cfg.GitHubWebhookSecret)

	router := apphttp.NewRouter(buildHandler, jobHandler, eventHandler, cfg.PushEventSecret)
	mux := nethttp.NewServeMux()
	mux.Handle("/swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	mux.Handle("/", router)

	addr := ":" + cfg.AppPort
	log.Printf("starting server on %s", addr)

	if err := nethttp.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
