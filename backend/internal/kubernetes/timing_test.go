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

func TestExecutionTimingClampsPostBuildHelperPhasesToBuildCompletion(t *testing.T) {
	buildStartedAt := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	buildFinishedAt := buildStartedAt.Add(4*time.Minute + 8*time.Second)
	helperFinishedAt := buildFinishedAt.Add(time.Second)
	for _, helperName := range []string{"artifact-collect", "cache-save", "workspace-publish"} {
		t.Run(helperName, func(t *testing.T) {
			timing := executionTiming(domain.ExecutionJob{}, nil, &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
				{Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(buildStartedAt), FinishedAt: metav1.NewTime(buildFinishedAt)}}},
				{Name: helperName, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(buildStartedAt), FinishedAt: metav1.NewTime(helperFinishedAt)}}},
			}}})

			buildPhase := executionTimingPhase(t, timing, "command")
			if buildPhase.StartedAt == nil || buildPhase.FinishedAt == nil || !buildPhase.StartedAt.Equal(buildStartedAt) || !buildPhase.FinishedAt.Equal(buildFinishedAt) {
				t.Fatalf("build phase=%+v", buildPhase)
			}
			helperPhase := executionTimingPhase(t, timing, phaseNameForContainer(helperName))
			if helperPhase.StartedAt == nil || helperPhase.FinishedAt == nil || !helperPhase.StartedAt.Equal(buildFinishedAt) || !helperPhase.FinishedAt.Equal(helperFinishedAt) {
				t.Fatalf("helper phase=%+v", helperPhase)
			}
			if helperPhase.FinishedAt.Sub(*helperPhase.StartedAt) != time.Second {
				t.Fatalf("helper duration=%s", helperPhase.FinishedAt.Sub(*helperPhase.StartedAt))
			}
		})
	}
}

func TestExecutionTimingPreservesContainerTimestampsWhenBuildCompletionCannotSafelyClamp(t *testing.T) {
	startedAt := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	beforeHelperFinishedAt := startedAt.Add(2 * time.Minute)
	tests := []struct {
		name              string
		containerStatuses []corev1.ContainerStatus
	}{
		{
			name: "build still running",
			containerStatuses: []corev1.ContainerStatus{
				{Name: "build", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(startedAt)}}},
				{Name: "artifact-collect", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(startedAt), FinishedAt: metav1.NewTime(finishedAt)}}},
			},
		},
		{
			name: "helper finishes before build",
			containerStatuses: []corev1.ContainerStatus{
				{Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(startedAt), FinishedAt: metav1.NewTime(beforeHelperFinishedAt)}}},
				{Name: "artifact-collect", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(startedAt), FinishedAt: metav1.NewTime(finishedAt)}}},
			},
		},
		{
			name: "build status absent",
			containerStatuses: []corev1.ContainerStatus{
				{Name: "artifact-collect", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(startedAt), FinishedAt: metav1.NewTime(finishedAt)}}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			timing := executionTiming(domain.ExecutionJob{}, nil, &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: test.containerStatuses}})
			helperPhase := executionTimingPhase(t, timing, "artifact_collect")
			if helperPhase.StartedAt == nil || helperPhase.FinishedAt == nil || !helperPhase.StartedAt.Equal(startedAt) || !helperPhase.FinishedAt.Equal(finishedAt) {
				t.Fatalf("helper phase=%+v", helperPhase)
			}
		})
	}
}

func TestExecutionTimingPreservesInitContainerTimestamps(t *testing.T) {
	initStartedAt := time.Date(2026, time.September, 14, 12, 0, 0, 0, time.UTC)
	initFinishedAt := initStartedAt.Add(time.Minute)
	buildFinishedAt := initFinishedAt.Add(4 * time.Minute)
	timing := executionTiming(domain.ExecutionJob{}, nil, &corev1.Pod{Status: corev1.PodStatus{
		InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "workspace-prepare", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(initStartedAt), FinishedAt: metav1.NewTime(initFinishedAt)}}},
			{Name: "cache-restore", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(initStartedAt), FinishedAt: metav1.NewTime(initFinishedAt)}}},
		},
		ContainerStatuses: []corev1.ContainerStatus{
			{Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{StartedAt: metav1.NewTime(initFinishedAt), FinishedAt: metav1.NewTime(buildFinishedAt)}}},
		},
	}})
	for _, phaseName := range []string{"workspace_prepare", "cache_restore"} {
		phase := executionTimingPhase(t, timing, phaseName)
		if phase.StartedAt == nil || phase.FinishedAt == nil || !phase.StartedAt.Equal(initStartedAt) || !phase.FinishedAt.Equal(initFinishedAt) {
			t.Fatalf("%s phase=%+v", phaseName, phase)
		}
	}
}

func executionTimingPhase(t *testing.T, timing domain.ExecutionTiming, name string) domain.ExecutionPhaseTiming {
	t.Helper()
	for _, phase := range timing.Phases {
		if phase.Name == name {
			return phase
		}
	}
	t.Fatalf("phase %q not found in %+v", name, timing.Phases)
	return domain.ExecutionPhaseTiming{}
}
