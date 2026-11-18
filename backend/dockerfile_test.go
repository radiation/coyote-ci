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
	base := strings.Index(dockerfile, "FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS base")
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

func TestDockerfileGeneratesCertificatesNatively(t *testing.T) {
	contents, readErr := os.ReadFile("Dockerfile")
	if readErr != nil {
		t.Fatal(readErr)
	}
	certificates, runtime, found := strings.Cut(string(contents), "FROM debian:bookworm-slim AS runtime-base\n")
	if !found || !strings.Contains(certificates, "FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS certificates\n") ||
		!strings.Contains(certificates, "apt-get install -y --no-install-recommends ca-certificates") {
		t.Fatal("CA bundle generation must run on the native build platform")
	}
	runtime, _, _ = strings.Cut(runtime, "\nFROM ")
	if !strings.Contains(runtime, "    git \\\n") || strings.Contains(runtime, "install -y --no-install-recommends ca-certificates") ||
		strings.Contains(runtime, "    ca-certificates \\\n") {
		t.Fatal("runtime must install target-platform Git without regenerating certificates under emulation")
	}
	if !strings.Contains(runtime, "COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt\n") {
		t.Fatal("runtime must retain the generated CA bundle for HTTPS")
	}
}

func TestDockerfileCrossCompilesWithNativeGoToolchain(t *testing.T) {
	contents, readErr := os.ReadFile("Dockerfile")
	if readErr != nil {
		t.Fatal(readErr)
	}
	dockerfile := string(contents)
	if !strings.Contains(dockerfile, "FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS base") {
		t.Fatal("Go toolchain must run on the build platform, not under target emulation")
	}
	for _, target := range []struct {
		stage string
		count int
	}{
		{stage: "source-builder", count: 2},
		{stage: "mcp-builder", count: 1},
		{stage: "migration-builder", count: 1},
	} {
		_, stage, found := strings.Cut(dockerfile, "FROM source AS "+target.stage+"\n")
		if !found {
			t.Fatalf("missing builder %s", target.stage)
		}
		stage, _, _ = strings.Cut(stage, "\nFROM ")
		if !strings.Contains(stage, "ARG TARGETOS\nARG TARGETARCH\n") ||
			strings.Count(stage, "RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build") != target.count {
			t.Fatalf("%s must cross-compile every binary for the requested target", target.stage)
		}
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

func TestFrontendDockerfileKeepsNativeDependenciesOutOfRuntime(t *testing.T) {
	contents, readErr := os.ReadFile(filepath.Join("..", "frontend", "Dockerfile"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	build, runtime, found := strings.Cut(string(contents), "\nFROM nginx:alpine\n")
	if !found || !strings.HasPrefix(build, "FROM --platform=$BUILDPLATFORM node:22-alpine AS build\n") {
		t.Fatal("frontend must build natively and retain a target-platform Nginx runtime")
	}
	if !strings.Contains(build, "RUN npm ci\n") || !strings.Contains(build, "npm run build") {
		t.Fatal("native dependencies must be installed and used in the same build stage")
	}
	if strings.Count(runtime, "COPY --from=") != 1 || !strings.Contains(runtime, "COPY --from=build /app/dist /usr/share/nginx/html\n") {
		t.Fatal("only static dist assets may cross from the native builder into the runtime")
	}
	ignore, ignoreErr := os.ReadFile(filepath.Join("..", "frontend", ".dockerignore"))
	if ignoreErr != nil {
		t.Fatal(ignoreErr)
	}
	for _, excluded := range []string{"node_modules", "dist", "coverage", "*.tsbuildinfo"} {
		if !strings.Contains("\n"+strings.TrimSpace(string(ignore))+"\n", "\n"+excluded+"\n") {
			t.Fatalf("frontend build context must exclude %s", excluded)
		}
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
