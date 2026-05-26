package handler

import (
	"net/http"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
)

type WorkerHandler struct {
	workers *workersvc.VisibilityService
}

func NewWorkerHandler(workers *workersvc.VisibilityService) *WorkerHandler {
	return &WorkerHandler{workers: workers}
}

func (h *WorkerHandler) ListWorkers(w http.ResponseWriter, r *http.Request) {
	workers, err := h.workers.ListWorkers(r.Context())
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	responses := make([]api.WorkerResponse, 0, len(workers))
	for _, worker := range workers {
		responses = append(responses, toWorkerResponse(worker))
	}

	writeDataJSON(w, http.StatusOK, api.WorkerListResponse{Workers: responses})
}

func toWorkerResponse(worker domain.WorkerVisibility) api.WorkerResponse {
	var lastHeartbeatAt *string
	if !worker.LastHeartbeatAt.IsZero() {
		value := worker.LastHeartbeatAt.Format(time.RFC3339)
		lastHeartbeatAt = &value
	}

	return api.WorkerResponse{
		ID:               worker.ID,
		Name:             worker.Name,
		Status:           string(worker.Status),
		LastHeartbeatAt:  lastHeartbeatAt,
		CreatedAt:        worker.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        worker.UpdatedAt.Format(time.RFC3339),
		CurrentBuildID:   worker.CurrentBuildID,
		CurrentBuildNum:  worker.CurrentBuildNum,
		CurrentStepID:    worker.CurrentStepID,
		CurrentStepIndex: worker.CurrentStepIndex,
		CurrentStepName:  worker.CurrentStepName,
		LeaseExpiresAt:   formatOptionalTime(worker.LeaseExpiresAt),
		ClaimedAt:        formatOptionalTime(worker.ClaimedAt),
		ProjectID:        worker.ProjectID,
		ProjectName:      worker.ProjectName,
		ProjectSlug:      worker.ProjectSlug,
		JobID:            worker.JobID,
		JobName:          worker.JobName,
		StaleLease:       worker.StaleLease,
		StaleHeartbeat:   worker.StaleHeartbeat,
	}
}
