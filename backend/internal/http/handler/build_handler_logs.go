package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

// GetBuildStepLogs godoc
// @Summary Get build step logs
// @Description Returns persisted log chunks for a build step.
// @Tags builds
// @Produce json
// @Param buildID path string true "Build ID"
// @Param stepIndex path int true "Step index"
// @Param after query int false "Replay cursor (exclusive sequence number)"
// @Param limit query int false "Maximum chunks to return"
// @Success 200 {object} api.StepLogsEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/steps/{stepIndex}/logs [get]
func (h *BuildHandler) GetBuildStepLogs(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "buildID")
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	if _, ok := h.authorizeBuildRead(w, r, buildID); !ok {
		return
	}
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildLogs) {
		return
	}

	stepIndex, ok := parseStepIndex(w, r)
	if !ok {
		return
	}

	var after int64
	afterStr := r.URL.Query().Get("after")
	if afterStr != "" {
		parsedAfter, err := strconv.ParseInt(afterStr, 10, 64)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid 'after' query parameter")
			return
		}
		if parsedAfter < 0 {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid 'after' query parameter")
			return
		}
		after = parsedAfter
	}

	limit := 200
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		parsedLimit, err := strconv.Atoi(limitStr)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid 'limit' query parameter")
			return
		}
		if parsedLimit < 1 {
			parsedLimit = 1
		}
		limit = parsedLimit
	}

	chunks, err := h.buildService.GetStepLogChunks(r.Context(), buildID, stepIndex, after, limit)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	respChunks := make([]api.StepLogChunkResponse, 0, len(chunks))
	next := after
	for _, chunk := range chunks {
		respChunks = append(respChunks, toStepLogChunkResponse(chunk))
		if chunk.SequenceNo > next {
			next = chunk.SequenceNo
		}
	}

	writeDataJSON(w, http.StatusOK, api.StepLogsResponse{
		BuildID:      buildID,
		StepIndex:    stepIndex,
		After:        after,
		NextSequence: next,
		Chunks:       respChunks,
	})
}

// StreamBuildStepLogs godoc
// @Summary Stream build step logs
// @Description Streams build step log chunks over SSE with cursor resume support.
// @Tags builds
// @Produce text/event-stream
// @Param buildID path string true "Build ID"
// @Param stepIndex path int true "Step index"
// @Param after query int false "Replay cursor (exclusive sequence number)"
// @Success 200 {string} string "SSE stream"
// @Failure 400 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /builds/{buildID}/steps/{stepIndex}/logs/stream [get]
func (h *BuildHandler) StreamBuildStepLogs(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "buildID")
	if buildID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "build id is required")
		return
	}
	if _, ok := h.authorizeBuildRead(w, r, buildID); !ok {
		return
	}
	if !requireAPITokenScope(w, r, domain.APITokenScopeBuildLogs) {
		return
	}

	stepIndex, ok := parseStepIndex(w, r)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "streaming not supported")
		return
	}

	after := parseQueryInt64(r, "after", 0)
	if lastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID")); lastEventID != "" {
		if parsed, err := strconv.ParseInt(lastEventID, 10, 64); err == nil && parsed > after {
			after = parsed
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeSSEComment(w, "connected")
	flusher.Flush()

	const maxSSEDuration = 30 * time.Minute
	ctx := r.Context()
	deadlineTimer := time.NewTimer(maxSSEDuration)
	defer deadlineTimer.Stop()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		chunks, err := h.buildService.GetStepLogChunks(ctx, buildID, stepIndex, after, 500)
		if err != nil {
			if errors.Is(err, buildsvc.ErrBuildNotFound) {
				return
			}
			writeSSEEvent(w, "error", 0, map[string]string{"message": err.Error()})
			flusher.Flush()
			return
		}

		for _, chunk := range chunks {
			resp := toStepLogChunkResponse(chunk)
			writeSSEEvent(w, "chunk", chunk.SequenceNo, resp)
			after = chunk.SequenceNo
		}
		flusher.Flush()

		select {
		case <-ctx.Done():
			return
		case <-deadlineTimer.C:
			writeSSEEvent(w, "timeout", 0, map[string]string{"message": "maximum stream duration exceeded"})
			flusher.Flush()
			return
		case <-ticker.C:
		}
	}
}

func parseStepIndex(w http.ResponseWriter, r *http.Request) (int, bool) {
	stepIndexRaw := chi.URLParam(r, "stepIndex")
	if strings.TrimSpace(stepIndexRaw) == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "step index is required")
		return 0, false
	}

	stepIndex, err := strconv.Atoi(stepIndexRaw)
	if err != nil || stepIndex < 0 {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "step index must be a non-negative integer")
		return 0, false
	}

	return stepIndex, true
}

func parseQueryInt64(r *http.Request, key string, fallback int64) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseQueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}
