package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type projectAuthorizer func(context.Context, auth.ProjectRoleLookup, auth.Mode, domain.User, string) (bool, error)

func normalizedAuthMode(mode auth.Mode) auth.Mode {
	if mode == "" {
		return auth.ModeDisabled
	}
	return mode
}

func authorizeGlobalAdmin(w http.ResponseWriter, r *http.Request, mode auth.Mode, message string) bool {
	mode = normalizedAuthMode(mode)
	if mode == auth.ModeDisabled {
		return true
	}
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return false
	}
	if !auth.IsGlobalAdmin(user) {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", message)
		return false
	}
	return true
}

func authorizeProject(w http.ResponseWriter, r *http.Request, mode auth.Mode, lookup auth.ProjectRoleLookup, projectID string, check projectAuthorizer, message string) bool {
	mode = normalizedAuthMode(mode)
	if mode == auth.ModeDisabled {
		return true
	}
	if strings.TrimSpace(projectID) == "" {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "project id is required")
		return false
	}
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return false
	}
	if lookup == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "project authorization is not configured")
		return false
	}
	allowed, err := check(r.Context(), lookup, mode, user, projectID)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return false
	}
	if !allowed {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", message)
		return false
	}
	return true
}

func projectAllowed(ctx context.Context, mode auth.Mode, lookup auth.ProjectRoleLookup, projectID string, check projectAuthorizer) (bool, error) {
	mode = normalizedAuthMode(mode)
	if mode == auth.ModeDisabled {
		return true, nil
	}
	if lookup == nil || strings.TrimSpace(projectID) == "" {
		return false, nil
	}
	user, ok := auth.CurrentUser(ctx)
	if !ok {
		return false, nil
	}
	return check(ctx, lookup, mode, user, projectID)
}
