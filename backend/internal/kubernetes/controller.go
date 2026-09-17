// Package kubernetes provides the asynchronous Kubernetes execution backend.
package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

const (
	managedByLabel                = "app.kubernetes.io/managed-by"
	terminalLogChunkSize          = 32 * 1024
	cancellationCleanupInterval   = 30 * time.Second
	workspaceHelperTokenPath      = "/var/run/secrets/coyote/workspace/token"
	workspaceKubernetesTokenDir   = "/var/run/secrets/kubernetes.io/serviceaccount"
	cacheHelperRoot               = "/coyote-cache"
	cacheHelperStateRoot          = "/coyote-cache-state"
	buildEphemeralStorageRequest  = "3Gi"
	buildEphemeralStorageLimit    = "8Gi"
	buildCPURequest               = "2"
	buildCPULimit                 = "2"
	buildMemoryRequest            = "4Gi"
	buildMemoryLimit              = "4Gi"
	helperEphemeralStorageRequest = "1Gi"
	helperEphemeralStorageLimit   = "4Gi"
	commandTimeoutExitCode        = 124
	jobLifecycleAllowanceSeconds  = 15 * 60
	commandTimeoutToolsVolume     = "command-timeout-tools"
	commandTimeoutToolsPath       = "/coyote-tools/worker"
)

type WorkspaceHelperConfig struct {
	Image                  string
	InternalAPIURL         string
	ServiceAccountName     string
	CacheEnabled           bool
	ArtifactCollectEnabled bool
}

type Client interface {
	GetJob(context.Context, string, string) (*batchv1.Job, error)
	CreateJob(context.Context, string, *batchv1.Job) (*batchv1.Job, error)
	DeleteJob(context.Context, string, string) error
	ListJobs(context.Context, string, string) ([]batchv1.Job, error)
	ListPods(context.Context, string, string) ([]corev1.Pod, error)
	GetPodLogs(context.Context, string, string, string) (io.ReadCloser, error)
}

type executionService interface {
	ClaimRunnableStep(context.Context) (workersvc.WorkerRunnableStep, bool, error)
	ValidateKubernetesRunnableStep(context.Context, workersvc.WorkerRunnableStep) error
	RenewRunnableStepLease(context.Context, workersvc.WorkerRunnableStep) (bool, error)
	GetExecutionJob(context.Context, string) (domain.ExecutionJob, error)
	UpdateRunnableStepTiming(context.Context, workersvc.WorkerRunnableStep, domain.ExecutionTiming) (bool, error)
	CompleteKubernetesRunnableStep(context.Context, workersvc.WorkerRunnableStep, runner.RunStepResult) (repository.StepCompletionOutcome, error)
}

type imageBuildController interface {
	ReconcileClaimed(context.Context, workersvc.WorkerRunnableStep) (bool, error)
}

type Controller struct {
	client                      Client
	service                     executionService
	logSink                     logs.LogSink
	namespace                   string
	workspacePublicationEnabled bool
	workspaceHelper             WorkspaceHelperConfig
	testStepNodeNames           []string
	maxInFlightJobs             int
	active                      map[string]*workersvc.WorkerRunnableStep
	activeImageBuilds           map[string]*workersvc.WorkerRunnableStep
	imageBuildController        imageBuildController
	terminalLogsPersisted       map[string]bool
	lastCancellationCleanupAt   time.Time
	now                         func() time.Time
}

func (c *Controller) WithImageBuildController(controller imageBuildController) *Controller {
	c.imageBuildController = controller
	return c
}

func NewController(client Client, service executionService, logSink logs.LogSink, namespace string) *Controller {
	return &Controller{client: client, service: service, logSink: logSink, namespace: defaultNamespace(namespace), maxInFlightJobs: 1, active: map[string]*workersvc.WorkerRunnableStep{}, activeImageBuilds: map[string]*workersvc.WorkerRunnableStep{}, terminalLogsPersisted: map[string]bool{}, now: func() time.Time { return time.Now().UTC() }}
}

