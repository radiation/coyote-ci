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

type UserHandler struct {
	users    *service.UserService
	authMode auth.Mode
}

func NewUserHandler(users *service.UserService, authMode auth.Mode) *UserHandler {
	return &UserHandler{users: users, authMode: authMode}
}

// ListUsers godoc
// @Summary List users
// @Description Lists Coyote users.
// @Tags users
// @Produce json
// @Success 200 {object} api.UserListEnvelope
// @Failure 403 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /users [get]
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.canManageUsers(r) {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", "global admin is required")
		return
	}

	users, err := h.users.ListUsers(r.Context())
	if err != nil {
		h.writeUserError(w, err)
		return
	}
	responses := make([]api.UserResponse, 0, len(users))
	for _, user := range users {
		responses = append(responses, toUserResponse(user))
	}
	writeDataJSON(w, http.StatusOK, api.UserListResponse{Users: responses})
}

// CreateUser godoc
// @Summary Create user
// @Description Creates a user without storing credentials.
// @Tags users
// @Accept json
// @Produce json
// @Param request body api.CreateUserRequest true "User create request"
// @Success 201 {object} api.UserEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /users [post]
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if !h.canManageUsers(r) {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", "global admin is required")
		return
	}

	var req api.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	user, err := h.users.CreateUser(r.Context(), service.CreateUserInput{
		Email:       req.Email,
		DisplayName: req.DisplayName,
		GlobalRole:  req.GlobalRole,
	})
	if err != nil {
		h.writeUserError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, toUserResponse(user))
}

// GetUser godoc
// @Summary Get user
// @Description Returns one user.
// @Tags users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} api.UserEnvelope
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /users/{id} [get]
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	if !h.canManageUsers(r) {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", "global admin is required")
		return
	}

	user, err := h.users.GetUser(r.Context(), strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		h.writeUserError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toUserResponse(user))
}

// UpdateUser godoc
// @Summary Update user
// @Description Updates user metadata and global role.
// @Tags users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param request body api.UpdateUserRequest true "User update request"
// @Success 200 {object} api.UserEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /users/{id} [patch]
func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if !h.canManageUsers(r) {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", "global admin is required")
		return
	}

	var req api.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	user, err := h.users.UpdateUser(r.Context(), strings.TrimSpace(chi.URLParam(r, "id")), service.UpdateUserInput{
		Email:       req.Email,
		DisplayName: service.OptionalStringPatch{Set: req.DisplayName.Set, Value: req.DisplayName.Value},
		GlobalRole:  req.GlobalRole,
	})
	if err != nil {
		h.writeUserError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toUserResponse(user))
}

// DeleteUser godoc
// @Summary Delete user
// @Description Deletes a user and cascades project memberships in durable storage.
// @Tags users
// @Param id path string true "User ID"
// @Success 204
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /users/{id} [delete]
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if !h.canManageUsers(r) {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", "global admin is required")
		return
	}

	if err := h.users.DeleteUser(r.Context(), strings.TrimSpace(chi.URLParam(r, "id"))); err != nil {
		h.writeUserError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetMe godoc
// @Summary Get current user
// @Description Returns the resolved request identity, or a synthetic disabled-mode user.
// @Tags users
// @Produce json
// @Success 200 {object} api.MeEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Router /me [get]
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		user = auth.DisabledModeUser()
	}
	authMethod := ""
	if method, ok := auth.CurrentAuthMethod(r.Context()); ok {
		authMethod = string(method)
	}
	writeDataJSON(w, http.StatusOK, api.MeResponse{AuthMode: string(h.authMode), AuthMethod: authMethod, EmailVerified: nil, User: toUserResponse(user)})
}

func (h *UserHandler) GetAuthConfig(w http.ResponseWriter, _ *http.Request) {
	var loginURL *string
	if h.authMode == auth.ModeOIDC {
		url := "/auth/login"
		loginURL = &url
	}
	writeDataJSON(w, http.StatusOK, api.AuthConfigResponse{AuthMode: string(h.authMode), LoginURL: loginURL})
}

func (h *UserHandler) canManageUsers(r *http.Request) bool {
	user, _ := auth.CurrentUser(r.Context())
	return auth.CanManageUsers(h.authMode, user)
}

func (h *UserHandler) writeUserError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrUserNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, repository.ErrUserEmailConflict) {
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if errors.Is(err, service.ErrUserEmailRequired) || errors.Is(err, service.ErrUserGlobalRoleInvalid) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func toUserResponse(user domain.User) api.UserResponse {
	return api.UserResponse{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		GlobalRole:  string(user.GlobalRole),
		CreatedAt:   formatUserTime(user.CreatedAt),
		UpdatedAt:   formatUserTime(user.UpdatedAt),
	}
}

func formatUserTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
