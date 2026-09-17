package imagebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"strings"
	"time"

	artifactpkg "github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/service"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
)

type executionService interface {
	GetExecutionJob(context.Context, string) (domain.ExecutionJob, error)
	GetBuild(context.Context, string) (domain.Build, error)
	GetBuildSteps(context.Context, string) ([]domain.BuildStep, error)
	RenewRunnableStepLease(context.Context, workersvc.WorkerRunnableStep) (bool, error)
	UpdateRunnableStepTiming(context.Context, workersvc.WorkerRunnableStep, domain.ExecutionTiming) (bool, error)
	CompleteKubernetesRunnableStep(context.Context, workersvc.WorkerRunnableStep, runner.RunStepResult) (repository.StepCompletionOutcome, error)
}

type Controller struct {
	service        executionService
	records        repository.ExternalImageBuildRepository
	builder        service.ImageBuilder
	stager         service.ImageBuildSourceStager
	sources        service.WorkspaceSourceArchivePreparer
	artifacts      repository.ArtifactRepository
	artifactStores *artifactpkg.StoreResolver
	active         *workersvc.WorkerRunnableStep
	now            func() time.Time
}

func (c *Controller) WithArtifactInputs(artifacts repository.ArtifactRepository, stores *artifactpkg.StoreResolver) *Controller {
	c.artifacts, c.artifactStores = artifacts, stores
	return c
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
		return false, c.complete(ctx, step, false, "invalid remote image build specification", nil)
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
		inputs, inputErr := c.resolveArtifactInputs(ctx, job, *spec.RemoteImageBuild)
		if inputErr != nil {
			return false, c.complete(ctx, step, false, inputErr.Error(), nil)
		}
		payload, sourceErr := c.sources.OpenSourceArchive(ctx, build, job, spec)
		if sourceErr != nil {
			return true, sourceErr
		}
		source, stageErr := c.stager.Stage(ctx, job.ID, payload.Archive, spec.RemoteImageBuild.ContextPath, inputs)
		closeErr := payload.Archive.Close()
		if stageErr != nil {
			return true, stageErr
		}
		if closeErr != nil {
			return true, closeErr
		}
		record.Source, record.ConsumedArtifacts, record.SubmissionState = source, imageBuildArtifacts(inputs), domain.ExternalImageBuildSubmissionStaged
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
			handle, findErr = c.builder.Submit(ctx, domain.ImageBuildRequest{ExecutionJobID: job.ID, Source: record.Source, Spec: *spec.RemoteImageBuild, Timeout: time.Duration(spec.TimeoutSeconds) * time.Second, Artifacts: record.ConsumedArtifacts})
			if findErr != nil {
				if !isRetryableSubmissionError(findErr) {
					return false, c.complete(ctx, step, false, submissionFailureMessage(findErr), nil)
				}
				continued, renewErr := c.service.RenewRunnableStepLease(ctx, step)
				if renewErr != nil {
					return true, errors.Join(findErr, renewErr)
				}
				if !continued {
					return false, nil
				}
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
	if result.Timing != nil {
		if _, timingErr := c.service.UpdateRunnableStepTiming(ctx, step, *result.Timing); timingErr != nil {
			log.Printf("DEBUG remote image build timing update failed execution_job_id=%s: %v", step.JobID, timingErr)
		}
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
		return false, c.complete(ctx, step, false, "remote image build succeeded without an immutable image digest", result.Timing)
	}
	return false, c.complete(ctx, step, result.Status == domain.ImageBuildStatusSuccess, result.FailureDetail, result.Timing)
}

func (c *Controller) cancel(ctx context.Context, jobID string) error {
	record, err := c.records.GetByExecutionJobID(ctx, jobID)
	if err != nil || strings.TrimSpace(record.ExternalBuildID) == "" {
		return nil
	}
	return c.builder.Cancel(ctx, domain.ImageBuildHandle{ID: record.ExternalBuildID, ResourceName: record.ExternalResourceName})
}

func (c *Controller) complete(ctx context.Context, step workersvc.WorkerRunnableStep, success bool, message string, timing *domain.ExecutionTiming) error {
	status, exitCode := runner.RunStepStatusSuccess, 0
	if !success {
		status, exitCode = runner.RunStepStatusFailed, 1
	}
	startedAt, finishedAt := c.now(), c.now()
	if timing != nil {
		for _, phase := range timing.Phases {
			if phase.Name == "command" && phase.StartedAt != nil && phase.FinishedAt != nil {
				startedAt, finishedAt = *phase.StartedAt, *phase.FinishedAt
				break
			}
		}
	}
	_, err := c.service.CompleteKubernetesRunnableStep(ctx, step, runner.RunStepResult{Status: status, ExitCode: exitCode, Stderr: message, StartedAt: startedAt, FinishedAt: finishedAt})
	return err
}

func (c *Controller) resolveArtifactInputs(ctx context.Context, job domain.ExecutionJob, spec domain.RemoteImageBuildSpec) ([]service.ImageBuildContextArtifact, error) {
	if len(spec.ArtifactInputs) == 0 {
		return nil, nil
	}
	if c.artifacts == nil || c.artifactStores == nil {
		return nil, errors.New("image build artifact inputs require artifact metadata and storage")
	}
	steps, err := c.service.GetBuildSteps(ctx, job.BuildID)
	if err != nil {
		return nil, fmt.Errorf("loading image build steps: %w", err)
	}
	allowed := make(map[string]struct{}, len(job.DependsOnNodeIDs))
	for _, nodeID := range job.DependsOnNodeIDs {
		allowed[nodeID] = struct{}{}
	}
	producerNodes := map[string]string{}
	for _, step := range steps {
		producerNodes[step.ID] = step.NodeID
	}
	artifacts, err := c.artifacts.ListByBuildID(ctx, job.BuildID)
	if err != nil {
		return nil, fmt.Errorf("loading image build artifacts: %w", err)
	}
	resolved := make([]service.ImageBuildContextArtifact, 0, len(spec.ArtifactInputs))
	for _, input := range spec.ArtifactInputs {
		var match *domain.BuildArtifact
		for index := range artifacts {
			candidate := &artifacts[index]
			if candidate.Name == input.Name && candidate.StepID != nil {
				if _, ok := allowed[producerNodes[*candidate.StepID]]; ok {
					if match != nil {
						return nil, fmt.Errorf("image build artifact %q is ambiguous", input.Name)
					}
					match = candidate
				}
			}
		}
		if match == nil {
			return nil, fmt.Errorf("image build artifact %q is not available from an upstream dependency", input.Name)
		}
		if strings.TrimSpace(match.TargetPlatform) != strings.TrimSpace(input.Platform) {
			return nil, fmt.Errorf("image build artifact %q platform %q does not match required platform %q", input.Name, match.TargetPlatform, input.Platform)
		}
		if match.ChecksumSHA256 == nil || len(*match.ChecksumSHA256) != 64 {
			return nil, fmt.Errorf("image build artifact %q has no valid checksum", input.Name)
		}
		store, storeErr := c.artifactStores.Resolve(match.StorageProvider)
		if storeErr != nil {
			return nil, fmt.Errorf("resolving image build artifact %q storage provider %q: %w", input.Name, match.StorageProvider, storeErr)
		}
		reader, openErr := store.Open(ctx, match.StorageKey)
		if openErr != nil {
			return nil, fmt.Errorf("opening image build artifact %q: %w", input.Name, openErr)
		}
		resolved = append(resolved, service.ImageBuildContextArtifact{Artifact: domain.ImageBuildArtifact{ID: match.ID, Name: match.Name, StorageKey: match.StorageKey, ChecksumSHA256: *match.ChecksumSHA256, SizeBytes: match.SizeBytes, TargetPlatform: match.TargetPlatform, Destination: input.Destination}, Source: &checksumReader{ReadCloser: reader, hash: sha256.New(), expected: *match.ChecksumSHA256, size: match.SizeBytes}})
	}
	return resolved, nil
}

func imageBuildArtifacts(inputs []service.ImageBuildContextArtifact) []domain.ImageBuildArtifact {
	out := make([]domain.ImageBuildArtifact, len(inputs))
	for i := range inputs {
		out[i] = inputs[i].Artifact
	}
	return out
}

type checksumReader struct {
	io.ReadCloser
	hash       hash.Hash
	expected   string
	size, read int64
}

func (r *checksumReader) Read(buffer []byte) (int, error) {
	count, err := r.ReadCloser.Read(buffer)
	if count > 0 {
		_, _ = r.hash.Write(buffer[:count])
		r.read += int64(count)
	}
	if err == io.EOF && (r.read != r.size || !strings.EqualFold(hex.EncodeToString(r.hash.Sum(nil)), r.expected)) {
		return count, errors.New("image build artifact checksum or size mismatch")
	}
	return count, err
}

type retryableSubmissionError interface {
	Retryable() bool
}

func isRetryableSubmissionError(err error) bool {
	var retryableErr retryableSubmissionError
	return errors.As(err, &retryableErr) && retryableErr.Retryable()
}

func submissionFailureMessage(err error) string {
	return "remote image build submission failed: " + err.Error()
}

func timePointer(value time.Time) *time.Time { return &value }
