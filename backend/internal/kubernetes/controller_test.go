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
		})
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
	if sink.text != client.logs || len(sink.writes) != 3 {
		t.Fatalf("writes=%d output length=%d", len(sink.writes), len(sink.text))
	}
	for _, write := range sink.writes {
		if len(write) > terminalLogChunkSize {
			t.Fatalf("write size=%d exceeds chunk size=%d", len(write), terminalLogChunkSize)
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

func TestControllerRejectsDurableWorkspacePublication(t *testing.T) {
	step := testStep()
	service := &fakeExecutionService{step: step, found: true}
	client := newFakeClient()
	controller := NewController(client, service, nil, "default").WithWorkspacePublicationEnabled(true)

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if client.createCalls != 0 || service.completeCalls != 1 || !strings.Contains(service.result.Stderr, "durable workspace publication") {
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
	if _, logErr := client.GetPodLogs(context.Background(), "default", "pod"); logErr == nil {
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
	stale            bool
	renewCalls       int
	completeCalls    int
	result           runner.RunStepResult
	durableStatus    domain.ExecutionJobStatus
	durableStatuses  map[string]domain.ExecutionJobStatus
	statusAfterRenew domain.ExecutionJobStatus
	completeErr      error
}

func (s *fakeExecutionService) ClaimRunnableStep(context.Context) (workersvc.WorkerRunnableStep, bool, error) {
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
	return !s.stale, nil
}
func (s *fakeExecutionService) GetExecutionJob(_ context.Context, jobID string) (domain.ExecutionJob, error) {
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
	return repository.StepCompletionCompleted, s.completeErr
}

type fakeClient struct {
	jobs        map[string]*batchv1.Job
	pods        []corev1.Pod
	logs        string
	getErr      error
	createRace  bool
	createCalls int
	deleteCalls int
	listCalls   int
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
	delete(c.jobs, name)
	return nil
}
func (c *fakeClient) ListJobs(context.Context, string, string) ([]batchv1.Job, error) {
	c.listCalls++
	jobs := make([]batchv1.Job, 0, len(c.jobs))
	for _, job := range c.jobs {
		jobs = append(jobs, *job)
	}
	return jobs, nil
}
func (c *fakeClient) ListPods(context.Context, string, string) ([]corev1.Pod, error) {
	return c.pods, nil
}
func (c *fakeClient) GetPodLogs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(c.logs)), nil
}

type recordingLogSink struct {
	text   string
	writes []string
	err    error
}

func (s *recordingLogSink) WriteStepLog(_ context.Context, _, _, line string) error {
	if s.err != nil {
		return s.err
	}
	s.text += line
	s.writes = append(s.writes, line)
	return nil
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

var _ logs.LogSink = (*recordingLogSink)(nil)
