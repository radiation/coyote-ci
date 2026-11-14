package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func coyoteArtifactFixture(t *testing.T, input CoyoteReleaseInput) map[string]CoyoteArtifact {
	t.Helper()
	directory := t.TempDir()
	artifacts := make(map[string]CoyoteArtifact)
	write := func(name string, data []byte) {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		artifacts[name] = CoyoteArtifact{Path: path, URL: "https://ci.example/artifacts/" + name, BuildID: input.BuildID, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Size: int64(len(data))}
	}
	var checksums strings.Builder
	for _, platform := range CoyoteCLIPlatforms {
		contents := []byte("binary archive " + platform)
		write("cli-"+platform, contents)
		extension := ".tar.gz"
		if strings.HasPrefix(platform, "windows-") {
			extension = ".zip"
		}
		fmt.Fprintf(&checksums, "%x  coyote_%s%s\n", sha256.Sum256(contents), strings.ReplaceAll(platform, "-", "_"), extension)
	}
	write("cli-checksums", []byte(checksums.String()))
	write("cli-metadata", []byte(fmt.Sprintf(`{"version":%q,"commit":%q}`, input.ApplicationVersion, input.SourceCommit)))
	write("compatibility", []byte(`{"schema_target":"57","upgrade_from":">=0.0.11 <0.0.41","maintenance":"active"}`))
	write("schema-target", []byte("57\n"))
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	metadata := []byte("name: coyote-ci\nversion: " + input.ApplicationVersion + "\nappVersion: " + input.ApplicationVersion + "\n")
	if err := archive.WriteHeader(&tar.Header{Name: "coyote-ci/Chart.yaml", Mode: 0600, Size: int64(len(metadata))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(metadata); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	write("helm-chart", buffer.Bytes())
	return artifacts
}

func TestVerifyCoyoteArtifacts(t *testing.T) {
	input := validCoyoteReleaseInput()
	artifacts := coyoteArtifactFixture(t, input)
	verified, err := VerifyCoyoteArtifacts(input, artifacts)
	if err != nil || len(verified.CLIArtifacts) != 6 || verified.Compatibility.SchemaTarget != "57" {
		t.Fatalf("verified=%+v error=%v", verified, err)
	}
	for _, mutation := range []string{"missing", "size", "checksum", "build", "unreadable", "cli-checksums", "cli-metadata", "compatibility", "schema-target", "helm-chart", "unknown-compatibility", "trailing-compatibility", "noncanonical-target", "missing-platform", "oversized-metadata"} {
		t.Run(mutation, func(t *testing.T) {
			values := coyoteArtifactFixture(t, input)
			name := mutation
			contents := []byte("invalid")
			switch mutation {
			case "missing":
				delete(values, "schema-target")
			case "size", "checksum", "build", "unreadable":
				artifact := values["cli-linux-amd64"]
				switch mutation {
				case "size":
					artifact.Size++
				case "checksum":
					artifact.SHA256 = strings.Repeat("0", 64)
				case "build":
					artifact.BuildID = "other"
				case "unreadable":
					artifact.Path = filepath.Join(t.TempDir(), "missing")
				}
				values["cli-linux-amd64"] = artifact
			case "missing-platform":
				values["unexpected"] = values["cli-linux-amd64"]
				delete(values, "cli-linux-amd64")
			default:
				switch mutation {
				case "unknown-compatibility":
					name = "compatibility"
					contents = []byte(`{"unknown":true}`)
				case "trailing-compatibility":
					name = "compatibility"
					contents = []byte(`{} {}`)
				case "noncanonical-target":
					name = "compatibility"
					contents = []byte(`{"schema_target":"057"}`)
				case "oversized-metadata":
					name = "cli-metadata"
					contents = bytes.Repeat([]byte("x"), 1024*1024+1)
				}
				artifact := values[name]
				if writeErr := os.WriteFile(artifact.Path, contents, 0600); writeErr != nil {
					t.Fatal(writeErr)
				}
				artifact.Size = int64(len(contents))
				artifact.SHA256 = fmt.Sprintf("%x", sha256.Sum256(contents))
				values[name] = artifact
			}
			if _, verifyErr := VerifyCoyoteArtifacts(input, values); verifyErr == nil {
				t.Fatalf("accepted %s", mutation)
			}
		})
	}
	if chartErr := verifyCoyoteChart(artifacts["helm-chart"].Path, "0.0.42"); chartErr == nil {
		t.Fatal("accepted mismatched chart version")
	}
}

func TestVerifyCoyoteMigrationTarget(t *testing.T) {
	directory := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in *'--network none --read-only --cap-drop ALL --platform linux/'*'@sha256:'*' target') ;; *) exit 2;; esac\nif [ \"$MIGRATION_TEST_TARGET\" = failed ]; then exit 1; fi\nprintf '%s\\n' \"$MIGRATION_TEST_TARGET\"\n"
	if err := os.WriteFile(filepath.Join(directory, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, target := range []string{"57", "58", "057", "failed"} {
		t.Run(target, func(t *testing.T) {
			t.Setenv("MIGRATION_TEST_TARGET", target)
			if err := VerifyCoyoteMigrationTarget(t.Context(), validCoyoteReleaseInput()); (err == nil) != (target == "57") {
				t.Fatalf("target=%s error=%v", target, err)
			}
		})
	}
	input := validCoyoteReleaseInput()
	input.Outputs = nil
	if err := VerifyCoyoteMigrationTarget(t.Context(), input); err == nil {
		t.Fatal("accepted missing migrate image")
	}
}

func TestPublishCoyoteChartRetriesAndConflicts(t *testing.T) {
	input := validCoyoteReleaseInput()
	artifacts := coyoteArtifactFixture(t, input)
	directory := t.TempDir()
	script := `#!/bin/sh
case "$1" in
pull)
  if [ "$HELM_TEST_MODE" = denied ]; then echo denied >&2; exit 1; fi
	if [ "$HELM_TEST_MODE" = ambiguous ]; then echo 'registry not found' >&2; exit 1; fi
	if [ ! -f "$HELM_TEST_REMOTE" ]; then
		if [ "$HELM_TEST_MODE" = helm4 ]; then printf 'Error: failed to perform "FetchReference" on source: registry.example/charts/coyote-ci:%s: not found\n' "$4" >&2; else echo 'manifest unknown' >&2; fi
		exit 1
	fi
	destination="$6/coyote-ci-$4.tgz"
	case "$2" in
		*@sha256:*)
			destination="$6/coyote-ci@sha256-${2##*:}.tgz"
			if [ "$HELM_TEST_MODE" = pinned-failed ]; then exit 1; fi
			if [ "$HELM_TEST_MODE" = pinned-missing ]; then exit 0; fi
			if [ "$HELM_TEST_MODE" = pinned-conflict ]; then echo different > "$destination"; exit 0; fi
			;;
	esac
	cp "$HELM_TEST_REMOTE" "$destination"
  ;;
push)
	printf 'push\n' >> "$HELM_TEST_PUSHES"
  if [ "$HELM_TEST_MODE" = failed ]; then exit 1; fi
  cp "$2" "$HELM_TEST_REMOTE"
  if [ "$HELM_TEST_MODE" = lost-response ]; then exit 1; fi
  ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(directory, "helm"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	inspectScript := `#!/bin/sh
[ "$1 $2 $3 $4 $5" = 'buildx imagetools inspect --format {{json .Manifest}}' ] || exit 2
[ "$6" = registry.example/charts/coyote-ci:0.0.41 ] || exit 2
case "$HELM_TEST_MODE" in
  inspect-failed) exit 1 ;;
  invalid-json) echo invalid ;;
  invalid-digest) echo '{"digest":"sha256:abc"}' ;;
  missing-digest) echo '{}' ;;
  *) echo '{"digest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}' ;;
