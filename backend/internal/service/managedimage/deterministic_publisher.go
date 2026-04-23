package managedimage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

// DeterministicPublisher is a local-first publisher used for this slice.
// It resolves the pipeline image to immutable digest form and derives a stable version label from the dependency fingerprint.
type DeterministicPublisher struct {
	runDocker func(ctx context.Context, args ...string) ([]byte, error)
}

func NewDeterministicPublisher() *DeterministicPublisher {
	return &DeterministicPublisher{runDocker: runDockerCommand}
}

func (p *DeterministicPublisher) Publish(_ context.Context, req PublishRequest) (PublishedImage, error) {
	versionLabel := normalizeDigest(req.DependencyFingerprint)
	if versionLabel == "" {
		return PublishedImage{}, fmt.Errorf("dependency fingerprint is required")
	}

	imageRef := strings.TrimSpace(resolvePipelineImageRef(req))
	if imageRef == "" {
		return PublishedImage{}, fmt.Errorf("unable to determine base image reference")
	}
	if immutableDigest := immutableImageDigest(imageRef); immutableDigest != "" {
		return PublishedImage{
			ImageRef:     imageRef,
			ImageDigest:  immutableDigest,
			VersionLabel: versionLabel[:12],
		}, nil
	}

	runDocker := p.runDocker
	if runDocker == nil {
		runDocker = runDockerCommand
	}
	resolvedRef, resolvedDigest, err := resolveImmutableImageRef(context.Background(), runDocker, imageRef)
	if err != nil {
		return PublishedImage{}, err
	}

	return PublishedImage{
		ImageRef:     resolvedRef,
		ImageDigest:  resolvedDigest,
		VersionLabel: versionLabel[:12],
	}, nil
}

func resolvePipelineImageRef(req PublishRequest) string {
	pipelinePath := strings.TrimSpace(req.PipelinePath)
	if pipelinePath != "" && strings.TrimSpace(req.RepoRoot) != "" {
		fullPath := filepath.Join(req.RepoRoot, filepath.FromSlash(filepath.Clean(pipelinePath)))
		raw, readErr := os.ReadFile(fullPath)
		if readErr == nil {
			if imageRef, parseErr := pipeline.PipelineImageRef(raw); parseErr == nil {
				if normalized := strings.TrimSpace(imageRef); normalized != "" {
					return normalized
				}
			}
		}
	}

	fallbackName := strings.TrimSpace(req.ManagedImageName)
	if fallbackName == "" {
		fallbackName = "build-image"
	}
	return "ghcr.io/coyote-ci/managed/" + sanitizeName(fallbackName) + ":latest"
}

func resolveImmutableImageRef(ctx context.Context, runDocker func(context.Context, ...string) ([]byte, error), imageRef string) (string, string, error) {
	if runDocker == nil {
		return "", "", fmt.Errorf("docker runner is not configured")
	}
	trimmed := strings.TrimSpace(imageRef)
	if trimmed == "" {
		return "", "", fmt.Errorf("image reference is required")
	}

	if _, err := runDocker(ctx, "pull", trimmed); err != nil {
		return "", "", fmt.Errorf("resolve immutable image ref %q: %w", trimmed, err)
	}

	output, err := runDocker(ctx, "image", "inspect", "--format", "{{join .RepoDigests \"\\n\"}}", trimmed)
	if err != nil {
		return "", "", fmt.Errorf("inspect image repo digests for %q: %w", trimmed, err)
	}

	repoName := imageRefWithoutDigestOrTag(trimmed)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		digest := immutableImageDigest(candidate)
		if digest == "" {
			continue
		}
		if repoName == "" || imageRefWithoutDigestOrTag(candidate) == repoName {
			return candidate, digest, nil
		}
	}

	return "", "", fmt.Errorf("inspect image repo digests for %q: no immutable digest found", trimmed)
}

func runDockerCommand(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func immutableImageDigest(imageRef string) string {
	trimmed := strings.TrimSpace(imageRef)
	parts := strings.SplitN(trimmed, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	digest := strings.TrimSpace(parts[1])
	if !strings.HasPrefix(digest, "sha256:") {
		return ""
	}
	if !isLowerHex64(strings.TrimPrefix(digest, "sha256:")) {
		return ""
	}
	return digest
}

func imageRefWithoutDigestOrTag(imageRef string) string {
	trimmed := strings.TrimSpace(imageRef)
	if trimmed == "" {
		return ""
	}

	withoutDigest := strings.SplitN(trimmed, "@", 2)[0]
	lastSlash := strings.LastIndex(withoutDigest, "/")
	lastColon := strings.LastIndex(withoutDigest, ":")
	if lastColon > lastSlash {
		return strings.TrimSpace(withoutDigest[:lastColon])
	}
	return strings.TrimSpace(withoutDigest)
}

func normalizeDigest(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if isLowerHex64(trimmed) {
		return trimmed
	}
	if trimmed == "" {
		return ""
	}
	h := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(h[:])
}

func isLowerHex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func sanitizeName(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "build-image"
	}
	var b strings.Builder
	for i := 0; i < len(trimmed); i++ {
		ch := trimmed[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			b.WriteByte(ch)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "build-image"
	}
	return out
}
