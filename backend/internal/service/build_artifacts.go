package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

func (s *BuildService) runPostCompletionSideEffects(ctx context.Context, request runner.RunStepRequest, appender logs.StepLogChunkAppender) error {
	build, err := s.buildRepo.GetByID(ctx, request.BuildID)
	if err != nil {
		return fmt.Errorf("fetching build for post-completion side effects: %w", err)
	}
	if !domain.IsTerminalBuildStatus(build.Status) {
		return nil
	}

	artifactPaths := []string{}
	if s.artifactRepo != nil && s.artifactCollector != nil && strings.TrimSpace(s.artifactWorkspaceRoot) != "" {
		if emitErr := s.writeSystemExecutionLogLine(ctx, request, appender, "Collecting artifacts"); emitErr != nil {
			return emitErr
		}

		var artifactErr error
		artifactPaths, artifactErr = s.collectArtifactsIfTerminal(ctx, request.BuildID)
		if artifactErr != nil {
			_ = s.writeSystemExecutionLogLine(ctx, request, appender, "Artifact collection failed")
			_ = s.writeSystemExecutionLogLine(ctx, request, appender, formatFailureReasonLine("artifact collection failed"))
			return artifactErr
		}
	}

	if emitErr := s.writeSystemExecutionLogLine(ctx, request, appender, "Finalizing build"); emitErr != nil {
		return emitErr
	}

	summaryErr := s.writeTerminalBuildSummary(ctx, request, appender, artifactPaths)
	cleanupErr := s.cleanupExecutionIfTerminal(ctx, request.BuildID)

	return errors.Join(summaryErr, cleanupErr)
}

func (s *BuildService) writeTerminalBuildSummary(ctx context.Context, request runner.RunStepRequest, appender logs.StepLogChunkAppender, artifactPaths []string) error {
	build, err := s.buildRepo.GetByID(ctx, request.BuildID)
	if err != nil {
		return fmt.Errorf("fetching build for summary: %w", err)
	}
	if !domain.IsTerminalBuildStatus(build.Status) {
		return nil
	}

	steps, err := s.buildRepo.GetStepsByBuildID(ctx, request.BuildID)
	if err != nil {
		return fmt.Errorf("fetching build steps for summary: %w", err)
	}

	completedSteps := 0
	for _, step := range steps {
		if step.Status == domain.BuildStepStatusSuccess || step.Status == domain.BuildStepStatusFailed {
			completedSteps++
		}
	}

	duration := terminalBuildSummaryDuration(build, time.Now().UTC())
	for _, line := range formatBuildSummaryLines(build.Status, duration, completedSteps, len(steps), artifactPaths) {
		if err := s.writeSystemExecutionLogLine(ctx, request, appender, line); err != nil {
			return err
		}
	}

	return nil
}

func terminalBuildSummaryDuration(build domain.Build, now time.Time) time.Duration {
	if build.FinishedAt != nil && !build.FinishedAt.IsZero() {
		if build.StartedAt != nil && !build.StartedAt.IsZero() && !build.FinishedAt.Before(*build.StartedAt) {
			return build.FinishedAt.Sub(*build.StartedAt)
		}
		if !build.CreatedAt.IsZero() && !build.FinishedAt.Before(build.CreatedAt) {
			return build.FinishedAt.Sub(build.CreatedAt)
		}
	}

	if build.CreatedAt.IsZero() {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Before(build.CreatedAt) {
		return 0
	}

	return now.Sub(build.CreatedAt)
}

func (s *BuildService) collectArtifactsIfTerminal(ctx context.Context, buildID string) ([]string, error) {
	if s.artifactRepo == nil || s.artifactCollector == nil || strings.TrimSpace(s.artifactWorkspaceRoot) == "" {
		return nil, nil
	}

	build, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return nil, fmt.Errorf("fetching build for artifact collection: %w", err)
	}
	if !domain.IsTerminalBuildStatus(build.Status) {
		return nil, nil
	}

	existing, err := s.artifactRepo.ListByBuildID(ctx, buildID)
	if err != nil {
		return nil, fmt.Errorf("checking existing artifacts: %w", err)
	}
	existingPaths := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		existingPaths[item.LogicalPath] = struct{}{}
	}

	workspacePath := filepath.Join(s.artifactWorkspaceRoot, strings.TrimSpace(buildID))
	provider := s.storageProviderName()

	// Step-level artifact collection: preferred path.
	steps, err := s.buildRepo.GetStepsByBuildID(ctx, buildID)
	if err != nil {
		return nil, fmt.Errorf("fetching steps for artifact collection: %w", err)
	}
	for _, step := range steps {
		if len(step.ArtifactPaths) == 0 {
			continue
		}
		log.Printf("artifact step collection start: build_id=%s step_id=%s step_name=%s patterns=%q", buildID, step.ID, step.Name, step.ArtifactPaths)
		if err = s.collectAndPersistArtifacts(ctx, buildID, &step.ID, provider, workspacePath, step.ArtifactPaths, existingPaths); err != nil {
			return nil, err
		}
	}

	// Pipeline-level artifact collection: backward compatibility.
	patterns, err := artifactPatternsFromBuild(build)
	if err != nil {
		return nil, fmt.Errorf("resolving build artifact declarations: %w", err)
	}
	if len(patterns) > 0 {
		log.Printf("artifact pipeline collection start: build_id=%s patterns=%q", buildID, patterns)
		if err := s.collectAndPersistArtifacts(ctx, buildID, nil, provider, workspacePath, patterns, existingPaths); err != nil {
			return nil, err
		}
	}

	return sortedArtifactPaths(existingPaths), nil
}

