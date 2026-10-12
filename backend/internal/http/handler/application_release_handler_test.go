package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	releasesvc "github.com/radiation/coyote-ci/backend/internal/service/release"
)

func TestApplicationReleaseHandler_Authorization(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	jobID := "job-1"
	repo := &applicationReleaseHandlerRepository{release: domain.ApplicationRelease{
		ID: "release-1", BuildID: "build-1", ProjectID: fixture.projectViewer.ID, JobID: &jobID,
		ApplicationVersion: "1.2.3", Status: domain.ApplicationReleaseStatusComplete, CreatedAt: time.Now().UTC(),
	}}
	handler := NewApplicationReleaseHandler(releasesvc.NewService(repo))
	handler.SetAuthorization(auth.ModeHeader, fixture.membershipService)

	tests := []struct {
		name   string
		method string
		user   domain.User
		token  []domain.APITokenScope
		want   int
	}{
		{"reader can inspect", http.MethodGet, fixture.viewer, nil, http.StatusOK},
		{"outsider cannot inspect", http.MethodGet, fixture.outsider, nil, http.StatusForbidden},
		{"reader token requires build read", http.MethodGet, fixture.viewer, []domain.APITokenScope{domain.APITokenScopeBuildLogs}, http.StatusForbidden},
		{"reader token with build read can inspect", http.MethodGet, fixture.viewer, []domain.APITokenScope{domain.APITokenScopeBuildRead}, http.StatusOK},
		{"owner can promote", http.MethodPost, fixture.owner, nil, http.StatusOK},
		{"maintainer can promote", http.MethodPost, fixture.maintainer, nil, http.StatusOK},
		{"viewer cannot promote", http.MethodPost, fixture.viewer, nil, http.StatusForbidden},
		{"maintainer token requires build run", http.MethodPost, fixture.maintainer, []domain.APITokenScope{domain.APITokenScopeBuildRead}, http.StatusForbidden},
		{"maintainer token with build run can promote", http.MethodPost, fixture.maintainer, []domain.APITokenScope{domain.APITokenScopeBuildRun}, http.StatusOK},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := addURLParam(httptest.NewRequest(testCase.method, "/application-releases/release-1", nil), "releaseID", "release-1")
			if testCase.token != nil {
				request = withScopedAPIToken(request, testCase.user, testCase.token...)
			} else {
				request = request.WithContext(auth.WithUser(request.Context(), testCase.user))
			}
			response := httptest.NewRecorder()
			if testCase.method == http.MethodGet {
				handler.Get(response, request)
			} else {
				handler.PromoteStable(response, request)
			}
			if response.Code != testCase.want {
				t.Fatalf("status=%d, want %d body=%s", response.Code, testCase.want, response.Body.String())
			}
		})
	}
}

type applicationReleaseHandlerRepository struct {
	release domain.ApplicationRelease
}

func (r *applicationReleaseHandlerRepository) Create(context.Context, domain.ApplicationRelease) (domain.ApplicationRelease, error) {
	return r.release, nil
}
func (r *applicationReleaseHandlerRepository) GetByBuildID(context.Context, string) (domain.ApplicationRelease, error) {
	return r.release, nil
}
func (r *applicationReleaseHandlerRepository) GetByID(_ context.Context, releaseID string) (domain.ApplicationRelease, error) {
	if releaseID != r.release.ID {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
	}
	return r.release, nil
}
func (r *applicationReleaseHandlerRepository) ListPending(context.Context, int) ([]domain.ApplicationRelease, error) {
	return nil, nil
}
func (r *applicationReleaseHandlerRepository) Reconcile(context.Context, string) (domain.ApplicationRelease, error) {
	return r.release, nil
}
func (r *applicationReleaseHandlerRepository) FailBuildRelease(context.Context, string, string) (domain.ApplicationRelease, error) {
	return r.release, nil
}
func (r *applicationReleaseHandlerRepository) PromoteStable(_ context.Context, releaseID string, _ *string) (domain.ApplicationRelease, error) {
	if releaseID != r.release.ID {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
	}
	if r.release.Status != domain.ApplicationReleaseStatusComplete {
		return domain.ApplicationRelease{}, repository.ErrApplicationReleaseIncomplete
	}
	return r.release, nil
}
