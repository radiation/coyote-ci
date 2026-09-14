package kubernetes

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func executionTiming(job domain.ExecutionJob, kubernetesJob *batchv1.Job, pod *corev1.Pod) domain.ExecutionTiming {
	phases := make([]domain.ExecutionPhaseTiming, 0, 10)
	var jobCreatedAt *time.Time
	var jobCompletedAt *time.Time
	if kubernetesJob != nil {
		jobCreatedAt = optionalTime(kubernetesJob.CreationTimestamp.Time)
		if job.StartedAt != nil || jobCreatedAt != nil {
			phases = append(phases, domain.ExecutionPhaseTiming{Name: "claim", StartedAt: job.StartedAt, FinishedAt: jobCreatedAt})
		}
		for _, condition := range kubernetesJob.Status.Conditions {
			if condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed {
				jobCompletedAt = optionalTime(condition.LastTransitionTime.Time)
				break
			}
		}
	} else if job.StartedAt != nil {
		phases = append(phases, domain.ExecutionPhaseTiming{Name: "claim", StartedAt: job.StartedAt})
	}
	if pod == nil {
		if jobCreatedAt != nil && jobCompletedAt != nil {
			phases = append(phases, domain.ExecutionPhaseTiming{Name: "total_execution", StartedAt: jobCreatedAt, FinishedAt: jobCompletedAt})
		}
		return domain.ExecutionTiming{Phases: phases}
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionTrue {
			if jobCreatedAt != nil {
				phases = append(phases, domain.ExecutionPhaseTiming{Name: "scheduling", StartedAt: jobCreatedAt, FinishedAt: optionalTime(condition.LastTransitionTime.Time)})
			}
			break
		}
	}
	phases = append(phases, containerPhases(pod.Status.InitContainerStatuses)...)
	phases = append(phases, containerPhases(pod.Status.ContainerStatuses)...)
	if jobCreatedAt != nil && jobCompletedAt != nil {
		phases = append(phases, domain.ExecutionPhaseTiming{Name: "total_execution", StartedAt: jobCreatedAt, FinishedAt: jobCompletedAt})
	}
	return domain.ExecutionTiming{Phases: phases}
}

func containerPhases(statuses []corev1.ContainerStatus) []domain.ExecutionPhaseTiming {
	phases := make([]domain.ExecutionPhaseTiming, 0, len(statuses))
	for _, status := range statuses {
		var startedAt, finishedAt *time.Time
		if status.State.Running != nil {
			startedAt = optionalTime(status.State.Running.StartedAt.Time)
		}
		if status.State.Terminated != nil {
			startedAt = optionalTime(status.State.Terminated.StartedAt.Time)
			finishedAt = optionalTime(status.State.Terminated.FinishedAt.Time)
		}
		if startedAt == nil && finishedAt == nil {
			continue
		}
		phases = append(phases, domain.ExecutionPhaseTiming{Name: phaseNameForContainer(status.Name), StartedAt: startedAt, FinishedAt: finishedAt})
	}
	return phases
}

func phaseNameForContainer(name string) string {
	switch name {
	case "command-timeout-install":
		return "command_timeout_tool_install"
	case "workspace-prepare":
		return "workspace_prepare"
	case "cache-restore":
		return "cache_restore"
	case "build":
		return "command"
	case "workspace-publish":
		return "workspace_publish"
	case "artifact-collect":
		return "artifact_collect"
	case "cache-save":
		return "cache_save"
	default:
		return name
	}
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copyValue := value.UTC()
	return &copyValue
}
