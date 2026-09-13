package cloudbuild

import (
	"testing"
	"time"

	googlecloudbuild "google.golang.org/api/cloudbuild/v1"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestBuildRequestDerivesConfiguredArtifactRegistryDestination(t *testing.T) {
	client := &Client{artifactRegistryRepository: "us-central1-docker.pkg.dev/coyote-prod/ci-images", runtimeServiceAccount: "cloud-build@coyote-prod.iam.gserviceaccount.com"}
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
	result := resultFromBuild(&googlecloudbuild.Build{Status: "SUCCESS", LogUrl: "https://logs", Results: &googlecloudbuild.Results{Images: []*googlecloudbuild.BuiltImage{{Name: "registry.example/ci/coyote-ci/backend", Digest: "sha256:abc"}}}})
	if result.Status != domain.ImageBuildStatusSuccess || result.ImageDigest != "sha256:abc" || result.ExternalLogURL != "https://logs" {
		t.Fatalf("result=%+v", result)
	}
	failed := resultFromBuild(&googlecloudbuild.Build{Status: "FAILURE", StatusDetail: "status detail", FailureInfo: &googlecloudbuild.FailureInfo{Detail: "failure detail"}})
	if failed.FailureDetail != "failure detail" {
		t.Fatalf("failure result=%+v", failed)
	}
}