func (c *Controller) WithMaxInFlightJobs(maxInFlightJobs int) *Controller {
	if maxInFlightJobs < 1 {
		maxInFlightJobs = 1
	}
	c.maxInFlightJobs = maxInFlightJobs
	return c
}

// WithWorkspacePublicationEnabled prevents this initial backend from bypassing
// the durable workspace publication requirement used by Docker execution.
func (c *Controller) WithWorkspacePublicationEnabled(enabled bool) *Controller {
	c.workspacePublicationEnabled = enabled
	return c
}

func (c *Controller) WithWorkspaceHelper(config WorkspaceHelperConfig) *Controller {
	c.workspaceHelper = config
	c.workspacePublicationEnabled = true
	return c
}

// WithTestStepNodeNames pins sequential steps to Kubernetes nodes for local integration testing.
func (c *Controller) WithTestStepNodeNames(names []string) *Controller {
	c.testStepNodeNames = c.testStepNodeNames[:0]
	for _, name := range names {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			c.testStepNodeNames = append(c.testStepNodeNames, trimmed)
		}
	}
	return c
}

func (c *Controller) Reconcile(ctx context.Context) error {
	var reconcileErrors []error
	for _, step := range c.activeSteps() {
		if err := c.reconcileActive(ctx, step); err != nil {
			reconcileErrors = append(reconcileErrors, err)
		}
	}
	for _, step := range c.activeImageBuildSteps() {
		stillActive, err := c.imageBuildController.ReconcileClaimed(ctx, step)
		if !stillActive {
			delete(c.activeImageBuilds, step.JobID)
		}
		if err != nil {
			reconcileErrors = append(reconcileErrors, err)
		}
	}

	for c.inFlightCount() < c.maxInFlightJobs {
		step, found, err := c.service.ClaimRunnableStep(ctx)
		if err != nil {
			reconcileErrors = append(reconcileErrors, err)
			break
		}
		if !found {
			break
		}
		if err := c.reconcileClaimed(ctx, step); err != nil {
			reconcileErrors = append(reconcileErrors, err)
			break
		}
	}
	if cleanupErr := c.cleanupCanceledJobsIfDue(ctx); cleanupErr != nil {
		reconcileErrors = append(reconcileErrors, cleanupErr)
	}
	return errors.Join(reconcileErrors...)
}

func (c *Controller) reconcileClaimed(ctx context.Context, step workersvc.WorkerRunnableStep) error {
	if step.ExecutionKind == domain.ExecutionKindImageBuild {
		if c.imageBuildController == nil {
			return c.complete(ctx, step, runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, Stderr: "image build execution controller is not configured", StartedAt: c.now(), FinishedAt: c.now()})
		}
		c.activeImageBuilds[step.JobID] = &step
		stillActive, imageBuildErr := c.imageBuildController.ReconcileClaimed(ctx, step)
		if !stillActive {
			delete(c.activeImageBuilds, step.JobID)
		}
		return imageBuildErr
	}
	if c.workspacePublicationEnabled && (strings.TrimSpace(c.workspaceHelper.Image) == "" || strings.TrimSpace(c.workspaceHelper.InternalAPIURL) == "" || strings.TrimSpace(c.workspaceHelper.ServiceAccountName) == "") {
		return c.complete(ctx, step, runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, Stderr: "kubernetes workspace helper configuration is incomplete", StartedAt: c.now(), FinishedAt: c.now()})
	}
	if step.TimeoutSeconds > 0 && strings.TrimSpace(c.workspaceHelper.Image) == "" {
		return c.complete(ctx, step, runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, Stderr: "kubernetes command timeout requires a workspace helper image", StartedAt: c.now(), FinishedAt: c.now()})
	}
	if validationErr := c.service.ValidateKubernetesRunnableStep(ctx, step); validationErr != nil {
		if !workersvc.IsKubernetesExecutionCapabilityError(validationErr) {
			return validationErr
		}
		return c.complete(ctx, step, runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, Stderr: validationErr.Error(), StartedAt: c.now(), FinishedAt: c.now()})
	}
	c.active[step.JobID] = &step
	return c.reconcileActive(ctx, step)
}

