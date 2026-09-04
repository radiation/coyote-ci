// Package kubernetes provides the asynchronous Kubernetes execution backend.
package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

const (
	managedByLabel              = "app.kubernetes.io/managed-by"
	terminalLogChunkSize        = 32 * 1024
	cancellationCleanupInterval = 30 * time.Second
)

type Client interface {
	GetJob(context.Context, string, string) (*batchv1.Job, error)
	CreateJob(context.Context, string, *batchv1.Job) (*batchv1.Job, error)
	DeleteJob(context.Context, string, string) error
	ListJobs(context.Context, string, string) ([]batchv1.Job, error)
	ListPods(context.Context, string, string) ([]corev1.Pod, error)
	GetPodLogs(context.Context, string, string) (io.ReadCloser, error)
}

type executionService interface {
	ClaimRunnableStep(context.Context) (workersvc.WorkerRunnableStep, bool, error)
	ValidateKubernetesRunnableStep(context.Context, workersvc.WorkerRunnableStep) error
	RenewRunnableStepLease(context.Context, workersvc.WorkerRunnableStep) (bool, error)
	GetExecutionJob(context.Context, string) (domain.ExecutionJob, error)
	CompleteKubernetesRunnableStep(context.Context, workersvc.WorkerRunnableStep, runner.RunStepResult) (repository.StepCompletionOutcome, error)
}

type Controller struct {
	client                      Client
	service                     executionService
	logSink                     logs.LogSink
	namespace                   string
	workspacePublicationEnabled bool
	active                      *workersvc.WorkerRunnableStep
	terminalLogsPersisted       map[string]bool
	lastCancellationCleanupAt   time.Time
	now                         func() time.Time
}

func NewController(client Client, service executionService, logSink logs.LogSink, namespace string) *Controller {
	return &Controller{client: client, service: service, logSink: logSink, namespace: defaultNamespace(namespace), terminalLogsPersisted: map[string]bool{}, now: func() time.Time { return time.Now().UTC() }}
}

// WithWorkspacePublicationEnabled prevents this initial backend from bypassing
// the durable workspace publication requirement used by Docker execution.
func (c *Controller) WithWorkspacePublicationEnabled(enabled bool) *Controller {
	c.workspacePublicationEnabled = enabled
	return c
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if c.active != nil {
		if cleanupErr := c.cleanupCanceledJobsIfDue(ctx); cleanupErr != nil {
			return cleanupErr
		}
		return c.reconcileActive(ctx, *c.active)
	}
	step, found, err := c.service.ClaimRunnableStep(ctx)
	if err != nil {
		return err
	}
	if !found {
		return c.cleanupCanceledJobsIfDue(ctx)
	}
	if c.workspacePublicationEnabled {
		return c.complete(ctx, step, runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, Stderr: "kubernetes execution backend does not yet support durable workspace publication", StartedAt: c.now(), FinishedAt: c.now()})
	}
	if validationErr := c.service.ValidateKubernetesRunnableStep(ctx, step); validationErr != nil {
		if !workersvc.IsKubernetesExecutionCapabilityError(validationErr) {
			return validationErr
		}
		return c.complete(ctx, step, runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, Stderr: validationErr.Error(), StartedAt: c.now(), FinishedAt: c.now()})
	}
	c.active = &step
	if cleanupErr := c.cleanupCanceledJobsIfDue(ctx); cleanupErr != nil {
		return cleanupErr
	}
	return c.reconcileActive(ctx, step)
}

func (c *Controller) reconcileActive(ctx context.Context, step workersvc.WorkerRunnableStep) error {
	durable, err := c.service.GetExecutionJob(ctx, step.JobID)
	if err != nil {
		return err
	}
	if durable.Status == domain.ExecutionJobStatusCanceled {
		c.active = nil
		return c.deleteJob(ctx, jobName(step.JobID))
	}
	if domain.IsTerminalExecutionJobStatus(durable.Status) {
		c.active = nil
		return nil
	}
	continued, renewErr := c.service.RenewRunnableStepLease(ctx, step)
	if renewErr != nil {
		return renewErr
	}
	if !continued {
		refreshed, refreshErr := c.service.GetExecutionJob(ctx, step.JobID)
		c.active = nil
		if refreshErr != nil {
			return refreshErr
		}
		if refreshed.Status == domain.ExecutionJobStatusCanceled {
			return c.deleteJob(ctx, jobName(step.JobID))
		}
		return nil
	}

	job, ensureErr := c.ensureJob(ctx, step)
	if ensureErr != nil {
		return ensureErr
	}
	if terminal, result := c.terminalResult(ctx, job, step); terminal {
		logErr := c.collectTerminalLogs(ctx, step, job.Name)
		if logErr != nil {
			return logErr
		}
		completeErr := c.complete(ctx, step, result)
		return completeErr
	}
	return nil
}

