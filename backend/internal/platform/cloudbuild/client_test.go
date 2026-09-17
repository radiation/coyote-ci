package cloudbuild

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	googlecloudbuild "google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestBuildRequestDerivesConfiguredArtifactRegistryDestination(t *testing.T) {
	client := &Client{projectID: "coyote-prod", artifactRegistryRepository: "us-central1-docker.pkg.dev/coyote-prod/ci-images", runtimeServiceAccount: "cloud-build@coyote-prod.iam.gserviceaccount.com"}
	build, err := client.buildRequest(domain.ImageBuildRequest{
		ExecutionJobID: "job-1",
		Source:         domain.ImageBuildSource{Bucket: "sources", Object: "job-1.tar.gz", Generation: "42"},
		Spec:           domain.RemoteImageBuildSpec{ContextPath: "backend", DockerfilePath: "backend/Dockerfile", TargetImageReference: "coyote-ci/backend"},
		Timeout:        10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	want := "us-central1-docker.pkg.dev/coyote-prod/ci-images/coyote-ci/backend"
	if len(build.Images) != 1 || build.Images[0] != want || build.Steps[0].Args[2] != "--tag="+want {
		t.Fatalf("image destination=%#v args=%#v, want %q", build.Images, build.Steps[0].Args, want)
	}
	if build.Timeout != "600s" {
		t.Fatalf("timeout=%q, want protobuf duration seconds", build.Timeout)
	}
	if build.Options == nil || build.Options.Logging != "CLOUD_LOGGING_ONLY" {
		t.Fatalf("logging options=%+v, want CLOUD_LOGGING_ONLY", build.Options)
	}
	if build.ServiceAccount != "projects/coyote-prod/serviceAccounts/cloud-build@coyote-prod.iam.gserviceaccount.com" {
		t.Fatalf("service account=%q", build.ServiceAccount)
	}
}

func TestBuildRequestSelectsExplicitTarget(t *testing.T) {
	client := &Client{artifactRegistryRepository: "registry.example/ci"}
	request := domain.ImageBuildRequest{ExecutionJobID: "job-1", Source: domain.ImageBuildSource{Generation: "1"}, Spec: domain.RemoteImageBuildSpec{ContextPath: "backend", DockerfilePath: "backend/Dockerfile", Target: "artifact-runtime", TargetImageReference: "coyote-ci/backend"}, Artifacts: []domain.ImageBuildArtifact{{Name: "coyote-server"}}}
	build, err := client.buildRequest(request)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if strings.Join(build.Steps[0].Args, " ") == "" || !contains(build.Steps[0].Args, "--target=artifact-runtime") {
		t.Fatalf("build args=%#v, want artifact runtime target", build.Steps[0].Args)
	}
	request.Spec.Target = ""
	build, err = client.buildRequest(request)
	if err != nil || contains(build.Steps[0].Args, "--target=artifact-runtime") {
		t.Fatalf("artifact build args=%#v err=%v", build.Steps[0].Args, err)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestBuildRequestRejectsNonLogicalImageName(t *testing.T) {
	client := &Client{artifactRegistryRepository: "us-central1-docker.pkg.dev/coyote-prod/ci-images"}
	_, err := client.buildRequest(domain.ImageBuildRequest{Spec: domain.RemoteImageBuildSpec{TargetImageReference: "../production/api"}})
	if err == nil {
		t.Fatal("expected non-logical image name to be rejected")
	}
}

func TestBuildRequestRejectsInvalidSourceGeneration(t *testing.T) {
	client := &Client{artifactRegistryRepository: "registry.example/ci"}
	_, err := client.buildRequest(domain.ImageBuildRequest{Source: domain.ImageBuildSource{Generation: "not-a-generation"}, Spec: domain.RemoteImageBuildSpec{TargetImageReference: "coyote-ci/backend"}})
	if err == nil {
		t.Fatal("expected invalid source generation to be rejected")
	}
}

func TestClientHelpers(t *testing.T) {
	client := &Client{projectID: "project", location: "us-central1", artifactRegistryRepository: "registry.example/ci"}
	if got := client.resourceName(domain.ImageBuildHandle{ID: "build-1"}); got != "projects/project/locations/us-central1/builds/build-1" {
		t.Fatalf("resource name=%q", got)
	}
	if got := client.resourceName(domain.ImageBuildHandle{ResourceName: "explicit"}); got != "explicit" {
		t.Fatalf("explicit resource name=%q", got)
	}
	for input, want := range map[string]domain.ImageBuildStatus{"PENDING": domain.ImageBuildStatusQueued, "QUEUED": domain.ImageBuildStatusQueued, "WORKING": domain.ImageBuildStatusRunning, "SUCCESS": domain.ImageBuildStatusSuccess, "CANCELLED": domain.ImageBuildStatusCanceled, "FAILURE": domain.ImageBuildStatusFailed} {
		if got := mapStatus(input); got != want {
			t.Fatalf("status %q=%q, want %q", input, got, want)
		}
	}
	result := resultFromBuild(&googlecloudbuild.Build{Status: "SUCCESS", CreateTime: "2026-09-14T10:00:00Z", StartTime: "2026-09-14T10:01:00Z", FinishTime: "2026-09-14T10:03:00Z", LogUrl: "https://logs", Results: &googlecloudbuild.Results{Images: []*googlecloudbuild.BuiltImage{{Name: "registry.example/ci/coyote-ci/backend", Digest: "sha256:abc"}}}})
	if result.Status != domain.ImageBuildStatusSuccess || result.ImageDigest != "sha256:abc" || result.ExternalLogURL != "https://logs" {
		t.Fatalf("result=%+v", result)
	}
	if result.Timing == nil || len(result.Timing.Phases) != 4 || result.Timing.Phases[1].Name != "queue" || result.Timing.Phases[1].StartedAt == nil || result.Timing.Phases[1].FinishedAt == nil || result.Timing.Phases[1].FinishedAt.Sub(*result.Timing.Phases[1].StartedAt) != time.Minute {
		t.Fatalf("timing=%+v", result.Timing)
	}
	failed := resultFromBuild(&googlecloudbuild.Build{Status: "FAILURE", StatusDetail: "status detail", FailureInfo: &googlecloudbuild.FailureInfo{Detail: "failure detail"}})
	if failed.FailureDetail != "failure detail" {
		t.Fatalf("failure result=%+v", failed)
	}
}

func TestClientCallsCloudBuildAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/builds"):
			var build googlecloudbuild.Build
			if decodeErr := json.NewDecoder(request.Body).Decode(&build); decodeErr != nil {
				t.Fatalf("decode submitted build: %v", decodeErr)
			}
			if build.Timeout != "60s" {
				t.Fatalf("submitted timeout=%q, want %q", build.Timeout, "60s")
			}
			_, _ = writer.Write([]byte(`{"metadata":{"build":{"id":"build-1","name":"projects/project/locations/us-central1/builds/build-1"}}}`))
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/builds/build-1"):
			_, _ = writer.Write([]byte(`{"id":"build-1","status":"SUCCESS","results":{"images":[{"digest":"sha256:abc"}]}}`))
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/builds"):
			_, _ = writer.Write([]byte(`{"builds":[{"id":"build-1","name":"projects/project/locations/us-central1/builds/build-1"}]}`))
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, ":cancel"):
			_, _ = writer.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()
	client, newErr := New(context.Background(), Config{ProjectID: "project", Location: "us-central1", RuntimeServiceAccount: "build@example.com", ArtifactRegistryRepository: "registry.example/ci"}, option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if newErr != nil {
		t.Fatalf("new client: %v", newErr)
	}
	handle, submitErr := client.Submit(context.Background(), domain.ImageBuildRequest{ExecutionJobID: "job-1", Source: domain.ImageBuildSource{Bucket: "sources", Object: "source.tar.gz", Generation: "1"}, Spec: domain.RemoteImageBuildSpec{ContextPath: "backend", DockerfilePath: "backend/Dockerfile", TargetImageReference: "coyote-ci/backend"}, Timeout: time.Minute})
	if submitErr != nil || handle.ID != "build-1" {
		t.Fatalf("submit handle=%+v err=%v", handle, submitErr)
	}
	result, getErr := client.Get(context.Background(), handle)
	if getErr != nil || result.ImageDigest != "sha256:abc" {
		t.Fatalf("get result=%+v err=%v", result, getErr)
	}
	adopted, found, findErr := client.FindByExecutionJobID(context.Background(), "job-1")
	if findErr != nil || !found || adopted.ID != "build-1" {
		t.Fatalf("find handle=%+v found=%t err=%v", adopted, found, findErr)
	}
	if cancelErr := client.Cancel(context.Background(), handle); cancelErr != nil {
		t.Fatalf("cancel: %v", cancelErr)
	}
}

func TestClientSubmitClassifiesProviderErrors(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		statusCode int
		retryable  bool
	}{
		{name: "bad request", statusCode: http.StatusBadRequest},
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, retryable: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(testCase.statusCode)
				_, _ = writer.Write([]byte(`{"error":{"code":` + strconv.Itoa(testCase.statusCode) + `,"message":"provider failure"}}`))
			}))
			defer server.Close()

			client, newErr := New(context.Background(), Config{ProjectID: "project", Location: "us-central1", RuntimeServiceAccount: "build@example.com", ArtifactRegistryRepository: "registry.example/ci"}, option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
			if newErr != nil {
				t.Fatalf("new client: %v", newErr)
			}
			_, submitErr := client.Submit(context.Background(), domain.ImageBuildRequest{ExecutionJobID: "job-1", Source: domain.ImageBuildSource{Bucket: "sources", Object: "source.tar.gz", Generation: "1"}, Spec: domain.RemoteImageBuildSpec{ContextPath: "backend", DockerfilePath: "backend/Dockerfile", TargetImageReference: "coyote-ci/backend"}, Timeout: time.Minute})
			var classifiedErr *submissionError
			if !errors.As(submitErr, &classifiedErr) || classifiedErr.Retryable() != testCase.retryable {
				t.Fatalf("submit error=%v retryable=%t, want %t", submitErr, classifiedErr != nil && classifiedErr.Retryable(), testCase.retryable)
			}
		})
	}
}