func (c *Controller) inFlightCount() int {
	return len(c.active) + len(c.activeImageBuilds)
}

func (c *Controller) activeSteps() []workersvc.WorkerRunnableStep {
	steps := make([]workersvc.WorkerRunnableStep, 0, len(c.active))
	for _, step := range c.active {
		steps = append(steps, *step)
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].JobID < steps[j].JobID })
	return steps
}

func (c *Controller) activeImageBuildSteps() []workersvc.WorkerRunnableStep {
	steps := make([]workersvc.WorkerRunnableStep, 0, len(c.activeImageBuilds))
	for _, step := range c.activeImageBuilds {
		steps = append(steps, *step)
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].JobID < steps[j].JobID })
	return steps
}

func (c *Controller) reconcileActive(ctx context.Context, step workersvc.WorkerRunnableStep) error {
	durable, err := c.service.GetExecutionJob(ctx, step.JobID)
	if err != nil {
		return err
	}
	if durable.Status == domain.ExecutionJobStatusCanceled {
		delete(c.active, step.JobID)
		return c.deleteJob(ctx, jobName(step.JobID))
	}
	if domain.IsTerminalExecutionJobStatus(durable.Status) {
		delete(c.active, step.JobID)
		return nil
	}
	job, ensureErr := c.ensureJob(ctx, step)
	if ensureErr != nil {
		return ensureErr
	}
	c.recordTiming(ctx, step, durable, job)
	if terminal, result := c.terminalResult(ctx, job, step); terminal {
		logErr := c.collectTerminalLogs(ctx, step, job.Name)
		if logErr != nil {
			stdlog.Printf("WARN Kubernetes terminal log collection failed job=%s: %v", job.Name, logErr)
		}
		return c.complete(ctx, step, result)
	}
	continued, renewErr := c.service.RenewRunnableStepLease(ctx, step)
	if renewErr != nil {
		return renewErr
	}
	if !continued {
		refreshed, refreshErr := c.service.GetExecutionJob(ctx, step.JobID)
		delete(c.active, step.JobID)
		if refreshErr != nil {
			return refreshErr
		}
		if refreshed.Status == domain.ExecutionJobStatusCanceled {
			return c.deleteJob(ctx, jobName(step.JobID))
		}
	}
	return nil
}

func (c *Controller) recordTiming(ctx context.Context, step workersvc.WorkerRunnableStep, durable domain.ExecutionJob, job *batchv1.Job) {
	pods, err := c.client.ListPods(ctx, c.namespace, labels.Set{"job-name": job.Name}.String())
	if err != nil {
		return
	}
	var pod *corev1.Pod
	if len(pods) > 0 {
		selected := newestPod(pods)
		pod = &selected
	}
	if _, updateErr := c.service.UpdateRunnableStepTiming(ctx, step, executionTiming(durable, job, pod)); updateErr != nil {
		stdlog.Printf("DEBUG Kubernetes execution timing update failed job=%s: %v", step.JobID, updateErr)
	}
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
	created, createErr := c.client.CreateJob(ctx, c.namespace, buildJobWithNodeName(c.namespace, step, c.workspaceHelper, c.testStepNodeName(step.StepIndex)))
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
		result := terminalJobResult(condition, c.now())
		pods, listErr := c.client.ListPods(ctx, c.namespace, labels.Set{"job-name": job.Name}.String())
		if listErr == nil && len(pods) > 0 {
			result = podResult(newestPod(pods), c.now())
		}
		if condition.Type == batchv1.JobComplete {
			result.Status = runner.RunStepStatusSuccess
			result.ExitCode = 0
		}
		if condition.Type == batchv1.JobFailed {
			result.Status = runner.RunStepStatusFailed
			if condition.Reason == "DeadlineExceeded" {
				result.TimedOut = true
			}
			if result.ExitCode == commandTimeoutExitCode && step.TimeoutSeconds > 0 {
				result.TimedOut = true
				result.Stderr = fmt.Sprintf("step execution timed out after %ds", step.TimeoutSeconds)
			}
			if strings.TrimSpace(result.Stderr) == "" {
				result.Stderr = terminalJobFailureMessage(condition)
			}
		}
		return true, result
	}
	return false, runner.RunStepResult{}
}

