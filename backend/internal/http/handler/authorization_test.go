package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNormalizedAuthMode(t *testing.T) {
	if normalizedAuthMode("") != auth.ModeDisabled {
		t.Fatalf("expected empty mode to normalize to disabled")
	}
	if normalizedAuthMode(auth.ModeOIDC) != auth.ModeOIDC {
		t.Fatalf("expected explicit mode to be preserved")
	}
}

func TestAuthorizeGlobalAdmin(t *testing.T) {
	admin := domain.User{ID: "admin-1", Email: "admin@example.com", GlobalRole: domain.GlobalRoleAdmin}
	member := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}

	tests := []struct {
		name           string
		mode           auth.Mode
		ctx            context.Context
		expectedOK     bool
		expectedStatus int
	}{
		{name: "disabled mode bypasses auth", mode: auth.ModeDisabled, ctx: context.Background(), expectedOK: true, expectedStatus: http.StatusOK},
		{name: "missing user is unauthorized", mode: auth.ModeOIDC, ctx: context.Background(), expectedOK: false, expectedStatus: http.StatusUnauthorized},
		{name: "non-admin is forbidden", mode: auth.ModeOIDC, ctx: auth.WithUser(context.Background(), member), expectedOK: false, expectedStatus: http.StatusForbidden},
		{name: "admin is allowed", mode: auth.ModeOIDC, ctx: auth.WithUser(context.Background(), admin), expectedOK: true, expectedStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(tc.ctx)
			response := httptest.NewRecorder()
			ok := authorizeGlobalAdmin(response, request, tc.mode, "global admin is required")
			if ok != tc.expectedOK {
				t.Fatalf("expected ok=%t, got %t", tc.expectedOK, ok)
			}
			if response.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d", tc.expectedStatus, response.Code)
			}
		})
	}
}

func TestAuthorizeProject(t *testing.T) {
	lookup := stubProjectRoleLookup{membership: domain.ProjectMembership{ProjectID: "project-1", UserID: "user-1", Role: domain.ProjectMemberRoleViewer}}
	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
	checkErr := errors.New("lookup failed")

	tests := []struct {
		name           string
		mode           auth.Mode
		projectID      string
		ctx            context.Context
		lookup         auth.ProjectRoleLookup
		check          projectAuthorizer
		expectedOK     bool
		expectedStatus int
	}{
		{name: "disabled mode bypasses checks", mode: auth.ModeDisabled, projectID: "", ctx: context.Background(), expectedOK: true, expectedStatus: http.StatusOK},
		{name: "blank project id is rejected", mode: auth.ModeOIDC, projectID: " ", ctx: auth.WithUser(context.Background(), user), lookup: lookup, check: auth.CanReadProject, expectedOK: false, expectedStatus: http.StatusBadRequest},
		{name: "missing user is unauthorized", mode: auth.ModeOIDC, projectID: "project-1", ctx: context.Background(), lookup: lookup, check: auth.CanReadProject, expectedOK: false, expectedStatus: http.StatusUnauthorized},
		{name: "missing lookup is internal error", mode: auth.ModeOIDC, projectID: "project-1", ctx: auth.WithUser(context.Background(), user), check: auth.CanReadProject, expectedOK: false, expectedStatus: http.StatusInternalServerError},
		{name: "authorizer error is internal error", mode: auth.ModeOIDC, projectID: "project-1", ctx: auth.WithUser(context.Background(), user), lookup: lookup, check: func(context.Context, auth.ProjectRoleLookup, auth.Mode, domain.User, string) (bool, error) {
			return false, checkErr
		}, expectedOK: false, expectedStatus: http.StatusInternalServerError},
		{name: "forbidden member is rejected", mode: auth.ModeOIDC, projectID: "project-1", ctx: auth.WithUser(context.Background(), user), lookup: lookup, check: auth.CanManageProjectJobs, expectedOK: false, expectedStatus: http.StatusForbidden},
		{name: "allowed member passes", mode: auth.ModeOIDC, projectID: "project-1", ctx: auth.WithUser(context.Background(), user), lookup: lookup, check: auth.CanReadProject, expectedOK: true, expectedStatus: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(tc.ctx)
			response := httptest.NewRecorder()
			ok := authorizeProject(response, request, tc.mode, tc.lookup, tc.projectID, tc.check, "forbidden")
			if ok != tc.expectedOK {
				t.Fatalf("expected ok=%t, got %t", tc.expectedOK, ok)
			}
			if response.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d", tc.expectedStatus, response.Code)
			}
		})
	}
}

func TestProjectAllowed(t *testing.T) {
	user := domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser}
	lookup := stubProjectRoleLookup{membership: domain.ProjectMembership{ProjectID: "project-1", UserID: "user-1", Role: domain.ProjectMemberRoleMaintainer}}
	checkErr := errors.New("boom")

	allowed, err := projectAllowed(context.Background(), auth.ModeDisabled, nil, "", auth.CanReadProject)
	if err != nil || !allowed {
		t.Fatalf("expected disabled mode to allow access, got allowed=%t err=%v", allowed, err)
	}

	allowed, err = projectAllowed(context.Background(), auth.ModeOIDC, nil, "project-1", auth.CanReadProject)
	if err != nil || allowed {
		t.Fatalf("expected nil lookup to deny without error, got allowed=%t err=%v", allowed, err)
	}

	allowed, err = projectAllowed(context.Background(), auth.ModeOIDC, lookup, " ", auth.CanReadProject)
	if err != nil || allowed {
		t.Fatalf("expected blank project id to deny without error, got allowed=%t err=%v", allowed, err)
	}

	allowed, err = projectAllowed(context.Background(), auth.ModeOIDC, lookup, "project-1", auth.CanReadProject)
	if err != nil || allowed {
		t.Fatalf("expected missing user to deny without error, got allowed=%t err=%v", allowed, err)
	}

	allowed, err = projectAllowed(auth.WithUser(context.Background(), user), auth.ModeOIDC, lookup, "project-1", auth.CanManageProjectJobs)
	if err != nil || !allowed {
		t.Fatalf("expected maintainer access, got allowed=%t err=%v", allowed, err)
	}

	allowed, err = projectAllowed(auth.WithUser(context.Background(), user), auth.ModeOIDC, lookup, "project-1", func(context.Context, auth.ProjectRoleLookup, auth.Mode, domain.User, string) (bool, error) {
		return false, checkErr
	})
	if !errors.Is(err, checkErr) || allowed {
		t.Fatalf("expected propagated error, got allowed=%t err=%v", allowed, err)
	}
}

type stubProjectRoleLookup struct {
	membership domain.ProjectMembership
	err        error
}

func (s stubProjectRoleLookup) GetProjectMembership(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	if s.err != nil {
		return domain.ProjectMembership{}, s.err
	}
	if s.membership.ProjectID != projectID || s.membership.UserID != userID {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return s.membership, nil
}

func decodeErrorResponse(t *testing.T, response *httptest.ResponseRecorder) api.ErrorResponse {
	t.Helper()
	var payload api.ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error response failed: %v", err)
	}
	return payload
}
