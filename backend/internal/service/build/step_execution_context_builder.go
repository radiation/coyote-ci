package build

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

// StepExecutionContext is the canonical execution plan for one step execution.
type StepExecutionContext struct {
	Build          domain.Build
	Step           *domain.BuildStep
	PersistedJob   *domain.ExecutionJob
	ExecutionImage string
	BuildSource    resolvedBuildSourceSpec

	ExecutionRequest runner.RunStepRequest
	StepWorkingDir   string
	StepCommand      string
	StepNumber       int
	TotalSteps       int

	ChunkAppender    logs.StepLogChunkAppender
	HasChunkAppender bool
}

type StepExecutionContextBuilder struct {
	service *BuildService
}

func NewStepExecutionContextBuilder(service *BuildService) *StepExecutionContextBuilder {
	return &StepExecutionContextBuilder{service: service}
}

func (b *StepExecutionContextBuilder) Build(ctx context.Context, request runner.RunStepRequest) (StepExecutionContext, error) {
	boundRequest, persistedJob, err := b.bindRequestToPersistedJob(ctx, request)
	if err != nil {
		if errors.Is(err, repository.ErrExecutionJobNotFound) {
			return StepExecutionContext{}, ErrExecutionJobNotFound
		}
		return StepExecutionContext{}, fmt.Errorf("binding request to persisted execution job: %w", err)
	}

	build, err := b.service.buildRepo.GetByID(ctx, boundRequest.BuildID)
	if err != nil {
		if errors.Is(err, repository.ErrBuildNotFound) {
			return StepExecutionContext{}, ErrBuildNotFound
		}
		return StepExecutionContext{}, fmt.Errorf("fetching build for step execution: %w", err)
	}

	steps, err := b.service.buildRepo.GetStepsByBuildID(ctx, boundRequest.BuildID)
	if err != nil {
		return StepExecutionContext{}, fmt.Errorf("fetching build steps for step execution: %w", mapRepoErr(err))
	}

	totalSteps := len(steps)
	if totalSteps <= 0 {
		totalSteps = boundRequest.StepIndex + 1
	}
	if totalSteps <= 0 {
		totalSteps = 1
	}

	stepNumber := boundRequest.StepIndex + 1
	if stepNumber <= 0 {
		stepNumber = 1
	}

	executionImage := b.service.resolveExecutionImage(build)
	buildSource := sourceSpecFromBuild(build)
	if persistedJob != nil {
		executionImage = persistedJob.Image
		buildSource = sourceSpecFromJob(*persistedJob)
	}

	// Ensure the execution request carries the resolved image for the runner.
	if strings.TrimSpace(boundRequest.Image) == "" {
		boundRequest.Image = executionImage
	}

	var chunkAppender logs.StepLogChunkAppender
	if appender, ok := b.service.logSink.(logs.StepLogChunkAppender); ok {
		chunkAppender = appender
	}

	return StepExecutionContext{
		Build:            build,
		Step:             selectExecutionStep(steps, boundRequest.StepID, boundRequest.StepIndex),
		PersistedJob:     persistedJob,
		ExecutionImage:   executionImage,
		BuildSource:      buildSource,
		ExecutionRequest: boundRequest,
		StepWorkingDir:   workspace.New(boundRequest.BuildID, "").ContainerWorkingDir(boundRequest.WorkingDir),
		StepCommand:      runner.RenderStepCommand(boundRequest.Command, boundRequest.Args),
		StepNumber:       stepNumber,
		TotalSteps:       totalSteps,
		ChunkAppender:    chunkAppender,
		HasChunkAppender: chunkAppender != nil,
	}, nil
}

func selectExecutionStep(steps []domain.BuildStep, stepID string, stepIndex int) *domain.BuildStep {
	if len(steps) == 0 {
		return nil
	}
	if strings.TrimSpace(stepID) != "" {
		for i := range steps {
			if strings.TrimSpace(steps[i].ID) == strings.TrimSpace(stepID) {
				value := steps[i]
				return &value
			}
		}
	}
	for i := range steps {
		if steps[i].StepIndex == stepIndex {
			value := steps[i]
			return &value
		}
	}
	return nil
}

func (b *StepExecutionContextBuilder) bindRequestToPersistedJob(ctx context.Context, request runner.RunStepRequest) (runner.RunStepRequest, *domain.ExecutionJob, error) {
	if b.service.executionJobRepo == nil {
		return request, nil, nil
	}

	var (
		job domain.ExecutionJob
		err error
	)

	jobID := strings.TrimSpace(request.JobID)
	if jobID != "" {
		job, err = b.service.executionJobRepo.GetJobByID(ctx, jobID)
		if err != nil {
			return request, nil, err
		}
	} else if strings.TrimSpace(request.StepID) != "" {
		job, err = b.service.executionJobRepo.GetJobByStepID(ctx, request.StepID)
		if err != nil {
			return request, nil, nil
		}
	}

	request.JobID = job.ID
	request.StepID = defaultString(request.StepID, job.StepID)
	request.StepIndex = job.StepIndex
	request.StepName = defaultString(job.Name, request.StepName)
	request.Image = job.Image
	if len(job.Command) > 0 {
		request.Command = defaultString(job.Command[0], "sh")
		if len(job.Command) > 1 {
			request.Args = append([]string(nil), job.Command[1:]...)
		} else {
			request.Args = []string{}
		}
	}
	request.Env = cloneEnv(job.Environment)
	request.WorkingDir = defaultString(job.WorkingDir, ".")
	if job.TimeoutSeconds != nil {
		request.TimeoutSeconds = maxInt(*job.TimeoutSeconds, 0)
	}

	if job.ClaimToken != nil && strings.TrimSpace(request.ClaimToken) == "" {
		request.ClaimToken = *job.ClaimToken
	}

	return request, &job, nil
}

func sourceSpecFromJob(job domain.ExecutionJob) resolvedBuildSourceSpec {
	return resolvedBuildSourceSpec{
		HasSource:     strings.TrimSpace(job.Source.RepositoryURL) != "",
		RepositoryURL: strings.TrimSpace(job.Source.RepositoryURL),
		Ref:           optionalStringValue(job.Source.RefName),
		CommitSHA:     strings.TrimSpace(job.Source.CommitSHA),
	}
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
