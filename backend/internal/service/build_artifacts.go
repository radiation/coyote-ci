package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

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

	patterns, err := artifactPatternsFromBuild(build)
	if err != nil {
		return nil, fmt.Errorf("resolving build artifact declarations: %w", err)
	}
	if len(patterns) == 0 {
		return sortedArtifactPaths(existingPaths), nil
	}

	workspacePath := filepath.Join(s.artifactWorkspaceRoot, strings.TrimSpace(buildID))
	log.Printf("artifact collection start: build_id=%s workspace_path=%s declared_patterns=%q storage_root=%s", buildID, workspacePath, patterns, artifactStoreRootForLog(s.artifactStore))
	collectResult, err := s.artifactCollector.Collect(ctx, artifact.CollectRequest{
		BuildID:          buildID,
		WorkspacePath:    workspacePath,
		Patterns:         patterns,
		SkipLogicalPaths: existingPaths,
	})
	if err != nil {
		log.Printf("artifact collection error: build_id=%s workspace_path=%s err=%v", buildID, workspacePath, err)
		return nil, err
	}
	for _, warning := range collectResult.Warnings {
		log.Printf("artifact collection warning: build_id=%s %s", buildID, warning)
	}
	log.Printf("artifact metadata persistence start: build_id=%s artifacts_to_persist=%d", buildID, len(collectResult.Artifacts))

	for _, item := range collectResult.Artifacts {
		log.Printf("artifact metadata persist: build_id=%s logical_path=%s storage_key=%s size_bytes=%d", buildID, item.LogicalPath, item.StorageKey, item.SizeBytes)
		_, err := s.artifactRepo.Create(ctx, domain.BuildArtifact{
			ID:             uuid.NewString(),
			BuildID:        buildID,
			LogicalPath:    item.LogicalPath,
			StorageKey:     item.StorageKey,
			SizeBytes:      item.SizeBytes,
			ContentType:    item.ContentType,
			ChecksumSHA256: item.ChecksumSHA256,
			CreatedAt:      time.Now().UTC(),
		})
		if err != nil {
			log.Printf("artifact metadata persistence error: build_id=%s logical_path=%s err=%v", buildID, item.LogicalPath, err)
			return nil, fmt.Errorf("persisting artifact metadata: %w", err)
		}
		existingPaths[item.LogicalPath] = struct{}{}
	}
	log.Printf("artifact metadata persistence complete: build_id=%s persisted=%d", buildID, len(collectResult.Artifacts))

	return sortedArtifactPaths(existingPaths), nil
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

	return pipelineFile.Artifacts.Paths, nil
}
