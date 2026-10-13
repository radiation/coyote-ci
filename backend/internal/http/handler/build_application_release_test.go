package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	releasesvc "github.com/radiation/coyote-ci/backend/internal/service/release"
)

func TestApplicationReleaseHandlerGetByBuild(t *testing.T) {
	fixture := newHandlerAuthzFixture(t)
	repo := &applicationReleaseHandlerRepository{release: domain.ApplicationRelease{
		ID: "release-1", BuildID: "build-1", ProjectID: fixture.projectViewer.ID, ApplicationVersion: "1.2.3",
		Status: domain.ApplicationReleaseStatusComplete, CreatedAt: time.Now().UTC(),
		Outputs: []domain.ApplicationReleaseOutput{{ID: "output-1", Kind: domain.ApplicationReleaseOutputImage, OutputKey: "image:server", Status: domain.ApplicationReleaseOutputPublished, DefinitionJSON: `{"target_image_reference":"coyote-server"}`}},
	}}
	handler := NewApplicationReleaseHandler(releasesvc.NewService(repo))
	handler.SetAuthorization(auth.ModeHeader, fixture.membershipService)
	for _, testCase := range []struct {
		name string
		user domain.User
		want int
	}{
		{name: "reader", user: fixture.viewer, want: http.StatusOK},
		{name: "outsider", user: fixture.outsider, want: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := addURLParam(httptest.NewRequest(http.MethodGet, "/api/builds/build-1/application-release", nil), "buildID", "build-1")
			request = request.WithContext(auth.WithUser(request.Context(), testCase.user))
			response := httptest.NewRecorder()
			handler.GetByBuild(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if testCase.want == http.StatusOK && !strings.Contains(response.Body.String(), `"target_image_reference":"coyote-server"`) {
				t.Fatalf("response lacks target reference: %s", response.Body.String())
			}
		})
	}
}
