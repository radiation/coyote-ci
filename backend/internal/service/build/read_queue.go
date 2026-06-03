package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	artifactmatch "github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	"github.com/radiation/coyote-ci/backend/internal/versioning"
)

func (s *BuildService) GetBuild(ctx context.Context, id string) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	return build, mapRepoErr(err)
}

func (s *BuildService) ListBuilds(ctx context.Context) ([]domain.Build, error) {
	return s.buildRepo.List(ctx)
}

func (s *BuildService) ListActiveBuilds(ctx context.Context) ([]domain.Build, error) {
	return s.buildRepo.ListActive(ctx)
}

func (s *BuildService) ListBuildsPaged(ctx context.Context, params repository.ListParams) ([]domain.Build, error) {
	return s.buildRepo.ListPaged(ctx, params)
}

func (s *BuildService) ListQueue(ctx context.Context, params repository.QueueListParams) ([]domain.QueueEntry, error) {
	return s.buildRepo.ListQueue(ctx, params)
}

func (s *BuildService) ListBuildsByJobID(ctx context.Context, jobID string) ([]domain.Build, error) {
	return s.buildRepo.ListByJobID(ctx, jobID)
}

func (s *BuildService) ListLatestBuildsByJobIDs(ctx context.Context, jobIDs []string) (map[string]domain.Build, error) {
	return s.buildRepo.ListLatestByJobIDs(ctx, jobIDs)
}

func (s *BuildService) GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error) {
	steps, err := s.buildRepo.GetStepsByBuildID(ctx, id)
	return steps, mapRepoErr(err)
}

func (s *BuildService) GetJobsByBuildID(ctx context.Context, buildID string) ([]domain.ExecutionJob, error) {
	if s.executionJobRepo == nil {
		return []domain.ExecutionJob{}, nil
	}
	return s.executionJobRepo.GetJobsByBuildID(ctx, buildID)
}

func (s *BuildService) GetJobByID(ctx context.Context, jobID string) (domain.ExecutionJob, error) {
	if s.executionJobRepo == nil {
		return domain.ExecutionJob{}, ErrExecutionJobRepoNotConfigured
	}
	job, err := s.executionJobRepo.GetJobByID(ctx, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrExecutionJobNotFound) {
			return domain.ExecutionJob{}, ErrExecutionJobNotFound
		}
		return domain.ExecutionJob{}, err
	}
	return job, nil
}

func (s *BuildService) GetJobOutputsByBuildID(ctx context.Context, buildID string) ([]domain.ExecutionJobOutput, error) {
	if s.executionOutputRepo == nil {
		return []domain.ExecutionJobOutput{}, nil
	}
	return s.executionOutputRepo.ListByBuildID(ctx, buildID)
}

func (s *BuildService) GetJobOutputsByJobID(ctx context.Context, jobID string) ([]domain.ExecutionJobOutput, error) {
	if s.executionOutputRepo == nil {
		return []domain.ExecutionJobOutput{}, nil
	}
	return s.executionOutputRepo.ListByJobID(ctx, jobID)
}

func (s *BuildService) ClaimNextRunnableJob(ctx context.Context, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	if s.executionJobRepo == nil {
		return domain.ExecutionJob{}, false, nil
	}
	return s.executionJobRepo.ClaimNextRunnableJob(ctx, claim)
}

func (s *BuildService) GetJobByStepID(ctx context.Context, stepID string) (domain.ExecutionJob, error) {
	if s.executionJobRepo == nil {
		return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
	}
	job, err := s.executionJobRepo.GetJobByStepID(ctx, stepID)
	if err != nil {
		return domain.ExecutionJob{}, err
	}
	return job, nil
}

func (s *BuildService) ClaimJobByStepID(ctx context.Context, stepID string, claim repository.StepClaim) (domain.ExecutionJob, bool, error) {
	if s.executionJobRepo == nil {
		return domain.ExecutionJob{}, false, nil
	}
	return s.executionJobRepo.ClaimJobByStepID(ctx, stepID, claim)
}

func (s *BuildService) RenewJobLease(ctx context.Context, jobID string, claimToken string, leaseExpiresAt time.Time) (domain.ExecutionJob, bool, error) {
	if s.executionJobRepo == nil {
		return domain.ExecutionJob{}, false, nil
	}
	job, outcome, err := s.executionJobRepo.RenewJobLease(ctx, jobID, claimToken, leaseExpiresAt)
	if err != nil {
		return domain.ExecutionJob{}, false, err
	}
	if outcome == repository.StepCompletionCompleted {
		return job, true, nil
	}
	if outcome == repository.StepCompletionStaleClaim {
		return job, false, ErrStaleStepClaim
	}
	return job, false, nil
}

