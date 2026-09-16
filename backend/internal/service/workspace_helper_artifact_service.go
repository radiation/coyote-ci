package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrWorkspaceHelperArtifactInvalidInput = errors.New("invalid workspace helper artifact input")

type WorkspaceHelperArtifactScope struct {
	StepID   string   `json:"step_id,omitempty"`
	Patterns []string `json:"patterns"`
}

type WorkspaceHelperArtifactPlan struct {
	Collect bool                           `json:"collect"`
	Scopes  []WorkspaceHelperArtifactScope `json:"scopes"`
}

type WorkspaceHelperArtifactServiceConfig struct {
	CapabilityAuthorizer WorkspacePrepareCapabilityAuthorizer
	ExecutionJobs        workspacePrepareExecutionJobRepository
	Builds               workspaceHelperCacheBuildRepository
	Artifacts            repository.ArtifactRepository
	Collector            *artifact.Collector
	Provider             domain.StorageProvider
}

type WorkspaceHelperArtifactService struct {
	capabilities  WorkspacePrepareCapabilityAuthorizer
	executionJobs workspacePrepareExecutionJobRepository
	builds        workspaceHelperCacheBuildRepository
	artifacts     repository.ArtifactRepository
	collector     *artifact.Collector
	provider      domain.StorageProvider
}

func NewWorkspaceHelperArtifactService(config WorkspaceHelperArtifactServiceConfig) (*WorkspaceHelperArtifactService, error) {
	if config.CapabilityAuthorizer == nil || config.ExecutionJobs == nil || config.Builds == nil || config.Artifacts == nil || config.Collector == nil {
		return nil, errors.New("workspace helper artifact service requires capability, execution job, build, artifact repository, and collector")
	}
	if config.Provider == "" {
		config.Provider = domain.StorageProviderFilesystem
	}
	return &WorkspaceHelperArtifactService{capabilities: config.CapabilityAuthorizer, executionJobs: config.ExecutionJobs, builds: config.Builds, artifacts: config.Artifacts, collector: config.Collector, provider: config.Provider}, nil
}

func (s *WorkspaceHelperArtifactService) Plan(ctx context.Context, capabilityToken, executionJobID, podUID string, buildSucceeded bool) (WorkspaceHelperArtifactPlan, error) {
	build, job, due, err := s.authorizeAndResolve(ctx, capabilityToken, executionJobID, podUID, buildSucceeded)
	if err != nil || !due {
		return WorkspaceHelperArtifactPlan{Collect: false}, err
	}
	return artifactPlanForJob(build, job, s.builds, ctx, buildSucceeded)
}

func (s *WorkspaceHelperArtifactService) Upload(ctx context.Context, capabilityToken, executionJobID, podUID, stepID, logicalPath string, buildSucceeded bool, source io.Reader) error {
	build, job, due, err := s.authorizeAndResolve(ctx, capabilityToken, executionJobID, podUID, buildSucceeded)
	if err != nil {
		return err
	}
	if !due || source == nil {
		return ErrWorkspaceHelperArtifactInvalidInput
	}
	scopes, planErr := artifactScopesForJob(build, job, s.builds, ctx, buildSucceeded)
	if planErr != nil {
		return planErr
	}
	if !scopeMatchesArtifact(scopes, stepID, logicalPath) {
		return ErrWorkspaceHelperArtifactInvalidInput
	}
	existing, listErr := s.artifacts.ListByBuildID(ctx, build.ID)
	if listErr != nil {
		return listErr
	}
	for _, item := range existing {
		if item.LogicalPath != logicalPath {
			continue
		}
		if strings.TrimSpace(stepID) == "" || (item.StepID != nil && *item.StepID == stepID) {
			return nil
		}
	}
	collected, collectErr := s.collector.CollectReader(ctx, artifact.CollectReaderRequest{BuildID: build.ID, StepID: stepID, LogicalPath: logicalPath, Source: source})
	if collectErr != nil {
		return collectErr
	}
	var persistedStepID *string
	if strings.TrimSpace(stepID) != "" {
		value := strings.TrimSpace(stepID)
		persistedStepID = &value
	}
	declaration := artifactDeclarationForJob(build, job, collected.LogicalPath)
	artifactType := declaration.Type
	if artifactType == "" {
		artifactType = domain.InferArtifactType(collected.LogicalPath, collected.ContentType)
	}
	_, createErr := s.artifacts.Create(ctx, domain.BuildArtifact{ID: collected.GeneratedID, BuildID: job.BuildID, StepID: persistedStepID, Name: declaration.Name, LogicalPath: collected.LogicalPath, ArtifactType: artifactType, StorageKey: collected.StorageKey, StorageProvider: s.provider, SizeBytes: collected.SizeBytes, ContentType: collected.ContentType, ChecksumSHA256: collected.ChecksumSHA256, TargetPlatform: declaration.Platform, CreatedAt: time.Now().UTC()})
	if errors.Is(createErr, repository.ErrArtifactConflict) {
		return nil
	}
	return createErr
}

