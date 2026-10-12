package authorization

import (
	"context"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrUnauthenticated = errors.New("authentication required")
var ErrForbidden = errors.New("forbidden")
var ErrProjectIDRequired = errors.New("project id is required")
var ErrProjectAuthorizationNotConfigured = errors.New("project authorization is not configured")
var ErrAPITokenIdentityUnavailable = errors.New("api token identity is not available")

type ProjectRoleLookup interface {
	GetProjectMembership(context.Context, string, string) (domain.ProjectMembership, error)
}

type ProjectMembershipBatchLookup interface {
	ListProjectMembershipsByUser(context.Context, string) ([]domain.ProjectMembership, error)
}

type Principal struct {
	User     domain.User
	APIToken *APITokenIdentity
}

type APITokenIdentity struct {
	ID     string
	Scopes []domain.APITokenScope
}

type MissingAPITokenScopeError struct {
	Scope domain.APITokenScope
}

func (e *MissingAPITokenScopeError) Error() string {
	return "api token does not have the required scope: " + string(e.Scope)
}

type Authorizer struct {
	AuthDisabled bool
	Memberships  ProjectRoleLookup
}

func NewAuthorizer(authDisabled bool, memberships ProjectRoleLookup) Authorizer {
	return Authorizer{AuthDisabled: authDisabled, Memberships: memberships}
}

func IsGlobalAdmin(user domain.User) bool {
	return user.GlobalRole == domain.GlobalRoleAdmin
}

func HasProjectRole(ctx context.Context, lookup ProjectRoleLookup, user domain.User, projectID string, minimum domain.ProjectMemberRole) (bool, error) {
	membership, err := lookup.GetProjectMembership(ctx, projectID, user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrProjectMembershipNotFound) {
			return false, nil
		}
		return false, err
	}
	return projectRoleRank(membership.Role) >= projectRoleRank(minimum), nil
}

func (a Authorizer) CanAccessProject(ctx context.Context, principal Principal, projectID string, minimum domain.ProjectMemberRole) (bool, error) {
	if a.AuthDisabled || IsGlobalAdmin(principal.User) {
		return true, nil
	}
	if a.Memberships == nil {
		return false, ErrProjectAuthorizationNotConfigured
	}
	return HasProjectRole(ctx, a.Memberships, principal.User, projectID, minimum)
}

func (a Authorizer) RequireProjectRole(ctx context.Context, principal Principal, projectID string, minimum domain.ProjectMemberRole) error {
	if a.AuthDisabled {
		return nil
	}
	if strings.TrimSpace(projectID) == "" {
		return ErrProjectIDRequired
	}
	if strings.TrimSpace(principal.User.ID) == "" {
		return ErrUnauthenticated
	}
	allowed, err := a.CanAccessProject(ctx, principal, projectID, minimum)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (a Authorizer) RequireAPITokenScope(principal Principal, scope domain.APITokenScope) error {
	if principal.APIToken == nil {
		return nil
	}
	if strings.TrimSpace(principal.APIToken.ID) == "" {
		return ErrAPITokenIdentityUnavailable
	}
	if domain.HasAPITokenScope(principal.APIToken.Scopes, scope) {
		return nil
	}
	return &MissingAPITokenScopeError{Scope: scope}
}

func (a Authorizer) FilterProjectIDs(ctx context.Context, principal Principal, projectIDs []string, minimum domain.ProjectMemberRole) (map[string]struct{}, error) {
	uniqueProjectIDs := make(map[string]struct{}, len(projectIDs))
	for _, projectID := range projectIDs {
		if trimmedProjectID := strings.TrimSpace(projectID); trimmedProjectID != "" {
			uniqueProjectIDs[trimmedProjectID] = struct{}{}
		}
	}
	if len(uniqueProjectIDs) == 0 || a.AuthDisabled || IsGlobalAdmin(principal.User) {
		return uniqueProjectIDs, nil
	}
	if strings.TrimSpace(principal.User.ID) == "" {
		return nil, ErrUnauthenticated
	}
	if a.Memberships == nil {
		return nil, ErrProjectAuthorizationNotConfigured
	}

	lookup := a.Memberships
	if batchLookup, ok := a.Memberships.(ProjectMembershipBatchLookup); ok {
		memberships, err := batchLookup.ListProjectMembershipsByUser(ctx, principal.User.ID)
		if err != nil {
			return nil, err
		}
		cachedMemberships := make(map[string]domain.ProjectMembership, len(memberships))
		for _, membership := range memberships {
			cachedMemberships[membership.ProjectID] = membership
		}
		lookup = staticProjectRoleLookup{memberships: cachedMemberships}
	}

	allowed := make(map[string]struct{}, len(uniqueProjectIDs))
	for projectID := range uniqueProjectIDs {
		hasRole, err := HasProjectRole(ctx, lookup, principal.User, projectID, minimum)
		if err != nil {
			return nil, err
		}
		if hasRole {
			allowed[projectID] = struct{}{}
		}
	}
	return allowed, nil
}

type staticProjectRoleLookup struct {
	memberships map[string]domain.ProjectMembership
}

func (s staticProjectRoleLookup) GetProjectMembership(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	membership, ok := s.memberships[projectID]
	if !ok || membership.UserID != userID {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return membership, nil
}

func projectRoleRank(role domain.ProjectMemberRole) int {
	switch role {
	case domain.ProjectMemberRoleOwner:
		return 3
	case domain.ProjectMemberRoleMaintainer:
		return 2
	case domain.ProjectMemberRoleViewer:
		return 1
	default:
		return 0
	}
}
