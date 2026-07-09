package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

func readOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (s *BuildService) prepareTriggerArtifactHandoff(ctx context.Context, build domain.Build) error {
	trigger := domain.NormalizeBuildTrigger(build.Trigger)
	if trigger.Kind != domain.BuildTriggerKindArtifact {
		return nil
	}

	producerProjectID := readOptionalString(trigger.ProducerProjectID)
	if producerProjectID == "" {
		return fmt.Errorf("artifact trigger producer project id is required")
	}
	if strings.TrimSpace(build.ProjectID) == "" {
		return fmt.Errorf("build project id is required")
	}
	if producerProjectID != strings.TrimSpace(build.ProjectID) {
		return fmt.Errorf("artifact trigger producer project mismatch")
	}

	producerBuildID := readOptionalString(trigger.ProducerBuildID)
	artifactID := readOptionalString(trigger.ArtifactID)
	artifactPath := readOptionalString(trigger.ArtifactPath)
	if producerBuildID == "" || artifactID == "" || artifactPath == "" {
		return fmt.Errorf("artifact trigger provenance is incomplete")
	}

	relativePath, err := workspace.TriggerArtifactRelativePath(artifactPath)
	if err != nil {
		return fmt.Errorf("invalid trigger artifact path: %w", err)
	}

	workspaceRoot := s.currentWorkspaceRoot()
	if workspaceRoot == "" {
		return ErrExecutionWorkspaceRootNotConfigured
	}
	buildWorkspace := workspace.New(build.ID, filepath.Join(workspaceRoot, strings.TrimSpace(build.ID)))
	destinationPath, err := buildWorkspace.ResolveRelativePath(relativePath)
	if err != nil {
		return fmt.Errorf("resolve trigger artifact handoff path: %w", err)
	}
	if _, statErr := os.Stat(destinationPath); statErr == nil {
		return fmt.Errorf("trigger artifact handoff path already exists: %s", relativePath)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat trigger artifact handoff path: %w", statErr)
	}

	meta, reader, err := s.OpenBuildArtifact(ctx, producerBuildID, artifactID)
	if err != nil {
		return err
	}
	defer func() {
		_ = reader.Close()
	}()
	if strings.TrimSpace(meta.LogicalPath) != artifactPath {
		return fmt.Errorf("trigger artifact path mismatch: expected %q, got %q", artifactPath, strings.TrimSpace(meta.LogicalPath))
	}

	expectedChecksum := readOptionalString(trigger.ArtifactChecksumSHA256)
	if expectedChecksum == "" {
		expectedChecksum = readOptionalString(meta.ChecksumSHA256)
	}
	if err := copyTriggerArtifactToWorkspace(buildWorkspace.HostRoot, destinationPath, reader, expectedChecksum); err != nil {
		return err
	}

	return nil
}

func copyTriggerArtifactToWorkspace(workspaceRoot string, destinationPath string, src io.Reader, expectedChecksum string) error {
	if mkdirErr := os.MkdirAll(workspaceRoot, 0o755); mkdirErr != nil {
		return fmt.Errorf("creating build workspace root: %w", mkdirErr)
	}
	destinationDir := filepath.Dir(destinationPath)
	if err := ensurePathUsesWorkspaceRoot(workspaceRoot, destinationDir); err != nil {
		return err
	}
	if mkdirErr := os.MkdirAll(destinationDir, 0o755); mkdirErr != nil {
		return fmt.Errorf("creating trigger artifact directory: %w", mkdirErr)
	}
	if err := ensurePathUsesWorkspaceRoot(workspaceRoot, destinationDir); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(destinationDir, ".trigger-artifact-*")
	if err != nil {
		return fmt.Errorf("creating trigger artifact temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, hasher), src); err != nil {
		return fmt.Errorf("writing trigger artifact content: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("syncing trigger artifact content: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing trigger artifact temp file: %w", err)
	}

	trimmedExpected := strings.TrimSpace(expectedChecksum)
	if trimmedExpected != "" {
		actualChecksum := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actualChecksum, trimmedExpected) {
			return fmt.Errorf("trigger artifact checksum mismatch: expected %s, got %s", trimmedExpected, actualChecksum)
		}
	}

	if err := os.Rename(tmpPath, destinationPath); err != nil {
		return fmt.Errorf("moving trigger artifact into place: %w", err)
	}
	return nil
}

func ensurePathUsesWorkspaceRoot(workspaceRoot string, targetPath string) error {
	trimmedRoot := strings.TrimSpace(workspaceRoot)
	if trimmedRoot == "" {
		return fmt.Errorf("workspace root is required")
	}
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(trimmedRoot))
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}

	resolvedTarget, err := resolveExistingPathWithinTarget(targetPath)
	if err != nil {
		return err
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil {
		return fmt.Errorf("compare trigger artifact path against workspace root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("trigger artifact handoff path escapes workspace root")
	}
	return nil
}

func resolveExistingPathWithinTarget(targetPath string) (string, error) {
	current := filepath.Clean(targetPath)
	for {
		_, statErr := os.Lstat(current)
		if statErr == nil {
			resolvedPath, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", fmt.Errorf("resolve trigger artifact path: %w", err)
			}
			return resolvedPath, nil
		}
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat trigger artifact path: %w", statErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve trigger artifact path: no existing ancestor for %q", targetPath)
		}
		current = parent
	}
}