func (s *BuildService) GetBuildLogs(ctx context.Context, id string) ([]logs.BuildLogLine, error) {
	if _, err := s.buildRepo.GetByID(ctx, id); err != nil {
		return nil, mapRepoErr(err)
	}

	reader, ok := s.logSink.(logs.LogReader)
	if !ok {
		return []logs.BuildLogLine{}, nil
	}

	return reader.GetBuildLogs(ctx, id)
}

func (s *BuildService) GetStepLogChunks(ctx context.Context, buildID string, stepIndex int, afterSequence int64, limit int) ([]logs.StepLogChunk, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return nil, mapRepoErr(err)
	}

	reader, ok := s.logSink.(logs.StepLogChunkReader)
	if !ok {
		return []logs.StepLogChunk{}, nil
	}

	return reader.ListStepLogChunks(ctx, buildID, stepIndex, afterSequence, limit)
}

func (s *BuildService) GetBuildArtifacts(ctx context.Context, buildID string) ([]domain.BuildArtifact, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return nil, mapRepoErr(err)
	}
	if s.artifactRepo == nil {
		return []domain.BuildArtifact{}, nil
	}

	artifacts, err := s.artifactRepo.ListByBuildID(ctx, buildID)
	if err != nil {
		return nil, err
	}

	return artifacts, nil
}

func (s *BuildService) OpenBuildArtifact(ctx context.Context, buildID string, artifactID string) (domain.BuildArtifact, io.ReadCloser, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return domain.BuildArtifact{}, nil, mapRepoErr(err)
	}
	if s.artifactRepo == nil || s.artifactStore == nil {
		return domain.BuildArtifact{}, nil, ErrArtifactNotFound
	}

	meta, err := s.artifactRepo.GetByID(ctx, buildID, artifactID)
	if err != nil {
		if errors.Is(err, repository.ErrArtifactNotFound) {
			return domain.BuildArtifact{}, nil, ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, nil, err
	}

	store := s.artifactStore
	if s.artifactStoreResolver != nil {
		provider := meta.StorageProvider
		if provider == "" {
			provider = domain.StorageProviderFilesystem
		}
		resolved, resolveErr := s.artifactStoreResolver.Resolve(provider)
		if resolveErr != nil {
			return domain.BuildArtifact{}, nil, fmt.Errorf("%w: provider=%s", ErrArtifactStorageProviderNotConfigured, provider)
		}
		store = resolved
	}

	stream, err := store.Open(ctx, meta.StorageKey)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.BuildArtifact{}, nil, ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, nil, err
	}

	return meta, stream, nil
}

func (s *BuildService) ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ClaimStepIfPending(ctx, buildID, stepIndex, workerID, startedAt)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ClaimPendingStep(ctx, buildID, stepIndex, claim)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ReclaimExpiredStep(ctx, buildID, stepIndex, reclaimBefore, claim)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) RenewStepLease(ctx context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, bool, error) {
	step, outcome, err := s.buildRepo.RenewStepLease(ctx, buildID, stepIndex, claimToken, leaseExpiresAt)
	if err != nil {
		return domain.BuildStep{}, false, mapRepoErr(err)
	}

	if outcome == repository.StepCompletionCompleted {
		return step, true, nil
	}
	if outcome == repository.StepCompletionStaleClaim {
		return step, false, ErrStaleStepClaim
	}
	if outcome == repository.StepCompletionDuplicateTerminal || outcome == repository.StepCompletionInvalidTransition {
		return domain.BuildStep{}, false, ErrInvalidBuildStepTransition
	}

	return domain.BuildStep{}, false, ErrInvalidBuildStepTransition
}

func (s *BuildService) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.QueueBuildWithTemplate(ctx, id, "")
}

func (s *BuildService) QueueBuildWithTemplate(ctx context.Context, id string, template string) (domain.Build, error) {
	return s.QueueBuildWithTemplateAndCustomSteps(ctx, id, template, nil)
}

