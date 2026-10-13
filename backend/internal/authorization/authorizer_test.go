package authorization

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestAuthorizerRequireProjectRole(t *testing.T) {
	lookup := &stubMembershipLookup{memberships: map[string]domain.ProjectMembership{
		"project-1": {ProjectID: "project-1", UserID: "viewer-1", Role: domain.ProjectMemberRoleViewer},
	}}
	viewer := Principal{User: domain.User{ID: "viewer-1", GlobalRole: domain.GlobalRoleUser}}
	admin := Principal{User: domain.User{ID: "admin-1", GlobalRole: domain.GlobalRoleAdmin}}
	outsider := Principal{User: domain.User{ID: "outsider-1", GlobalRole: domain.GlobalRoleUser}}
	authorizer := NewAuthorizer(false, lookup)

	if err := authorizer.RequireProjectRole(context.Background(), viewer, "project-1", domain.ProjectMemberRoleViewer); err != nil {
		t.Fatalf("expected viewer read access, got %v", err)
	}
	if err := authorizer.RequireProjectRole(context.Background(), viewer, "project-1", domain.ProjectMemberRoleMaintainer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected viewer maintainer denial, got %v", err)
	}
	if err := authorizer.RequireProjectRole(context.Background(), outsider, "project-1", domain.ProjectMemberRoleViewer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected outsider denial, got %v", err)
	}
	if err := authorizer.RequireProjectRole(context.Background(), admin, "project-1", domain.ProjectMemberRoleOwner); err != nil {
		t.Fatalf("expected global admin access, got %v", err)
	}
}

func TestAuthorizerRequireAPITokenScope(t *testing.T) {
	authorizer := NewAuthorizer(false, nil)
	principal := Principal{
		User: domain.User{ID: "user-1"},
		APIToken: &APITokenIdentity{
			ID:     "token-1",
			Scopes: []domain.APITokenScope{domain.APITokenScopeBuildRead},
		},
	}

	if err := authorizer.RequireAPITokenScope(principal, domain.APITokenScopeBuildRead); err != nil {
		t.Fatalf("expected scoped token to be allowed, got %v", err)
	}
	err := authorizer.RequireAPITokenScope(principal, domain.APITokenScopeBuildLogs)
	var scopeErr *MissingAPITokenScopeError
	if !errors.As(err, &scopeErr) || scopeErr.Scope != domain.APITokenScopeBuildLogs {
		t.Fatalf("expected missing build:logs error, got %v", err)
	}
	if got := scopeErr.Error(); got != "api token does not have the required scope: build:logs" {
		t.Fatalf("unexpected scope error text %q", got)
	}
}

func TestAuthorizerErrorAndDisabledCases(t *testing.T) {
	principal := Principal{User: domain.User{ID: "user-1"}}
	authorizer := NewAuthorizer(false, nil)

	if err := authorizer.RequireProjectRole(context.Background(), principal, "", domain.ProjectMemberRoleViewer); !errors.Is(err, ErrProjectIDRequired) {
		t.Fatalf("expected missing project ID error, got %v", err)
	}
	if err := authorizer.RequireProjectRole(context.Background(), Principal{}, "project-1", domain.ProjectMemberRoleViewer); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected unauthenticated error, got %v", err)
	}
	if err := authorizer.RequireProjectRole(context.Background(), principal, "project-1", domain.ProjectMemberRoleViewer); !errors.Is(err, ErrProjectAuthorizationNotConfigured) {
		t.Fatalf("expected unconfigured authorization error, got %v", err)
	}
	if err := authorizer.RequireAPITokenScope(Principal{APIToken: &APITokenIdentity{}}, domain.APITokenScopeBuildRead); !errors.Is(err, ErrAPITokenIdentityUnavailable) {
		t.Fatalf("expected unavailable token identity error, got %v", err)
	}
	if allowed, accessErr := authorizer.CanAccessProject(context.Background(), principal, "project-1", domain.ProjectMemberRoleViewer); allowed || !errors.Is(accessErr, ErrProjectAuthorizationNotConfigured) {
		t.Fatalf("expected unconfigured access result, allowed=%t err=%v", allowed, accessErr)
	}

	disabled := NewAuthorizer(true, nil)
	if err := disabled.RequireProjectRole(context.Background(), Principal{}, "", domain.ProjectMemberRoleOwner); err != nil {
		t.Fatalf("expected disabled authorization bypass, got %v", err)
	}
	allowed, filterErr := disabled.FilterProjectIDs(context.Background(), Principal{}, []string{"project-1", "project-1", " "}, domain.ProjectMemberRoleOwner)
	if filterErr != nil {
		t.Fatalf("filter disabled authorization failed: %v", filterErr)
	}
	if len(allowed) != 1 {
		t.Fatalf("expected disabled authorization to keep project IDs, got %v", allowed)
	}
}

func TestAuthorizerFilterProjectIDsUsesBatchedLookup(t *testing.T) {
	lookup := &stubMembershipLookup{
		batched: true,
		memberships: map[string]domain.ProjectMembership{
			"project-viewer": {ProjectID: "project-viewer", UserID: "user-1", Role: domain.ProjectMemberRoleViewer},
			"project-owner":  {ProjectID: "project-owner", UserID: "user-1", Role: domain.ProjectMemberRoleOwner},
		},
	}
	authorizer := NewAuthorizer(false, lookup)
	allowed, err := authorizer.FilterProjectIDs(
		context.Background(),
		Principal{User: domain.User{ID: "user-1"}},
		[]string{"project-viewer", "project-owner", "project-other", "project-owner"},
		domain.ProjectMemberRoleMaintainer,
	)
	if err != nil {
		t.Fatalf("filter project IDs failed: %v", err)
	}
	if _, ok := allowed["project-owner"]; !ok {
		t.Fatalf("expected owner project to be allowed: %v", allowed)
	}
	if _, ok := allowed["project-viewer"]; ok {
		t.Fatalf("expected viewer project to be denied: %v", allowed)
	}
	if _, ok := allowed["project-other"]; ok {
		t.Fatalf("expected missing membership project to be denied: %v", allowed)
	}
	if lookup.listCalls != 1 {
		t.Fatalf("expected one batched lookup, got %d", lookup.listCalls)
	}
	if lookup.getCalls != 0 {
		t.Fatalf("expected no individual lookups, got %d", lookup.getCalls)
	}
}

type stubMembershipLookup struct {
	memberships map[string]domain.ProjectMembership
	batched     bool
	getCalls    int
	listCalls   int
}

func (s *stubMembershipLookup) GetProjectMembership(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	s.getCalls++
	membership, ok := s.memberships[projectID]
	if !ok || membership.UserID != userID {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return membership, nil
}

func (s *stubMembershipLookup) ListProjectMembershipsByUser(_ context.Context, userID string) ([]domain.ProjectMembership, error) {
	s.listCalls++
	if !s.batched {
		return nil, errors.New("unexpected batched lookup")
	}
	memberships := make([]domain.ProjectMembership, 0, len(s.memberships))
	for _, membership := range s.memberships {
		if membership.UserID == userID {
			memberships = append(memberships, membership)
		}
	}
	return memberships, nil
}
