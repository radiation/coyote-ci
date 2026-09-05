package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

var errKubernetesExecutionCapabilityUnavailable = errors.New("kubernetes execution capability is unavailable")

type kubernetesExecutionBoundary interface {
	GetJobByID(context.Context, string) (domain.ExecutionJob, error)
	HandleStepResult(context.Context, runner.RunStepRequest, runner.RunStepResult) (buildsvc.StepCompletionReport, error)
}

// KubernetesExecutionCapabilityError identifies execution semantics that the
// initial Kubernetes backend cannot yet preserve.
type KubernetesExecutionCapabilityError struct {
	Feature string
}

func (e *KubernetesExecutionCapabilityError) Error() string {
	return "kubernetes execution backend does not yet support " + e.Feature
}

func IsKubernetesExecutionCapabilityError(err error) bool {
	var capabilityErr *KubernetesExecutionCapabilityError
	return errors.Is(err, errKubernetesExecutionCapabilityUnavailable) || errors.As(err, &capabilityErr)
}

func (w *ExecutionWorkerService) ValidateKubernetesRunnableStep(ctx context.Context, step WorkerRunnableStep) error {
	if strings.TrimSpace(step.JobID) == "" {
		return &KubernetesExecutionCapabilityError{Feature: "legacy execution jobs"}
	}

	boundary, ok := w.builds.(kubernetesExecutionBoundary)
	if !ok {
		return errKubernetesExecutionCapabilityUnavailable
	}
	job, err := boundary.GetJobByID(ctx, step.JobID)
	if err != nil {
		return err
	}
	var spec domain.ExecutionJobSpec
	if decodeErr := json.Unmarshal([]byte(job.ResolvedSpecJSON), &spec); decodeErr != nil || spec.WorkspaceInput.Mode == "" {
		return &KubernetesExecutionCapabilityError{Feature: "an execution job without a valid workspace input plan"}
	}
	if !w.kubernetesWorkspaceLifecycleEnabled {
		if spec.WorkspaceInput.Mode != domain.WorkspaceInputModeSource {
			return &KubernetesExecutionCapabilityError{Feature: "predecessor, fan-out, or fan-in workspaces without trusted workspace helpers"}
		}
		if strings.TrimSpace(job.Source.RepositoryURL) != "" {
			return &KubernetesExecutionCapabilityError{Feature: "repository checkout without trusted workspace helpers"}
		}
	} else if spec.WorkspaceInput.Mode == domain.WorkspaceInputModeFanIn {
		return &KubernetesExecutionCapabilityError{Feature: "fan-in workspaces"}
	}

	steps, err := w.builds.GetBuildSteps(ctx, step.BuildID)
	if err != nil {
		return err
	}
	for _, buildStep := range steps {
		if buildStep.Cache != nil {
			return &KubernetesExecutionCapabilityError{Feature: "cache restore or save"}
		}
		if len(buildStep.ArtifactPaths) > 0 {
			return &KubernetesExecutionCapabilityError{Feature: "artifact collection"}
		}
	}
	if !w.kubernetesWorkspaceLifecycleEnabled && len(steps) != 1 {
		return &KubernetesExecutionCapabilityError{Feature: "multi-step pipelines without trusted workspace helpers"}
	}

	build, err := w.builds.GetBuild(ctx, step.BuildID)
	if err != nil {
		return err
	}
	if build.PipelineConfigYAML != nil && strings.TrimSpace(*build.PipelineConfigYAML) != "" {
		resolved, loadErr := pipeline.LoadAndResolve([]byte(*build.PipelineConfigYAML))
		if loadErr != nil {
			return fmt.Errorf("loading pipeline for kubernetes capability validation: %w", loadErr)
		}
		if len(resolved.Artifacts.Paths) > 0 {
			return &KubernetesExecutionCapabilityError{Feature: "artifact collection"}
		}
	}
	return nil
}

func (w *ExecutionWorkerService) SetKubernetesWorkspaceLifecycleEnabled(enabled bool) {
	w.kubernetesWorkspaceLifecycleEnabled = enabled
}

func (w *ExecutionWorkerService) RenewRunnableStepLease(ctx context.Context, step WorkerRunnableStep) (bool, error) {
	w.recordHeartbeat(ctx, true)
	return w.renewStepLease(ctx, step)
}

func (w *ExecutionWorkerService) GetExecutionJob(ctx context.Context, jobID string) (domain.ExecutionJob, error) {
	boundary, ok := w.builds.(kubernetesExecutionBoundary)
	if !ok {
		return domain.ExecutionJob{}, errKubernetesExecutionCapabilityUnavailable
	}
	return boundary.GetJobByID(ctx, jobID)
}

func (w *ExecutionWorkerService) CompleteKubernetesRunnableStep(ctx context.Context, step WorkerRunnableStep, result runner.RunStepResult) (repository.StepCompletionOutcome, error) {
	boundary, ok := w.builds.(kubernetesExecutionBoundary)
	if !ok {
		return repository.StepCompletionInvalidTransition, errKubernetesExecutionCapabilityUnavailable
	}
	report, err := boundary.HandleStepResult(ctx, runner.RunStepRequest{
		BuildID: step.BuildID, JobID: step.JobID, StepID: step.StepID, StepIndex: step.StepIndex,
		StepName: step.StepName, WorkerID: step.WorkerID, ClaimToken: step.ClaimToken, RequireAtomicExecutionCompletion: true,
	}, result)
	if err == nil && report.SideEffectErr != nil {
		err = report.SideEffectErr
	}
	return report.CompletionOutcome, err
}

func (w *ExecutionWorkerService) KubernetesLeaseDuration() time.Duration {
	return w.leaseDuration
}