func (s *BuildService) QueueBuildWithTemplateAndCustomSteps(ctx context.Context, id string, template string, customSteps []QueueBuildCustomStepInput) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	if !domain.CanTransitionBuild(build.Status, domain.BuildStatusQueued) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	normalizedTemplate := strings.ToLower(strings.TrimSpace(template))
	if normalizedTemplate == BuildTemplateCustom {
		customTemplateSteps, customStepsErr := buildStepsForCustomTemplate(id, customSteps)
		if customStepsErr != nil {
			return domain.Build{}, customStepsErr
		}
		queuedBuild, queueErr := s.buildRepo.QueueBuild(ctx, id, customTemplateSteps)
		if queueErr != nil {
			return domain.Build{}, queueErr
		}
		if durableJobsErr := s.createDurableJobsForBuild(ctx, queuedBuild, customTemplateSteps); durableJobsErr != nil {
			log.Printf("WARNING: durable job creation failed for build_id=%s (build already persisted): %v", queuedBuild.ID, durableJobsErr)
			return domain.Build{}, fmt.Errorf("create execution jobs for build %s: %w", queuedBuild.ID, durableJobsErr)
		}
		return queuedBuild, nil
	}

	steps := buildStepsForTemplate(id, normalizedTemplate)
	queuedBuild, err := s.buildRepo.QueueBuild(ctx, id, steps)
	if err != nil {
		return domain.Build{}, err
	}
	if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
		log.Printf("WARNING: durable job creation failed for build_id=%s (build already persisted): %v", queuedBuild.ID, err)
		return domain.Build{}, fmt.Errorf("create execution jobs for build %s: %w", queuedBuild.ID, err)
	}
	return queuedBuild, nil
}

func (s *BuildService) StartBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.PrepareBuildExecution(ctx, id)
}

func (s *BuildService) CompleteBuild(ctx context.Context, id string) (domain.Build, error) {
	build, err := s.transitionBuildStatus(ctx, id, domain.BuildStatusSuccess, nil)
	if err != nil {
		return domain.Build{}, err
	}
	if tagErr := s.autoTagBuildOutputs(ctx, build); tagErr != nil {
		log.Printf("WARNING: automatic version tagging failed for build_id=%s: %v", build.ID, tagErr)
		if isGeneratedArtifactVersionConflict(tagErr) {
			return build, fmt.Errorf("automatic generated artifact version tagging failed for build %s: %w", build.ID, tagErr)
		}
	}
	return build, nil
}

func isGeneratedArtifactVersionConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, repository.ErrVersionTagConflict) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "generated version tagging failed") &&
		strings.Contains(message, repository.ErrVersionTagConflict.Error())
}

func (s *BuildService) FailBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusFailed, nil)
}

func (s *BuildService) autoTagBuildOutputs(ctx context.Context, build domain.Build) error {
	if s.versionTagger == nil || build.JobID == nil || strings.TrimSpace(*build.JobID) == "" {
		return nil
	}
	jobID := strings.TrimSpace(*build.JobID)

	artifacts := []domain.BuildArtifact{}
	if s.artifactRepo != nil {
		var artifactErr error
		artifacts, artifactErr = s.artifactRepo.ListByBuildID(ctx, build.ID)
		if artifactErr != nil {
			return artifactErr
		}
	}
	return s.autoTagDeclaredArtifactOutputs(ctx, build, jobID, artifacts)
}

type artifactVersionTagPlan struct {
	ArtifactID   string
	ArtifactName string
	LogicalPath  string
	Pattern      string
	Template     string
	Channel      string
}

