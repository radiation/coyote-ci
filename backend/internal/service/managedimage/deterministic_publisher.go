package managedimage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

// DeterministicPublisher is a local-first publisher used for this slice.
// It produces immutable digest refs derived from the dependency fingerprint.
type DeterministicPublisher struct{}

func NewDeterministicPublisher() *DeterministicPublisher {
	return &DeterministicPublisher{}
}

func (p *DeterministicPublisher) Publish(_ context.Context, req PublishRequest) (PublishedImage, error) {
	digest := normalizeDigest(req.DependencyFingerprint)
	if digest == "" {
		return PublishedImage{}, fmt.Errorf("dependency fingerprint is required")
	}

	base := strings.TrimSpace(resolveBaseImageRef(req))
	if base == "" {
		return PublishedImage{}, fmt.Errorf("unable to determine base image reference")
	}

	return PublishedImage{
		ImageRef:     base + "@sha256:" + digest,
		ImageDigest:  "sha256:" + digest,
		VersionLabel: digest[:12],
	}, nil
}

func resolveBaseImageRef(req PublishRequest) string {
	pipelinePath := strings.TrimSpace(req.PipelinePath)
	if pipelinePath != "" && strings.TrimSpace(req.RepoRoot) != "" {
		fullPath := filepath.Join(req.RepoRoot, filepath.FromSlash(filepath.Clean(pipelinePath)))
		raw, readErr := os.ReadFile(fullPath)
		if readErr == nil {
			if imageRef, parseErr := pipeline.PipelineImageRef(raw); parseErr == nil {
				if normalized := imageRefWithoutDigestOrTag(imageRef); normalized != "" {
					return normalized
				}
			}
		}
	}

	fallbackName := strings.TrimSpace(req.ManagedImageName)
	if fallbackName == "" {
		fallbackName = "build-image"
	}
	return "ghcr.io/coyote-ci/managed/" + sanitizeName(fallbackName)
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