func terminalJobResult(condition batchv1.JobCondition, now time.Time) runner.RunStepResult {
	result := runner.RunStepResult{Status: runner.RunStepStatusFailed, ExitCode: -1, StartedAt: now, FinishedAt: now}
	if condition.Type == batchv1.JobComplete {
		result.Status = runner.RunStepStatusSuccess
		result.ExitCode = 0
		return result
	}
	result.Stderr = terminalJobFailureMessage(condition)
	return result
}

func terminalJobFailureMessage(condition batchv1.JobCondition) string {
	details := strings.TrimSpace(strings.Join([]string{condition.Reason, condition.Message}, ": "))
	if details == "" {
		return "kubernetes job failed without a recoverable build container exit status"
	}
	return "kubernetes job failed: " + details
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
	if !buildContainerTerminated(pod) {
		return nil
	}
	stream, err := c.client.GetPodLogs(ctx, c.namespace, pod.Name, "build")
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
		delete(c.active, step.JobID)
		return nil
	}
	delete(c.active, step.JobID)
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
		if _, active := c.active[executionJobID]; active {
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

func buildJob(namespace string, step workersvc.WorkerRunnableStep, helpers ...WorkspaceHelperConfig) *batchv1.Job {
	return buildJobWithNodeName(namespace, step, WorkspaceHelperConfig{}, "", helpers...)
}

func buildJobWithNodeName(namespace string, step workersvc.WorkerRunnableStep, helper WorkspaceHelperConfig, nodeName string, helpers ...WorkspaceHelperConfig) *batchv1.Job {
	backoffLimit := int32(0)
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName(step.JobID), Namespace: namespace, Labels: executionLabels(step)}}
	job.Spec.BackoffLimit = &backoffLimit
	if step.TimeoutSeconds > 0 {
		deadline := int64(step.TimeoutSeconds + jobLifecycleAllowanceSeconds)
		job.Spec.ActiveDeadlineSeconds = &deadline
	}
	if len(helpers) > 0 {
		helper = helpers[0]
	}
	podSpec := corev1.PodSpec{
		RestartPolicy:                corev1.RestartPolicyNever,
		AutomountServiceAccountToken: boolPtr(false),
		Volumes:                      []corev1.Volume{{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
		Containers: []corev1.Container{{
			Name: "build", Image: step.Image, Command: []string{step.Command}, Args: append([]string(nil), step.Args...),
			Env: environment(step.Env), WorkingDir: workspace.ResolveVisibleWorkingDir(workspace.DefaultContainerRoot, step.WorkingDir),
			VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workspace.DefaultContainerRoot}},
			Resources:    buildContainerResources(),
		}},
	}
	podSpec.NodeName = strings.TrimSpace(nodeName)
	if strings.TrimSpace(helper.Image) != "" {
		podSpec.ServiceAccountName = helper.ServiceAccountName
		podSpec.Volumes = append(podSpec.Volumes, helperCapabilityVolume("workspace-prepare-token", workspaceHelperPrepareAudience), helperCapabilityVolume("workspace-publish-token", workspaceHelperPublishAudience), kubernetesAPIIdentityVolume())
		podSpec.InitContainers = []corev1.Container{workspacePrepareContainer(helper, step)}
		if step.TimeoutSeconds > 0 {
			podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{Name: commandTimeoutToolsVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
			podSpec.InitContainers = append([]corev1.Container{commandTimeoutInstallContainer(helper)}, podSpec.InitContainers...)
			podSpec.Containers[0] = commandTimeoutBuildContainer(podSpec.Containers[0], step.TimeoutSeconds)
		}
		podSpec.Containers = append(podSpec.Containers, workspacePublishContainer(helper, step))
		if helper.ArtifactCollectEnabled {
			podSpec.Volumes = append(podSpec.Volumes, helperCapabilityVolume("artifact-collect-token", workspaceHelperArtifactCollectAudience))
			podSpec.Containers = append(podSpec.Containers, artifactCollectContainer(helper, step))
		}
		if step.Cache != nil && helper.CacheEnabled {
			preset, presetErr := cachepkg.ResolvePreset(step.Cache.Preset, step.WorkingDir)
			if presetErr != nil {
				panic(fmt.Sprintf("validated Kubernetes cache preset: %v", presetErr))
			}
			podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{Name: "cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}, corev1.Volume{Name: "cache-helper-state", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}, helperCapabilityVolume("cache-restore-token", workspaceHelperCacheRestoreAudience), helperCapabilityVolume("cache-save-token", workspaceHelperCacheSaveAudience))
			podSpec.InitContainers = append(podSpec.InitContainers, cacheRestoreContainer(helper, step, preset))
			podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, cacheBuildMounts(preset)...)
			podSpec.Containers = append(podSpec.Containers, cacheSaveContainer(helper, step, preset))
		}
	}
	job.Spec.Template = corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: executionLabels(step), Annotations: executionAnnotations(step)},
		Spec:       podSpec,
	}
	return job
}

