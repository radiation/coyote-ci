package build

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type RetryJobResult struct {
	Build domain.Build
	Job   domain.ExecutionJob
}

func (s *BuildService) RetryJob(ctx context.Context, jobID string) (RetryJobResult, error) {
	if s.executionJobRepo == nil {
		return RetryJobResult{}, ErrExecutionJobRepoNotConfigured
	}

	failedJob, err := s.executionJobRepo.GetJobByID(ctx, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrExecutionJobNotFound) {
			return RetryJobResult{}, ErrExecutionJobNotFound
		}
		return RetryJobResult{}, err
	}
	if failedJob.Status != domain.ExecutionJobStatusFailed {
		return RetryJobResult{}, ErrExecutionJobNotRetryable
	}

	sourceBuild, err := s.buildRepo.GetByID(ctx, failedJob.BuildID)
	if err != nil {
		return RetryJobResult{}, mapRepoErr(err)
	}
	sourceSteps, err := s.buildRepo.GetStepsByBuildID(ctx, failedJob.BuildID)
	if err != nil {
		return RetryJobResult{}, mapRepoErr(err)
	}

	var sourceStep *domain.BuildStep
	for i := range sourceSteps {
		if sourceSteps[i].StepIndex == failedJob.StepIndex {
			step := sourceSteps[i]
			sourceStep = &step
			break
		}
	}
	if sourceStep == nil {
		return RetryJobResult{}, ErrInvalidRerunStepIndex
	}

	now := time.Now().UTC()
	rerunFrom := failedJob.StepIndex
	newBuild := buildAttemptFromSource(sourceBuild, now, &rerunFrom)
	newStep := cloneStepForAttempt(newBuild.ID, *sourceStep, 0)
	createdBuild, err := s.buildRepo.CreateQueuedBuild(ctx, newBuild, []domain.BuildStep{newStep})
	if err != nil {
		return RetryJobResult{}, err
	}

	newJob := cloneRetryJobAttempt(failedJob, createdBuild.ID, newStep, now)
	createdJobs, err := s.executionJobRepo.CreateJobsForBuild(ctx, []domain.ExecutionJob{newJob})
	if err != nil {
		return RetryJobResult{}, err
	}
	if len(createdJobs) == 0 {
		return RetryJobResult{}, repository.ErrExecutionJobNotFound
	}

	if cloneErr := s.cloneDeclaredOutputsForAttempts(ctx, map[string]string{failedJob.ID: createdJobs[0].ID}, createdBuild.ID); cloneErr != nil {
		return RetryJobResult{}, cloneErr
	}

	return RetryJobResult{Build: createdBuild, Job: createdJobs[0]}, nil
}

