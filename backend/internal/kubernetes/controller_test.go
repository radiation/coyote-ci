package kubernetes

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
)

func TestControllerCreatesDeterministicSecureJob(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	controller := NewController(client, service, nil, "ci")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	job := client.jobs[jobName(step.JobID)]
	if job == nil {
		t.Fatal("expected Kubernetes Job")
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("backoff limit=%v", job.Spec.BackoffLimit)
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != "alpine:3.20" || strings.Join(container.Command, " ") != "sh" || strings.Join(container.Args, " ") != "-c echo ok" {
		t.Fatalf("container=%#v", container)
	}
	if job.Spec.Template.Spec.AutomountServiceAccountToken == nil || *job.Spec.Template.Spec.AutomountServiceAccountToken {
		t.Fatal("build Pod must not mount a service account token")
	}
	if job.Labels["coyote-ci.io/execution-job-id"] != step.JobID || job.Labels[managedByLabel] != "coyote-ci" {
		t.Fatalf("labels=%v", job.Labels)
	}
	if job.Labels["coyote-ci.io/node-id"] != "build-node" || job.Labels["coyote-ci.io/attempt"] != "3" {
		t.Fatalf("metadata labels=%v", job.Labels)
	}
	if job.Spec.Template.Spec.Volumes[0].EmptyDir == nil {
		t.Fatal("expected emptyDir workspace")
	}
}

func TestControllerCreatesWorkspaceHelperLifecycle(t *testing.T) {
	step := testStep()
	helper := WorkspaceHelperConfig{Image: "coyote-worker:test", InternalAPIURL: "http://coyote.internal", ServiceAccountName: "coyote-workspace-helper"}
	job := buildJob("ci", step, helper)
	pod := job.Spec.Template.Spec
	if pod.ServiceAccountName != helper.ServiceAccountName || len(pod.InitContainers) != 1 || len(pod.Containers) != 2 {
		t.Fatalf("pod composition=%#v", pod)
	}
	if pod.InitContainers[0].Name != "workspace-prepare" || strings.Join(pod.InitContainers[0].Command, " ") != "/app/worker workspace prepare" {
		t.Fatalf("prepare=%#v", pod.InitContainers[0])
	}
	publish := pod.Containers[1]
	if publish.Name != "workspace-publish" || strings.Join(publish.Command, " ") != "/app/worker workspace publish-after-build" {
		t.Fatalf("publish=%#v", publish)
	}
	build := pod.Containers[0]
	if len(build.VolumeMounts) != 1 || build.VolumeMounts[0].Name != "workspace" {
		t.Fatalf("build mounts=%#v", build.VolumeMounts)
	}
	if !hasMount(pod.InitContainers[0], "workspace-prepare-token") || hasMount(pod.InitContainers[0], "workspace-publish-token") || hasMount(pod.InitContainers[0], "workspace-kubernetes-api") {
		t.Fatalf("prepare mounts=%#v", pod.InitContainers[0].VolumeMounts)
	}
	if !hasMount(publish, "workspace-publish-token") || !hasMount(publish, "workspace-kubernetes-api") || hasMount(publish, "workspace-prepare-token") {
		t.Fatalf("publish mounts=%#v", publish.VolumeMounts)
	}
	for _, volume := range pod.Volumes {
		if volume.Name == "workspace" || volume.Projected == nil || len(volume.Projected.Sources) == 0 {
			continue
		}
		if volume.Name == "workspace-prepare-token" && volume.Projected.Sources[0].ServiceAccountToken.Audience != workspaceHelperPrepareAudience {
			t.Fatalf("prepare audience=%q", volume.Projected.Sources[0].ServiceAccountToken.Audience)
		}
		if volume.Name == "workspace-publish-token" && volume.Projected.Sources[0].ServiceAccountToken.Audience != workspaceHelperPublishAudience {
			t.Fatalf("publish audience=%q", volume.Projected.Sources[0].ServiceAccountToken.Audience)
		}
		if volume.Name == "workspace-kubernetes-api" && volume.Projected.Sources[0].ServiceAccountToken.Audience != "" {
			t.Fatalf("kubernetes API audience=%q", volume.Projected.Sources[0].ServiceAccountToken.Audience)
		}
	}
}

func TestBuildJobWithoutWorkspaceHelpersHasOnlyBuildContainer(t *testing.T) {
	job := buildJob("ci", testStep())
	pod := job.Spec.Template.Spec
	if pod.ServiceAccountName != "" || len(pod.InitContainers) != 0 || len(pod.Containers) != 1 || pod.Containers[0].Name != "build" {
		t.Fatalf("pod=%#v", pod)
	}
}

func TestBuildJobPinsConfiguredStepNode(t *testing.T) {
	step := testStep()
	step.StepIndex = 1
	job := buildJobWithNodeName("ci", step, WorkspaceHelperConfig{}, "coyote-ci-worker2")
	if job.Spec.Template.Spec.NodeName != "coyote-ci-worker2" {
		t.Fatalf("node name=%q", job.Spec.Template.Spec.NodeName)
	}

	controller := NewController(newFakeClient(), &fakeExecutionService{}, nil, "ci").WithTestStepNodeNames([]string{" coyote-ci-worker ", "coyote-ci-worker2", ""})
	if controller.testStepNodeName(0) != "coyote-ci-worker" || controller.testStepNodeName(1) != "coyote-ci-worker2" || controller.testStepNodeName(2) != "" {
		t.Fatalf("test node names=%v", controller.testStepNodeNames)
	}
}

func TestControllerWithWorkspaceHelperEnablesLifecycle(t *testing.T) {
	controller := NewController(newFakeClient(), &fakeExecutionService{}, nil, "ci")
	helper := WorkspaceHelperConfig{Image: "coyote-worker:test", InternalAPIURL: "http://coyote.internal", ServiceAccountName: "coyote-workspace-helper"}
	if got := controller.WithWorkspaceHelper(helper); got != controller || !controller.workspacePublicationEnabled || controller.workspaceHelper != helper {
		t.Fatalf("controller=%#v", controller)
	}
}

func TestControllerRenewsPendingJobLease(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = buildJob("default", step)
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if service.renewCalls != 2 || service.completeCalls != 0 {
		t.Fatalf("renewals=%d completions=%d", service.renewCalls, service.completeCalls)
	}
}

func TestControllerRestartReclaimsExistingDeterministicJob(t *testing.T) {
	step := testStep()
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = buildJob("default", step)
	service := &fakeExecutionService{step: step, found: true}
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.createCalls != 0 || service.renewCalls != 1 {
		t.Fatalf("creates=%d renewals=%d", client.createCalls, service.renewCalls)
	}
}

func TestControllerDoesNotCompleteAfterStaleClaim(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true, stale: true}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if service.completeCalls != 0 {
		t.Fatal("stale controller must not finalize")
	}
}