func TestNewRequiresCloudBuildConfiguration(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("expected incomplete configuration error")
	}
}

func TestIsRetryableAPIError(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "bad request", err: &googleapi.Error{Code: http.StatusBadRequest}, want: false},
		{name: "rate limited", err: &googleapi.Error{Code: http.StatusTooManyRequests}, want: true},
		{name: "unavailable", err: &googleapi.Error{Code: http.StatusServiceUnavailable}, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isRetryableAPIError(testCase.err); got != testCase.want {
				t.Fatalf("retryable=%t, want %t", got, testCase.want)
			}
		})
	}
}

func TestIsRetryableAPIErrorForContextAndNetworkErrors(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "context canceled", err: context.Canceled},
		{name: "context deadline", err: context.DeadlineExceeded},
		{name: "network timeout", err: &net.DNSError{IsTimeout: true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if !isRetryableAPIError(testCase.err) {
				t.Fatalf("error %v must be retryable", testCase.err)
			}
		})
	}
}

func TestSubmissionErrorPreservesCauseAndRetryability(t *testing.T) {
	cause := errors.New("cloud build unavailable")
	err := &submissionError{err: cause, retryable: true}
	if err.Error() != cause.Error() || !errors.Is(err, cause) || !err.Retryable() {
		t.Fatalf("submission error=%v retryable=%t", err, err.Retryable())
	}
}