func (s *BuildService) RerunBuildFromStep(ctx context.Context, buildID string, stepIndex int) (domain.Build, error) {
	if s.executionJobRepo == nil {
		return domain.Build{}, ErrExecutionJobRepoNotConfigured
	}

	sourceBuild, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}
	sourceSteps, err := s.buildRepo.GetStepsByBuildID(ctx, buildID)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	selectedSteps := make([]domain.BuildStep, 0)
	for _, step := range sourceSteps {
		if step.StepIndex >= stepIndex {
			selectedSteps = append(selectedSteps, step)
		}
	}
	if len(selectedSteps) == 0 {
		return domain.Build{}, ErrInvalidRerunStepIndex
	}
	sort.Slice(selectedSteps, func(i, j int) bool {
		return selectedSteps[i].StepIndex < selectedSteps[j].StepIndex
	})

	now := time.Now().UTC()
	rerunFrom := stepIndex
	newBuild := buildAttemptFromSource(sourceBuild, now, &rerunFrom)

	newSteps := make([]domain.BuildStep, 0, len(selectedSteps))
	originalStepByNewIndex := make(map[int]int, len(selectedSteps))
	for idx, sourceStep := range selectedSteps {
		cloned := cloneStepForAttempt(newBuild.ID, sourceStep, idx)
		newSteps = append(newSteps, cloned)
		originalStepByNewIndex[idx] = sourceStep.StepIndex
	}

	createdBuild, err := s.buildRepo.CreateQueuedBuild(ctx, newBuild, newSteps)
	if err != nil {
		return domain.Build{}, err
	}

	sourceJobs, err := s.executionJobRepo.GetJobsByBuildID(ctx, sourceBuild.ID)
	if err != nil {
		return domain.Build{}, err
	}
	latestJobByStep := latestJobsByStepIndex(sourceJobs)

	jobsToCreate := make([]domain.ExecutionJob, 0, len(newSteps))
	sourceToCreatedJobID := map[string]string{}
	for _, step := range newSteps {
		originalIdx := originalStepByNewIndex[step.StepIndex]
		if sourceJob, ok := latestJobByStep[originalIdx]; ok {
			cloned := cloneJobAttemptFromPrior(sourceJob, createdBuild.ID, step, now)
			jobsToCreate = append(jobsToCreate, cloned)
			sourceToCreatedJobID[sourceJob.ID] = cloned.ID
			continue
		}

		fallback := fallbackRerunJobFromStep(createdBuild, step, s.resolveExecutionImage(sourceBuild), now)
		jobsToCreate = append(jobsToCreate, fallback)
	}

	_, createJobsErr := s.executionJobRepo.CreateJobsForBuild(ctx, jobsToCreate)
	if createJobsErr != nil {
		return domain.Build{}, createJobsErr
	}

	if cloneErr := s.cloneDeclaredOutputsForAttempts(ctx, sourceToCreatedJobID, createdBuild.ID); cloneErr != nil {
		return domain.Build{}, cloneErr
	}

	return createdBuild, nil
}

func (s *BuildService) RerunBuild(ctx context.Context, buildID string) (domain.Build, error) {
	sourceBuild, err := s.buildRepo.GetByID(ctx, buildID)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}
	sourceSteps, err := s.buildRepo.GetStepsByBuildID(ctx, buildID)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}
	validateErr := validateBuildRerunContext(sourceBuild, sourceSteps)
	if validateErr != nil {
		return domain.Build{}, validateErr
	}

	sort.Slice(sourceSteps, func(left, right int) bool {
		return sourceSteps[left].StepIndex < sourceSteps[right].StepIndex
	})

	now := time.Now().UTC()
	newBuild := buildAttemptFromSource(sourceBuild, now, nil)
	newSteps := make([]domain.BuildStep, 0, len(sourceSteps))
	for index, sourceStep := range sourceSteps {
		newSteps = append(newSteps, cloneStepForAttempt(newBuild.ID, sourceStep, index))
	}

	createdBuild, err := s.buildRepo.CreateQueuedBuild(ctx, newBuild, newSteps)
	if err != nil {
		return domain.Build{}, err
	}
	createJobsErr := s.createDurableJobsForBuild(ctx, createdBuild, newSteps)
	if createJobsErr != nil {
		return domain.Build{}, createJobsErr
	}

	return createdBuild, nil
}

func validateBuildRerunContext(sourceBuild domain.Build, sourceSteps []domain.BuildStep) error {
	if strings.TrimSpace(sourceBuild.ID) == "" || strings.TrimSpace(sourceBuild.ProjectID) == "" {
		return ErrBuildRerunUnavailable
	}
	if !domain.IsTerminalBuildStatus(sourceBuild.Status) {
		return ErrInvalidBuildStatusTransition
	}
	if len(sourceSteps) == 0 {
		return ErrBuildRerunUnavailable
	}
	return nil
}

