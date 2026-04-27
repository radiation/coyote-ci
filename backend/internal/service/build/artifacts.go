package build

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

	var tagErr error
	if build.Status == domain.BuildStatusSuccess {
		if emitErr := s.writeSystemExecutionLogLine(ctx, request, appender, "Assigning version tags"); emitErr != nil {
			return emitErr
		}
		tagErr = s.autoTagBuildOutputs(ctx, build)
		if tagErr != nil {
			_ = s.writeSystemExecutionLogLine(ctx, request, appender, "Automatic version tagging failed")
			_ = s.writeSystemExecutionLogLine(ctx, request, appender, formatFailureReasonLine("automatic version tagging failed"))
		}
	}

	if emitErr := s.writeSystemExecutionLogLine(ctx, request, appender, "Finalizing build"); emitErr != nil {
		return emitErr
	}

	summaryErr := s.writeTerminalBuildSummary(ctx, request, appender, artifactPaths)
	cleanupErr := s.cleanupExecutionIfTerminal(ctx, request.BuildID)

	return errors.Join(summaryErr, cleanupErr, tagErr)
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
	identityKeys := make(map[string]struct{}, len(existing))
	allLogicalPaths := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		identityKeys[artifactIdentityKey(item.StepID, item.LogicalPath)] = struct{}{}
		allLogicalPaths[item.LogicalPath] = struct{}{}
	}

	workspacePath := filepath.Join(s.artifactWorkspaceRoot, strings.TrimSpace(buildID))
	provider := s.storageProviderName()
	stepTypeHints, err := stepArtifactTypeHintsFromBuild(build)
	if err != nil {
		return nil, fmt.Errorf("resolving step artifact declarations: %w", err)
	}

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
		var collected []string
		declarations := declarationsForPatterns(step.ArtifactPaths, stepTypeHints[step.StepIndex])
		collected, err = s.collectAndPersistArtifacts(ctx, buildID, &step.ID, provider, workspacePath, declarations, identityKeys)
		if err != nil {
			return nil, err
		}
		for _, p := range collected {
			allLogicalPaths[p] = struct{}{}
		}
	}

	// Pipeline-level artifact collection: backward compatibility.
	declarations, err := artifactDeclarationsFromBuild(build)
	if err != nil {
		return nil, fmt.Errorf("resolving build artifact declarations: %w", err)
	}
	if len(declarations) > 0 {
		log.Printf("artifact pipeline collection start: build_id=%s patterns=%q", buildID, declarationPaths(declarations))
		collected, err := s.collectAndPersistArtifacts(ctx, buildID, nil, provider, workspacePath, declarations, identityKeys)
		if err != nil {
			return nil, err
		}
		for _, p := range collected {
			allLogicalPaths[p] = struct{}{}
		}
	}

	return sortedArtifactPaths(allLogicalPaths), nil
}