func (c *Controller) ensureJob(ctx context.Context, step workersvc.WorkerRunnableStep) (*batchv1.Job, error) {
	name := jobName(step.JobID)
	job, err := c.client.GetJob(ctx, c.namespace, name)
	if err == nil {
		if !belongsToExecution(job, step.JobID) {
			return nil, fmt.Errorf("kubernetes job %s does not belong to execution job %s", name, step.JobID)
		}
		return job, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	created, createErr := c.client.CreateJob(ctx, c.namespace, buildJob(c.namespace, step))
	if createErr == nil {
		return created, nil
	}
	if !apierrors.IsAlreadyExists(createErr) {
		return nil, createErr
	}
	job, err = c.client.GetJob(ctx, c.namespace, name)
	if err != nil {
		return nil, err
	}
	if !belongsToExecution(job, step.JobID) {
		return nil, fmt.Errorf("kubernetes job %s does not belong to execution job %s", name, step.JobID)
	}
	return job, nil
}

func (c *Controller) terminalResult(ctx context.Context, job *batchv1.Job, step workersvc.WorkerRunnableStep) (bool, runner.RunStepResult) {
	for _, condition := range job.Status.Conditions {
		if (condition.Type != batchv1.JobComplete && condition.Type != batchv1.JobFailed) || condition.Status != corev1.ConditionTrue {
			continue
		}
		pods, err := c.client.ListPods(ctx, c.namespace, labels.Set{"job-name": job.Name}.String())
		if err != nil || len(pods) == 0 {
			return false, runner.RunStepResult{}
		}
		pod := newestPod(pods)
		result := podResult(pod, c.now())
		if condition.Type == batchv1.JobComplete {
			result.Status = runner.RunStepStatusSuccess
			result.ExitCode = 0
		}
		if condition.Type == batchv1.JobFailed && condition.Reason == "DeadlineExceeded" {
			result.TimedOut = true
		}
		return true, result
	}
	return false, runner.RunStepResult{}
}

func (c *Controller) collectTerminalLogs(ctx context.Context, step workersvc.WorkerRunnableStep, jobName string) error {
	if c.terminalLogsPersisted[step.JobID] {
		return nil
	}
	if c.logSink == nil {
		return nil
	}
	pods, err := c.client.ListPods(ctx, c.namespace, labels.Set{"job-name": jobName}.String())
	if err != nil || len(pods) == 0 {
		return err
	}
	pod := newestPod(pods)
	stream, err := c.client.GetPodLogs(ctx, c.namespace, pod.Name)
	if err != nil {
		return err
	}
	buffer := make([]byte, terminalLogChunkSize)
	for {
		count, readErr := stream.Read(buffer)
		if count > 0 {
			if writeErr := c.writeTerminalLogChunk(ctx, step, string(buffer[:count])); writeErr != nil {
				return errors.Join(writeErr, stream.Close())
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return errors.Join(readErr, stream.Close())
		}
	}
	if closeErr := stream.Close(); closeErr != nil {
		return closeErr
	}
	c.terminalLogsPersisted[step.JobID] = true
	return nil
}

func (c *Controller) writeTerminalLogChunk(ctx context.Context, step workersvc.WorkerRunnableStep, text string) error {
	if appender, ok := c.logSink.(logs.StepLogChunkAppender); ok {
		_, err := appender.AppendStepLogChunk(ctx, logs.StepLogChunk{
			BuildID: step.BuildID, StepID: step.StepID, StepIndex: step.StepIndex, StepName: step.StepName,
			Stream: logs.StepLogStreamStdout, ChunkText: text, CreatedAt: c.now(),
		})
		return err
	}
	return c.logSink.WriteStepLog(ctx, step.BuildID, step.StepName, text)
}

func (c *Controller) complete(ctx context.Context, step workersvc.WorkerRunnableStep, result runner.RunStepResult) error {
	outcome, err := c.service.CompleteKubernetesRunnableStep(ctx, step, result)
	if err != nil {
		return err
	}
	if outcome == repository.StepCompletionStaleClaim || outcome == repository.StepCompletionDuplicateTerminal {
		c.active = nil
		return nil
	}
	c.active = nil
	return nil
}

func (c *Controller) deleteJob(ctx context.Context, name string) error {
	err := c.client.DeleteJob(ctx, c.namespace, name)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *Controller) cleanupCanceledJobs(ctx context.Context) error {
	jobs, err := c.client.ListJobs(ctx, c.namespace, labels.Set{managedByLabel: "coyote-ci"}.String())
	if err != nil {
		return err
	}
	for _, job := range jobs {
		executionJobID := strings.TrimSpace(job.Labels["coyote-ci.io/execution-job-id"])
		if executionJobID == "" || job.Name != jobName(executionJobID) {
			continue
		}
		if c.active != nil && executionJobID == c.active.JobID {
			continue
		}
		durable, getErr := c.service.GetExecutionJob(ctx, executionJobID)
		if getErr != nil {
			return getErr
		}
		if durable.Status == domain.ExecutionJobStatusCanceled {
			if deleteErr := c.deleteJob(ctx, job.Name); deleteErr != nil {
				return deleteErr
			}
		}
	}
	return nil
}

func (c *Controller) cleanupCanceledJobsIfDue(ctx context.Context) error {
	if !c.lastCancellationCleanupAt.IsZero() && c.now().Sub(c.lastCancellationCleanupAt) < cancellationCleanupInterval {
		return nil
	}
	if err := c.cleanupCanceledJobs(ctx); err != nil {
		return err
	}
	c.lastCancellationCleanupAt = c.now()
	return nil
}

func buildJob(namespace string, step workersvc.WorkerRunnableStep) *batchv1.Job {
	backoffLimit := int32(0)
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName(step.JobID), Namespace: namespace, Labels: executionLabels(step)}}
	job.Spec.BackoffLimit = &backoffLimit
	if step.TimeoutSeconds > 0 {
		deadline := int64(step.TimeoutSeconds)
		job.Spec.ActiveDeadlineSeconds = &deadline
	}
	job.Spec.Template = corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: executionLabels(step), Annotations: executionAnnotations(step)},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			AutomountServiceAccountToken: boolPtr(false),
			Volumes:                      []corev1.Volume{{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
			Containers: []corev1.Container{{
				Name: "build", Image: step.Image, Command: []string{step.Command}, Args: append([]string(nil), step.Args...),
				Env: environment(step.Env), WorkingDir: workspace.ResolveVisibleWorkingDir(workspace.DefaultContainerRoot, step.WorkingDir),
				VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workspace.DefaultContainerRoot}},
			}},
		},
	}
	return job
}

