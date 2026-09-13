package imagebuild

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/service"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
)

type executionService interface {
	GetExecutionJob(context.Context, string) (domain.ExecutionJob, error)
	GetBuild(context.Context, string) (domain.Build, error)
	RenewRunnableStepLease(context.Context, workersvc.WorkerRunnableStep) (bool, error)
	CompleteKubernetesRunnableStep(context.Context, workersvc.WorkerRunnableStep, runner.RunStepResult) (repository.StepCompletionOutcome, error)
}

type Controller struct {
	service executionService
	records repository.ExternalImageBuildRepository
	builder service.ImageBuilder
	stager  service.ImageBuildSourceStager
	sources service.WorkspaceSourceArchivePreparer
	active  *workersvc.WorkerRunnableStep
	now     func() time.Time
}

func NewController(execution executionService, records repository.ExternalImageBuildRepository, builder service.ImageBuilder, stager service.ImageBuildSourceStager, sources service.WorkspaceSourceArchivePreparer) (*Controller, error) {
	if execution == nil || records == nil || builder == nil || stager == nil || sources == nil {
		return nil, fmt.Errorf("image build controller requires execution, record, builder, stager, and source dependencies")
	}
	return &Controller{service: execution, records: records, builder: builder, stager: stager, sources: sources, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (c *Controller) ReconcileClaimed(ctx context.Context, step workersvc.WorkerRunnableStep) (bool, error) {
	job, err := c.service.GetExecutionJob(ctx, step.JobID)
	if err != nil {
		return true, err
	}
	if job.Status == domain.ExecutionJobStatusCanceled || domain.IsTerminalExecutionJobStatus(job.Status) {
		return false, c.cancel(ctx, job.ID)
	}
	var spec domain.ExecutionJobSpec
	decodeErr := json.Unmarshal([]byte(job.ResolvedSpecJSON), &spec)
	if decodeErr != nil || spec.ExecutionKind != domain.ExecutionKindImageBuild || spec.RemoteImageBuild == nil {
		return false, c.complete(ctx, step, false, "invalid remote image build specification")
	}
	record, err := c.records.CreateIntent(ctx, domain.ExternalImageBuild{ExecutionJobID: job.ID, Provider: domain.ImageBuildProviderCloudBuild, TargetImageReference: spec.RemoteImageBuild.TargetImageReference})
	if err != nil {
		return true, err
	}
	if record.SubmissionState == domain.ExternalImageBuildSubmissionIntent {
		build, buildErr := c.service.GetBuild(ctx, job.BuildID)
		if buildErr != nil {
			return true, buildErr
		}
		payload, sourceErr := c.sources.OpenSourceArchive(ctx, build, job, spec)
		if sourceErr != nil {
			return true, sourceErr
		}
		source, stageErr := c.stager.Stage(ctx, job.ID, payload.Archive)
		closeErr := payload.Archive.Close()
		if stageErr != nil {
			return true, stageErr
		}
		if closeErr != nil {
			return true, closeErr
		}
		record.Source, record.SubmissionState = source, domain.ExternalImageBuildSubmissionStaged
		record, err = c.records.Update(ctx, record)
		if err != nil {
			return true, err
		}
	}
	if record.SubmissionState == domain.ExternalImageBuildSubmissionStaged {
		handle, found, findErr := c.builder.FindByExecutionJobID(ctx, job.ID)
		if findErr != nil {
			return true, findErr
		}
		if !found {
			handle, findErr = c.builder.Submit(ctx, domain.ImageBuildRequest{ExecutionJobID: job.ID, Source: record.Source, Spec: *spec.RemoteImageBuild, Timeout: time.Duration(spec.TimeoutSeconds) * time.Second})
			if findErr != nil {
				return true, findErr
			}
		}
		record.ExternalBuildID, record.ExternalResourceName = handle.ID, handle.ResourceName
		record.SubmissionState, record.SubmittedAt = domain.ExternalImageBuildSubmissionSubmitted, timePointer(c.now())
		record, err = c.records.Update(ctx, record)
		if err != nil {
			return true, err
		}
	}
	result, getErr := c.builder.Get(ctx, domain.ImageBuildHandle{ID: record.ExternalBuildID, ResourceName: record.ExternalResourceName})
	if getErr != nil {
		return true, getErr
	}
	record.LastProviderStatus, record.ExternalLogURL, record.FailureDetail = string(result.Status), result.ExternalLogURL, result.FailureDetail
	if !result.Status.Terminal() {
		if _, err := c.records.Update(ctx, record); err != nil {
			return true, err
		}
		_, err := c.service.RenewRunnableStepLease(ctx, step)
		return true, err
	}
	record.SubmissionState, record.TerminalResult, record.ImageDigest = domain.ExternalImageBuildSubmissionTerminal, string(result.Status), result.ImageDigest
	if _, err := c.records.Update(ctx, record); err != nil {
		return true, err
	}
	if result.Status == domain.ImageBuildStatusSuccess && strings.TrimSpace(result.ImageDigest) == "" {
		return false, c.complete(ctx, step, false, "remote image build succeeded without an immutable image digest")
	}
	return false, c.complete(ctx, step, result.Status == domain.ImageBuildStatusSuccess, result.FailureDetail)
}

func (c *Controller) cancel(ctx context.Context, jobID string) error {
	record, err := c.records.GetByExecutionJobID(ctx, jobID)
	if err != nil || strings.TrimSpace(record.ExternalBuildID) == "" {
		return nil
	}
	return c.builder.Cancel(ctx, domain.ImageBuildHandle{ID: record.ExternalBuildID, ResourceName: record.ExternalResourceName})
}

func (c *Controller) complete(ctx context.Context, step workersvc.WorkerRunnableStep, success bool, message string) error {
	status, exitCode := runner.RunStepStatusSuccess, 0
	if !success {
		status, exitCode = runner.RunStepStatusFailed, 1
	}
	_, err := c.service.CompleteKubernetesRunnableStep(ctx, step, runner.RunStepResult{Status: status, ExitCode: exitCode, Stderr: message, StartedAt: c.now(), FinishedAt: c.now()})
	return err
}

func timePointer(value time.Time) *time.Time { return &value }
