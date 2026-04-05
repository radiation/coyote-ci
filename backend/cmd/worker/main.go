package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	nethttp "net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/storage"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	dockerrunner "github.com/radiation/coyote-ci/backend/internal/runner/docker"
	"github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
	"github.com/radiation/coyote-ci/backend/internal/service"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

const defaultPollInterval = 10 * time.Second

type workerIterationService interface {
	ClaimRunnableStep(ctx context.Context) (service.RunnableStep, bool, error)
	ExecuteRunnableStep(ctx context.Context, step service.RunnableStep) (service.StepExecutionReport, error)
}

type workerStatusProvider interface {
	RecoveryStats() service.WorkerRecoveryStats
}

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

	buildRepo := repositorypostgres.NewBuildRepository(db)
	executionJobRepo := repositorypostgres.NewExecutionJobRepository(db)
	executionJobOutputRepo := repositorypostgres.NewExecutionJobOutputRepository(db)
	artifactRepo := repositorypostgres.NewArtifactRepository(db)
	artifactStore, artifactProvider := resolveArtifactStore(cfg)
	stepRunner := resolveStepRunner(cfg)
	logSink := logs.NewPostgresSink(db)
	buildService := service.NewBuildService(buildRepo, stepRunner, logSink)
	buildService.SetExecutionJobRepository(executionJobRepo)
	buildService.SetExecutionJobOutputRepository(executionJobOutputRepo)
	buildService.SetDefaultExecutionImage(cfg.ExecutionDefaultImage)
	buildService.SetExecutionWorkspaceRoot(cfg.ExecutionWorkspaceRoot)
	buildService.SetArtifactPersistence(artifactRepo, artifactStore, cfg.ExecutionWorkspaceRoot, artifactProvider)
	leaseDuration := time.Duration(cfg.StepLeaseSeconds) * time.Second
	workerService := service.NewWorkerServiceWithLease(buildService, defaultWorkerID(), leaseDuration)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startWorkerStatusServer(ctx, cfg.WorkerStatusAddr, workerService)

	log.Printf("starting worker loop")
	if err := runWorkerLoop(ctx, workerService, defaultPollInterval); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker loop failed: %v", err)
	}
	log.Printf("worker stopped")
}

func resolveStepRunner(cfg config.Config) runner.Runner {
	switch strings.ToLower(strings.TrimSpace(cfg.ExecutionBackend)) {
	case "", "docker":
		workspace := source.NewHostWorkspaceMaterializer(cfg.ExecutionWorkspaceRoot)
		return dockerrunner.New(dockerrunner.Options{
			Workspace:         workspace,
			DefaultImage:      cfg.ExecutionDefaultImage,
			MountDockerSocket: cfg.MountDockerSocket,
		})
	case "inprocess", "local":
		return inprocess.NewWithWorkspaceRoot(cfg.ExecutionWorkspaceRoot)
	default:
		log.Printf("unknown execution backend %q; falling back to inprocess", cfg.ExecutionBackend)
		return inprocess.NewWithWorkspaceRoot(cfg.ExecutionWorkspaceRoot)
	}
}

func defaultWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}

	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func runWorkerLoop(ctx context.Context, worker workerIterationService, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := runWorkerIteration(ctx, worker); err != nil {
				log.Printf("worker polling/claiming error: %v", err)
			}
		}
	}
}

func runWorkerIteration(ctx context.Context, worker workerIterationService) error {
	log.Printf("polling for runnable work")

	step, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		return err
	}
	if !found {
		log.Printf("no runnable work found")
		return nil
	}

	if _, err := worker.ExecuteRunnableStep(ctx, step); err != nil {
		return err
	}
	log.Printf("worker iteration completed for claimed work: build_id=%s step=%s", step.BuildID, step.StepName)

	return nil
}

func startWorkerStatusServer(ctx context.Context, addr string, worker workerStatusProvider) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return
	}

	srv := &nethttp.Server{
		Addr:    trimmed,
		Handler: newWorkerStatusHandler(worker),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("worker status server shutdown error: %v", err)
		}
	}()

	go func() {
		log.Printf("worker status server listening on %s", trimmed)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			log.Printf("worker status server error: %v", err)
		}
	}()
}

func newWorkerStatusHandler(worker workerStatusProvider) nethttp.Handler {
	mux := nethttp.NewServeMux()
	mux.HandleFunc("/healthz", func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/internal/status/worker", func(w nethttp.ResponseWriter, req *nethttp.Request) {
		if req.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}

		resp := struct {
			WorkerRecovery service.WorkerRecoveryStats `json:"worker_recovery"`
			TimestampUTC   time.Time                   `json:"timestamp_utc"`
		}{
			WorkerRecovery: worker.RecoveryStats(),
			TimestampUTC:   time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			w.WriteHeader(nethttp.StatusInternalServerError)
			return
		}
	})

	return mux
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
