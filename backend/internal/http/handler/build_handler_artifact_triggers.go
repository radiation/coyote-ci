package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
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
			artifactName = trimOptionalArtifactTriggerString(&artifact.Name)
			sizeBytes := artifact.SizeBytes
			artifactSizeBytes = &sizeBytes
		}

		deliveries = append(deliveries, api.BuildArtifactTriggerDeliveryResponse{
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
		})
	}

	writeDataJSON(w, http.StatusOK, api.BuildArtifactTriggerDeliveriesResponse{
		BuildID:                  strings.TrimSpace(buildID),
		BuildTriggerKind:         string(build.Trigger.Kind),
		RecursiveDispatchBlocked: build.Trigger.Kind == domain.BuildTriggerKindArtifact,
		Summary:                  summary,
		Deliveries:               deliveries,
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