func TestControllerWaitsForTerminalJobPod(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if service.completeCalls != 0 {
		t.Fatal("controller must wait until it can observe the build Pod")
	}
}

func TestControllerCompletesTerminalOutcomesAndLogs(t *testing.T) {
	for _, test := range []struct {
		name        string
		exitCode    int32
		reason      string
		wantStatus  runner.RunStepStatus
		wantTimeout bool
	}{
		{name: "success", exitCode: 0, reason: "Completed", wantStatus: runner.RunStepStatusSuccess},
		{name: "failure", exitCode: 7, reason: "Error", wantStatus: runner.RunStepStatusFailed},
		{name: "timeout", exitCode: 1, reason: "DeadlineExceeded", wantStatus: runner.RunStepStatusFailed, wantTimeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			step := testStep()
			service := &fakeExecutionService{step: step, found: true}
			client := newFakeClient()
			client.jobs[jobName(step.JobID)] = completedJob(step, test.exitCode, test.reason, "message")
			if test.wantTimeout {
				client.jobs[jobName(step.JobID)].Status.Conditions[0].Reason = "DeadlineExceeded"
			}
			client.pods = []corev1.Pod{terminatedBuildPod(test.exitCode, test.reason, "message")}
			client.logs = "terminal output\n"
			sink := &recordingLogSink{}
			controller := NewController(client, service, sink, "default")

			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if service.completeCalls != 1 || service.result.Status != test.wantStatus || service.result.TimedOut != test.wantTimeout {
				t.Fatalf("result=%#v complete=%d", service.result, service.completeCalls)
			}
			if sink.text != "terminal output\n" {
				t.Fatalf("logs=%q", sink.text)
			}
			if client.logContainer != "build" {
				t.Fatalf("log container=%q", client.logContainer)
			}
		})
	}
}