func jobName(executionJobID string) string {
	return "coyote-exec-" + strings.ToLower(strings.TrimSpace(executionJobID))
}

func executionLabels(step workersvc.WorkerRunnableStep) map[string]string {
	labels := map[string]string{managedByLabel: "coyote-ci", "coyote-ci.io/execution-job-id": step.JobID, "coyote-ci.io/build-id": step.BuildID}
	if nodeID := sanitizeLabel(step.NodeID); nodeID != "" {
		labels["coyote-ci.io/node-id"] = nodeID
	}
	if step.AttemptNumber > 0 {
		labels["coyote-ci.io/attempt"] = fmt.Sprintf("%d", step.AttemptNumber)
	}
	return labels
}

func executionAnnotations(step workersvc.WorkerRunnableStep) map[string]string {
	if strings.TrimSpace(step.ClaimToken) == "" {
		return nil
	}
	return map[string]string{executionClaimDigestAnnotation: domain.ExecutionJobClaimDigest(step.ClaimToken)}
}

func belongsToExecution(job *batchv1.Job, executionJobID string) bool {
	return job != nil && job.Labels[managedByLabel] == "coyote-ci" && job.Labels["coyote-ci.io/execution-job-id"] == executionJobID
}
func defaultNamespace(namespace string) string {
	if strings.TrimSpace(namespace) == "" {
		return "default"
	}
	return strings.TrimSpace(namespace)
}
func boolPtr(value bool) *bool { return &value }

func environment(values map[string]string) []corev1.EnvVar {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]corev1.EnvVar, 0, len(keys))
	for _, key := range keys {
		result = append(result, corev1.EnvVar{Name: key, Value: values[key]})
	}
	return result
}

func sanitizeLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			return r
		}
		return '-'
	}, value)
	return strings.Trim(value, "-.")
}

func newestPod(pods []corev1.Pod) corev1.Pod {
	sort.SliceStable(pods, func(i, j int) bool { return pods[i].CreationTimestamp.After(pods[j].CreationTimestamp.Time) })
	return pods[0]
}

func podResult(pod corev1.Pod, now time.Time) runner.RunStepResult {
	result := runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, StartedAt: pod.CreationTimestamp.Time, FinishedAt: now}
	if result.StartedAt.IsZero() {
		result.StartedAt = now
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != "build" || status.State.Terminated == nil {
			continue
		}
		terminated := status.State.Terminated
		result.ExitCode = int(terminated.ExitCode)
		result.Stderr = strings.TrimSpace(strings.Join([]string{terminated.Reason, terminated.Message}, ": "))
		if !terminated.StartedAt.IsZero() {
			result.StartedAt = terminated.StartedAt.Time
		}
		if !terminated.FinishedAt.IsZero() {
			result.FinishedAt = terminated.FinishedAt.Time
		}
		if terminated.ExitCode == 0 {
			result.Status = runner.RunStepStatusSuccess
		}
	}
	return result
}
