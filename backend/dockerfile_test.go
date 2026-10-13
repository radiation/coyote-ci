package backend_test

import (
	"os"
	"path/filepath"
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
	source := strings.Index(dockerfile, "FROM base AS source\n")
	sourceBuilder := strings.Index(dockerfile, "FROM base AS source-builder")
	sourceRuntime := strings.Index(dockerfile, "FROM runtime-base AS source-runtime\n")
	if runtimeBase < 0 || artifactRuntime < 0 || base < 0 || source < 0 || sourceBuilder >= 0 || sourceRuntime < 0 {
		t.Fatalf("Dockerfile is missing a required runtime or source-build stage")
	}
	if runtimeBase >= artifactRuntime || artifactRuntime >= base || base >= source {
		t.Fatal("artifact-runtime must precede every Go source-build stage")
	}
	if strings.LastIndex(dockerfile, "FROM ") != sourceRuntime {
		t.Fatal("source-runtime must remain the final default stage")
	}
}

func TestDockerfileMCPRuntimeTarget(t *testing.T) {
	contents, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	dockerfile := string(contents)
	for _, expected := range []string{
		"go build -ldflags",
		"FROM source AS mcp-builder",
		"-o mcp ./cmd/mcp",
		"FROM runtime-base AS source-runtime-mcp",
		"COPY --from=mcp-builder --chown=coyote:coyote /app/mcp /app/mcp",
		"USER 10001:10001",
		"ENTRYPOINT [\"/app/mcp\"]",
		"http://127.0.0.1:8081/readyz",
	} {
		if !strings.Contains(dockerfile, expected) {
			t.Fatalf("Dockerfile MCP target is missing %q", expected)
		}
	}
	if strings.Contains(dockerfile[strings.Index(dockerfile, "FROM source AS source-builder"):strings.Index(dockerfile, "FROM source AS mcp-builder")], "./cmd/mcp") {
		t.Fatal("regular source builder must not compile the MCP executable")
	}
}

func TestComposeMCPServicesAreOptionalAndLoopbackBound(t *testing.T) {
	for _, composeFile := range []string{"docker-compose.yml", "docker-compose.prod.yml"} {
		contents, err := os.ReadFile(filepath.Join("..", composeFile))
		if err != nil {
			t.Fatalf("read %s: %v", composeFile, err)
		}
		compose := string(contents)
		for _, expected := range []string{
			"  mcp:",
			"target: source-runtime-mcp",
			"profiles: [\"mcp\"]",
			"MCP_ALLOW_DISABLED_AUTH: \"false\"",
			"AUTH_MODE: ${MCP_AUTH_MODE:-header}",
			"http://127.0.0.1:8081/readyz",
		} {
			if !strings.Contains(compose, expected) {
				t.Fatalf("%s MCP service is missing %q", composeFile, expected)
			}
		}
		mcp := composeServiceBlock(t, compose, "mcp")
		for _, forbidden := range []string{
			"/var/run/docker.sock",
			"artifact_data:",
			"workspace_revision_data:",
			"/tmp/coyote-builds",
		} {
			if strings.Contains(mcp, forbidden) {
				t.Fatalf("%s MCP service must not include %q", composeFile, forbidden)
			}
		}
	}

	contents, err := os.ReadFile(filepath.Join("..", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	if !strings.Contains(string(contents), "\"127.0.0.1:8081:8081\"") {
		t.Fatal("development MCP port must bind only to localhost")
	}
}

func composeServiceBlock(t *testing.T, compose, service string) string {
	t.Helper()

	prefix := "\n  " + service + ":"
	start := strings.Index(compose, prefix)
	if start < 0 {
		t.Fatalf("service %q is missing", service)
	}
	remaining := compose[start+1:]
	next := strings.Index(remaining, "\n  ")
	if next < 0 {
		return remaining
	}
	return remaining[:next]
}
