package kubernetes

import (
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestExecutionTimingExtractsKnownKubernetesPhaseBoundaries(t *testing.T) {
	claimedAt := time.Date(2026, time.September, 14, 10, 0, 0, 0, time.UTC)
	jobCreatedAt := claimedAt.Add(time.Minute)
	scheduledAt := jobCreatedAt.Add(5 * time.Minute)
	startedAt := scheduledAt.Add(time.Minute)
	finishedAt := startedAt.Add(2 * time.Minute)
	pod := &corev1.Pod{Status: corev1.PodStatus{
		Conditions:            []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(scheduledAt)}},
		InitContainerStatuses: []corev1.ContainerStatus{{Name: "workspace-prepare", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(startedAt), FinishedAt: metav1.NewTime(finishedAt)}}}},
		ContainerStatuses:     []corev1.ContainerStatus{{Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(finishedAt), FinishedAt: metav1.NewTime(finishedAt.Add(time.Minute))}}}},
	}}
	timing := executionTiming(domain.ExecutionJob{StartedAt: &claimedAt}, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(jobCreatedAt)}}, pod)
	if len(timing.Phases) != 4 || timing.Phases[0].Name != "claim" || timing.Phases[1].Name != "scheduling" || timing.Phases[2].Name != "workspace_prepare" || timing.Phases[3].Name != "command" {
		t.Fatalf("phases=%+v", timing.Phases)
	}
	if scheduling := timing.Phases[1]; scheduling.StartedAt == nil || scheduling.FinishedAt == nil || scheduling.FinishedAt.Sub(*scheduling.StartedAt) != 5*time.Minute {
		t.Fatalf("scheduling=%+v", scheduling)
	}
}

func TestExecutionTimingAllowsMissingPodStatus(t *testing.T) {
	timing := executionTiming(domain.ExecutionJob{}, &batchv1.Job{}, &corev1.Pod{})
	if len(timing.Phases) != 0 {
		t.Fatalf("timing=%+v", timing)
	}
}

func TestExecutionTimingCapturesTerminalAndPartialContainerPhases(t *testing.T) {
	createdAt := time.Date(2026, time.September, 14, 10, 0, 0, 0, time.UTC)
	completedAt := createdAt.Add(10 * time.Minute)
	runningAt := createdAt.Add(6 * time.Minute)
	timing := executionTiming(domain.ExecutionJob{}, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(createdAt)},
		Status:     batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(completedAt)}}},
	}, &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{Name: "build", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(runningAt)}}},
		{Name: "ignored"},
	}}})
	if len(timing.Phases) != 3 || timing.Phases[0].Name != "claim" || timing.Phases[1].Name != "command" || timing.Phases[2].Name != "total_execution" {
		t.Fatalf("phases=%+v", timing.Phases)
	}
	if total := timing.Phases[2]; total.StartedAt == nil || total.FinishedAt == nil || total.FinishedAt.Sub(*total.StartedAt) != 10*time.Minute {
		t.Fatalf("total=%+v", total)
	}
}

func TestExecutionTimingPreservesClaimWithoutKubernetesJob(t *testing.T) {
	claimedAt := time.Date(2026, time.September, 14, 10, 0, 0, 0, time.UTC)
	timing := executionTiming(domain.ExecutionJob{StartedAt: &claimedAt}, nil, nil)
	if len(timing.Phases) != 1 || timing.Phases[0].Name != "claim" || timing.Phases[0].StartedAt == nil || timing.Phases[0].FinishedAt != nil {
		t.Fatalf("phases=%+v", timing.Phases)
	}
}
