package build

import (
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestArtifactPatternsFromBuild_RepoPipelinePathScoped(t *testing.T) {
	source := pipelineSourceRepo
	pipelinePath := "scenarios/success-basic/coyote.yml"
	yaml := `
version: 1
steps:
  - name: run
    run: ./scripts/run.sh
artifacts:
  paths:
    - output/**
`

	patterns, err := artifactPatternsFromBuild(domain.Build{
		PipelineConfigYAML: &yaml,
		PipelineSource:     &source,
		PipelinePath:       &pipelinePath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0] != "scenarios/success-basic/output/**" {
		t.Fatalf("expected scoped artifact path, got %q", patterns[0])
	}
}

func TestArtifactPatternsFromBuild_RepoRootPipelineUnchanged(t *testing.T) {
	source := pipelineSourceRepo
	pipelinePath := "coyote.yml"
	yaml := `
version: 1
steps:
  - name: run
    run: ./scripts/run.sh
artifacts:
  paths:
    - output/**
`

	patterns, err := artifactPatternsFromBuild(domain.Build{
		PipelineConfigYAML: &yaml,
		PipelineSource:     &source,
		PipelinePath:       &pipelinePath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0] != "output/**" {
		t.Fatalf("expected root artifact path, got %q", patterns[0])
	}
}

func TestArtifactPatternsFromBuild_DefaultRepoPipelinePathUnchanged(t *testing.T) {
	source := pipelineSourceRepo
	pipelinePath := ".coyote/pipeline.yml"
	yaml := `
version: 1
steps:
  - name: run
    run: ./scripts/run.sh
artifacts:
  paths:
    - output/**
`

	patterns, err := artifactPatternsFromBuild(domain.Build{
		PipelineConfigYAML: &yaml,
		PipelineSource:     &source,
		PipelinePath:       &pipelinePath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0] != "output/**" {
		t.Fatalf("expected default pipeline artifact path rooted at workspace, got %q", patterns[0])
	}
}

func TestArtifactPatternsFromBuild_InlinePipelineUnchanged(t *testing.T) {
	source := pipelineSourceInline
	yaml := `
version: 1
steps:
  - name: run
    run: ./scripts/run.sh
artifacts:
  paths:
    - output/**
`

	patterns, err := artifactPatternsFromBuild(domain.Build{
		PipelineConfigYAML: &yaml,
		PipelineSource:     &source,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0] != "output/**" {
		t.Fatalf("expected inline artifact path, got %q", patterns[0])
	}
}

func TestArtifactPatternsFromBuild_RejectsTraversal(t *testing.T) {
	source := pipelineSourceRepo
	pipelinePath := "scenarios/success-basic/coyote.yml"
	yaml := `
version: 1
steps:
  - name: run
    run: ./scripts/run.sh
artifacts:
  paths:
    - ../../secret/**
`

	_, err := artifactPatternsFromBuild(domain.Build{
		PipelineConfigYAML: &yaml,
		PipelineSource:     &source,
		PipelinePath:       &pipelinePath,
	})
	if err == nil {
		t.Fatal("expected traversal error")
	}
	if !strings.Contains(err.Error(), "workspace") && !strings.Contains(err.Error(), "repository root") {
		t.Fatalf("expected traversal validation error, got %v", err)
	}
}

func TestArtifactDeclarationsFromBuild_PreservesExplicitType(t *testing.T) {
	source := pipelineSourceInline
	yaml := `
version: 1
steps:
  - name: run
    run: ./scripts/run.sh
artifacts:
  - path: images/backend-image.tar
    type: docker_image
`

	declarations, err := artifactDeclarationsFromBuild(domain.Build{
		PipelineConfigYAML: &yaml,
		PipelineSource:     &source,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(declarations) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(declarations))
	}
	if declarations[0].Type != domain.ArtifactTypeDockerImage {
		t.Fatalf("expected docker_image type, got %q", declarations[0].Type)
	}
}

func TestArtifactDeclarationsFromBuild_PreservesExplicitName(t *testing.T) {
	source := pipelineSourceInline
	yaml := `
version: 1
steps:
  - name: run
    run: ./scripts/run.sh
artifacts:
  - path: images/backend-image.tar
    name: coyote-ci/backend
    type: docker_image
`

	declarations, err := artifactDeclarationsFromBuild(domain.Build{
		PipelineConfigYAML: &yaml,
		PipelineSource:     &source,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(declarations) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(declarations))
	}
	if declarations[0].Name != "coyote-ci/backend" {
		t.Fatalf("expected named declaration, got %q", declarations[0].Name)
	}
}

func TestStepArtifactTypeHintsFromBuild_UsesResolvedStepOrder(t *testing.T) {
	yaml := `
version: 1
steps:
  - group:
      name: build
      steps:
        - name: frontend
          run: npm run build
          artifacts:
            - path: frontend-image.tar
              type: docker_image
        - name: backend
          run: go build ./...
          artifacts:
            - path: backend/dist/coyote-server
              type: unknown
`

	hints, err := stepArtifactTypeHintsFromBuild(domain.Build{PipelineConfigYAML: &yaml})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hints[0]["frontend-image.tar"] != domain.ArtifactTypeDockerImage {
		t.Fatalf("expected docker_image hint for resolved step 0, got %#v", hints[0])
	}
	if hints[1]["backend/dist/coyote-server"] != domain.ArtifactTypeUnknown {
		t.Fatalf("expected unknown hint for resolved step 1, got %#v", hints[1])
	}
}

func TestArtifactIdentityKey(t *testing.T) {
	stepA := "step-a"
	stepB := "step-b"

	tests := []struct {
		name     string
		stepID   *string
		path     string
		expected string
	}{
		{"shared", nil, "dist/app", "shared:dist/app"},
		{"step-scoped", &stepA, "dist/app", "step:step-a:dist/app"},
		{"different steps same path differ", &stepB, "dist/app", "step:step-b:dist/app"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := artifactIdentityKey(tc.stepID, tc.path)
			if got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestSkipPathsForScope(t *testing.T) {
	stepA := "step-a"
	stepB := "step-b"
	identityKeys := map[string]struct{}{
		"step:step-a:dist/app":        {},
		"step:step-b:dist/app":        {},
		"shared:reports/junit.xml":    {},
		"step:step-a:reports/out.xml": {},
	}

	t.Run("step-a scope", func(t *testing.T) {
		skip := skipPathsForScope(identityKeys, &stepA)
		if len(skip) != 2 {
			t.Fatalf("expected 2 skip paths for step-a, got %d: %v", len(skip), skip)
		}
		if _, ok := skip["dist/app"]; !ok {
			t.Fatal("expected dist/app in skip set")
		}
		if _, ok := skip["reports/out.xml"]; !ok {
			t.Fatal("expected reports/out.xml in skip set")
		}
	})

	t.Run("step-b scope", func(t *testing.T) {
		skip := skipPathsForScope(identityKeys, &stepB)
		if len(skip) != 1 {
			t.Fatalf("expected 1 skip path for step-b, got %d: %v", len(skip), skip)
		}
		if _, ok := skip["dist/app"]; !ok {
			t.Fatal("expected dist/app in skip set")
		}
	})

	t.Run("shared scope", func(t *testing.T) {
		skip := skipPathsForScope(identityKeys, nil)
		if len(skip) != 1 {
			t.Fatalf("expected 1 skip path for shared, got %d: %v", len(skip), skip)
		}
		if _, ok := skip["reports/junit.xml"]; !ok {
			t.Fatal("expected reports/junit.xml in skip set")
		}
	})
}