func TestControllerPersistsTerminalLogsAsStepChunks(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
	client.pods = []corev1.Pod{terminatedBuildPod(0, "Completed", "")}
	client.logs = "terminal output\n"
	sink := &recordingLogSink{}
	controller := NewController(client, service, sink, "default")

	if reconcileErr := controller.Reconcile(context.Background()); reconcileErr != nil {
		t.Fatalf("reconcile: %v", reconcileErr)
	}
	if len(sink.chunks) != 1 {
		t.Fatalf("chunks=%#v", sink.chunks)
	}
	chunk := sink.chunks[0]
	if chunk.BuildID != step.BuildID || chunk.StepID != step.StepID || chunk.StepIndex != step.StepIndex || chunk.StepName != step.StepName || chunk.Stream != logs.StepLogStreamStdout || chunk.ChunkText != client.logs {
		t.Fatalf("chunk=%#v", chunk)
	}
}

func TestControllerCancellationDeletesJobWithoutCompletion(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true, durableStatus: domain.ExecutionJobStatusCanceled}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = buildJob("default", step)
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.deleteCalls != 1 || service.completeCalls != 0 {
		t.Fatalf("deletes=%d completions=%d", client.deleteCalls, service.completeCalls)
	}
}

func TestControllerDeletesJobWhenCancellationRacesLeaseLoss(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true, stale: true, statusAfterRenew: domain.ExecutionJobStatusCanceled}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = buildJob("default", step)
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.deleteCalls != 1 || service.completeCalls != 0 {
		t.Fatalf("deletes=%d completions=%d", client.deleteCalls, service.completeCalls)
	}
}

func TestControllerSweepDeletesCanceledJobAfterRestart(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{durableStatuses: map[string]domain.ExecutionJobStatus{step.JobID: domain.ExecutionJobStatusCanceled}}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = buildJob("default", step)
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.deleteCalls != 1 || service.completeCalls != 0 {
		t.Fatalf("deletes=%d completions=%d", client.deleteCalls, service.completeCalls)
	}
}

func TestControllerSweepDeletesCanceledJobWhileBusy(t *testing.T) {
	activeStep := testStep()
	canceledStep := activeStep
	canceledStep.JobID = "bb58bf9a-09db-4b80-a66e-61fbd2209a09"
	service := &fakeExecutionService{step: activeStep, found: true, durableStatuses: map[string]domain.ExecutionJobStatus{activeStep.JobID: domain.ExecutionJobStatusRunning, canceledStep.JobID: domain.ExecutionJobStatusCanceled}}
	client := newFakeClient()
	client.jobs[jobName(activeStep.JobID)] = buildJob("default", activeStep)
	client.jobs[jobName(canceledStep.JobID)] = buildJob("default", canceledStep)
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.jobs[jobName(canceledStep.JobID)] != nil {
		t.Fatal("expected canceled orphan Job to be deleted while active work is present")
	}
	if client.jobs[jobName(activeStep.JobID)] == nil {
		t.Fatal("active Job must not be deleted by the orphan cleanup sweep")
	}
}

func TestControllerIgnoresNonTrueTerminalCondition(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	job := completedJob(step, 7, "Error", "message")
	job.Status.Conditions[0].Status = corev1.ConditionFalse
	client.jobs[jobName(step.JobID)] = job
	client.pods = []corev1.Pod{terminatedBuildPod(7, "Error", "message")}
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if service.completeCalls != 0 {
		t.Fatal("non-true failure condition must not finalize")
	}
}

func TestControllerUsesJobDeadlineExceededForTimeout(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	job := completedJob(step, 1, "Error", "message")
	job.Status.Conditions[0].Reason = "DeadlineExceeded"
	client.jobs[jobName(step.JobID)] = job
	client.pods = []corev1.Pod{terminatedBuildPod(1, "Error", "message")}
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !service.result.TimedOut || service.result.Status != runner.RunStepStatusFailed {
		t.Fatalf("result=%#v", service.result)
	}
}

