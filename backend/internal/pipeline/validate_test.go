package pipeline

import (
	"strings"
	"testing"
)

func TestValidate_MissingVersion(t *testing.T) {
	pf := &PipelineFile{
		Version: 0,
		Steps:   []StepDef{{Name: "x", Run: "echo"}},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for version 0")
	}
	assertContains(t, err.Error(), "version")
}

func TestValidate_UnsupportedVersion(t *testing.T) {
	pf := &PipelineFile{
		Version: 99,
		Steps:   []StepDef{{Name: "x", Run: "echo"}},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for version 99")
	}
	assertContains(t, err.Error(), "unsupported version")
}

func TestValidate_EmptySteps(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps:   nil,
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for empty steps")
	}
	assertContains(t, err.Error(), "at least one step")
}

func TestValidate_EmptyStepName(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "", Run: "echo"},
		},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for empty step name")
	}
	assertContains(t, err.Error(), "name")
	assertContains(t, err.Error(), "required")
}

func TestValidate_EmptyRunCommand(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "Build", Run: ""},
		},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for empty run")
	}
	assertContains(t, err.Error(), "run")
	assertContains(t, err.Error(), "required")
}

func TestValidate_DuplicateStepNames(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "Lint", Run: "lint"},
			{Name: "lint", Run: "lint again"},
		},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for duplicate step names")
	}
	assertContains(t, err.Error(), "duplicate")
}

func TestValidate_NegativeTimeout(t *testing.T) {
	neg := -5
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "Build", Run: "make", TimeoutSeconds: &neg},
		},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
	assertContains(t, err.Error(), "timeout_seconds")
}

func TestValidate_ZeroTimeout(t *testing.T) {
	zero := 0
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "Build", Run: "make", TimeoutSeconds: &zero},
		},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for zero timeout")
	}
	assertContains(t, err.Error(), "timeout_seconds")
}

func TestValidate_AbsoluteWorkingDir(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "Build", Run: "make", WorkingDir: "/usr/src/app"},
		},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for absolute working_dir")
	}
	assertContains(t, err.Error(), "relative path")
}

func TestValidate_InvalidEnvKey_TopLevel(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Env:     map[string]string{"123BAD": "val"},
		Steps:   []StepDef{{Name: "x", Run: "echo"}},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for invalid top-level env key")
	}
	assertContains(t, err.Error(), "invalid env key")
}

func TestValidate_InvalidEnvKey_StepLevel(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "x", Run: "echo", Env: map[string]string{"bad-key": "val"}},
		},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for invalid step env key")
	}
	assertContains(t, err.Error(), "invalid env key")
}

func TestValidate_ValidEnvKeys(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Env:     map[string]string{"MY_VAR": "1", "_UNDERSCORE": "2"},
		Steps: []StepDef{
			{Name: "x", Run: "echo", Env: map[string]string{"FOO_BAR": "baz"}},
		},
	}
	if err := Validate(pf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_PipelineImage_WhitespaceRejected(t *testing.T) {
	pf := &PipelineFile{
		Version:  1,
		Pipeline: PipelineMeta{Image: "   "},
		Steps:    []StepDef{{Name: "Build", Run: "make"}},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error for whitespace pipeline.image")
	}
	assertContains(t, err.Error(), "pipeline.image")
	assertContains(t, err.Error(), "non-empty")
}

func TestValidate_PipelineImage_SetAccepted(t *testing.T) {
	pf := &PipelineFile{
		Version:  1,
		Pipeline: PipelineMeta{Image: "golang:1.24"},
		Steps:    []StepDef{{Name: "Build", Run: "make"}},
	}

	if err := Validate(pf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	neg := -1
	pf := &PipelineFile{
		Version: 2,
		Steps: []StepDef{
			{Name: "", Run: "echo", TimeoutSeconds: &neg, WorkingDir: "/abs"},
			{Name: "OK", Run: ""},
		},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected multiple errors")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(verrs) < 4 {
		t.Errorf("expected at least 4 validation errors, got %d: %v", len(verrs), verrs)
	}
}

func TestValidate_NilTimeout_OK(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "Build", Run: "make"},
		},
	}
	if err := Validate(pf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_PathTraversalWorkingDir(t *testing.T) {
	cases := []string{"../secret", "../../etc", "..", "foo/../../etc"}
	for _, wd := range cases {
		pf := &PipelineFile{
			Version: 1,
			Steps:   []StepDef{{Name: "Build", Run: "make", WorkingDir: wd}},
		}
		err := Validate(pf)
		if err == nil {
			t.Errorf("expected error for path-traversal working_dir %q, got nil", wd)
			continue
		}
		assertContains(t, err.Error(), "relative path")
	}
}

func TestValidate_RelativeWorkingDir_OK(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "Build", Run: "make", WorkingDir: "backend/src"},
		},
	}
	if err := Validate(pf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_Artifacts_EmptyPathRejected(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps:   []StepDef{{Name: "Build", Run: "make"}},
		Artifacts: ArtifactDef{
			Paths: []string{"dist/**", "   "},
		},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected artifact validation error")
	}
	assertContains(t, err.Error(), "artifact")
	assertContains(t, err.Error(), "required")
}