func (s *BuildService) RerunBuildFromJob(ctx context.Context, jobID string) (domain.Build, error) {
	if s.executionJobRepo == nil {
		return domain.Build{}, ErrExecutionJobRepoNotConfigured
	}
	job, err := s.executionJobRepo.GetJobByID(ctx, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrExecutionJobNotFound) {
			return domain.Build{}, ErrExecutionJobNotFound
		}
		return domain.Build{}, err
	}
	return s.RerunBuildFromStep(ctx, job.BuildID, job.StepIndex)
}

func buildAttemptFromSource(source domain.Build, now time.Time, rerunFrom *int) domain.Build {
	attempt := maxInt(source.AttemptNumber, 1) + 1
	buildID := uuid.NewString()
	queuedAt := now
	build := domain.Build{
		ID:                    buildID,
		ProjectID:             source.ProjectID,
		JobID:                 cloneStringPtr(source.JobID),
		Priority:              domain.NormalizePriority(source.Priority),
		Status:                domain.BuildStatusQueued,
		CreatedAt:             now,
		QueuedAt:              &queuedAt,
		CurrentStepIndex:      0,
		AttemptNumber:         attempt,
		RerunOfBuildID:        &source.ID,
		RerunFromStepIdx:      rerunFrom,
		PipelineConfigYAML:    cloneStringPtr(source.PipelineConfigYAML),
		PipelineName:          cloneStringPtr(source.PipelineName),
		PipelineSource:        cloneStringPtr(source.PipelineSource),
		PipelinePath:          cloneStringPtr(source.PipelinePath),
		Source:                cloneSourceSpec(source.Source),
		RepoURL:               cloneStringPtr(source.RepoURL),
		Ref:                   cloneStringPtr(source.Ref),
		CommitSHA:             cloneStringPtr(source.CommitSHA),
		SourceRef:             cloneStringPtr(source.SourceRef),
		SourceSHA:             cloneStringPtr(source.SourceSHA),
		TriggerType:           domain.BuildTriggerTypeRerun,
		Trigger:               cloneBuildTrigger(source.Trigger),
		RequestedImageRef:     cloneStringPtr(source.RequestedImageRef),
		ResolvedImageRef:      cloneStringPtr(source.ResolvedImageRef),
		ImageSourceKind:       source.ImageSourceKind,
		ManagedImageID:        cloneStringPtr(source.ManagedImageID),
		ManagedImageVersionID: cloneStringPtr(source.ManagedImageVersionID),
	}
	return domain.NormalizeBuildMetadata(build)
}