func TestPodResultClassifiesWorkspaceHelperFailures(t *testing.T) {
	now := time.Now().UTC()
	for _, testCase := range []struct {
		name string
		pod  corev1.Pod
		want string
	}{
		{name: "prepare", pod: corev1.Pod{Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{{Name: "workspace-prepare", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}}}}}, want: "workspace revision prepare"},
		{name: "publish", pod: corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "workspace-publish", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}}, {Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}}}}, want: "workspace revision publish"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := podResult(testCase.pod, now)
			if result.Status != runner.RunStepStatusFailed || !strings.Contains(result.Stderr, testCase.want) {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestControllerCompletesWorkspacePrepareFailureWithoutBuildLogs(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
	client.pods = []corev1.Pod{{Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{{Name: "workspace-prepare", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}}}}}}
	controller := NewController(client, service, &recordingLogSink{}, "default")

	if reconcileErr := controller.Reconcile(context.Background()); reconcileErr != nil {
		t.Fatalf("reconcile: %v", reconcileErr)
	}
	if service.completeCalls != 1 || !strings.Contains(service.result.Stderr, "workspace revision prepare") {
		t.Fatalf("completion=%d result=%#v", service.completeCalls, service.result)
	}
	if client.logContainer != "" {
		t.Fatalf("unexpected build log request for helper failure: %q", client.logContainer)
	}
}

func TestControllerDoesNotDuplicateLogsWhenCompletionRetries(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true, completeErr: errors.New("temporary completion failure")}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
	client.pods = []corev1.Pod{terminatedBuildPod(0, "Completed", "")}
	client.logs = "terminal output\n"
	sink := &recordingLogSink{}
	controller := NewController(client, service, sink, "default")

	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("expected first completion failure")
	}
	service.completeErr = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("retry reconcile: %v", err)
	}
	if sink.text != "terminal output\n" || service.completeCalls != 2 {
		t.Fatalf("logs=%q completions=%d", sink.text, service.completeCalls)
	}
}

func TestControllerDefersCompletionWhenTerminalLogPersistenceFails(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
	client.pods = []corev1.Pod{terminatedBuildPod(0, "Completed", "")}
	client.logs = "terminal output\n"
	sink := &recordingLogSink{err: errors.New("log store unavailable")}
	controller := NewController(client, service, sink, "default")

	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("expected terminal log error")
	}
	if service.completeCalls != 0 {
		t.Fatal("log failure must defer completion")
	}
}

func TestControllerStreamsTerminalLogsInBoundedChunks(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
	client.pods = []corev1.Pod{terminatedBuildPod(0, "Completed", "")}
	client.logs = strings.Repeat("x", terminalLogChunkSize*2+1)
	sink := &recordingLogSink{}
	controller := NewController(client, service, sink, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if sink.text != client.logs || len(sink.chunks) != 3 {
		t.Fatalf("chunks=%d output length=%d", len(sink.chunks), len(sink.text))
	}
	for _, chunk := range sink.chunks {
		if len(chunk.ChunkText) > terminalLogChunkSize {
			t.Fatalf("chunk size=%d exceeds chunk size=%d", len(chunk.ChunkText), terminalLogChunkSize)
		}
	}
}

func TestControllerUnsupportedShapeCompletesWithoutCreatingJob(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true, validateErr: &workersvc.KubernetesExecutionCapabilityError{Feature: "cache restore or save"}}
	client := newFakeClient()
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.createCalls != 0 || service.completeCalls != 1 || !strings.Contains(service.result.Stderr, "cache restore") {
		t.Fatalf("creates=%d completion=%d result=%#v", client.createCalls, service.completeCalls, service.result)
	}
}

func TestControllerRetriesTransientValidationErrorWithoutCompletion(t *testing.T) {
	step := testStep()
	validationErr := errors.New("temporary database outage")
	service := &fakeExecutionService{step: step, found: true, validateErr: validationErr}
	client := newFakeClient()
	controller := NewController(client, service, nil, "default")

	err := controller.Reconcile(context.Background())
	if !errors.Is(err, validationErr) {
		t.Fatalf("reconcile error = %v, want validation error", err)
	}
	if client.createCalls != 0 || service.completeCalls != 0 {
		t.Fatalf("creates=%d completion=%d", client.createCalls, service.completeCalls)
	}
}

