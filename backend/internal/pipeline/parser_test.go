package pipeline

import (
	"testing"
)

func TestParse_ValidMinimal(t *testing.T) {
	yaml := `
version: 1
steps:
  - name: Lint
    run: golangci-lint run
artifacts:
  - dist/**
  - reports/*.xml
`
	pf, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if pf.Version != 1 {
		t.Errorf("expected version 1, got %d", pf.Version)
	}
	if len(pf.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(pf.Steps))
	}
	if pf.Steps[0].Name != "Lint" {
		t.Errorf("expected step name Lint, got %q", pf.Steps[0].Name)
	}
	if pf.Steps[0].Run != "golangci-lint run" {
		t.Errorf("expected run 'golangci-lint run', got %q", pf.Steps[0].Run)
	}
}

func TestParse_FullConfig(t *testing.T) {
	yaml := `
version: 1
pipeline:
  name: backend-ci
  image: golang:1.24
env:
  KEY: value
steps:
  - name: Lint
    run: golangci-lint run
    timeout_seconds: 300
    working_dir: backend
    env:
      FOO: bar
  - name: Test
    run: go test ./...
artifacts:
  - dist/**
  - reports/*.xml
`
	pf, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if pf.Pipeline.Name != "backend-ci" {
		t.Errorf("expected pipeline name backend-ci, got %q", pf.Pipeline.Name)
	}
	if pf.Pipeline.Image != "golang:1.24" {
		t.Errorf("expected pipeline image golang:1.24, got %q", pf.Pipeline.Image)
	}
	if pf.Env["KEY"] != "value" {
		t.Errorf("expected top-level env KEY=value")
	}
	if len(pf.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(pf.Steps))
	}
	if pf.Steps[0].WorkingDir != "backend" {
		t.Errorf("expected working_dir backend, got %q", pf.Steps[0].WorkingDir)
	}
	if pf.Steps[0].TimeoutSeconds == nil || *pf.Steps[0].TimeoutSeconds != 300 {
		t.Errorf("expected timeout_seconds 300")
	}
	if pf.Steps[0].Env["FOO"] != "bar" {
		t.Errorf("expected step env FOO=bar")
	}
	if pf.Steps[1].TimeoutSeconds != nil {
		t.Errorf("expected nil timeout_seconds for step 2")
	}
	if len(pf.Artifacts.Paths) != 2 {
		t.Fatalf("expected 2 artifact paths, got %d", len(pf.Artifacts.Paths))
	}
	if pf.Artifacts.Paths[0] != "dist/**" {
		t.Errorf("expected first artifact path dist/**, got %q", pf.Artifacts.Paths[0])
	}
}

func TestParse_CommandAliasForRun(t *testing.T) {
	yaml := `
version: 1
steps:
  - name: run
    command: ./scripts/run.sh
`
	pf, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(pf.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(pf.Steps))
	}
	if pf.Steps[0].Run != "./scripts/run.sh" {
		t.Fatalf("expected run to be aliased from command, got %q", pf.Steps[0].Run)
	}
}