func TestValidate_Artifacts_PathTraversalRejected(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps:   []StepDef{{Name: "Build", Run: "make"}},
		Artifacts: ArtifactDef{
			Paths: []string{"reports/../secret.txt"},
		},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected artifact path traversal error")
	}
	assertContains(t, err.Error(), "within workspace")
}

func TestValidate_Artifacts_AbsolutePathRejected(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps:   []StepDef{{Name: "Build", Run: "make"}},
		Artifacts: ArtifactDef{
			Paths: []string{"/tmp/out.txt"},
		},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected artifact absolute path error")
	}
	assertContains(t, err.Error(), "relative")
}

func TestValidate_Artifacts_BackslashPathRejected(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps:   []StepDef{{Name: "Build", Run: "make"}},
		Artifacts: ArtifactDef{
			Paths: []string{"dist\\output.txt"},
		},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected artifact backslash path error")
	}
	assertContains(t, err.Error(), "forward slashes")
}

func TestValidate_Artifacts_Valid(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps:   []StepDef{{Name: "Build", Run: "make"}},
		Artifacts: ArtifactDef{
			Paths: []string{"dist/**", "reports/*.xml"},
		},
	}

	if err := Validate(pf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_CachePresetRequired(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{{
			Name:  "test",
			Run:   "go test ./...",
			Cache: &CacheDef{},
		}},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected error when cache.preset missing")
	}
	assertContains(t, err.Error(), "preset")
}

func TestValidate_CacheUnknownPresetRejected(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{{
			Name: "test",
			Run:  "go test ./...",
			Cache: &CacheDef{
				Preset: "unknown",
			},
		}},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected unknown preset validation error")
	}
	assertContains(t, err.Error(), "unknown")
}

func TestValidate_GroupNameRequired(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{{
			Group: &StepGroupDef{
				Name:  "",
				Steps: []StepDef{{Name: "unit", Run: "pytest tests/unit"}},
			},
		}},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected validation error for empty group name")
	}
	assertContains(t, err.Error(), "group.name")
}

func TestValidate_GroupMustContainSteps(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{{
			Group: &StepGroupDef{Name: "matrix", Steps: nil},
		}},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected validation error for empty group")
	}
	assertContains(t, err.Error(), "group must contain at least one step")
}

func TestValidate_NestedGroupsRejected(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{{
			Group: &StepGroupDef{
				Name: "outer",
				Steps: []StepDef{{
					Group: &StepGroupDef{
						Name:  "inner",
						Steps: []StepDef{{Name: "leaf", Run: "echo ok"}},
					},
				}},
			},
		}},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected validation error for nested group")
	}
	assertContains(t, err.Error(), "nested groups are not allowed")
}

func TestValidate_DuplicateStepNamesAfterGroupExpansion(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{
			{Name: "lint", Run: "golangci-lint run"},
			{Group: &StepGroupDef{
				Name: "matrix",
				Steps: []StepDef{
					{Name: "Lint", Run: "pytest tests/unit"},
				},
			}},
		},
	}

	err := Validate(pf)
	if err == nil {
		t.Fatal("expected duplicate validation error")
	}
	assertContains(t, err.Error(), "duplicate")
}

func TestValidate_CachePolicyRejectedWhenInvalid(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{{
			Name: "test",
			Run:  "go test ./...",
			Cache: &CacheDef{
				Preset: "go",
				Policy: "bad-policy",
			},
		}},
	}
	err := Validate(pf)
	if err == nil {
		t.Fatal("expected invalid cache policy validation error")
	}
	assertContains(t, err.Error(), "policy")
}

func TestValidate_CachePresetPolicyValid(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{{
			Name: "test",
			Run:  "go test ./...",
			Cache: &CacheDef{
				Preset: "node",
				Policy: "pull",
			},
		}},
	}
	if err := Validate(pf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_CachePresetDefaultPolicyValid(t *testing.T) {
	pf := &PipelineFile{
		Version: 1,
		Steps: []StepDef{{
			Name: "test",
			Run:  "go test ./...",
			Cache: &CacheDef{
				Preset: "python-uv",
			},
		}},
	}
	if err := Validate(pf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(s), strings.ToLower(substr)) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}
