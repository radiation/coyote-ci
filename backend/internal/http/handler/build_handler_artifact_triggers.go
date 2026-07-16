package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

// GetBuildArtifactTriggers godoc
// @Summary List build artifact trigger deliveries
// @Description Returns persisted producer-side artifact trigger delivery records for a build.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Success 200 {object} api.BuildArtifactTriggerDeliveriesEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/artifact-triggers [get]
func (h *BuildHandler) GetBuildArtifactTriggers(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "buildID")
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	build, ok := h.authorizeBuildRead(w, r, buildID)
	if !ok {
		return
	}
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRead) {
		return
	}
	if h.jobs == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "job service is not configured")
		return
	}

	artifacts, err := h.buildService.GetBuildArtifacts(r.Context(), buildID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	views, err := h.jobs.ListArtifactTriggerDeliveriesByProducerBuildID(r.Context(), buildID)
	if err != nil {
		if err == service.ErrJobArtifactTriggerDeliveryRepositoryNotConfigured {
			writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "artifact trigger delivery repository is not configured")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	artifactsByID := make(map[string]domain.BuildArtifact, len(artifacts))
	for _, artifact := range artifacts {
		artifactsByID[artifact.ID] = artifact
	}

	deliveries := make([]api.BuildArtifactTriggerDeliveryResponse, 0, len(views))
	summary := api.BuildArtifactTriggerDeliverySummaryResponse{}
	for _, view := range views {
		delivery := view.Delivery
		summary.DeliveryCount++
		switch delivery.Status {
		case domain.ArtifactTriggerDeliveryStatusQueued:
			summary.QueuedCount++
		case domain.ArtifactTriggerDeliveryStatusFailed:
			summary.FailedCount++
		}

		var artifactName *string
		var artifactSizeBytes *int64
		if artifact, ok := artifactsByID[delivery.ArtifactID]; ok {
			artifactName, artifactSizeBytes = buildArtifactTriggerArtifactMetadata(artifact)
		}

		deliveries = append(deliveries, toBuildArtifactTriggerDeliveryResponse(view, artifactName, artifactSizeBytes))
	}

	writeDataJSON(w, http.StatusOK, api.BuildArtifactTriggerDeliveriesResponse{
		BuildID:                  strings.TrimSpace(buildID),
		BuildTriggerKind:         string(build.Trigger.Kind),
		RecursiveDispatchBlocked: build.Trigger.Kind == domain.BuildTriggerKindArtifact,
		Summary:                  summary,
		Deliveries:               deliveries,
	})
}

// RetryArtifactTriggerDelivery godoc
// @Summary Retry artifact trigger delivery
// @Description Retries a failed artifact-trigger delivery by delivery id.
// @Tags artifact-trigger-deliveries
// @Produce json
// @Param deliveryID path string true "Delivery ID"
// @Success 200 {object} api.BuildArtifactTriggerDeliveryRetryEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /artifact-trigger-deliveries/{deliveryID}/retry [post]
func (h *BuildHandler) RetryArtifactTriggerDelivery(w http.ResponseWriter, r *http.Request) {
	deliveryID := strings.TrimSpace(chi.URLParam(r, "deliveryID"))
	if deliveryID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "delivery id is required")
		return
	}
	if h.jobs == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "job service is not configured")
		return
	}
	view, err := h.jobs.GetArtifactTriggerDeliveryByID(r.Context(), deliveryID)
	if err != nil {
		h.writeArtifactTriggerDeliveryError(w, err)
		return
	}
	if !authorizeProject(w, r, h.authMode, h.projectRoles, view.Delivery.ProducerProjectID, auth.CanTriggerBuild, "project owner or maintainer is required") {
		return
	}
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildRun) {
		return
	}
	retryResult, err := h.jobs.RetryArtifactTriggerDelivery(r.Context(), deliveryID)
	if err != nil {
		h.writeArtifactTriggerDeliveryError(w, err)
		return
	}
	artifactName, artifactSizeBytes, artifactErr := h.buildArtifactTriggerArtifactMetadata(r, retryResult.View.Delivery)
	if artifactErr != nil {
		h.writeArtifactTriggerDeliveryError(w, artifactErr)
		return
	}
	writeDataJSON(w, http.StatusOK, api.BuildArtifactTriggerDeliveryRetryResponse{
		Result:   retryResult.Result,
		Message:  retryResult.Message,
		Delivery: toBuildArtifactTriggerDeliveryResponse(retryResult.View, artifactName, artifactSizeBytes),
	})
}

func timeFormatRFC3339() string {
	return "2006-01-02T15:04:05Z07:00"
}

func trimOptionalArtifactTriggerString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (h *BuildHandler) buildArtifactTriggerArtifactMetadata(r *http.Request, delivery domain.ArtifactTriggerDelivery) (*string, *int64, error) {
	artifacts, err := h.buildService.GetBuildArtifacts(r.Context(), delivery.ProducerBuildID)
	if err != nil {
		return nil, nil, err
	}
	for _, artifact := range artifacts {
		if artifact.ID == delivery.ArtifactID {
			artifactName, artifactSizeBytes := buildArtifactTriggerArtifactMetadata(artifact)
			return artifactName, artifactSizeBytes, nil
		}
	}
	return nil, nil, nil
}

func buildArtifactTriggerArtifactMetadata(artifact domain.BuildArtifact) (*string, *int64) {
	artifactName := trimOptionalArtifactTriggerString(&artifact.Name)
	sizeBytes := artifact.SizeBytes
	return artifactName, &sizeBytes
}

func toBuildArtifactTriggerDeliveryResponse(view service.ArtifactTriggerDeliveryView, artifactName *string, artifactSizeBytes *int64) api.BuildArtifactTriggerDeliveryResponse {
	delivery := view.Delivery
	return api.BuildArtifactTriggerDeliveryResponse{
		DeliveryID:        delivery.ID,
		Status:            string(delivery.Status),
		CreatedAt:         delivery.CreatedAt.Format(timeFormatRFC3339()),
		UpdatedAt:         delivery.UpdatedAt.Format(timeFormatRFC3339()),
		ProducerBuildID:   delivery.ProducerBuildID,
		ProducerProjectID: delivery.ProducerProjectID,
		ProducerJobID:     delivery.ProducerJobID,
		ArtifactID:        delivery.ArtifactID,
		ArtifactPath:      delivery.ArtifactPath,
		ArtifactName:      artifactName,
		ArtifactSizeBytes: artifactSizeBytes,
		ConsumerJobID:     delivery.ConsumerJobID,
		ConsumerJobName:   view.ConsumerJobName,
		DownstreamBuildID: delivery.QueuedBuildID,
		ErrorMessage:      trimOptionalArtifactTriggerString(delivery.ErrorMessage),
	}
}

func (h *BuildHandler) writeArtifactTriggerDeliveryError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrJobArtifactTriggerDeliveryRepositoryNotConfigured) || errors.Is(err, service.ErrJobBuildServiceNotConfigured) {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if errors.Is(err, repository.ErrArtifactTriggerDeliveryNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "artifact_trigger_delivery_not_found", "artifact trigger delivery not found")
		return
	}
	if errors.Is(err, service.ErrArtifactTriggerDeliveryQueuedBuildConflict) || errors.Is(err, service.ErrArtifactTriggerDeliveryPendingRetryDeferred) || errors.Is(err, service.ErrArtifactTriggerDeliveryRetryNotSupported) {
		writeErrorJSON(w, http.StatusConflict, "artifact_trigger_delivery_not_retryable", err.Error())
		return
	}
	h.writeServiceError(w, err)
}
