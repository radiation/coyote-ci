package cloudbuild

import (
	"testing"
	"time"

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
