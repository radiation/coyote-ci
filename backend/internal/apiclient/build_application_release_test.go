package apiclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientGetBuildApplicationRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/builds/build-1/application-release" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"release-1","build_id":"build-1","project_id":"project-1","application_version":"1.2.3","status":"complete","outputs":[{"key":"image:server","target_image_reference":"coyote-server"}]}}`))
	}))
	defer server.Close()
	client, newErr := New(server.URL, "token", "coyote/test", nil)
	if newErr != nil {
		t.Fatalf("New(): %v", newErr)
	}
	release, getErr := client.GetBuildApplicationRelease(context.Background(), "build-1")
	if getErr != nil || release.ID != "release-1" || release.Outputs[0].TargetImageReference != "coyote-server" {
		t.Fatalf("GetBuildApplicationRelease()=%+v, %v", release, getErr)
	}
}

func TestClientGetBuildApplicationReleaseNotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, newErr := New(server.URL, "", "coyote/test", nil)
	if newErr != nil {
		t.Fatalf("New(): %v", newErr)
	}
	_, getErr := client.GetBuildApplicationRelease(context.Background(), "missing")
	var apiErr *Error
	if !errors.As(getErr, &apiErr) || apiErr.Kind != ErrorKindNotFound {
		t.Fatalf("error=%v, want not-found API error", getErr)
	}
}
