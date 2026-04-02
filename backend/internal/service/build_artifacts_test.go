package service

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
