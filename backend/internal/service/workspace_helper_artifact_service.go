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
	build, _, due, err := s.authorizeAndResolve(ctx, capabilityToken, executionJobID, podUID, buildSucceeded)
	if err != nil || !due {
		return WorkspaceHelperArtifactPlan{Collect: false}, err
	}
	return artifactPlanForBuild(build, s.builds, ctx)
}

func (s *WorkspaceHelperArtifactService) Upload(ctx context.Context, capabilityToken, executionJobID, podUID, stepID, logicalPath string, buildSucceeded bool, source io.Reader) error {
	build, job, due, err := s.authorizeAndResolve(ctx, capabilityToken, executionJobID, podUID, buildSucceeded)
	if err != nil {
		return err
	}
	if !due || source == nil {
		return ErrWorkspaceHelperArtifactInvalidInput
	}
	scopes, planErr := artifactScopesForBuild(build, s.builds, ctx)
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
	_, createErr := s.artifacts.Create(ctx, domain.BuildArtifact{ID: collected.GeneratedID, BuildID: job.BuildID, StepID: persistedStepID, LogicalPath: collected.LogicalPath, ArtifactType: domain.InferArtifactType(collected.LogicalPath, collected.ContentType), StorageKey: collected.StorageKey, StorageProvider: s.provider, SizeBytes: collected.SizeBytes, ContentType: collected.ContentType, ChecksumSHA256: collected.ChecksumSHA256, CreatedAt: time.Now().UTC()})
	if errors.Is(createErr, repository.ErrArtifactConflict) {
		return nil
	}
	return createErr
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
	for _, step := range steps {
		if step.ID == job.StepID || domain.IsTerminalStepStatus(step.Status) {
			continue
		}
		if buildSucceeded || step.StepIndex < job.StepIndex {
			return build, job, false, nil
		}
	}
	return build, job, true, nil
}

func artifactPlanForBuild(build domain.Build, builds workspaceHelperCacheBuildRepository, ctx context.Context) (WorkspaceHelperArtifactPlan, error) {
	scopes, err := artifactScopesForBuild(build, builds, ctx)
	if err != nil {
		return WorkspaceHelperArtifactPlan{}, err
	}
	return WorkspaceHelperArtifactPlan{Collect: len(scopes) > 0, Scopes: scopes}, nil
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
