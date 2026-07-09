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
		return fmt.Errorf("trigger artifact path mismatch: expected %s, got %s", artifactPath, strings.TrimSpace(meta.LogicalPath))
	}

	expectedChecksum := readOptionalString(trigger.ArtifactChecksumSHA256)
	if expectedChecksum == "" {
		expectedChecksum = readOptionalString(meta.ChecksumSHA256)
	}
	if err := copyTriggerArtifactToWorkspace(destinationPath, reader, expectedChecksum); err != nil {
		return err
	}

	return nil
}

func copyTriggerArtifactToWorkspace(destinationPath string, src io.Reader, expectedChecksum string) error {
	if mkdirErr := os.MkdirAll(filepath.Dir(destinationPath), 0o755); mkdirErr != nil {
		return fmt.Errorf("creating trigger artifact directory: %w", mkdirErr)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(destinationPath), ".trigger-artifact-*")
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