func TestControllerRejectsIncompleteWorkspaceHelperConfiguration(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	controller := NewController(client, service, nil, "default").WithWorkspacePublicationEnabled(true)

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.createCalls != 0 || service.completeCalls != 1 || !strings.Contains(service.result.Stderr, "configuration is incomplete") {
		t.Fatalf("creates=%d completion=%d result=%#v", client.createCalls, service.completeCalls, service.result)
	}
}

func TestControllerReturnsTransientAPIErrorWithoutCompletion(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.getErr = errors.New("temporary API outage")
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("expected transient error")
	}
	if service.completeCalls != 0 {
		t.Fatal("transient error must not complete execution")
	}
}

func TestControllerRejectsMismatchedExistingJob(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName(step.JobID)}}
	controller := NewController(client, service, nil, "")

	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("expected ownership error")
	}
	if service.completeCalls != 0 {
		t.Fatal("mismatched Job must not complete execution")
	}
}

func TestControllerTreatsAlreadyExistsAsIdempotent(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	client.createRace = true
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.createCalls != 1 || client.jobs[jobName(step.JobID)] == nil {
		t.Fatalf("creates=%d jobs=%v", client.createCalls, client.jobs)
	}
}

func TestControllerReturnsOperationalErrorsWithoutCompletion(t *testing.T) {
	tests := []struct {
		name    string
		service *fakeExecutionService
		client  *fakeClient
		active  bool
		wantErr string
	}{
		{name: "claim", service: &fakeExecutionService{claimErr: errors.New("claim unavailable")}, client: newFakeClient(), wantErr: "claim unavailable"},
		{name: "cancellation sweep", service: &fakeExecutionService{}, client: &fakeClient{jobs: map[string]*batchv1.Job{}, listJobsErr: errors.New("list unavailable")}, wantErr: "list unavailable"},
		{name: "durable execution lookup", service: &fakeExecutionService{getErr: errors.New("execution job unavailable")}, client: newFakeClient(), active: true, wantErr: "execution job unavailable"},
		{name: "lease renewal", service: &fakeExecutionService{renewErr: errors.New("renew unavailable")}, client: newFakeClient(), active: true, wantErr: "renew unavailable"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			step := testStep()
			testCase.service.step = step
			testCase.service.found = true
			controller := NewController(testCase.client, testCase.service, nil, "default")
			if testCase.active {
				controller.active = &step
			}
			err := controller.Reconcile(context.Background())
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("reconcile error = %v, want %q", err, testCase.wantErr)
			}
			if testCase.service.completeCalls != 0 {
				t.Fatal("operational error must not complete execution")
			}
		})
	}
}

func TestControllerDefersCompletionOnTerminalPodOrLogReadFailure(t *testing.T) {
	tests := []struct {
		name    string
		client  *fakeClient
		wantErr bool
	}{
		{name: "pod list", client: &fakeClient{jobs: map[string]*batchv1.Job{}, listPodsErr: errors.New("pod list unavailable")}},
		{name: "log stream", client: &fakeClient{jobs: map[string]*batchv1.Job{}, pods: []corev1.Pod{terminatedBuildPod(0, "Completed", "")}, getLogsErr: errors.New("log stream unavailable")}, wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			step := testStep()
			testCase.client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
			service := &fakeExecutionService{step: step, found: true}
			controller := NewController(testCase.client, service, &recordingLogSink{}, "default")
			err := controller.Reconcile(context.Background())
			if testCase.wantErr && (err == nil || !strings.Contains(err.Error(), "unavailable")) {
				t.Fatalf("reconcile error = %v", err)
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if service.completeCalls != 0 {
				t.Fatal("terminal observation failure must defer completion")
			}
		})
	}
}

func TestControllerHandlesStaleCompletionAndDeletionFailure(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true, completeOutcome: repository.StepCompletionStaleClaim}
	client := newFakeClient()
	client.jobs[jobName(step.JobID)] = completedJob(step, 0, "Completed", "")
	client.pods = []corev1.Pod{terminatedBuildPod(0, "Completed", "")}
	controller := NewController(client, service, nil, "default")

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("stale completion reconcile: %v", err)
	}
	if controller.active != nil {
		t.Fatal("stale completion must release active claim")
	}

	service = &fakeExecutionService{step: step, found: true, durableStatus: domain.ExecutionJobStatusCanceled}
	client = newFakeClient()
	client.jobs[jobName(step.JobID)] = buildJob("default", step)
	client.deleteErr = errors.New("delete unavailable")
	controller = NewController(client, service, nil, "default")
	controller.active = &step
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "delete unavailable") {
		t.Fatalf("delete reconcile error = %v", err)
	}
}

