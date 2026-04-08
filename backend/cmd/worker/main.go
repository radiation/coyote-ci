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

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	"github.com/radiation/coyote-ci/backend/internal/platform/dbopen"
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
	artifactRepo := repositorypostgres.NewArtifactRepository(db)
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
	stepRunner := resolveStepRunner(cfg)
	logSink := logs.NewPostgresSink(db)
	buildService := service.NewBuildServiceFromConfig(buildRepo, stepRunner, logSink, service.BuildServiceConfig{
		ExecutionJobRepo:    executionJobRepo,
		ExecutionOutputRepo: executionJobOutputRepo,
		DefaultImage:        cfg.ExecutionDefaultImage,
		ExecutionWorkspace:  cfg.ExecutionWorkspaceRoot,
		ArtifactRepo:        artifactRepo,
		ArtifactResolver:    artifactResolver,
		ArtifactWorkspace:   cfg.ExecutionWorkspaceRoot,
	})
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
