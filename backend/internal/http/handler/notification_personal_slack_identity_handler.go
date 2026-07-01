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
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

// GetMySlackIdentity godoc
// @Summary Get my Slack identity
// @Description Returns the authenticated user's linked personal Slack identity and safe workspace readiness metadata.
// @Tags users
// @Produce json
// @Success 200 {object} api.MySlackIdentityEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/slack-identity [get]
func (h *NotificationHandler) GetMySlackIdentity(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.personalSlack == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	state, err := h.personalSlack.Get(r.Context(), user)
	if err != nil {
		h.writePersonalSlackIdentityError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toMySlackIdentityResponse(state))
}

// ResolveMySlackIdentity godoc
// @Summary Resolve my Slack identity
// @Description Resolves a personal Slack identity candidate for the authenticated user by exact authenticated-email match without persisting it.
// @Tags users
// @Accept json
// @Produce json
// @Param request body api.ResolveMySlackIdentityRequest true "Resolve my Slack identity request"
// @Success 200 {object} api.ResolveMySlackIdentityEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse
// @Failure 502 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/slack-identity/resolve [post]
func (h *NotificationHandler) ResolveMySlackIdentity(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.personalSlack == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req api.ResolveMySlackIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Method != service.SlackIdentityResolutionMethodAuthenticatedEmail {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", service.ErrUserSlackIdentityResolutionMethodInvalid.Error())
		return
	}

	candidate, matched, err := h.personalSlack.ResolveByAuthenticatedEmail(r.Context(), user)
	if err != nil {
		log.Printf("WARN personal slack resolve failed: actor_user_id=%s err=%v", strings.TrimSpace(user.ID), err)
		h.writePersonalSlackIdentityError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toResolveMySlackIdentityResponse(req.Method, candidate, matched))
}

// CreateMySlackIdentity godoc
// @Summary Link my Slack identity
// @Description Confirms and persists the authenticated user's personal Slack identity after server-side revalidation.
// @Tags users
// @Accept json
// @Produce json
// @Param request body api.CreateMySlackIdentityRequest true "Link my Slack identity request"
// @Success 200 {object} api.UserSlackIdentityEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse
// @Failure 502 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/slack-identity [post]
func (h *NotificationHandler) CreateMySlackIdentity(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.personalSlack == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req api.CreateMySlackIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	linked, err := h.personalSlack.Link(r.Context(), user, service.LinkUserSlackIdentityInput{
		ResolutionMethod:       req.ResolutionMethod,
		WorkspaceIntegrationID: req.WorkspaceIntegrationID,
		SlackWorkspaceID:       req.SlackWorkspaceID,
		SlackUserID:            req.SlackUserID,
	})
	if err != nil {
		h.writePersonalSlackIdentityError(w, err)
		return
	}

	state, stateErr := h.personalSlack.Get(r.Context(), user)
	if stateErr != nil {
		h.writePersonalSlackIdentityError(w, stateErr)
		return
	}
	writeDataJSON(w, http.StatusOK, toUserSlackIdentityResponse(linked, state.Workspace))
}

// PatchMySlackIdentity godoc
// @Summary Enable or disable my Slack identity
// @Description Pauses or resumes the authenticated user's linked personal Slack identity without changing the linked Slack member.
// @Tags users
// @Accept json
// @Produce json
// @Param request body api.PatchMySlackIdentityRequest true "Patch my Slack identity request"
// @Success 200 {object} api.UserSlackIdentityEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/slack-identity [patch]
func (h *NotificationHandler) PatchMySlackIdentity(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.personalSlack == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req api.PatchMySlackIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	linked, err := h.personalSlack.SetEnabled(r.Context(), user, req.Enabled)
	if err != nil {
		h.writePersonalSlackIdentityError(w, err)
		return
	}

	state, stateErr := h.personalSlack.Get(r.Context(), user)
	if stateErr != nil {
		h.writePersonalSlackIdentityError(w, stateErr)
		return
	}
	writeDataJSON(w, http.StatusOK, toUserSlackIdentityResponse(linked, state.Workspace))
}