func TestParse_UnknownField(t *testing.T) {
	yaml := `
version: 1
steps:
  - name: Lint
    run: golangci-lint run
    bogus_field: true
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected parse error for unknown field")
	}
	if _, ok := err.(*ParseError); !ok {
		t.Errorf("expected *ParseError, got %T", err)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	yaml := `{{{not valid yaml`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected parse error for invalid YAML")
	}
}

func TestParseAndValidate_Valid(t *testing.T) {
	yaml := `
version: 1
steps:
  - name: Build
    run: make build
`
	pf, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.Version != 1 {
		t.Errorf("expected version 1, got %d", pf.Version)
	}
}

func TestParseAndValidate_BadVersion(t *testing.T) {
	yaml := `
version: 2
steps:
  - name: Build
    run: make build
`
	_, err := ParseAndValidate([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for version 2")
	}
}

func TestResolve_EnvMerge(t *testing.T) {
	yaml := `
version: 1
env:
  GLOBAL: one
  SHARED: from-pipeline
steps:
  - name: Step1
    run: echo hello
    env:
      SHARED: from-step
      LOCAL: step-only
  - name: Step2
    run: echo world
`
	pf, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rp := Resolve(pf)

	if rp.Steps[0].Env["GLOBAL"] != "one" {
		t.Errorf("step1 should inherit GLOBAL=one")
	}
	if rp.Steps[0].Env["SHARED"] != "from-step" {
		t.Errorf("step1 SHARED should be overridden to from-step, got %q", rp.Steps[0].Env["SHARED"])
	}
	if rp.Steps[0].Env["LOCAL"] != "step-only" {
		t.Errorf("step1 should have LOCAL=step-only")
	}
	if rp.Steps[1].Env["GLOBAL"] != "one" {
		t.Errorf("step2 should inherit GLOBAL=one")
	}
	if rp.Steps[1].Env["SHARED"] != "from-pipeline" {
		t.Errorf("step2 SHARED should remain from-pipeline, got %q", rp.Steps[1].Env["SHARED"])
	}
	if _, ok := rp.Steps[1].Env["LOCAL"]; ok {
		t.Errorf("step2 should not have LOCAL")
	}
}

func TestResolve_PipelineName(t *testing.T) {
	yaml := `
version: 1
pipeline:
  name: my-pipeline
steps:
  - name: Build
    run: make
`
	pf, _ := ParseAndValidate([]byte(yaml))
	rp := Resolve(pf)
	if rp.Name != "my-pipeline" {
		t.Errorf("expected pipeline name my-pipeline, got %q", rp.Name)
	}
}

func TestResolve_PipelineImage(t *testing.T) {
	yaml := `
version: 1
pipeline:
  name: my-pipeline
  image: golang:1.24
steps:
  - name: Build
    run: make
`
	pf, _ := ParseAndValidate([]byte(yaml))
	rp := Resolve(pf)
	if rp.Image != "golang:1.24" {
		t.Errorf("expected pipeline image golang:1.24, got %q", rp.Image)
	}
}

func TestResolve_PipelineImage_Optional(t *testing.T) {
	yaml := `
version: 1
pipeline:
  name: my-pipeline
steps:
  - name: Build
    run: make
`
	pf, _ := ParseAndValidate([]byte(yaml))
	rp := Resolve(pf)
	if rp.Image != "" {
		t.Errorf("expected empty pipeline image when not configured, got %q", rp.Image)
	}
}

func TestResolve_TimeoutSeconds(t *testing.T) {
	yaml := `
version: 1
steps:
  - name: WithTimeout
    run: sleep 10
    timeout_seconds: 60
  - name: NoTimeout
    run: echo fast
`
	pf, _ := ParseAndValidate([]byte(yaml))
	rp := Resolve(pf)
	if rp.Steps[0].TimeoutSeconds != 60 {
		t.Errorf("expected timeout 60, got %d", rp.Steps[0].TimeoutSeconds)
	}
	if rp.Steps[1].TimeoutSeconds != 0 {
		t.Errorf("expected timeout 0, got %d", rp.Steps[1].TimeoutSeconds)
	}
}

func TestLoadAndResolve_FullRoundTrip(t *testing.T) {
	yaml := `
version: 1
pipeline:
  name: backend-ci
env:
  CI: "true"
steps:
  - name: Lint
    run: golangci-lint run
    working_dir: backend
    timeout_seconds: 300
    env:
      LINT_STRICT: "1"
  - name: Test
    run: go test ./...
    env:
      CI: "true"
`
	rp, err := LoadAndResolve([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Name != "backend-ci" {
		t.Errorf("name: got %q", rp.Name)
	}
	if len(rp.Steps) != 2 {
		t.Fatalf("steps: expected 2, got %d", len(rp.Steps))
	}
	if rp.Steps[0].WorkingDir != "backend" {
		t.Errorf("step0 working_dir: got %q", rp.Steps[0].WorkingDir)
	}
	if rp.Steps[0].Env["CI"] != "true" {
		t.Errorf("step0 should inherit CI=true from pipeline env")
	}
	if rp.Steps[0].Env["LINT_STRICT"] != "1" {
		t.Errorf("step0 should have LINT_STRICT=1")
	}
}

func TestResolve_Artifacts(t *testing.T) {
	yaml := `
version: 1
steps:
  - name: Build
    run: make build
artifacts:
  - dist/**
  - reports/*.xml
`

	pf, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rp := Resolve(pf)
	if len(rp.Artifacts.Paths) != 2 {
		t.Fatalf("expected 2 resolved artifact paths, got %d", len(rp.Artifacts.Paths))
	}
	if rp.Artifacts.Paths[0] != "dist/**" || rp.Artifacts.Paths[1] != "reports/*.xml" {
		t.Fatalf("unexpected artifact paths: %#v", rp.Artifacts.Paths)
	}
}

func TestParse_Artifacts_ObjectForm(t *testing.T) {
	yaml := `
version: 1
steps:
  - name: Build
    run: make build
artifacts:
  - path: pipeline.txt
  - path: success.doc
`

	pf, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected parse/validate error: %v", err)
	}

	if len(pf.Artifacts.Paths) != 2 {
		t.Fatalf("expected 2 artifact paths, got %d", len(pf.Artifacts.Paths))
	}
	if pf.Artifacts.Paths[0] != "pipeline.txt" || pf.Artifacts.Paths[1] != "success.doc" {
		t.Fatalf("unexpected artifact paths: %#v", pf.Artifacts.Paths)
	}
}

func TestParseAndValidate_Artifacts_SimpleListEndToEnd(t *testing.T) {
	yaml := `
version: 1
pipeline:
  name: my-pipeline
steps:
  - name: greet
    run: echo "Hello from pipeline" > pipeline.txt
  - name: build
    run: echo "Hell yeah" > success.doc
artifacts:
  - pipeline.txt
  - success.doc
`

	pf, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected parse/validate error: %v", err)
	}

	if len(pf.Artifacts.Paths) != 2 {
		t.Fatalf("expected 2 artifact paths, got %d", len(pf.Artifacts.Paths))
	}
	if pf.Artifacts.Paths[0] != "pipeline.txt" || pf.Artifacts.Paths[1] != "success.doc" {
		t.Fatalf("unexpected artifact paths: %#v", pf.Artifacts.Paths)
	}
}

func TestResolve_StepLevelArtifacts(t *testing.T) {
	yaml := `
version: 1
steps:
  - name: Build
    run: make build
    artifacts:
      - dist/**
      - build/output.tar.gz
  - name: Test
    run: go test ./...
artifacts:
  - reports/*.xml
`
	pf, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rp := Resolve(pf)

	if len(rp.Steps[0].ArtifactPaths) != 2 {
		t.Fatalf("expected 2 step-level artifact paths on step 0, got %d", len(rp.Steps[0].ArtifactPaths))
	}
	if rp.Steps[0].ArtifactPaths[0] != "dist/**" {
		t.Fatalf("expected dist/**, got %q", rp.Steps[0].ArtifactPaths[0])
	}

	if len(rp.Steps[1].ArtifactPaths) != 0 {
		t.Fatalf("expected 0 step-level artifact paths on step 1, got %d", len(rp.Steps[1].ArtifactPaths))
	}

	if len(rp.Artifacts.Paths) != 1 {
		t.Fatalf("expected 1 pipeline-level artifact path, got %d", len(rp.Artifacts.Paths))
	}
	if rp.Artifacts.Paths[0] != "reports/*.xml" {
		t.Fatalf("expected reports/*.xml, got %q", rp.Artifacts.Paths[0])
	}
}