func artifactDeclarationForJob(build domain.Build, job domain.ExecutionJob, logicalPath string) domain.ArtifactDeclaration {
	if build.PipelineConfigYAML == nil || strings.TrimSpace(*build.PipelineConfigYAML) == "" {
		return domain.ArtifactDeclaration{}
	}
	resolved, err := pipeline.LoadAndResolve([]byte(*build.PipelineConfigYAML))
	if err != nil {
		return domain.ArtifactDeclaration{}
	}
	for _, step := range resolved.Steps {
		if step.NodeID != job.NodeID {
			continue
		}
		for _, declaration := range step.ArtifactDecls {
			if declaration.Path == logicalPath {
				return declaration
			}
		}
	}
	return domain.ArtifactDeclaration{}
}

func (s *WorkspaceHelperArtifactService) authorizeAndResolve(ctx context.Context, token, executionJobID, podUID string, buildSucceeded bool) (domain.Build, domain.ExecutionJob, bool, error) {
	if _, err := s.capabilities.Authorize(ctx, token, strings.TrimSpace(executionJobID), strings.TrimSpace(podUID), domain.WorkspaceHelperRoleArtifactCollect); err != nil {
		return domain.Build{}, domain.ExecutionJob{}, false, err
	}
	job, err := s.executionJobs.GetJobByID(ctx, strings.TrimSpace(executionJobID))
	if err != nil {
		return domain.Build{}, domain.ExecutionJob{}, false, err
	}
	build, err := s.builds.GetByID(ctx, job.BuildID)
	if err != nil {
		return domain.Build{}, domain.ExecutionJob{}, false, err
	}
	steps, err := s.builds.GetStepsByBuildID(ctx, build.ID)
	if err != nil {
		return domain.Build{}, domain.ExecutionJob{}, false, err
	}
	if buildSucceeded {
		return build, job, true, nil
	}
	for _, step := range steps {
		if step.ID == job.StepID || domain.IsTerminalStepStatus(step.Status) {
			continue
		}
		if step.StepIndex < job.StepIndex {
			return build, job, false, nil
		}
	}
	return build, job, true, nil
}

func artifactPlanForJob(build domain.Build, job domain.ExecutionJob, builds workspaceHelperCacheBuildRepository, ctx context.Context, buildSucceeded bool) (WorkspaceHelperArtifactPlan, error) {
	scopes, err := artifactScopesForJob(build, job, builds, ctx, buildSucceeded)
	if err != nil {
		return WorkspaceHelperArtifactPlan{}, err
	}
	return WorkspaceHelperArtifactPlan{Collect: len(scopes) > 0, Scopes: scopes}, nil
}

func artifactScopesForJob(build domain.Build, job domain.ExecutionJob, builds workspaceHelperCacheBuildRepository, ctx context.Context, buildSucceeded bool) ([]WorkspaceHelperArtifactScope, error) {
	scopes, err := artifactScopesForBuild(build, builds, ctx)
	if err != nil {
		return nil, err
	}
	if buildSucceeded {
		return scopesForSuccessfulJob(scopes, job, build, builds, ctx)
	}
	return scopes, nil
}

func scopesForSuccessfulJob(scopes []WorkspaceHelperArtifactScope, job domain.ExecutionJob, build domain.Build, builds workspaceHelperCacheBuildRepository, ctx context.Context) ([]WorkspaceHelperArtifactScope, error) {
	filtered := make([]WorkspaceHelperArtifactScope, 0, len(scopes))
	for _, scope := range scopes {
		if scope.StepID == job.StepID {
			filtered = append(filtered, scope)
		}
	}
	steps, err := builds.GetStepsByBuildID(ctx, build.ID)
	if err != nil {
		return nil, err
	}
	for _, step := range steps {
		if step.ID != job.StepID && (step.StepIndex > job.StepIndex || !domain.IsTerminalStepStatus(step.Status)) {
			return filtered, nil
		}
	}
	for _, scope := range scopes {
		if scope.StepID == "" {
			filtered = append(filtered, scope)
		}
	}
	return filtered, nil
}

func artifactScopesForBuild(build domain.Build, builds workspaceHelperCacheBuildRepository, ctx context.Context) ([]WorkspaceHelperArtifactScope, error) {
	steps, err := builds.GetStepsByBuildID(ctx, build.ID)
	if err != nil {
		return nil, err
	}
	scopes := make([]WorkspaceHelperArtifactScope, 0, len(steps)+1)
	for _, step := range steps {
		if len(step.ArtifactPaths) > 0 {
			scopes = append(scopes, WorkspaceHelperArtifactScope{StepID: step.ID, Patterns: append([]string(nil), step.ArtifactPaths...)})
		}
	}
	if build.PipelineConfigYAML == nil || strings.TrimSpace(*build.PipelineConfigYAML) == "" {
		return scopes, nil
	}
	resolved, err := pipeline.LoadAndResolve([]byte(*build.PipelineConfigYAML))
	if err != nil {
		return nil, err
	}
	if len(resolved.Artifacts.Paths) > 0 {
		scopes = append(scopes, WorkspaceHelperArtifactScope{Patterns: append([]string(nil), resolved.Artifacts.Paths...)})
	}
	return scopes, nil
}

func scopeMatchesArtifact(scopes []WorkspaceHelperArtifactScope, stepID, logicalPath string) bool {
	for _, scope := range scopes {
		if strings.TrimSpace(scope.StepID) != strings.TrimSpace(stepID) {
			continue
		}
		for _, pattern := range scope.Patterns {
			if artifact.MatchPathPattern(pattern, logicalPath) {
				return true
			}
		}
	}
	return false
}
