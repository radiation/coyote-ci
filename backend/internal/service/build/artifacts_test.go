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

func TestStepArtifactDeclarationsFromBuild_UsesResolvedStepOrder(t *testing.T) {
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
              name: coyote-ci/frontend
              type: docker_image
        - name: backend
          run: go build ./...
          artifacts:
            - path: backend/dist/coyote-server
              name: coyote-ci/server
              type: unknown
`

	declarations, err := stepArtifactDeclarationsFromBuild(domain.Build{PipelineConfigYAML: &yaml})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(declarations[0]) != 1 {
		t.Fatalf("expected one declaration for resolved step 0, got %#v", declarations[0])
	}
	if declarations[0][0].Path != "frontend-image.tar" || declarations[0][0].Name != "coyote-ci/frontend" || declarations[0][0].Type != domain.ArtifactTypeDockerImage {
		t.Fatalf("unexpected step 0 declaration: %#v", declarations[0][0])
	}
	if len(declarations[1]) != 1 {
		t.Fatalf("expected one declaration for resolved step 1, got %#v", declarations[1])
	}
	if declarations[1][0].Path != "backend/dist/coyote-server" || declarations[1][0].Name != "coyote-ci/server" || declarations[1][0].Type != domain.ArtifactTypeUnknown {
		t.Fatalf("unexpected step 1 declaration: %#v", declarations[1][0])
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
