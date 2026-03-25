package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

const defaultPollInterval = 10 * time.Second

type workerIterationService interface {
	ClaimRunnableStep(ctx context.Context) (service.RunnableStep, bool, error)
	ExecuteRunnableStep(ctx context.Context, step service.RunnableStep) (service.StepExecutionReport, error)
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
	stepRunner := inprocess.New(nil)
	logSink := logs.NewMemorySink()
	buildService := service.NewBuildService(buildRepo, stepRunner, logSink)
	workerService := service.NewWorkerService(buildService)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("starting worker loop")
	if err := runWorkerLoop(ctx, workerService, defaultPollInterval); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker loop failed: %v", err)
	}
	log.Printf("worker stopped")
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
