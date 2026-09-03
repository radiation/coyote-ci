package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type workspaceHelperCapabilityExchanger interface {
	Exchange(context.Context, string, domain.WorkspaceHelperCapability) (string, domain.WorkspaceHelperCapability, error)
}

type WorkspaceHelperHandler struct {
	capabilities workspaceHelperCapabilityExchanger
}

func NewWorkspaceHelperHandler(capabilities workspaceHelperCapabilityExchanger) *WorkspaceHelperHandler {
	return &WorkspaceHelperHandler{capabilities: capabilities}
}

func (h *WorkspaceHelperHandler) ExchangeCapability(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.capabilities == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "unavailable", "workspace helper exchange is not configured")
		return
	}
	projectedToken, ok := bearerToken(r)
	if !ok || projectedToken == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workload identity is required")
		return
	}
	var request api.WorkspaceHelperCapabilityExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	capability := domain.WorkspaceHelperCapability{ExecutionJobID: strings.TrimSpace(request.ExecutionJobID), PodUID: strings.TrimSpace(request.PodUID), Role: domain.WorkspaceHelperRole(strings.TrimSpace(request.Role))}
	token, issued, err := h.capabilities.Exchange(r.Context(), projectedToken, capability)
	if err != nil {
		if errors.Is(err, service.ErrWorkspaceHelperUnauthorized) || errors.Is(err, service.ErrWorkspaceHelperCapabilityMalformed) {
			log.Printf("WARN workspace helper capability denied execution_job_id=%s pod_uid=%s role=%s", capability.ExecutionJobID, capability.PodUID, capability.Role)
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper authorization failed")
			return
		}
		log.Printf("ERROR workspace helper capability exchange failed execution_job_id=%s pod_uid=%s role=%s err=%v", capability.ExecutionJobID, capability.PodUID, capability.Role, err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	log.Printf("INFO workspace helper capability issued execution_job_id=%s pod_uid=%s role=%s", issued.ExecutionJobID, issued.PodUID, issued.Role)
	writeDataJSON(w, http.StatusCreated, api.WorkspaceHelperCapabilityExchangeResponse{Capability: token, ExpiresAt: issued.ExpiresAt.UTC().Format(time.RFC3339)})
}

func bearerToken(r *http.Request) (string, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}