func cloneBuildTrigger(source domain.BuildTrigger) domain.BuildTrigger {
	normalized := domain.NormalizeBuildTrigger(source)
	return domain.BuildTrigger{
		Kind:            normalized.Kind,
		SCMProvider:     cloneStringPtr(normalized.SCMProvider),
		EventType:       cloneStringPtr(normalized.EventType),
		RepositoryOwner: cloneStringPtr(normalized.RepositoryOwner),
		RepositoryName:  cloneStringPtr(normalized.RepositoryName),
		RepositoryURL:   cloneStringPtr(normalized.RepositoryURL),
		RawRef:          cloneStringPtr(normalized.RawRef),
		Ref:             cloneStringPtr(normalized.Ref),
		RefType:         cloneStringPtr(normalized.RefType),
		RefName:         cloneStringPtr(normalized.RefName),
		Deleted:         cloneBoolPtr(normalized.Deleted),
		CommitSHA:       cloneStringPtr(normalized.CommitSHA),
		DeliveryID:      cloneStringPtr(normalized.DeliveryID),
		Actor:           cloneStringPtr(normalized.Actor),
	}
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneSourceSpec(spec *domain.SourceSpec) *domain.SourceSpec {
	if spec == nil {
		return nil
	}
	return domain.NewSourceSpec(spec.RepositoryURL, optionalStringValue(spec.Ref), optionalStringValue(spec.CommitSHA))
}

func cloneStepForAttempt(buildID string, sourceStep domain.BuildStep, newStepIndex int) domain.BuildStep {
	return domain.BuildStep{
		ID:                    uuid.NewString(),
		BuildID:               buildID,
		StepIndex:             newStepIndex,
		NodeID:                defaultValue(sourceStep.NodeID, domain.FallbackNodeID(newStepIndex)),
		GroupName:             cloneStringPtr(sourceStep.GroupName),
		DependsOnNodes:        append([]string(nil), sourceStep.DependsOnNodes...),
		Name:                  sourceStep.Name,
		Image:                 sourceStep.Image,
		Command:               sourceStep.Command,
		Args:                  append([]string(nil), sourceStep.Args...),
		Env:                   cloneEnv(sourceStep.Env),
		WorkingDir:            defaultValue(sourceStep.WorkingDir, "."),
		TimeoutSeconds:        maxInt(sourceStep.TimeoutSeconds, 0),
		ArtifactPaths:         append([]string(nil), sourceStep.ArtifactPaths...),
		Cache:                 sourceStep.Cache.Clone(),
		Status:                domain.BuildStepStatusPending,
		RequestedImageRef:     cloneStringPtr(sourceStep.RequestedImageRef),
		ResolvedImageRef:      cloneStringPtr(sourceStep.ResolvedImageRef),
		ImageSourceKind:       sourceStep.ImageSourceKind,
		ManagedImageID:        cloneStringPtr(sourceStep.ManagedImageID),
		ManagedImageVersionID: cloneStringPtr(sourceStep.ManagedImageVersionID),
	}
}

func cloneRetryJobAttempt(prior domain.ExecutionJob, newBuildID string, newStep domain.BuildStep, now time.Time) domain.ExecutionJob {
	return cloneJobAttempt(prior, newBuildID, newStep, maxInt(prior.AttemptNumber, 1)+1, &prior.ID, lineageRootFromPrior(prior), now)
}

func cloneJobAttemptFromPrior(prior domain.ExecutionJob, newBuildID string, newStep domain.BuildStep, now time.Time) domain.ExecutionJob {
	return cloneJobAttempt(prior, newBuildID, newStep, maxInt(prior.AttemptNumber, 1)+1, &prior.ID, lineageRootFromPrior(prior), now)
}

func lineageRootFromPrior(prior domain.ExecutionJob) *string {
	if prior.LineageRootJobID != nil && *prior.LineageRootJobID != "" {
		value := *prior.LineageRootJobID
		return &value
	}
	value := prior.ID
	return &value
}

func cloneJobAttempt(prior domain.ExecutionJob, newBuildID string, newStep domain.BuildStep, attemptNumber int, retryOfJobID *string, lineageRootJobID *string, now time.Time) domain.ExecutionJob {
	cloned := domain.ExecutionJob{
		ID:               uuid.NewString(),
		BuildID:          newBuildID,
		StepID:           newStep.ID,
		Name:             prior.Name,
		StepIndex:        newStep.StepIndex,
		AttemptNumber:    maxInt(attemptNumber, 1),
		RetryOfJobID:     retryOfJobID,
		LineageRootJobID: lineageRootJobID,
		Status:           domain.ExecutionJobStatusQueued,
		QueueName:        prior.QueueName,
		Image:            prior.Image,
		WorkingDir:       defaultValue(prior.WorkingDir, "."),
		Command:          append([]string(nil), prior.Command...),
		Environment:      cloneEnv(prior.Environment),
		TimeoutSeconds:   prior.TimeoutSeconds,
		PipelineFilePath: prior.PipelineFilePath,
		ContextDir:       prior.ContextDir,
		Source:           prior.Source,
		SpecVersion:      prior.SpecVersion,
		SpecDigest:       prior.SpecDigest,
		ResolvedSpecJSON: prior.ResolvedSpecJSON,
		CreatedAt:        now,
		OutputRefs:       []domain.ArtifactRef{},
	}
	if cloned.Name == "" {
		cloned.Name = newStep.Name
	}
	if cloned.SpecDigest == nil {
		cloned.SpecDigest = domain.BuildSpecDigest(cloned.ResolvedSpecJSON)
	}
	return cloned
}

func fallbackRerunJobFromStep(build domain.Build, step domain.BuildStep, image string, now time.Time) domain.ExecutionJob {
	timeout := step.TimeoutSeconds
	command := append([]string{defaultValue(step.Command, "sh")}, append([]string(nil), step.Args...)...)
	sourceRef := plannerSourceRef(build.Source)
	source := domain.SourceSnapshotRef{
		RepositoryURL: plannerSourceRepositoryURL(build.Source, build.RepoURL),
		CommitSHA:     plannerSourceCommitSHA(build.Source, build.CommitSHA),
		RefName:       sourceRef,
	}
	spec := domain.ExecutionJobSpec{
		Version:          1,
		Image:            strings.TrimSpace(image),
		WorkingDir:       defaultValue(step.WorkingDir, "."),
		Command:          command,
		Environment:      cloneEnv(step.Env),
		TimeoutSeconds:   maxInt(timeout, 0),
		PipelineFilePath: optionalValue(build.PipelinePath),
		ContextDir:       plannerContextDirFromPipelinePath(build.PipelinePath),
		Source:           source,
	}
	specJSON, _ := spec.ToJSON()
	jobID := uuid.NewString()
	return domain.ExecutionJob{
		ID:               jobID,
		BuildID:          build.ID,
		StepID:           step.ID,
		Name:             step.Name,
		StepIndex:        step.StepIndex,
		AttemptNumber:    1,
		LineageRootJobID: &jobID,
		Status:           domain.ExecutionJobStatusQueued,
		Image:            spec.Image,
		WorkingDir:       spec.WorkingDir,
		Command:          command,
		Environment:      spec.Environment,
		TimeoutSeconds:   &timeout,
		PipelineFilePath: optionalPointer(spec.PipelineFilePath),
		ContextDir:       optionalPointer(spec.ContextDir),
		Source:           source,
		SpecVersion:      1,
		SpecDigest:       domain.BuildSpecDigest(specJSON),
		ResolvedSpecJSON: specJSON,
		CreatedAt:        now,
		OutputRefs:       []domain.ArtifactRef{},
	}
}

func latestJobsByStepIndex(jobs []domain.ExecutionJob) map[int]domain.ExecutionJob {
	out := map[int]domain.ExecutionJob{}
	for _, job := range jobs {
		current, exists := out[job.StepIndex]
		if !exists || job.AttemptNumber > current.AttemptNumber || (job.AttemptNumber == current.AttemptNumber && job.CreatedAt.After(current.CreatedAt)) {
			out[job.StepIndex] = job
		}
	}
	return out
}

func (s *BuildService) cloneDeclaredOutputsForAttempts(ctx context.Context, sourceToCreatedJobID map[string]string, newBuildID string) error {
	if s.executionOutputRepo == nil || len(sourceToCreatedJobID) == 0 {
		return nil
	}

	clonedOutputs := make([]domain.ExecutionJobOutput, 0)
	for sourceJobID, createdJobID := range sourceToCreatedJobID {
		sourceOutputs, err := s.executionOutputRepo.ListByJobID(ctx, sourceJobID)
		if err != nil {
			return err
		}
		for _, output := range sourceOutputs {
			clonedOutputs = append(clonedOutputs, domain.ExecutionJobOutput{
				ID:           uuid.NewString(),
				JobID:        createdJobID,
				BuildID:      newBuildID,
				Name:         output.Name,
				Kind:         output.Kind,
				DeclaredPath: output.DeclaredPath,
				Status:       domain.ExecutionJobOutputStatusDeclared,
				CreatedAt:    time.Now().UTC(),
			})
		}
	}
	if len(clonedOutputs) == 0 {
		return nil
	}
	_, err := s.executionOutputRepo.CreateMany(ctx, clonedOutputs)
	return err
}
