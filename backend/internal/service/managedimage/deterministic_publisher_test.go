package managedimage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const testImmutableGolangRef = "golang@sha256:1e598ea5752ae26c093b746fd73c5095af97d6f2d679c43e83e0eac484a33dc3"

func TestDeterministicPublisherPublish_ResolvesTaggedImageToImmutableDigest(t *testing.T) {
	repoRoot := t.TempDir()
	mustWrite(t, filepath.Join(repoRoot, ".coyote", "pipeline.yml"), []byte("version: 1\npipeline:\n  image: golang:1.26.2\n"))

	var calls [][]string
	publisher := &DeterministicPublisher{
		runDocker: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			switch {
			case len(args) == 2 && args[0] == "pull" && args[1] == "golang:1.26.2":
				return []byte("pulled"), nil
			case len(args) == 5 && args[0] == "image" && args[1] == "inspect" && args[4] == "golang:1.26.2":
				return []byte(testImmutableGolangRef + "\n"), nil
			default:
				return nil, errors.New("unexpected docker invocation")
			}
		},
	}

	published, err := publisher.Publish(context.Background(), PublishRequest{
		DependencyFingerprint: strings.Repeat("a", 64),
		RepoRoot:              repoRoot,
		PipelinePath:          ".coyote/pipeline.yml",
	})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if published.ImageRef != testImmutableGolangRef {
		t.Fatalf("unexpected image ref: %q", published.ImageRef)
	}
	if published.ImageDigest != "sha256:1e598ea5752ae26c093b746fd73c5095af97d6f2d679c43e83e0eac484a33dc3" {
		t.Fatalf("unexpected image digest: %q", published.ImageDigest)
	}
	if published.VersionLabel != strings.Repeat("a", 12) {
		t.Fatalf("unexpected version label: %q", published.VersionLabel)
	}
	if len(calls) != 2 {
		t.Fatalf("expected two docker calls, got %d", len(calls))
	}
}

func TestDeterministicPublisherPublish_PreservesExistingImmutableRef(t *testing.T) {
	repoRoot := t.TempDir()
	immutableRef := testImmutableGolangRef
	mustWrite(t, filepath.Join(repoRoot, ".coyote", "pipeline.yml"), []byte("version: 1\npipeline:\n  image: "+immutableRef+"\n"))

	publisher := &DeterministicPublisher{
		runDocker: func(_ context.Context, _ ...string) ([]byte, error) {
			t.Fatal("did not expect docker for an already immutable ref")
			return nil, nil
		},
	}

	published, err := publisher.Publish(context.Background(), PublishRequest{
		DependencyFingerprint: strings.Repeat("b", 64),
		RepoRoot:              repoRoot,
		PipelinePath:          ".coyote/pipeline.yml",
	})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if published.ImageRef != immutableRef {
		t.Fatalf("unexpected image ref: %q", published.ImageRef)
	}
	if published.ImageDigest != "sha256:1e598ea5752ae26c093b746fd73c5095af97d6f2d679c43e83e0eac484a33dc3" {
		t.Fatalf("unexpected image digest: %q", published.ImageDigest)
	}
	if published.VersionLabel != strings.Repeat("b", 12) {
		t.Fatalf("unexpected version label: %q", published.VersionLabel)
	}
}