func TestClientsetDelegatesKubernetesOperations(t *testing.T) {
	client := &clientset{client: kubernetesfake.NewClientset()}
	job := buildJob("default", testStep())
	created, createErr := client.CreateJob(context.Background(), "default", job)
	if createErr != nil {
		t.Fatalf("create: %v", createErr)
	}
	if _, getErr := client.GetJob(context.Background(), "default", created.Name); getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "default", Labels: map[string]string{"job-name": created.Name}}}
	if _, podErr := client.client.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{}); podErr != nil {
		t.Fatalf("create pod: %v", podErr)
	}
	pods, listErr := client.ListPods(context.Background(), "default", "job-name="+created.Name)
	if listErr != nil || len(pods) != 1 {
		t.Fatalf("pods=%v err=%v", pods, listErr)
	}
	if _, logErr := client.GetPodLogs(context.Background(), "default", "pod", "build"); logErr == nil {
		t.Log("fake client supplied a pod log stream")
	}
	if deleteErr := client.DeleteJob(context.Background(), "default", created.Name); deleteErr != nil {
		t.Fatalf("delete: %v", deleteErr)
	}
	if _, getErr := client.GetJob(context.Background(), "default", created.Name); !apierrors.IsNotFound(getErr) {
		t.Fatalf("expected deleted Job, got %v", getErr)
	}
}

type fakeExecutionService struct {
	step             workersvc.WorkerRunnableStep
	found            bool
	claimUsed        bool
	validateErr      error
	claimErr         error
	getErr           error
	stale            bool
	renewCalls       int
	renewErr         error
	completeCalls    int
	result           runner.RunStepResult
	durableStatus    domain.ExecutionJobStatus
	durableStatuses  map[string]domain.ExecutionJobStatus
	statusAfterRenew domain.ExecutionJobStatus
	completeErr      error
	completeOutcome  repository.StepCompletionOutcome
}

func (s *fakeExecutionService) ClaimRunnableStep(context.Context) (workersvc.WorkerRunnableStep, bool, error) {
	if s.claimErr != nil {
		return workersvc.WorkerRunnableStep{}, false, s.claimErr
	}
	if s.claimUsed {
		return workersvc.WorkerRunnableStep{}, false, nil
	}
	s.claimUsed = true
	return s.step, s.found, nil
}
func (s *fakeExecutionService) ValidateKubernetesRunnableStep(context.Context, workersvc.WorkerRunnableStep) error {
	return s.validateErr
}
func (s *fakeExecutionService) RenewRunnableStepLease(context.Context, workersvc.WorkerRunnableStep) (bool, error) {
	s.renewCalls++
	return !s.stale, s.renewErr
}
func (s *fakeExecutionService) GetExecutionJob(_ context.Context, jobID string) (domain.ExecutionJob, error) {
	if s.getErr != nil {
		return domain.ExecutionJob{}, s.getErr
	}
	if s.renewCalls > 0 && s.statusAfterRenew != "" {
		return domain.ExecutionJob{Status: s.statusAfterRenew}, nil
	}
	if status, found := s.durableStatuses[jobID]; found {
		return domain.ExecutionJob{Status: status}, nil
	}
	status := s.durableStatus
	if status == "" {
		status = domain.ExecutionJobStatusRunning
	}
	return domain.ExecutionJob{Status: status}, nil
}
func (s *fakeExecutionService) CompleteKubernetesRunnableStep(_ context.Context, _ workersvc.WorkerRunnableStep, result runner.RunStepResult) (repository.StepCompletionOutcome, error) {
	s.completeCalls++
	s.result = result
	if s.completeOutcome != "" {
		return s.completeOutcome, s.completeErr
	}
	return repository.StepCompletionCompleted, s.completeErr
}

