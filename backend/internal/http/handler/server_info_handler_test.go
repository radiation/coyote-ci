package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/versioninfo"
)

func TestServerInfoHandler_GetInfo(t *testing.T) {
	originalVersion := versioninfo.Version
	originalCommit := versioninfo.Commit
	originalBuildDate := versioninfo.BuildDate
	t.Cleanup(func() {
		versioninfo.Version = originalVersion
		versioninfo.Commit = originalCommit
		versioninfo.BuildDate = originalBuildDate
	})

	versioninfo.Version = "1.2.3"
	versioninfo.Commit = "abc123"
	versioninfo.BuildDate = "2026-07-03T12:00:00Z"

	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	res := httptest.NewRecorder()

	NewServerInfoHandler().GetInfo(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}

	var body api.ServerInfoEnvelope
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Version != "1.2.3" || body.Data.Commit != "abc123" || body.Data.BuildDate != "2026-07-03T12:00:00Z" {
		t.Fatalf("unexpected body: %+v", body.Data)
	}
	if body.Data.APIVersion != versioninfo.APIVersion {
		t.Fatalf("expected api version %q, got %q", versioninfo.APIVersion, body.Data.APIVersion)
	}
}