esac
`
	if writeErr := os.WriteFile(filepath.Join(directory, "docker"), []byte(inspectScript), 0700); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, mode := range []string{"success", "helm4", "lost-response", "failed", "denied", "ambiguous", "inspect-failed", "invalid-json", "invalid-digest", "missing-digest", "pinned-failed", "pinned-missing", "pinned-conflict", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			remote := filepath.Join(t.TempDir(), "chart")
			t.Setenv("HELM_TEST_REMOTE", remote)
			t.Setenv("HELM_TEST_MODE", mode)
			pushes := filepath.Join(t.TempDir(), "pushes")
			t.Setenv("HELM_TEST_PUSHES", pushes)
			if mode == "conflict" {
				if err := os.WriteFile(remote, []byte("different"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			chart, err := PublishCoyoteChart(context.Background(), artifacts["helm-chart"].Path, "oci://registry.example/charts/coyote-ci", input.ApplicationVersion)
			if mode == "success" || mode == "helm4" || mode == "lost-response" {
				if err != nil || chart.Digest != "sha256:"+strings.Repeat("f", 64) {
					t.Fatalf("chart=%+v error=%v", chart, err)
				}
				retried, retryErr := PublishCoyoteChart(context.Background(), artifacts["helm-chart"].Path, chart.Repository, input.ApplicationVersion)
				if retryErr != nil || *retried != *chart {
					t.Fatalf("retry=%+v error=%v", retried, retryErr)
				}
				calls, readErr := os.ReadFile(pushes)
				if readErr != nil || string(calls) != "push\n" {
					t.Fatalf("retry pushed again: %q error=%v", calls, readErr)
				}
			} else if err == nil {
				t.Fatalf("accepted %s", mode)
			}
		})
	}
	if _, err := PublishCoyoteChart(context.Background(), artifacts["helm-chart"].Path, "https://invalid", input.ApplicationVersion); err == nil {
		t.Fatal("invalid repository accepted")
	}
	if _, err := PublishCoyoteChart(context.Background(), "missing", "oci://registry.example/charts/coyote-ci", input.ApplicationVersion); err == nil {
		t.Fatal("missing archive accepted")
	}
}
