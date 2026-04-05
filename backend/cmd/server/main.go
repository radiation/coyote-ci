package main

import (
	"context"
	"expvar"
	"log"
	nethttp "net/http"
	"strings"

	"cloud.google.com/go/storage"

	docs "github.com/radiation/coyote-ci/backend/docs"
	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	apphttp "github.com/radiation/coyote-ci/backend/internal/http"
	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/observability"
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
// @BasePath /api
// @schemes http

func main() {
	cfg := config.Load()
	docs.SwaggerInfo.BasePath = "/api"

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
	artifactStore, artifactProvider := resolveArtifactStore(cfg)
	logSink := logs.NewPostgresSink(db)
	buildService := service.NewBuildService(buildRepo, nil, logSink)
	buildService.SetExecutionJobRepository(executionJobRepo)
	buildService.SetExecutionJobOutputRepository(executionJobOutputRepo)
	buildService.SetRepoFetcher(source.NewGitFetcher())
	buildService.SetArtifactPersistence(artifactRepo, artifactStore, cfg.ExecutionWorkspaceRoot, artifactProvider)
	jobService := service.NewJobService(jobRepo, buildService)
	webhookService := service.NewWebhookIngressService(webhookDeliveryRepo, jobService)
	webhookMetrics := observability.NewExpvarWebhookIngressMetrics()
	webhookService.SetMetrics(webhookMetrics)
	buildHandler := handler.NewBuildHandler(buildService)
	jobHandler := handler.NewJobHandler(jobService)
	eventHandler := handler.NewEventHandler(jobService, webhookService, webhookMetrics, cfg.GitHubWebhookSecret)

	router := apphttp.NewRouter(buildHandler, jobHandler, eventHandler, cfg.PushEventSecret)
	mux := nethttp.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	mux.Handle("/swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	mux.Handle("/", router)

	addr := ":" + cfg.AppPort
	log.Printf("starting server on %s", addr)

	if err := nethttp.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func resolveArtifactStore(cfg config.Config) (artifact.Store, domain.StorageProvider) {
	switch strings.ToLower(strings.TrimSpace(cfg.ArtifactStorageProvider)) {
	case "gcs":
		if cfg.ArtifactGCSBucket == "" {
			log.Printf("ARTIFACT_STORAGE_PROVIDER=gcs but ARTIFACT_GCS_BUCKET is empty; falling back to filesystem")
			return artifact.NewFilesystemStore(cfg.ArtifactStorageRoot), domain.StorageProviderFilesystem
		}
		ctx := context.Background()
		client, err := storage.NewClient(ctx)
		if err != nil {
			log.Printf("failed to create GCS client: %v; falling back to filesystem", err)
			return artifact.NewFilesystemStore(cfg.ArtifactStorageRoot), domain.StorageProviderFilesystem
		}
		store, err := artifact.NewGCSStore(client, artifact.GCSStoreConfig{
			Bucket:  cfg.ArtifactGCSBucket,
			Prefix:  cfg.ArtifactGCSPrefix,
			Project: cfg.ArtifactGCSProject,
		})
		if err != nil {
			log.Printf("failed to create GCS artifact store: %v; falling back to filesystem", err)
			return artifact.NewFilesystemStore(cfg.ArtifactStorageRoot), domain.StorageProviderFilesystem
		}
		log.Printf("artifact storage: gcs bucket=%s prefix=%s", cfg.ArtifactGCSBucket, cfg.ArtifactGCSPrefix)
		return store, domain.StorageProviderGCS
	default:
		return artifact.NewFilesystemStore(cfg.ArtifactStorageRoot), domain.StorageProviderFilesystem
	}
}
