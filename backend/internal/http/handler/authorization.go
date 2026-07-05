package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type projectAuthorizer func(context.Context, auth.ProjectRoleLookup, auth.Mode, domain.User, string) (bool, error)
type projectMembershipBatchLookup interface {
	ListProjectMembershipsByUser(context.Context, string) ([]domain.ProjectMembership, error)
}
type staticProjectRoleLookup struct {
	memberships map[string]domain.ProjectMembership
}

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

func requireAPITokenScope(w http.ResponseWriter, r *http.Request, scope domain.APITokenScope) bool {
	method, ok := auth.CurrentAuthMethod(r.Context())
	if !ok || method != auth.MethodAPIToken {
		return true
	}
	token, ok := auth.CurrentAuthenticatedAPIToken(r.Context())
	if !ok {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "api token context is not available")
		return false
	}
	if domain.HasAPITokenScope(token.Scopes, scope) {
		return true
	}
	writeErrorJSON(w, http.StatusForbidden, "missing_token_scope", "api token does not have the required scope: "+string(scope))
	return false
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

func allowedProjectsForUser(ctx context.Context, mode auth.Mode, lookup auth.ProjectRoleLookup, projectIDs []string, check projectAuthorizer) (map[string]struct{}, error) {
	mode = normalizedAuthMode(mode)
	if mode == auth.ModeDisabled {
		return nil, nil
	}
	if lookup == nil {
		return map[string]struct{}{}, nil
	}
	user, ok := auth.CurrentUser(ctx)
	if !ok {
		return map[string]struct{}{}, nil
	}

	uniqueProjectIDs := make(map[string]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		trimmedProjectID := strings.TrimSpace(projectID)
		if trimmedProjectID != "" {
			uniqueProjectIDs[trimmedProjectID] = struct{}{}
		}
	}
	if len(uniqueProjectIDs) == 0 {
		return map[string]struct{}{}, nil
	}
	if auth.IsGlobalAdmin(user) {
		return uniqueProjectIDs, nil
	}

	effectiveLookup := lookup
	if batchLookup, ok := lookup.(projectMembershipBatchLookup); ok {
		memberships, err := batchLookup.ListProjectMembershipsByUser(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		cachedMemberships := make(map[string]domain.ProjectMembership, len(memberships))
		for _, membership := range memberships {
			cachedMemberships[membership.ProjectID] = membership
		}
		effectiveLookup = staticProjectRoleLookup{memberships: cachedMemberships}
	}

	allowed := make(map[string]struct{}, len(uniqueProjectIDs))
	for projectID := range uniqueProjectIDs {
		projectAllowed, err := check(ctx, effectiveLookup, mode, user, projectID)
		if err != nil {
			return nil, err
		}
		if projectAllowed {
			allowed[projectID] = struct{}{}
		}
	}
	return allowed, nil
}

func (s staticProjectRoleLookup) GetProjectMembership(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	membership, ok := s.memberships[projectID]
	if !ok || membership.UserID != userID {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return membership, nil
}
