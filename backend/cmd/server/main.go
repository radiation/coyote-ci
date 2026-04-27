package main

import (
	"context"
	"errors"
	"expvar"
	"log"
	nethttp "net/http"
	"time"

	docs "github.com/radiation/coyote-ci/backend/docs"
	"github.com/radiation/coyote-ci/backend/internal/artifact"
	apphttp "github.com/radiation/coyote-ci/backend/internal/http"
	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	"github.com/radiation/coyote-ci/backend/internal/platform/dbopen"
	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/service"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	managedimagesvc "github.com/radiation/coyote-ci/backend/internal/service/managedimage"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"
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
	log.Printf("database config: %s", dbopen.ConfigMode(cfg))

	dbURL, dbPoolCfg := dbopen.FromConfig(cfg)
	db, err := platformdb.Open(dbURL, dbPoolCfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("error closing database: %v", closeErr)
		}
	}()

	buildRepo := repositorypostgres.NewBuildRepository(db)
	executionJobRepo := repositorypostgres.NewExecutionJobRepository(db)
	executionJobOutputRepo := repositorypostgres.NewExecutionJobOutputRepository(db)
	jobRepo := repositorypostgres.NewJobRepository(db)
	jobManagedImageConfigRepo := repositorypostgres.NewJobManagedImageConfigRepository(db)
	sourceCredentialRepo := repositorypostgres.NewSourceCredentialRepository(db)
	managedImageCatalogRepo := repositorypostgres.NewManagedImageCatalogRepository(db)
	versionTagRepo := repositorypostgres.NewVersionTagRepository(db)
	webhookDeliveryRepo := repositorypostgres.NewWebhookDeliveryRepository(db)
	artifactRepo := repositorypostgres.NewArtifactRepository(db)
	managedImageRefresher := managedimagesvc.NewService(
		source.NewGitFetcher(),
		jobManagedImageConfigRepo,
		sourceCredentialRepo,
		managedImageCatalogRepo,
		managedimagesvc.NewDeterministicPublisher(),
		source.NewGitWriteBackClient(),
		source.NewGitHubPullRequestClient("", nil),
	)
	artifactResolver, err := artifact.ResolveStores(artifact.StoreConfig{
		Provider:    cfg.ArtifactStorageProvider,
		StorageRoot: cfg.ArtifactStorageRoot,
		GCSBucket:   cfg.ArtifactGCSBucket,
		GCSPrefix:   cfg.ArtifactGCSPrefix,
		GCSProject:  cfg.ArtifactGCSProject,
		Strict:      cfg.ArtifactStorageStrict,
	})
	if err != nil {
		log.Fatalf("failed to resolve artifact stores: %v", err)
	}
	logSink := logs.NewPostgresSink(db)
	versionTagService := versiontagsvc.NewService(versionTagRepo)
	artifactService := artifactsvc.NewService(artifactRepo)
	buildService := buildsvc.NewBuildServiceFromConfig(buildRepo, nil, logSink, buildsvc.BuildServiceConfig{
		ExecutionJobRepo:      executionJobRepo,
		ExecutionOutputRepo:   executionJobOutputRepo,
		RepoFetcher:           source.NewGitFetcher(),
		ManagedImageRefresher: managedImageRefresher,
		VersionTagger:         versionTagService,
		ArtifactRepo:          artifactRepo,
		ArtifactResolver:      artifactResolver,
		ArtifactWorkspace:     cfg.ExecutionWorkspaceRoot,
		ExecutionWorkspace:    cfg.ExecutionWorkspaceRoot,
		DefaultImage:          cfg.ExecutionDefaultImage,
	})
	jobService := service.NewJobService(jobRepo, buildService).WithManagedImageConfigRepository(jobManagedImageConfigRepo, sourceCredentialRepo)
	sourceCredentialService := service.NewSourceCredentialService(sourceCredentialRepo)
	webhookService := webhooksvc.NewDeliveryIngressService(webhookDeliveryRepo, jobService)
	webhookMetrics := observability.NewExpvarWebhookIngressMetrics()
	webhookService.SetMetrics(webhookMetrics)
	buildHandler := handler.NewBuildHandler(buildService)
	buildHandler.SetVersionTagService(versionTagService)
	artifactHandler := handler.NewArtifactHandler(artifactService)
	artifactHandler.SetVersionTagService(versionTagService)
	jobHandler := handler.NewJobHandler(jobService)
	versionTagHandler := handler.NewVersionTagHandler(versionTagService)
	credentialHandler := handler.NewSourceCredentialHandler(sourceCredentialService)
	eventHandler := handler.NewEventHandler(jobService, webhookService, webhookMetrics, cfg.GitHubWebhookSecret)
	readyHandler := handler.NewReadinessHandler(handler.ReadinessCheckFunc(func(ctx context.Context) error {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := db.PingContext(checkCtx); err != nil {
			return err
		}

		const readinessQuery = `
			SELECT
				to_regclass('public.goose_db_version') IS NOT NULL
				AND to_regclass('public.builds') IS NOT NULL
				AND to_regclass('public.build_jobs') IS NOT NULL
		`
		var ready bool
		if err := db.QueryRowContext(checkCtx, readinessQuery).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return errors.New("database schema not ready")
		}
		return nil
	}))

	router := apphttp.NewRouter(buildHandler, artifactHandler, jobHandler, versionTagHandler, credentialHandler, eventHandler, cfg.PushEventSecret)
	mux := nethttp.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	mux.Handle("/swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	mux.Handle("/readyz", nethttp.HandlerFunc(readyHandler.Ready))
	mux.Handle("/api/readyz", nethttp.HandlerFunc(readyHandler.Ready))
	mux.Handle("/", router)

	addr := ":" + cfg.AppPort
	log.Printf("starting server on %s", addr)

	if err := nethttp.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