type fakeClient struct {
	jobs         map[string]*batchv1.Job
	pods         []corev1.Pod
	logs         string
	logContainer string
	getErr       error
	createErr    error
	deleteErr    error
	listJobsErr  error
	listPodsErr  error
	getLogsErr   error
	createRace   bool
	createCalls  int
	deleteCalls  int
	listCalls    int
}

func newFakeClient() *fakeClient { return &fakeClient{jobs: map[string]*batchv1.Job{}} }
func (c *fakeClient) GetJob(_ context.Context, _ string, name string) (*batchv1.Job, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	job := c.jobs[name]
	if job == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "batch", Resource: "jobs"}, name)
	}
	return job, nil
}
func (c *fakeClient) CreateJob(_ context.Context, _ string, job *batchv1.Job) (*batchv1.Job, error) {
	c.createCalls++
	if c.createErr != nil {
		return nil, c.createErr
	}
	if c.createRace {
		c.createRace = false
		c.jobs[job.Name] = job
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: "batch", Resource: "jobs"}, job.Name)
	}
	if c.jobs[job.Name] != nil {
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: "batch", Resource: "jobs"}, job.Name)
	}
	c.jobs[job.Name] = job
	return job, nil
}
func (c *fakeClient) DeleteJob(_ context.Context, _ string, name string) error {
	c.deleteCalls++
	if c.deleteErr != nil {
		return c.deleteErr
	}
	delete(c.jobs, name)
	return nil
}
func (c *fakeClient) ListJobs(context.Context, string, string) ([]batchv1.Job, error) {
	c.listCalls++
	if c.listJobsErr != nil {
		return nil, c.listJobsErr
	}
	jobs := make([]batchv1.Job, 0, len(c.jobs))
	for _, job := range c.jobs {
		jobs = append(jobs, *job)
	}
	return jobs, nil
}
func (c *fakeClient) ListPods(context.Context, string, string) ([]corev1.Pod, error) {
	return c.pods, c.listPodsErr
}
func (c *fakeClient) GetPodLogs(_ context.Context, _ string, _ string, container string) (io.ReadCloser, error) {
	c.logContainer = container
	if c.getLogsErr != nil {
		return nil, c.getLogsErr
	}
	return io.NopCloser(strings.NewReader(c.logs)), nil
}

type recordingLogSink struct {
	text   string
	chunks []logs.StepLogChunk
	err    error
}

func (s *recordingLogSink) WriteStepLog(_ context.Context, _, _, line string) error {
	if s.err != nil {
		return s.err
	}
	s.text += line
	return nil
}

func (s *recordingLogSink) AppendStepLogChunk(_ context.Context, chunk logs.StepLogChunk) (logs.StepLogChunk, error) {
	if s.err != nil {
		return logs.StepLogChunk{}, s.err
	}
	s.text += chunk.ChunkText
	s.chunks = append(s.chunks, chunk)
	return chunk, nil
}

func testStep() workersvc.WorkerRunnableStep {
	return workersvc.WorkerRunnableStep{BuildID: "build-1", JobID: "7f1cc887-8a8c-4310-9f13-53ff7c8e04ef", StepID: "step-1", StepIndex: 0, StepName: "test", WorkerID: "worker-1", ClaimToken: "claim-1", NodeID: "build-node", AttemptNumber: 3, Image: "alpine:3.20", Command: "sh", Args: []string{"-c", "echo ok"}, Env: map[string]string{"A": "b"}, WorkingDir: ".", TimeoutSeconds: 30}
}
func completedJob(step workersvc.WorkerRunnableStep, exitCode int32, reason, message string) *batchv1.Job {
	job := buildJob("default", step)
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if exitCode != 0 {
		job.Status.Conditions[0].Type = batchv1.JobFailed
	}
	job.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Minute))
	return job
}
func terminatedBuildPod(exitCode int32, reason, message string) corev1.Pod {
	now := metav1.NewTime(time.Now())
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "build-pod", CreationTimestamp: now}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "build", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: exitCode, Reason: reason, Message: message, StartedAt: now, FinishedAt: now}}}}}}
}

func hasMount(container corev1.Container, name string) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == name {
			return true
		}
	}
	return false
}

var _ logs.LogSink = (*recordingLogSink)(nil)