// collectAndPersistArtifacts collects artifacts from the workspace and persists metadata.
func (s *BuildService) collectAndPersistArtifacts(ctx context.Context, buildID string, stepID *string, provider domain.StorageProvider, workspacePath string, declarations []domain.ArtifactDeclaration, identityKeys map[string]struct{}) ([]string, error) {
	stepIDStr := ""
	if stepID != nil {
		stepIDStr = *stepID
	}

	skipPaths := skipPathsForScope(identityKeys, stepID)
	artifactTypes := declarationTypeIndex(declarations)

	collectResult, err := s.artifactCollector.Collect(ctx, artifact.CollectRequest{
		BuildID:          buildID,
		StepID:           stepIDStr,
		WorkspacePath:    workspacePath,
		Patterns:         declarationPaths(declarations),
		SkipLogicalPaths: skipPaths,
	})
	if err != nil {
		log.Printf("artifact collection error: build_id=%s step_id=%s err=%v", buildID, stepIDStr, err)
		return nil, err
	}
	for _, warning := range collectResult.Warnings {
		log.Printf("artifact collection warning: build_id=%s %s", buildID, warning)
	}

	var collected []string
	for _, item := range collectResult.Artifacts {
		log.Printf("artifact metadata persist: build_id=%s step_id=%s logical_path=%s storage_key=%s size_bytes=%d", buildID, stepIDStr, item.LogicalPath, item.StorageKey, item.SizeBytes)
		_, err := s.artifactRepo.Create(ctx, domain.BuildArtifact{
			ID:              item.GeneratedID,
			BuildID:         buildID,
			StepID:          stepID,
			LogicalPath:     item.LogicalPath,
			ArtifactType:    artifactTypes[item.LogicalPath],
			StorageKey:      item.StorageKey,
			StorageProvider: provider,
			SizeBytes:       item.SizeBytes,
			ContentType:     item.ContentType,
			ChecksumSHA256:  item.ChecksumSHA256,
			CreatedAt:       time.Now().UTC(),
		})
		if err != nil {
			log.Printf("artifact metadata persistence error: build_id=%s logical_path=%s err=%v", buildID, item.LogicalPath, err)
			return nil, fmt.Errorf("persisting artifact metadata: %w", err)
		}
		identityKeys[artifactIdentityKey(stepID, item.LogicalPath)] = struct{}{}
		collected = append(collected, item.LogicalPath)
	}

	return collected, nil
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

// artifactIdentityKey produces a composite key for artifact deduplication that
// distinguishes step-scoped from shared artifacts.
func artifactIdentityKey(stepID *string, logicalPath string) string {
	if stepID != nil && *stepID != "" {
		return "step:" + *stepID + ":" + logicalPath
	}
	return "shared:" + logicalPath
}

// skipPathsForScope extracts logical paths from composite identity keys that
// belong to the given scope (step or shared).
func skipPathsForScope(identityKeys map[string]struct{}, stepID *string) map[string]struct{} {
	var prefix string
	if stepID != nil && *stepID != "" {
		prefix = "step:" + *stepID + ":"
	} else {
		prefix = "shared:"
	}
	result := make(map[string]struct{})
	for key := range identityKeys {
		after, ok := strings.CutPrefix(key, prefix)
		if ok {
			result[after] = struct{}{}
		}
	}
	return result
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

func declarationPaths(declarations []domain.ArtifactDeclaration) []string {
	paths := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		paths = append(paths, strings.TrimSpace(declaration.Path))
	}
	return paths
}

func declarationTypeIndex(declarations []domain.ArtifactDeclaration) map[string]domain.ArtifactType {
	indexed := make(map[string]domain.ArtifactType, len(declarations))
	for _, declaration := range declarations {
		trimmedPath := strings.TrimSpace(declaration.Path)
		if trimmedPath == "" || declaration.Type == "" {
			continue
		}
		indexed[trimmedPath] = declaration.Type
	}
	return indexed
}

func declarationsForPatterns(patterns []string, typeHints map[string]domain.ArtifactType) []domain.ArtifactDeclaration {
	declarations := make([]domain.ArtifactDeclaration, 0, len(patterns))
	for _, pattern := range patterns {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			continue
		}
		declarations = append(declarations, domain.ArtifactDeclaration{
			Path: trimmed,
			Type: typeHints[trimmed],
		})
	}
	return declarations
}

func stepArtifactTypeHintsFromBuild(build domain.Build) (map[int]map[string]domain.ArtifactType, error) {
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
	resolved := pipeline.Resolve(pipelineFile)
	hints := make(map[int]map[string]domain.ArtifactType, len(resolved.Steps))
	for index, step := range resolved.Steps {
		declared := declarationTypeIndex(step.ArtifactDecls)
		if len(declared) == 0 {
			continue
		}
		hints[index] = declared
	}
	return hints, nil
}

func artifactDeclarationsFromBuild(build domain.Build) ([]domain.ArtifactDeclaration, error) {
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

	declarations := append([]domain.ArtifactDeclaration(nil), pipelineFile.Artifacts.Declarations...)
	if build.PipelineSource != nil && *build.PipelineSource == pipelineSourceRepo && build.PipelinePath != nil {
		normalizedPipelinePath := path.Clean(filepath.ToSlash(strings.TrimSpace(*build.PipelinePath)))
		if normalizedPipelinePath != pipelineFilePath {
			pipelineDir := path.Clean(path.Dir(normalizedPipelinePath))
			if pipelineDir == "" {
				pipelineDir = "."
			}
			if pipelineDir != "." {
				for index, declaration := range declarations {
					normalized := path.Clean(path.Join(pipelineDir, strings.TrimSpace(declaration.Path)))
					if normalized == ".." || strings.HasPrefix(normalized, "../") {
						return nil, fmt.Errorf("artifact path escapes repository root")
					}
					declarations[index].Path = normalized
				}
			}
		}
	}

	return declarations, nil
}

func artifactPatternsFromBuild(build domain.Build) ([]string, error) {
	declarations, err := artifactDeclarationsFromBuild(build)
	if err != nil {
		return nil, err
	}
	return declarationPaths(declarations), nil
}