func (s *BuildService) autoTagDeclaredArtifactOutputs(ctx context.Context, build domain.Build, jobID string, artifacts []domain.BuildArtifact) error {
	plans, err := s.plannedArtifactVersionTags(ctx, build, artifacts)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		version, resolveErr := versioning.ResolveArtifactVersionTemplate(plan.Template, build)
		if resolveErr != nil {
			return fmt.Errorf("generated version tagging failed for artifact path=%q name=%q declaration=%q template=%q: %w", plan.LogicalPath, plan.ArtifactName, plan.Pattern, plan.Template, resolveErr)
		}
		if _, createErr := s.versionTagger.CreateVersionTags(ctx, jobID, versiontagsvc.CreateVersionTagsInput{
			Kind:        string(domain.VersionTagKindVersion),
			Version:     version,
			ArtifactIDs: []string{plan.ArtifactID},
		}); createErr != nil {
			return fmt.Errorf("generated version tagging failed for artifact path=%q name=%q declaration=%q resolved_version=%q channel=%q: %w", plan.LogicalPath, plan.ArtifactName, plan.Pattern, version, plan.Channel, createErr)
		}
		if strings.TrimSpace(plan.Channel) == "" {
			continue
		}
		if _, createErr := s.versionTagger.CreateVersionTags(ctx, jobID, versiontagsvc.CreateVersionTagsInput{
			Kind:        string(domain.VersionTagKindChannel),
			Version:     strings.TrimSpace(plan.Channel),
			ArtifactIDs: []string{plan.ArtifactID},
		}); createErr != nil {
			return fmt.Errorf("generated channel tagging failed for artifact path=%q name=%q declaration=%q resolved_version=%q channel=%q: %w", plan.LogicalPath, plan.ArtifactName, plan.Pattern, version, plan.Channel, createErr)
		}
	}
	return nil
}

func (s *BuildService) plannedArtifactVersionTags(ctx context.Context, build domain.Build, artifacts []domain.BuildArtifact) ([]artifactVersionTagPlan, error) {
	if len(artifacts) == 0 || build.PipelineConfigYAML == nil || strings.TrimSpace(*build.PipelineConfigYAML) == "" {
		return nil, nil
	}
	buildDeclarations, err := artifactDeclarationsFromBuild(build)
	if err != nil {
		return nil, err
	}
	stepDeclarations, err := stepArtifactDeclarationsFromBuild(build)
	if err != nil {
		return nil, err
	}
	steps, err := s.buildRepo.GetStepsByBuildID(ctx, build.ID)
	if err != nil {
		return nil, err
	}
	stepIndexByID := make(map[string]int, len(steps))
	for _, step := range steps {
		stepIndexByID[step.ID] = step.StepIndex
	}

	plans := make([]artifactVersionTagPlan, 0)
	for _, item := range artifacts {
		declaration, ok := matchingArtifactDeclaration(item, stepIndexByID, buildDeclarations, stepDeclarations)
		if !ok || declaration.Version == nil || strings.TrimSpace(declaration.Version.Template) == "" {
			continue
		}
		pattern := strings.TrimSpace(declaration.Path)
		if buildDeclaration, found := firstMatchingArtifactDeclaration(item.LogicalPath, buildDeclarations); found && buildDeclaration.Version != nil && strings.TrimSpace(buildDeclaration.Version.Template) != "" {
			pattern = strings.TrimSpace(buildDeclaration.Path)
		}
		plans = append(plans, artifactVersionTagPlan{
			ArtifactID:   item.ID,
			ArtifactName: strings.TrimSpace(item.Name),
			LogicalPath:  item.LogicalPath,
			Pattern:      pattern,
			Template:     strings.TrimSpace(declaration.Version.Template),
			Channel:      strings.TrimSpace(declaration.Version.Channel),
		})
	}
	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].LogicalPath != plans[j].LogicalPath {
			return plans[i].LogicalPath < plans[j].LogicalPath
		}
		return plans[i].ArtifactID < plans[j].ArtifactID
	})
	return plans, nil
}

func matchingArtifactDeclaration(item domain.BuildArtifact, stepIndexByID map[string]int, buildDeclarations []domain.ArtifactDeclaration, stepDeclarations map[int][]domain.ArtifactDeclaration) (domain.ArtifactDeclaration, bool) {
	buildDeclaration, buildFound := firstMatchingArtifactDeclaration(item.LogicalPath, buildDeclarations)
	if item.StepID != nil {
		if stepIndex, ok := stepIndexByID[*item.StepID]; ok {
			if declaration, found := firstMatchingArtifactDeclaration(item.LogicalPath, stepDeclarations[stepIndex]); found {
				if buildFound {
					return mergeArtifactDeclarations(declaration, buildDeclaration), true
				}
				return declaration, true
			}
		}
	}
	if buildFound {
		return buildDeclaration, true
	}
	return domain.ArtifactDeclaration{}, false
}

func firstMatchingArtifactDeclaration(logicalPath string, declarations []domain.ArtifactDeclaration) (domain.ArtifactDeclaration, bool) {
	for _, declaration := range declarations {
		if artifactmatch.MatchPathPattern(declaration.Path, logicalPath) {
			return declaration, true
		}
	}
	return domain.ArtifactDeclaration{}, false
}
