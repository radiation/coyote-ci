package backend_test

import (
	"os"
	"strings"
	"testing"
)

func TestDockerfileArtifactRuntimePrecedesSourceBuildStages(t *testing.T) {
	contents, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	dockerfile := string(contents)
	runtimeBase := strings.Index(dockerfile, "FROM debian:bookworm-slim AS runtime-base")
	artifactRuntime := strings.Index(dockerfile, "FROM runtime-base AS artifact-runtime")
	base := strings.Index(dockerfile, "FROM golang:${GO_VERSION} AS base")
	sourceBuilder := strings.Index(dockerfile, "FROM base AS source-builder")
	sourceRuntime := strings.Index(dockerfile, "FROM runtime-base AS source-runtime")
	if runtimeBase < 0 || artifactRuntime < 0 || base < 0 || sourceBuilder < 0 || sourceRuntime < 0 {
		t.Fatalf("Dockerfile is missing a required runtime or source-build stage")
	}
	if runtimeBase >= artifactRuntime || artifactRuntime >= base || base >= sourceBuilder {
		t.Fatal("artifact-runtime must precede every Go source-build stage")
	}
	if strings.LastIndex(dockerfile, "FROM ") != sourceRuntime {
		t.Fatal("source-runtime must remain the final default stage")
	}
}
