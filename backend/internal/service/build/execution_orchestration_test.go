package build

import (
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestBuildServiceDeclaredOutputsForBuild(t *testing.T) {
	svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
	pipelineYAML := "version: 1\nsteps:\n  - name: build\n    run: make build\nartifacts:\n  paths:\n    - dist/**\n    - reports/*.xml\n"
	jobs := []domain.ExecutionJob{{ID: "job-1", BuildID: "build-1"}, {ID: "job-2", BuildID: "build-1"}}

	outputs, outputErr := svc.declaredOutputsForBuild(domain.Build{ID: "build-1", PipelineConfigYAML: stringPtr(pipelineYAML)}, jobs)
	if outputErr != nil {
		t.Fatalf("declared outputs returned error: %v", outputErr)
	}
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}
	if outputs[0].JobID != "job-2" || outputs[1].JobID != "job-2" {
		t.Fatalf("expected outputs declared on last job, got %+v", outputs)
	}
	if outputs[0].DeclaredPath != "dist/**" || outputs[1].DeclaredPath != "reports/*.xml" {
		t.Fatalf("expected declared artifact paths, got %+v", outputs)
	}

	emptyOutputs, emptyErr := svc.declaredOutputsForBuild(domain.Build{ID: "build-1"}, jobs)
	if emptyErr != nil {
		t.Fatalf("empty pipeline output check returned error: %v", emptyErr)
	}
	if len(emptyOutputs) != 0 {
		t.Fatalf("expected no outputs for empty pipeline config, got %+v", emptyOutputs)
	}

	invalidYAML := "version: 2"
	invalidOutputs, invalidErr := svc.declaredOutputsForBuild(domain.Build{ID: "build-1", PipelineConfigYAML: stringPtr(invalidYAML)}, jobs)
	if invalidErr != nil {
		t.Fatalf("invalid pipeline output check should be best effort, got %v", invalidErr)
	}
	if len(invalidOutputs) != 0 {
		t.Fatalf("expected no outputs for invalid pipeline config, got %+v", invalidOutputs)
	}
}

func TestBuildServiceResolveExecutionImageFallbacks(t *testing.T) {
	svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
	svc.SetDefaultExecutionImage("alpine:3")

	if got := svc.resolveExecutionImage(domain.Build{}); got != "alpine:3" {
		t.Fatalf("expected default image, got %q", got)
	}

	invalidYAML := "version: 2"
	if got := svc.resolveExecutionImage(domain.Build{PipelineConfigYAML: stringPtr(invalidYAML)}); got != "alpine:3" {
		t.Fatalf("expected default image for invalid yaml, got %q", got)
	}

	pipelineYAML := "version: 1\npipeline:\n  image: golang:1.26\nsteps:\n  - name: test\n    run: go test ./...\n"
	if got := svc.resolveExecutionImage(domain.Build{PipelineConfigYAML: stringPtr(pipelineYAML)}); got != "golang:1.26" {
		t.Fatalf("expected pipeline image, got %q", got)
	}
}

func TestJoinSideEffectErrors(t *testing.T) {
	first := errors.New("first")
	second := errors.New("second")

	if got := joinSideEffectErrors(nil, nil); got != nil {
		t.Fatalf("expected nil when both errors are nil, got %v", got)
	}
	if got := joinSideEffectErrors(first, nil); got != first {
		t.Fatalf("expected existing error, got %v", got)
	}
	if got := joinSideEffectErrors(nil, second); got != second {
		t.Fatalf("expected additional error, got %v", got)
	}
	joined := joinSideEffectErrors(first, second)
	if !errors.Is(joined, first) || !errors.Is(joined, second) {
		t.Fatalf("expected joined error to contain both errors, got %v", joined)
	}
}