func cacheBuildMounts(preset cachepkg.Preset) []corev1.VolumeMount {
	mounts := make([]corev1.VolumeMount, 0, len(preset.CachePaths))
	for index, target := range preset.CachePaths {
		mounts = append(mounts, corev1.VolumeMount{Name: "cache", MountPath: target, SubPath: fmt.Sprintf("paths/%03d", index)})
	}
	return mounts
}

func cacheHelperEnvironment(config WorkspaceHelperConfig, step workersvc.WorkerRunnableStep, preset cachepkg.Preset, role domain.WorkspaceHelperRole) []corev1.EnvVar {
	env := append(workspaceHelperEnvironment(config, step), corev1.EnvVar{Name: "COYOTE_WORKSPACE_PATH", Value: workspace.DefaultContainerRoot}, corev1.EnvVar{Name: "COYOTE_WORKSPACE_HELPER_POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}}, corev1.EnvVar{Name: "COYOTE_WORKSPACE_HELPER_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}}, corev1.EnvVar{Name: "COYOTE_CACHE_ROOT", Value: cacheHelperRoot}, corev1.EnvVar{Name: "COYOTE_CACHE_STATE_ROOT", Value: cacheHelperStateRoot}, corev1.EnvVar{Name: "COYOTE_CACHE_PRESET", Value: preset.Name}, corev1.EnvVar{Name: "COYOTE_CACHE_POLICY", Value: string(domain.NormalizeCachePolicy(step.Cache.Policy))}, corev1.EnvVar{Name: "COYOTE_CACHE_WORKING_DIR", Value: step.WorkingDir}, corev1.EnvVar{Name: "COYOTE_CACHE_BUILD_IMAGE", Value: step.Image})
	if preset.Name == "go" {
		env = append(env, corev1.EnvVar{Name: "COYOTE_CACHE_COMPONENTS", Value: "split"})
	}
	return append(env, corev1.EnvVar{Name: "COYOTE_WORKSPACE_HELPER_ROLE", Value: string(role)})
}

func cacheRestoreContainer(config WorkspaceHelperConfig, step workersvc.WorkerRunnableStep, preset cachepkg.Preset) corev1.Container {
	return corev1.Container{Name: "cache-restore", Image: config.Image, ImagePullPolicy: corev1.PullAlways, Command: []string{"/app/worker", "cache", "restore"}, Env: cacheHelperEnvironment(config, step, preset, domain.WorkspaceHelperRoleCacheRestore), VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workspace.DefaultContainerRoot}, {Name: "cache", MountPath: cacheHelperRoot}, {Name: "cache-helper-state", MountPath: cacheHelperStateRoot}, {Name: "cache-restore-token", MountPath: "/var/run/secrets/coyote/workspace", ReadOnly: true}}, Resources: helperContainerResources()}
}

