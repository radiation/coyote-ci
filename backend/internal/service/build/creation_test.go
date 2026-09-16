package build

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

func TestPipelineStepsToDomainPreservesImageBuildArtifactInputs(t *testing.T) {
	resolvedInputs := []domain.ImageBuildArtifactInput{
		{Name: "coyote-server", Destination: "dist/coyote-server"},
		{Name: "coyote-worker", Destination: "dist/coyote-worker"},
	}
	steps := pipelineStepsToDomain("build-1", []pipeline.ResolvedStep{{
		ExecutionKind: domain.ExecutionKindImageBuild,
		RemoteImageBuild: &domain.RemoteImageBuildSpec{
			ContextPath:          "backend",
			DockerfilePath:       "backend/Dockerfile",
			TargetImageReference: "coyote-ci/backend",
			ArtifactInputs:       resolvedInputs,
		},
	}})

	if len(steps) != 1 || steps[0].RemoteImageBuild == nil {
		t.Fatalf("steps = %+v", steps)
	}
	got := steps[0].RemoteImageBuild.ArtifactInputs
	if len(got) != 2 || got[0].Name != "coyote-server" || got[0].Destination != "dist/coyote-server" || got[1].Name != "coyote-worker" || got[1].Destination != "dist/coyote-worker" {
		t.Fatalf("artifact inputs = %+v", got)
	}
	steps[0].RemoteImageBuild.ArtifactInputs[0].Name = "changed"
	if resolvedInputs[0].Name != "coyote-server" {
		t.Fatalf("source artifact input was mutated: %q", resolvedInputs[0].Name)
	}
}
