package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	"github.com/radiation/coyote-ci/backend/internal/service"
	storepostgres "github.com/radiation/coyote-ci/backend/internal/store/postgres"
)

const defaultPollInterval = 2 * time.Second

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

	buildStore := storepostgres.NewBuildStore(db)
	buildService := service.NewBuildService(buildStore)
	workerService := service.NewWorkerService(buildService)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("starting worker loop")
	if err := runWorkerLoop(ctx, workerService, defaultPollInterval); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker loop failed: %v", err)
	}
	log.Printf("worker stopped")
}

func runWorkerLoop(ctx context.Context, worker *service.WorkerService, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := runWorkerIteration(ctx, worker); err != nil {
				log.Printf("worker iteration failed: %v", err)
			}
		}
	}
}

func runWorkerIteration(_ context.Context, _ *service.WorkerService) error {
	// Worker claim/dequeue flow is not implemented yet; keep loop wiring minimal and runnable.
	return nil
}