func cacheSaveContainer(config WorkspaceHelperConfig, step workersvc.WorkerRunnableStep, preset cachepkg.Preset) corev1.Container {
	return corev1.Container{Name: "cache-save", Image: config.Image, ImagePullPolicy: corev1.PullAlways, Command: []string{"/app/worker", "cache", "save-after-build"}, Env: cacheHelperEnvironment(config, step, preset, domain.WorkspaceHelperRoleCacheSave), VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workspace.DefaultContainerRoot}, {Name: "cache", MountPath: cacheHelperRoot}, {Name: "cache-helper-state", MountPath: cacheHelperStateRoot}, {Name: "cache-save-token", MountPath: "/var/run/secrets/coyote/workspace", ReadOnly: true}, {Name: "workspace-kubernetes-api", MountPath: workspaceKubernetesTokenDir, ReadOnly: true}}, Resources: helperContainerResources()}
}

func (c *Controller) testStepNodeName(stepIndex int) string {
	if stepIndex < 0 || stepIndex >= len(c.testStepNodeNames) {
		return ""
	}
	return c.testStepNodeNames[stepIndex]
}

func helperCapabilityVolume(name, audience string) corev1.Volume {
	return corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Audience: audience, Path: "token"}}}}}}
}

func kubernetesAPIIdentityVolume() corev1.Volume {
	return corev1.Volume{Name: "workspace-kubernetes-api", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token"}}, {ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}}, {DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}}}}}}}}
}

func workspaceHelperEnvironment(config WorkspaceHelperConfig, step workersvc.WorkerRunnableStep) []corev1.EnvVar {
	return []corev1.EnvVar{{Name: "COYOTE_INTERNAL_API_URL", Value: config.InternalAPIURL}, {Name: "COYOTE_WORKSPACE_HELPER_EXECUTION_JOB_ID", Value: step.JobID}, {Name: "COYOTE_WORKSPACE_HELPER_TOKEN_PATH", Value: workspaceHelperTokenPath}, {Name: "COYOTE_WORKSPACE_HELPER_POD_UID", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.uid"}}}}
}

func workspacePrepareContainer(config WorkspaceHelperConfig, step workersvc.WorkerRunnableStep) corev1.Container {
	env := append(workspaceHelperEnvironment(config, step), corev1.EnvVar{Name: "COYOTE_WORKSPACE_DESTINATION", Value: workspace.DefaultContainerRoot})
	return corev1.Container{Name: "workspace-prepare", Image: config.Image, ImagePullPolicy: corev1.PullAlways, Command: []string{"/app/worker", "workspace", "prepare"}, Env: env, VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workspace.DefaultContainerRoot}, {Name: "workspace-prepare-token", MountPath: "/var/run/secrets/coyote/workspace", ReadOnly: true}}, Resources: helperContainerResources()}
}

func commandTimeoutInstallContainer(config WorkspaceHelperConfig) corev1.Container {
	return corev1.Container{Name: "command-timeout-install", Image: config.Image, ImagePullPolicy: corev1.PullAlways, Command: []string{"/app/worker", "timeout", "install"}, VolumeMounts: []corev1.VolumeMount{{Name: commandTimeoutToolsVolume, MountPath: "/coyote-tools"}}, Resources: helperContainerResources()}
}

func commandTimeoutBuildContainer(container corev1.Container, timeoutSeconds int) corev1.Container {
	command := container.Command[0]
	container.Command = []string{commandTimeoutToolsPath}
	container.Args = append([]string{"timeout", fmt.Sprintf("%d", timeoutSeconds), command}, container.Args...)
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: commandTimeoutToolsVolume, MountPath: "/coyote-tools", ReadOnly: true})
	return container
}

func workspacePublishContainer(config WorkspaceHelperConfig, step workersvc.WorkerRunnableStep) corev1.Container {
	env := append(workspaceHelperEnvironment(config, step), corev1.EnvVar{Name: "COYOTE_WORKSPACE_PATH", Value: workspace.DefaultContainerRoot}, corev1.EnvVar{Name: "COYOTE_WORKSPACE_HELPER_POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}}, corev1.EnvVar{Name: "COYOTE_WORKSPACE_HELPER_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}})
	return corev1.Container{Name: "workspace-publish", Image: config.Image, ImagePullPolicy: corev1.PullAlways, Command: []string{"/app/worker", "workspace", "publish-after-build"}, Env: env, VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workspace.DefaultContainerRoot}, {Name: "workspace-publish-token", MountPath: "/var/run/secrets/coyote/workspace", ReadOnly: true}, {Name: "workspace-kubernetes-api", MountPath: workspaceKubernetesTokenDir, ReadOnly: true}}, Resources: helperContainerResources()}
}