// DeleteMySlackIdentity godoc
// @Summary Unlink my Slack identity
// @Description Removes the authenticated user's linked personal Slack identity without changing the Slack workspace integration.
// @Tags users
// @Produce json
// @Success 204 {object} nil
// @Failure 401 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/slack-identity [delete]
func (h *NotificationHandler) DeleteMySlackIdentity(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.personalSlack == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "notification endpoint is not available")
		return
	}

	user, ok := h.currentRequestUser(r)
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	if err := h.personalSlack.Unlink(r.Context(), user); err != nil {
		h.writePersonalSlackIdentityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *NotificationHandler) writePersonalSlackIdentityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrUserSlackIdentityUserIDRequired),
		errors.Is(err, service.ErrUserSlackIdentityEmailRequired),
		errors.Is(err, service.ErrUserSlackIdentityResolutionMethodInvalid),
		errors.Is(err, service.ErrUserSlackIdentitySlackWorkspaceIDRequired),
		errors.Is(err, service.ErrUserSlackIdentitySlackUserIDRequired),
		errors.Is(err, service.ErrUserSlackIdentityWorkspaceIntegrationIDRequired),
		errors.Is(err, service.ErrUserSlackIdentityEnabledRequired):
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, service.ErrUserSlackIdentityWorkspaceNotConfigured),
		errors.Is(err, service.ErrUserSlackIdentityWorkspaceDisabled),
		errors.Is(err, service.ErrUserSlackIdentityCandidateChanged),
		errors.Is(err, service.ErrUserSlackIdentityConflict),
		errors.Is(err, repository.ErrUserSlackIdentityConflict),
		errors.Is(err, repository.ErrSlackWorkspaceIntegrationLinkedIdentitiesExist):
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, service.ErrUserSlackIdentityMemberUnavailable),
		errors.Is(err, repository.ErrUserSlackIdentityNotFound):
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, service.ErrSlackWorkspaceInvalidAuth),
		errors.Is(err, service.ErrSlackWorkspaceTokenRevoked),
		errors.Is(err, service.ErrSlackWorkspaceAccountInactive),
		errors.Is(err, service.ErrSlackWorkspaceMalformedResponse):
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, service.ErrSlackWorkspaceRateLimited):
		writeErrorJSON(w, http.StatusTooManyRequests, "rate_limited", err.Error())
	case errors.Is(err, service.ErrSlackWorkspaceUpstream), errors.Is(err, context.DeadlineExceeded):
		writeErrorJSON(w, http.StatusBadGateway, "upstream_failure", "slack request failed")
	case errors.Is(err, context.Canceled):
		writeErrorJSON(w, http.StatusBadGateway, "upstream_failure", "slack request canceled")
	default:
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func toMySlackIdentityResponse(state service.UserSlackIdentityState) api.MySlackIdentityResponse {
	response := api.MySlackIdentityResponse{WorkspaceStatus: state.WorkspaceStatus}
	if state.Workspace != nil {
		workspace := toSlackIdentityWorkspaceResponse(*state.Workspace)
		response.Workspace = &workspace
	}
	if state.Identity != nil {
		identity := toUserSlackIdentityResponse(*state.Identity, state.Workspace)
		response.Identity = &identity
	}
	return response
}

func toResolveMySlackIdentityResponse(method string, candidate *service.ResolvedUserSlackIdentityCandidate, matched bool) api.ResolveMySlackIdentityResponse {
	response := api.ResolveMySlackIdentityResponse{Method: method, Matched: matched}
	if candidate != nil {
		candidateResponse := toResolvedSlackIdentityCandidateResponse(*candidate)
		response.Candidate = &candidateResponse
	}
	return response
}

func toResolvedSlackIdentityCandidateResponse(candidate service.ResolvedUserSlackIdentityCandidate) api.ResolvedSlackIdentityCandidateResponse {
	return api.ResolvedSlackIdentityCandidateResponse{
		Workspace:       toSlackIdentityWorkspaceResponse(candidate.Workspace),
		SlackUserID:     candidate.SlackUserID,
		DisplayName:     candidate.DisplayName,
		RealName:        candidate.RealName,
		Handle:          candidate.Handle,
		ProfileImageURL: candidate.ProfileImageURL,
	}
}

func toUserSlackIdentityResponse(identity domain.UserSlackIdentity, workspace *service.SlackWorkspaceReference) api.UserSlackIdentityResponse {
	response := api.UserSlackIdentityResponse{
		ID:              identity.ID,
		SlackUserID:     identity.SlackUserID,
		DisplayName:     identity.SlackDisplayName,
		RealName:        identity.SlackRealName,
		Handle:          identity.SlackHandle,
		ProfileImageURL: identity.ProfileImageURL,
		Enabled:         identity.Enabled,
		LinkedAt:        identity.LinkedAt.Format(time.RFC3339),
	}
	if workspace != nil {
		workspaceResponse := toSlackIdentityWorkspaceResponse(*workspace)
		response.Workspace = workspaceResponse
	}
	if identity.LastVerifiedAt != nil {
		value := identity.LastVerifiedAt.Format(time.RFC3339)
		response.LastVerifiedAt = &value
	}
	return response
}

func toSlackIdentityWorkspaceResponse(workspace service.SlackWorkspaceReference) api.SlackIdentityWorkspaceResponse {
	return api.SlackIdentityWorkspaceResponse{
		ID:                workspace.ID,
		SlackWorkspaceID:  workspace.SlackWorkspaceID,
		Name:              workspace.Name,
		LastTestSucceeded: workspace.LastTestSucceeded,
	}
}
