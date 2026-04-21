package pipeline

import (
	"strings"
	"testing"
)

func TestUpdatePipelineImageRef_DeterministicAndScoped(t *testing.T) {
	input := []byte("version: 1\npipeline:\n  name: demo\n  image: golang:1.26.2\nsteps:\n  - name: test\n    run: go test ./...\n")

	updated1, changed1, err := UpdatePipelineImageRef(input, "registry.example.com/coyote/managed/go@sha256:1111")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !changed1 {
		t.Fatal("expected first update to report changed")
	}

	updated2, changed2, err := UpdatePipelineImageRef(updated1, "registry.example.com/coyote/managed/go@sha256:1111")
	if err != nil {
		t.Fatalf("idempotent update failed: %v", err)
	}
	if changed2 {
		t.Fatal("expected idempotent update to report unchanged")
	}
	if string(updated1) != string(updated2) {
		t.Fatal("expected deterministic output for idempotent image pinning")
	}
	if !strings.Contains(string(updated1), "image: registry.example.com/coyote/managed/go@sha256:1111") {
		t.Fatalf("expected updated image pin in output: %s", string(updated1))
	}
	if !strings.Contains(string(updated1), "name: test") || !strings.Contains(string(updated1), "run: go test ./...") {
		t.Fatalf("expected non-image fields to remain present: %s", string(updated1))
	}
}

func TestPipelineImageRef(t *testing.T) {
	input := []byte("version: 1\npipeline:\n  name: demo\n  image: golang@sha256:abcd\n")
	ref, err := PipelineImageRef(input)
	if err != nil {
		t.Fatalf("image ref read failed: %v", err)
	}
	if ref != "golang@sha256:abcd" {
		t.Fatalf("unexpected image ref: %q", ref)
	}
}
