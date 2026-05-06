package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type ProjectMembershipHandler struct {
	memberships *service.ProjectMembershipService
	authMode    auth.Mode
}

func NewProjectMembershipHandler(memberships *service.ProjectMembershipService, authMode auth.Mode) *ProjectMembershipHandler {
	return &ProjectMembershipHandler{memberships: memberships, authMode: authMode}
}

// ListProjectMembers godoc
// @Summary List project members
// @Description Lists project memberships with basic user fields.
// @Tags project-memberships
// @Produce json
// @Param id path string true "Project ID"
// @Success 200 {object} api.ProjectMembershipListEnvelope
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects/{id}/members [get]
func (h *ProjectMembershipHandler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID := projectIDParam(r)
	if !h.canManageProjectMembers(w, r, projectID) {
		return
	}

	members, err := h.memberships.ListProjectMembers(r.Context(), projectID)
	if err != nil {
		h.writeMembershipError(w, err)
		return
	}
	responses := make([]api.ProjectMembershipResponse, 0, len(members))
	for _, member := range members {
		responses = append(responses, toProjectMembershipWithUserResponse(member))
	}
	writeDataJSON(w, http.StatusOK, api.ProjectMembershipListResponse{Members: responses})
}

// UpsertProjectMember godoc
// @Summary Add or replace project member
// @Description Creates or updates a project membership.
// @Tags project-memberships
// @Accept json
// @Produce json
// @Param id path string true "Project ID"
// @Param user_id path string true "User ID"
// @Param request body api.UpsertProjectMembershipRequest true "Project membership request"
// @Success 200 {object} api.ProjectMembershipEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects/{id}/members/{user_id} [put]
func (h *ProjectMembershipHandler) UpsertProjectMember(w http.ResponseWriter, r *http.Request) {
	h.upsertProjectMember(w, r)
}

// UpdateProjectMember godoc
// @Summary Update project member
// @Description Updates a project membership role.
// @Tags project-memberships
// @Accept json
// @Produce json
// @Param id path string true "Project ID"
// @Param user_id path string true "User ID"
// @Param request body api.UpsertProjectMembershipRequest true "Project membership request"
// @Success 200 {object} api.ProjectMembershipEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects/{id}/members/{user_id} [patch]
func (h *ProjectMembershipHandler) UpdateProjectMember(w http.ResponseWriter, r *http.Request) {
	h.upsertProjectMember(w, r)
}

func (h *ProjectMembershipHandler) upsertProjectMember(w http.ResponseWriter, r *http.Request) {
	projectID := projectIDParam(r)
	if !h.canManageProjectMembers(w, r, projectID) {
		return
	}

	var req api.UpsertProjectMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	membership, err := h.memberships.UpsertProjectMembership(r.Context(), service.UpsertProjectMembershipInput{
		ProjectID: projectID,
		UserID:    strings.TrimSpace(chi.URLParam(r, "user_id")),
		Role:      req.Role,
	})
	if err != nil {
		h.writeMembershipError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toProjectMembershipResponse(membership))
}

// DeleteProjectMember godoc
// @Summary Delete project member
// @Description Removes a project membership.
// @Tags project-memberships
// @Param id path string true "Project ID"
// @Param user_id path string true "User ID"
// @Success 204
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /projects/{id}/members/{user_id} [delete]
func (h *ProjectMembershipHandler) DeleteProjectMember(w http.ResponseWriter, r *http.Request) {
	projectID := projectIDParam(r)
	if !h.canManageProjectMembers(w, r, projectID) {
		return
	}

	if err := h.memberships.DeleteProjectMembership(r.Context(), projectID, strings.TrimSpace(chi.URLParam(r, "user_id"))); err != nil {
		h.writeMembershipError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProjectMembershipHandler) canManageProjectMembers(w http.ResponseWriter, r *http.Request, projectID string) bool {
	user, _ := auth.CurrentUser(r.Context())
	allowed, err := auth.CanManageProjectMembers(r.Context(), h.memberships, h.authMode, user, projectID)
	if err != nil {
		h.writeMembershipError(w, err)
		return false
	}
	if !allowed {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", "global admin or project owner is required")
		return false
	}
	return true
}

func (h *ProjectMembershipHandler) writeMembershipError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrProjectNotFound) || errors.Is(err, repository.ErrUserNotFound) || errors.Is(err, repository.ErrProjectMembershipNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, service.ErrProjectMembershipRoleInvalid) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func projectIDParam(r *http.Request) string {
	projectID := strings.TrimSpace(chi.URLParam(r, "project_id"))
	if projectID == "" {
		projectID = strings.TrimSpace(chi.URLParam(r, "id"))
	}
	return projectID
}

func toProjectMembershipResponse(membership domain.ProjectMembership) api.ProjectMembershipResponse {
	return api.ProjectMembershipResponse{
		ProjectID: membership.ProjectID,
		UserID:    membership.UserID,
		Role:      string(membership.Role),
		CreatedAt: membership.CreatedAt.Format(time.RFC3339),
		UpdatedAt: membership.UpdatedAt.Format(time.RFC3339),
	}
}

func toProjectMembershipWithUserResponse(membership domain.ProjectMembershipWithUser) api.ProjectMembershipResponse {
	response := toProjectMembershipResponse(membership.ProjectMembership)
	response.Email = membership.Email
	response.DisplayName = membership.DisplayName
	return response
}
