package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
)

type workerHandlerBuildBoundary struct{}

func (workerHandlerBuildBoundary) ListActiveBuilds(_ context.Context) ([]domain.Build, error) {
	return []domain.Build{}, nil
}

func (workerHandlerBuildBoundary) GetBuildSteps(_ context.Context, _ string) ([]domain.BuildStep, error) {
	return []domain.BuildStep{}, nil
}

func (workerHandlerBuildBoundary) GetJobsByBuildID(_ context.Context, _ string) ([]domain.ExecutionJob, error) {
	return []domain.ExecutionJob{}, nil
}

func TestWorkerHandler_ListWorkers(t *testing.T) {
	repo := memoryrepo.NewWorkerRepository()
	now := time.Now().UTC()
	_, _ = repo.UpsertHeartbeat(context.Background(), domain.WorkerHeartbeat{
		ID:          "worker-a",
		Name:        "worker-a",
		HeartbeatAt: now,
	})

	svc := workersvc.NewVisibilityService(repo, workerHandlerBuildBoundary{})
	svc.SetStaleAfter(90 * time.Second)
	h := NewWorkerHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/workers", nil)
	rr := httptest.NewRecorder()
	h.ListWorkers(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var envelope struct {
		Data struct {
			Workers []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"workers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(envelope.Data.Workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(envelope.Data.Workers))
	}
	if envelope.Data.Workers[0].ID != "worker-a" {
		t.Fatalf("expected worker id worker-a, got %q", envelope.Data.Workers[0].ID)
	}
	if envelope.Data.Workers[0].Status != "idle" {
		t.Fatalf("expected worker status idle, got %q", envelope.Data.Workers[0].Status)
	}
}
