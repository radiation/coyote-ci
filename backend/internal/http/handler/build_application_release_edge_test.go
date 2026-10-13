package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	releasesvc "github.com/radiation/coyote-ci/backend/internal/service/release"
)

func TestApplicationReleaseHandlerGetByBuildRejectsUnavailableAndMissingBuildID(t *testing.T) {
	t.Run("release support unavailable", func(t *testing.T) {
		response := httptest.NewRecorder()
		NewApplicationReleaseHandler(nil).GetByBuild(response, httptest.NewRequest(http.MethodGet, "/api/builds/build-1/application-release", nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
	t.Run("build ID missing", func(t *testing.T) {
		response := httptest.NewRecorder()
		NewApplicationReleaseHandler(releasesvc.NewService(&applicationReleaseHandlerRepository{})).GetByBuild(response, httptest.NewRequest(http.MethodGet, "/api/builds//application-release", nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
}