// collectAndPersistArtifacts collects artifacts from the workspace and persists metadata.
func (s *BuildService) collectAndPersistArtifacts(ctx context.Context, buildID string, stepID *string, provider domain.StorageProvider, workspacePath string, patterns []string, existingPaths map[string]struct{}) error {
	stepIDStr := ""
	if stepID != nil {
		stepIDStr = *stepID
	}

	collectResult, err := s.artifactCollector.Collect(ctx, artifact.CollectRequest{
		BuildID:          buildID,
		StepID:           stepIDStr,
		WorkspacePath:    workspacePath,
		Patterns:         patterns,
		SkipLogicalPaths: existingPaths,
	})
	if err != nil {
		log.Printf("artifact collection error: build_id=%s step_id=%s err=%v", buildID, stepIDStr, err)
		return err
	}
	for _, warning := range collectResult.Warnings {
		log.Printf("artifact collection warning: build_id=%s %s", buildID, warning)
	}

	for _, item := range collectResult.Artifacts {
		log.Printf("artifact metadata persist: build_id=%s step_id=%s logical_path=%s storage_key=%s size_bytes=%d", buildID, stepIDStr, item.LogicalPath, item.StorageKey, item.SizeBytes)
		_, err := s.artifactRepo.Create(ctx, domain.BuildArtifact{
			ID:              item.GeneratedID,
			BuildID:         buildID,
			StepID:          stepID,
			LogicalPath:     item.LogicalPath,
			StorageKey:      item.StorageKey,
			StorageProvider: provider,
			SizeBytes:       item.SizeBytes,
			ContentType:     item.ContentType,
			ChecksumSHA256:  item.ChecksumSHA256,
			CreatedAt:       time.Now().UTC(),
		})
		if err != nil {
			log.Printf("artifact metadata persistence error: build_id=%s logical_path=%s err=%v", buildID, item.LogicalPath, err)
			return fmt.Errorf("persisting artifact metadata: %w", err)
		}
		existingPaths[item.LogicalPath] = struct{}{}
	}

	return nil
}

// storageProviderName returns the provider identifier for the configured artifact store.
func (s *BuildService) storageProviderName() domain.StorageProvider {
	if s.artifactStorageProvider != "" {
		return s.artifactStorageProvider
	}
	return domain.StorageProviderFilesystem
}

func sortedArtifactPaths(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for logicalPath := range values {
		out = append(out, logicalPath)
	}
	sort.Strings(out)
	return out
}

func artifactStoreRootForLog(store artifact.Store) string {
	if store == nil {
		return ""
	}
	if reporter, ok := store.(interface{ RootPath() string }); ok {
		return strings.TrimSpace(reporter.RootPath())
	}
	return ""
}

func artifactPatternsFromBuild(build domain.Build) ([]string, error) {
	if build.PipelineConfigYAML == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*build.PipelineConfigYAML)
	if trimmed == "" {
		return nil, nil
	}

	pipelineFile, err := pipeline.ParseAndValidate([]byte(trimmed))
	if err != nil {
		return nil, err
	}

	patterns := append([]string(nil), pipelineFile.Artifacts.Paths...)
	if build.PipelineSource != nil && *build.PipelineSource == pipelineSourceRepo && build.PipelinePath != nil {
		normalizedPipelinePath := path.Clean(filepath.ToSlash(strings.TrimSpace(*build.PipelinePath)))
		if normalizedPipelinePath != pipelineFilePath {
			pipelineDir := path.Clean(path.Dir(normalizedPipelinePath))
			if pipelineDir == "" {
				pipelineDir = "."
			}
			if pipelineDir != "." {
				for i, patternValue := range patterns {
					normalized := path.Clean(path.Join(pipelineDir, strings.TrimSpace(patternValue)))
					if normalized == ".." || strings.HasPrefix(normalized, "../") {
						return nil, fmt.Errorf("artifact path escapes repository root")
					}
					patterns[i] = normalized
				}
			}
		}
	}

	return patterns, nil
}