func artifactCollectContainer(config WorkspaceHelperConfig, step workersvc.WorkerRunnableStep) corev1.Container {
	env := append(workspaceHelperEnvironment(config, step), corev1.EnvVar{Name: "COYOTE_WORKSPACE_PATH", Value: workspace.DefaultContainerRoot}, corev1.EnvVar{Name: "COYOTE_WORKSPACE_HELPER_POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}}, corev1.EnvVar{Name: "COYOTE_WORKSPACE_HELPER_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}})
	return corev1.Container{Name: "artifact-collect", Image: config.Image, ImagePullPolicy: corev1.PullAlways, Command: []string{"/app/worker", "artifact", "collect-after-build"}, Env: env, VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: workspace.DefaultContainerRoot}, {Name: "artifact-collect-token", MountPath: "/var/run/secrets/coyote/workspace", ReadOnly: true}, {Name: "workspace-kubernetes-api", MountPath: workspaceKubernetesTokenDir, ReadOnly: true}}, Resources: helperContainerResources()}
}

func buildContainerResources() corev1.ResourceRequirements {
	resources := ephemeralStorageResources(buildEphemeralStorageRequest, buildEphemeralStorageLimit)
	resources.Requests[corev1.ResourceCPU] = resource.MustParse(buildCPURequest)
	resources.Limits[corev1.ResourceCPU] = resource.MustParse(buildCPULimit)
	resources.Requests[corev1.ResourceMemory] = resource.MustParse(buildMemoryRequest)
	resources.Limits[corev1.ResourceMemory] = resource.MustParse(buildMemoryLimit)
	return resources
}

func helperContainerResources() corev1.ResourceRequirements {
	return ephemeralStorageResources(helperEphemeralStorageRequest, helperEphemeralStorageLimit)
}

func ephemeralStorageResources(request, limit string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse(request)}, Limits: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse(limit)}}
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
	if strings.EqualFold(strings.TrimSpace(pod.Status.Reason), "Evicted") {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == "build" && status.State.Terminated != nil {
				result.ExitCode = int(status.State.Terminated.ExitCode)
			}
		}
		message := strings.TrimSpace(pod.Status.Message)
		if message == "" {
			message = "kubernetes node eviction"
		}
		result.Stderr = "execution pod evicted: " + message
		return result
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == "workspace-prepare" && status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			result.Stderr = "workspace revision prepare: " + strings.TrimSpace(strings.Join([]string{status.State.Terminated.Reason, status.State.Terminated.Message}, ": "))
			return result
		}
		if status.Name == "cache-restore" && status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			result.ExitCode = int(status.State.Terminated.ExitCode)
			result.Stderr = "cache restore: " + strings.TrimSpace(strings.Join([]string{status.State.Terminated.Reason, status.State.Terminated.Message}, ": "))
			return result
		}
	}
	cacheSaveFailure := ""
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "cache-save" && status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			cacheSaveFailure = strings.TrimSpace(strings.Join([]string{status.State.Terminated.Reason, status.State.Terminated.Message}, ": "))
			continue
		}
		if status.Name == "workspace-publish" && status.State.Terminated != nil && status.State.Terminated.ExitCode != 0 {
			result.ExitCode = int(status.State.Terminated.ExitCode)
			result.Stderr = "workspace revision publish: " + strings.TrimSpace(strings.Join([]string{status.State.Terminated.Reason, status.State.Terminated.Message}, ": "))
			return result
		}
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
	if cacheSaveFailure != "" {
		stdlog.Printf("WARN Kubernetes cache save side effect failed pod=%s: %s", pod.Name, cacheSaveFailure)
	}
	return result
}

func buildContainerTerminated(pod corev1.Pod) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "build" && status.State.Terminated != nil {
			return true
		}
	}
	return false
}
