package build

// Core BuildService lifecycle and template tests.
// Execution-path tests live in build_service_execution_test.go.
// Pipeline and repo creation tests live in build_service_pipeline_repo_test.go.

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	steprunner "github.com/radiation/coyote-ci/backend/internal/runner"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

type fakeBuildVersionTagger struct {
	jobID string
	input versiontagsvc.CreateVersionTagsInput
	calls int
	err   error
}

type recordingBuildNotifier struct {
	builds []domain.Build
	err    error
}

func (n *recordingBuildNotifier) NotifyTerminalBuild(_ context.Context, build domain.Build) error {
	n.builds = append(n.builds, build)
	return n.err
}

func (f *fakeBuildVersionTagger) CreateVersionTags(_ context.Context, jobID string, input versiontagsvc.CreateVersionTagsInput) ([]domain.VersionTag, error) {
	f.calls++
	f.jobID = jobID
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	return []domain.VersionTag{}, nil
}

func TestGeneratedArtifactVersionConflictHelper(t *testing.T) {
	if isGeneratedArtifactVersionConflict(nil) {
		t.Fatal("expected nil error to not be a conflict")
	}
	if !isGeneratedArtifactVersionConflict(repository.ErrVersionTagConflict) {
		t.Fatal("expected repository conflict sentinel to be recognized")
	}
	wrapped := errors.New("generated version tagging failed for artifact path=\"dist/app\" name=\"\" declaration=\"dist/**\" resolved_version=\"0.1.7\" channel=\"latest\": version tag already exists for target")
	if !isGeneratedArtifactVersionConflict(wrapped) {
		t.Fatal("expected wrapped generated version conflict text to be recognized")
	}
	nonConflict := errors.New("generated channel tagging failed for artifact path=\"dist/app\": version tag already exists for target")
	if isGeneratedArtifactVersionConflict(nonConflict) {
		t.Fatal("expected non-version conflict text to be ignored")
	}
}

func TestMatchingArtifactDeclaration_BuildOnlyAndNoMatchBranches(t *testing.T) {
	buildDeclarations := []domain.ArtifactDeclaration{{
		Path:    "dist/*.tgz",
		Version: &domain.ArtifactVersionDeclaration{Template: "1.2.{build_number}", Channel: "latest"},
	}}
	stepDeclarations := map[int][]domain.ArtifactDeclaration{
		0: {{Path: "dist/app.tgz", Name: "pkg/app", Type: domain.ArtifactTypeNPMPackage}},
	}
	stepID := "step-1"
	item := domain.BuildArtifact{ID: "artifact-1", StepID: &stepID, LogicalPath: "dist/app.tgz"}
	declaration, ok := matchingArtifactDeclaration(item, map[string]int{"step-1": 0}, buildDeclarations, stepDeclarations)
	if !ok {
		t.Fatal("expected matching declaration")
	}
	if declaration.Name != "pkg/app" || declaration.Type != domain.ArtifactTypeNPMPackage || declaration.Version == nil || declaration.Version.Template != "1.2.{build_number}" {
		t.Fatalf("expected merged step/build declaration, got %#v", declaration)
	}

	buildOnly, ok := matchingArtifactDeclaration(domain.BuildArtifact{LogicalPath: "dist/other.tgz"}, nil, buildDeclarations, nil)
	if !ok || buildOnly.Version == nil || buildOnly.Version.Channel != "latest" {
		t.Fatalf("expected build-only declaration, got %#v ok=%v", buildOnly, ok)
	}

	if _, ok := matchingArtifactDeclaration(domain.BuildArtifact{LogicalPath: "reports/out.xml"}, nil, buildDeclarations, nil); ok {
		t.Fatal("expected no declaration match")
	}
}

type fakeBuildRepository struct {
	build         domain.Build
	steps         []domain.BuildStep
	createErr     error
	getErr        error
	updateErr     error
	updateCalls   int
	updatedID     string
	updatedStatus domain.BuildStatus
	updatedCommit string
}

type fakeAtomicCancelBuildRepository struct {
	*fakeBuildRepository
	updatedSteps          int
	cancelErr             error
	includesExecutionJobs bool
}

func (r *fakeAtomicCancelBuildRepository) CancelBuild(_ context.Context, _ string, reason string, canceledAt time.Time) (domain.Build, int, error) {
	if r.cancelErr != nil {
		return domain.Build{}, 0, r.cancelErr
	}
	r.build.Status = domain.BuildStatusCanceled
	r.build.FinishedAt = &canceledAt
	r.build.ErrorMessage = &reason
	return r.build, r.updatedSteps, nil
}

func (r *fakeAtomicCancelBuildRepository) CancelBuildIncludesExecutionJobs() bool {
	return r.includesExecutionJobs
}

type fakeExecutionJobCancelRepository struct {
	cancelCalls int
	updatedJobs int
	cancelErr   error
}

func (r *fakeExecutionJobCancelRepository) CreateJobsForBuild(_ context.Context, jobs []domain.ExecutionJob) ([]domain.ExecutionJob, error) {
	return jobs, nil
}

func (r *fakeExecutionJobCancelRepository) GetJobsByBuildID(_ context.Context, _ string) ([]domain.ExecutionJob, error) {
	return []domain.ExecutionJob{}, nil
}

func (r *fakeExecutionJobCancelRepository) GetJobByID(_ context.Context, _ string) (domain.ExecutionJob, error) {
	return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
}

func (r *fakeExecutionJobCancelRepository) GetJobByStepID(_ context.Context, _ string) (domain.ExecutionJob, error) {
	return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
}

func (r *fakeExecutionJobCancelRepository) ClaimNextRunnableJob(_ context.Context, _ repository.StepClaim) (domain.ExecutionJob, bool, error) {
	return domain.ExecutionJob{}, false, nil
}

func (r *fakeExecutionJobCancelRepository) ClaimJobByStepID(_ context.Context, _ string, _ repository.StepClaim) (domain.ExecutionJob, bool, error) {
	return domain.ExecutionJob{}, false, nil
}

func (r *fakeExecutionJobCancelRepository) RenewJobLease(_ context.Context, _ string, _ string, _ time.Time) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, nil
}

func (r *fakeExecutionJobCancelRepository) CompleteJobSuccess(_ context.Context, _ string, _ string, _ time.Time, _ int, _ []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, nil
}

func (r *fakeExecutionJobCancelRepository) CompleteJobFailure(_ context.Context, _ string, _ string, _ time.Time, _ string, _ *int, _ []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return domain.ExecutionJob{}, repository.StepCompletionInvalidTransition, nil
}

func (r *fakeExecutionJobCancelRepository) CancelJobsForBuild(_ context.Context, _ string, _ string, _ time.Time) (int, error) {
	r.cancelCalls++
	if r.cancelErr != nil {
		return 0, r.cancelErr
	}
	return r.updatedJobs, nil
}

func (r *fakeBuildRepository) Create(_ context.Context, build domain.Build) (domain.Build, error) {
	if r.createErr != nil {
		return domain.Build{}, r.createErr
	}

	build = domain.NormalizeBuildMetadata(build)
	r.build = build
	return build, nil
}

func (r *fakeBuildRepository) CreateQueuedBuild(_ context.Context, build domain.Build, steps []domain.BuildStep) (domain.Build, error) {
	if r.createErr != nil {
		return domain.Build{}, r.createErr
	}

	build.Status = domain.BuildStatusQueued
	build = domain.NormalizeBuildMetadata(build)
	r.build = build
	r.steps = append([]domain.BuildStep(nil), steps...)

	return build, nil
}

func (r *fakeBuildRepository) List(_ context.Context) ([]domain.Build, error) {
	if r.build.ID == "" {
		return []domain.Build{}, nil
	}

	return []domain.Build{r.build}, nil
}

func (r *fakeBuildRepository) ListActive(ctx context.Context) ([]domain.Build, error) {
	builds, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	active := make([]domain.Build, 0, len(builds))
	for _, build := range builds {
		if build.Status != domain.BuildStatusPreparing && build.Status != domain.BuildStatusQueued && build.Status != domain.BuildStatusRunning {
			continue
		}
		active = append(active, build)
	}
	return active, nil
}

func (r *fakeBuildRepository) ListByJobID(_ context.Context, jobID string) ([]domain.Build, error) {
	if r.build.ID == "" {
		return []domain.Build{}, nil
	}
	if r.build.JobID != nil && *r.build.JobID == jobID {
		return []domain.Build{r.build}, nil
	}
	return []domain.Build{}, nil
}

func (r *fakeBuildRepository) ListLatestByJobIDs(_ context.Context, jobIDs []string) (map[string]domain.Build, error) {
	latest := map[string]domain.Build{}
	if r.build.ID == "" || r.build.JobID == nil {
		return latest, nil
	}
	for _, jobID := range jobIDs {
		if *r.build.JobID == jobID {
			latest[jobID] = r.build
		}
	}
	return latest, nil
}

func (r *fakeBuildRepository) ListPaged(ctx context.Context, _ repository.ListParams) ([]domain.Build, error) {
	return r.List(ctx)
}

func (r *fakeBuildRepository) ListQueue(ctx context.Context, _ repository.QueueListParams) ([]domain.QueueEntry, error) {
	builds, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]domain.QueueEntry, 0, len(builds))
	for _, build := range builds {
		entries = append(entries, domain.QueueEntry{Build: build})
	}
	return entries, nil
}

func (r *fakeBuildRepository) GetByID(_ context.Context, _ string) (domain.Build, error) {
	if r.getErr != nil {
		return domain.Build{}, r.getErr
	}

	return r.build, nil
}

func (r *fakeBuildRepository) UpdateStatus(_ context.Context, id string, status domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	r.updateCalls++
	r.updatedID = id
	r.updatedStatus = status

	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	r.build.Status = status
	r.build.ErrorMessage = errorMessage
	return r.build, nil
}

func (r *fakeBuildRepository) QueueBuild(_ context.Context, id string, steps []domain.BuildStep) (domain.Build, error) {
	r.updateCalls++
	r.updatedID = id
	r.updatedStatus = domain.BuildStatusQueued

	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	r.build.Status = domain.BuildStatusQueued
	r.steps = append([]domain.BuildStep(nil), steps...)

	return r.build, nil
}

func (r *fakeBuildRepository) UpdateSourceCommitSHA(_ context.Context, id string, commitSHA string) (domain.Build, error) {
	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	if r.build.ID != "" && r.build.ID != id {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	trimmed := strings.TrimSpace(commitSHA)
	r.updatedCommit = trimmed
	if trimmed == "" {
		r.build.CommitSHA = nil
	} else {
		r.build.CommitSHA = &trimmed
	}
	r.build.Source = domain.NewSourceSpec(buildReadOptionalString(r.build.RepoURL), buildReadOptionalString(r.build.Ref), buildReadOptionalString(r.build.CommitSHA))
	return r.build, nil
}

func (r *fakeBuildRepository) UpdateImageExecution(_ context.Context, id string, requestedRef *string, resolvedRef *string, sourceKind domain.ImageSourceKind, managedImageID *string, managedImageVersionID *string) (domain.Build, error) {
	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	if r.build.ID != "" && r.build.ID != id {
		return domain.Build{}, repository.ErrBuildNotFound
	}

	r.build.RequestedImageRef = requestedRef
	r.build.ResolvedImageRef = resolvedRef
	r.build.ImageSourceKind = sourceKind
	r.build.ManagedImageID = managedImageID
	r.build.ManagedImageVersionID = managedImageVersionID

	return r.build, nil
}

func (r *fakeBuildRepository) GetStepsByBuildID(_ context.Context, _ string) ([]domain.BuildStep, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}

	steps := make([]domain.BuildStep, len(r.steps))
	copy(steps, r.steps)
	return steps, nil
}

func (r *fakeBuildRepository) ClaimStepIfPending(_ context.Context, _ string, stepIndex int, _ *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, false, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}
		if r.steps[i].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}
		r.steps[i].Status = domain.BuildStepStatusRunning
		r.steps[i].StartedAt = &startedAt
		return r.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) ClaimPendingStep(_ context.Context, _ string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, false, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}
		if r.steps[i].Status != domain.BuildStepStatusPending {
			return domain.BuildStep{}, false, nil
		}
		r.steps[i].Status = domain.BuildStepStatusRunning
		r.steps[i].WorkerID = &claim.WorkerID
		r.steps[i].ClaimToken = &claim.ClaimToken
		r.steps[i].ClaimedAt = &claim.ClaimedAt
		r.steps[i].LeaseExpiresAt = &claim.LeaseExpiresAt
		r.steps[i].StartedAt = &claim.ClaimedAt
		return r.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) ReclaimExpiredStep(_ context.Context, _ string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, false, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}
		if r.steps[i].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, false, nil
		}
		if r.steps[i].LeaseExpiresAt == nil || r.steps[i].LeaseExpiresAt.After(reclaimBefore) {
			return domain.BuildStep{}, false, nil
		}
		r.steps[i].WorkerID = &claim.WorkerID
		r.steps[i].ClaimToken = &claim.ClaimToken
		r.steps[i].ClaimedAt = &claim.ClaimedAt
		r.steps[i].LeaseExpiresAt = &claim.LeaseExpiresAt
		return r.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) RenewStepLease(_ context.Context, _ string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, repository.StepCompletionOutcome, error) {
	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}
		if domain.IsTerminalStepStatus(r.steps[i].Status) {
			return r.steps[i], repository.StepCompletionDuplicateTerminal, nil
		}
		if r.steps[i].Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, repository.StepCompletionInvalidTransition, nil
		}
		if r.steps[i].ClaimToken == nil || *r.steps[i].ClaimToken != claimToken {
			return r.steps[i], repository.StepCompletionStaleClaim, nil
		}
		r.steps[i].LeaseExpiresAt = &leaseExpiresAt
		return r.steps[i], repository.StepCompletionCompleted, nil
	}

	return domain.BuildStep{}, repository.StepCompletionInvalidTransition, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) UpdateStepByIndex(_ context.Context, _ string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}

		r.steps[i].Status = update.Status
		if update.ExitCode != nil {
			r.steps[i].ExitCode = update.ExitCode
		}
		if update.Stdout != nil {
			r.steps[i].Stdout = update.Stdout
		}
		if update.Stderr != nil {
			r.steps[i].Stderr = update.Stderr
		}
		if update.StartedAt != nil {
			r.steps[i].StartedAt = update.StartedAt
		}
		if update.FinishedAt != nil {
			r.steps[i].FinishedAt = update.FinishedAt
		}
		return r.steps[i], nil
	}

	return domain.BuildStep{}, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) CompleteStepIfRunning(_ context.Context, _ string, stepIndex int, update repository.StepUpdate) (domain.BuildStep, bool, error) {
	if r.updateErr != nil {
		return domain.BuildStep{}, false, r.updateErr
	}

	for i := range r.steps {
		if r.steps[i].StepIndex != stepIndex {
			continue
		}

		if r.steps[i].Status != domain.BuildStepStatusRunning {
			return r.steps[i], false, nil
		}

		r.steps[i].Status = update.Status
		if update.ExitCode != nil {
			r.steps[i].ExitCode = update.ExitCode
		}
		if update.Stdout != nil {
			r.steps[i].Stdout = update.Stdout
		}
		if update.Stderr != nil {
			r.steps[i].Stderr = update.Stderr
		}
		if update.ErrorMessage != nil {
			r.steps[i].ErrorMessage = update.ErrorMessage
		} else if update.Status == domain.BuildStepStatusSuccess {
			r.steps[i].ErrorMessage = nil
		}
		if update.StartedAt != nil {
			r.steps[i].StartedAt = update.StartedAt
		}
		if update.FinishedAt != nil {
			r.steps[i].FinishedAt = update.FinishedAt
		}

		return r.steps[i], true, nil
	}

	return domain.BuildStep{}, false, repository.ErrBuildNotFound
}

func (r *fakeBuildRepository) CompleteStep(_ context.Context, request repository.CompleteStepRequest) (repository.CompleteStepResult, error) {
	buildID := request.BuildID
	stepIndex := request.StepIndex
	update := request.Update

	if request.RequireClaim {
		for i := range r.steps {
			if r.steps[i].StepIndex != stepIndex {
				continue
			}

			if domain.IsTerminalStepStatus(r.steps[i].Status) {
				return repository.CompleteStepResult{Step: r.steps[i], Outcome: repository.StepCompletionDuplicateTerminal}, nil
			}
			if r.steps[i].Status != domain.BuildStepStatusRunning {
				return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
			}
			if r.steps[i].ClaimToken == nil || *r.steps[i].ClaimToken != request.ClaimToken {
				return repository.CompleteStepResult{Step: r.steps[i], Outcome: repository.StepCompletionStaleClaim}, nil
			}

			break
		}
	}

	step, completed, err := r.CompleteStepIfRunning(context.Background(), buildID, stepIndex, update)
	if err != nil {
		return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, err
	}
	if !completed {
		if domain.IsTerminalStepStatus(step.Status) {
			return repository.CompleteStepResult{Step: step, Outcome: repository.StepCompletionDuplicateTerminal}, nil
		}
		if step.ID == "" && step.Name == "" {
			return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, repository.ErrBuildNotFound
		}
		return repository.CompleteStepResult{Outcome: repository.StepCompletionInvalidTransition}, nil
	}

	if update.Status == domain.BuildStepStatusFailed {
		r.build.Status = domain.BuildStatusFailed
		r.build.ErrorMessage = step.ErrorMessage
		return repository.CompleteStepResult{Step: step, Outcome: repository.StepCompletionCompleted}, nil
	}

	nextIndex := stepIndex + 1
	if nextIndex > r.build.CurrentStepIndex {
		r.build.CurrentStepIndex = nextIndex
	}

	hasNext := false
	for idx := range r.steps {
		if r.steps[idx].StepIndex > stepIndex {
			hasNext = true
			break
		}
	}

	if !hasNext {
		r.build.Status = domain.BuildStatusSuccess
		r.build.ErrorMessage = nil
	}

	return repository.CompleteStepResult{Step: step, Outcome: repository.StepCompletionCompleted}, nil
}

func (r *fakeBuildRepository) UpdateCurrentStepIndex(_ context.Context, _ string, currentStepIndex int) (domain.Build, error) {
	if r.updateErr != nil {
		return domain.Build{}, r.updateErr
	}

	r.build.CurrentStepIndex = currentStepIndex
	return r.build, nil
}

type fakeRunner struct {
	result      steprunner.RunStepResult
	err         error
	called      bool
	lastRequest steprunner.RunStepRequest
}

func (r *fakeRunner) RunStep(_ context.Context, request steprunner.RunStepRequest) (steprunner.RunStepResult, error) {
	r.called = true
	r.lastRequest = request
	if r.err != nil {
		return steprunner.RunStepResult{}, r.err
	}
	return r.result, nil
}

type fakeBuildScopedRunner struct {
	fakeRunner
	prepareCalls int
	cleanupCalls int
	lastPrepare  steprunner.PrepareBuildRequest
	prepareErr   error
	cleanupErr   error
	onCleanup    func()
}

func (r *fakeBuildScopedRunner) PrepareBuild(_ context.Context, request steprunner.PrepareBuildRequest) error {
	r.prepareCalls++
	r.lastPrepare = request
	return r.prepareErr
}

func (r *fakeBuildScopedRunner) CleanupBuild(_ context.Context, _ string) error {
	r.cleanupCalls++
	if r.onCleanup != nil {
		r.onCleanup()
	}
	return r.cleanupErr
}

func (r *fakeBuildScopedRunner) RunStepStream(ctx context.Context, request steprunner.RunStepRequest, _ steprunner.StepOutputCallback) (steprunner.RunStepResult, error) {
	return r.RunStep(ctx, request)
}

type fakeWorkspaceSourceResolver struct {
	cloneErr       error
	checkoutErr    error
	resolvedCommit string
	cloneCalls     int
	checkoutCalls  int
	lastWorkspace  string
	lastRepoURL    string
	lastSpec       source.WorkspaceSourceSpec
}

func (r *fakeWorkspaceSourceResolver) CloneIntoWorkspace(_ context.Context, workspacePath string, repositoryURL string) error {
	r.cloneCalls++
	r.lastWorkspace = workspacePath
	r.lastRepoURL = repositoryURL
	if r.cloneErr != nil {
		return r.cloneErr
	}
	return nil
}

func (r *fakeWorkspaceSourceResolver) CheckoutWorkspaceSource(_ context.Context, workspacePath string, spec source.WorkspaceSourceSpec) (string, error) {
	r.checkoutCalls++
	r.lastWorkspace = workspacePath
	r.lastSpec = spec
	if r.checkoutErr != nil {
		return "", r.checkoutErr
	}
	if strings.TrimSpace(r.resolvedCommit) == "" {
		return "abc123def456", nil
	}
	return strings.TrimSpace(r.resolvedCommit), nil
}

type fakeLogSink struct {
	err    error
	calls  int
	lines  []string
	builds []string
	steps  []string
}

type fakeArtifactRepository struct {
	artifacts map[string][]domain.BuildArtifact
	onCreate  func(domain.BuildArtifact)
}

func (r *fakeArtifactRepository) Create(_ context.Context, artifact domain.BuildArtifact) (domain.BuildArtifact, error) {
	if r.artifacts == nil {
		r.artifacts = map[string][]domain.BuildArtifact{}
	}
	r.artifacts[artifact.BuildID] = append(r.artifacts[artifact.BuildID], artifact)
	if r.onCreate != nil {
		r.onCreate(artifact)
	}
	return artifact, nil
}

func (r *fakeArtifactRepository) ListByBuildID(_ context.Context, buildID string) ([]domain.BuildArtifact, error) {
	items := r.artifacts[buildID]
	out := make([]domain.BuildArtifact, len(items))
	copy(out, items)
	return out, nil
}

func (r *fakeArtifactRepository) Browse(_ context.Context, params repository.BrowseArtifactsParams) ([]domain.ArtifactBrowseRecord, error) {
	trimmedQuery := strings.TrimSpace(strings.ToLower(params.Query))
	out := make([]domain.ArtifactBrowseRecord, 0)
	for buildID, items := range r.artifacts {
		for _, item := range items {
			if trimmedQuery != "" && !strings.Contains(strings.ToLower(item.LogicalPath), trimmedQuery) {
				continue
			}
			out = append(out, domain.ArtifactBrowseRecord{
				Artifact: item,
				Build: domain.Build{
					ID: buildID,
				},
			})
		}
	}
	grouped := domain.GroupArtifacts(out)
	start := params.Offset
	if start > len(grouped) {
		start = len(grouped)
	}
	end := len(grouped)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}
	grouped = grouped[start:end]
	var paged []domain.ArtifactBrowseRecord
	for _, item := range grouped {
		for _, v := range item.Versions {
			paged = append(paged, domain.ArtifactBrowseRecord(v))
		}
	}
	return paged, nil
}

func (r *fakeArtifactRepository) ListCatalog(_ context.Context, params repository.ArtifactCatalogParams) ([]domain.ArtifactRecord, error) {
	trimmedQuery := strings.TrimSpace(strings.ToLower(params.Query))
	out := make([]domain.ArtifactRecord, 0)
	for buildID, items := range r.artifacts {
		if params.BuildID != "" && buildID != params.BuildID {
			continue
		}
		for _, item := range items {
			if trimmedQuery != "" && !strings.Contains(strings.ToLower(item.LogicalPath), trimmedQuery) {
				continue
			}
			out = append(out, domain.ArtifactRecord{
				Artifact: item,
				Build: domain.Build{
					ID: buildID,
				},
			})
		}
	}
	start := params.Offset
	if start > len(out) {
		start = len(out)
	}
	end := len(out)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}
	return out[start:end], nil
}

func (r *fakeArtifactRepository) GetCatalogByID(_ context.Context, artifactID string) (domain.ArtifactRecord, error) {
	for buildID, items := range r.artifacts {
		for _, item := range items {
			if item.ID == artifactID {
				return domain.ArtifactRecord{
					Artifact: item,
					Build: domain.Build{
						ID: buildID,
					},
				}, nil
			}
		}
	}
	return domain.ArtifactRecord{}, repository.ErrArtifactNotFound
}

func (r *fakeArtifactRepository) GetByID(_ context.Context, buildID string, artifactID string) (domain.BuildArtifact, error) {
	for _, item := range r.artifacts[buildID] {
		if item.ID == artifactID {
			return item, nil
		}
	}
	return domain.BuildArtifact{}, repository.ErrArtifactNotFound
}

func (r *fakeArtifactRepository) ListByStepID(_ context.Context, stepID string) ([]domain.BuildArtifact, error) {
	var out []domain.BuildArtifact
	for _, items := range r.artifacts {
		for _, item := range items {
			if item.StepID != nil && *item.StepID == stepID {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

type recordingStore struct {
	events *[]string
}

func (s *recordingStore) Save(_ context.Context, key string, src io.Reader) (int64, error) {
	body, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	*s.events = append(*s.events, "save:"+key)
	return int64(len(body)), nil
}

func (s *recordingStore) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type failingStore struct {
	events *[]string
	err    error
}

func (s *failingStore) Save(_ context.Context, key string, _ io.Reader) (int64, error) {
	if s.events != nil {
		*s.events = append(*s.events, "save:"+key)
	}
	return 0, s.err
}

func (s *failingStore) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, s.err
}

func (s *fakeLogSink) WriteStepLog(_ context.Context, buildID string, stepName string, line string) error {
	if s.err != nil {
		return s.err
	}
	s.calls++
	s.builds = append(s.builds, buildID)
	s.steps = append(s.steps, stepName)
	s.lines = append(s.lines, line)
	return nil
}

func TestNewBuildService(t *testing.T) {
	repo := &fakeBuildRepository{}
	svc := NewBuildService(repo, nil, nil)

	if svc == nil {
		t.Fatal("expected service instance, got nil")
	}
}

func TestBuildService_CreateBuild(t *testing.T) {
	tests := []struct {
		name        string
		input       CreateBuildInput
		repo        *fakeBuildRepository
		expectErr   error
		errContains string
	}{
		{
			name:      "missing project id",
			input:     CreateBuildInput{},
			repo:      &fakeBuildRepository{},
			expectErr: ErrProjectIDRequired,
		},
		{
			name:        "repository create fails",
			input:       CreateBuildInput{ProjectID: "project-1"},
			repo:        &fakeBuildRepository{createErr: errors.New("create failed")},
			errContains: "create failed",
		},
		{
			name:  "success",
			input: CreateBuildInput{ProjectID: "project-1"},
			repo:  &fakeBuildRepository{},
		},
		{
			name:  "priority zero defaults to five",
			input: CreateBuildInput{ProjectID: "project-1", Priority: 0},
			repo:  &fakeBuildRepository{},
		},
		{
			name:      "priority above range rejected",
			input:     CreateBuildInput{ProjectID: "project-1", Priority: domain.MaxPriority + 1},
			repo:      &fakeBuildRepository{},
			expectErr: ErrBuildPriorityOutOfRange,
		},
		{
			name:      "priority below range rejected",
			input:     CreateBuildInput{ProjectID: "project-1", Priority: -1},
			repo:      &fakeBuildRepository{},
			expectErr: ErrBuildPriorityOutOfRange,
		},
		{
			name:      "source missing repository url",
			input:     CreateBuildInput{ProjectID: "project-1", Source: &CreateBuildSourceInput{Ref: "main"}},
			repo:      &fakeBuildRepository{},
			expectErr: ErrRepoURLRequired,
		},
		{
			name:      "source missing ref and commit",
			input:     CreateBuildInput{ProjectID: "project-1", Source: &CreateBuildSourceInput{RepositoryURL: "https://github.com/org/repo.git"}},
			repo:      &fakeBuildRepository{},
			expectErr: ErrSourceTargetRequired,
		},
		{
			name: "source accepted",
			input: CreateBuildInput{ProjectID: "project-1", Source: &CreateBuildSourceInput{
				RepositoryURL: "https://github.com/org/repo.git",
				Ref:           "main",
			}},
			repo: &fakeBuildRepository{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewBuildService(tc.repo, nil, nil)

			build, err := svc.CreateBuild(context.Background(), tc.input)
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
				return
			}

			if tc.errContains != "" {
				if err == nil || err.Error() != tc.errContains {
					t.Fatalf("expected error %q, got %v", tc.errContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if build.ID == "" {
				t.Fatal("expected generated build id")
			}

			if build.ProjectID != tc.input.ProjectID {
				t.Fatalf("expected project id %q, got %q", tc.input.ProjectID, build.ProjectID)
			}

			if build.Status != domain.BuildStatusPending {
				t.Fatalf("expected status %q, got %q", domain.BuildStatusPending, build.Status)
			}

			if build.Priority != domain.DefaultPriority {
				t.Fatalf("expected priority %d, got %d", domain.DefaultPriority, build.Priority)
			}

			if build.CreatedAt.IsZero() {
				t.Fatal("expected created_at to be set")
			}

			if build.CreatedAt.Location() != time.UTC {
				t.Fatal("expected created_at to be UTC")
			}

			if tc.input.Source != nil {
				if build.Source == nil {
					t.Fatal("expected build source to be persisted")
				}
				if build.Source.RepositoryURL != tc.input.Source.RepositoryURL {
					t.Fatalf("expected source repository %q, got %q", tc.input.Source.RepositoryURL, build.Source.RepositoryURL)
				}
			}
		})
	}
}

func TestBuildService_CreateBuild_WithStepsAutoQueues(t *testing.T) {
	repo := &fakeBuildRepository{}
	svc := NewBuildService(repo, nil, nil)

	build, err := svc.CreateBuild(context.Background(), CreateBuildInput{
		ProjectID: "project-1",
		Steps: []CreateBuildStepInput{
			{Name: "checkout", Command: "git", Args: []string{"checkout", "."}, Env: map[string]string{"A": "1"}, WorkingDir: "/workspace", TimeoutSeconds: 120},
			{Name: "test"},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if build.Status != domain.BuildStatusQueued {
		t.Fatalf("expected queued status, got %q", build.Status)
	}
	if len(repo.steps) != 2 {
		t.Fatalf("expected 2 persisted steps, got %d", len(repo.steps))
	}
	if repo.steps[0].StepIndex != 0 || repo.steps[0].Name != "checkout" {
		t.Fatalf("expected first step checkout@0, got %s@%d", repo.steps[0].Name, repo.steps[0].StepIndex)
	}
	if repo.steps[0].Command != "git" {
		t.Fatalf("expected first step command git, got %q", repo.steps[0].Command)
	}
	if len(repo.steps[0].Args) != 2 || repo.steps[0].Args[0] != "checkout" {
		t.Fatalf("expected first step args to be persisted, got %+v", repo.steps[0].Args)
	}
	if repo.steps[0].WorkingDir != "/workspace" {
		t.Fatalf("expected first step working dir /workspace, got %q", repo.steps[0].WorkingDir)
	}
	if repo.steps[0].TimeoutSeconds != 120 {
		t.Fatalf("expected first step timeout 120, got %d", repo.steps[0].TimeoutSeconds)
	}
}

func TestBuildService_CreateBuild_DefaultsManualTrigger(t *testing.T) {
	repo := &fakeBuildRepository{}
	svc := NewBuildService(repo, nil, nil)

	build, err := svc.CreateBuild(context.Background(), CreateBuildInput{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if build.Trigger.Kind != domain.BuildTriggerKindManual {
		t.Fatalf("expected manual trigger kind, got %q", build.Trigger.Kind)
	}
	if build.TriggerType != domain.BuildTriggerTypeManual {
		t.Fatalf("expected manual trigger type, got %q", build.TriggerType)
	}
	if build.Trigger.SCMProvider != nil {
		t.Fatalf("expected nil scm_provider for manual build, got %v", build.Trigger.SCMProvider)
	}
	if build.SourceRef != nil || build.SourceSHA != nil || build.TriggeredBy != nil {
		t.Fatalf("expected nil build metadata for local manual build, got source_ref=%v source_sha=%v triggered_by=%v", build.SourceRef, build.SourceSHA, build.TriggeredBy)
	}
}

func TestBuildService_CreateBuild_PopulatesSourceMetadata(t *testing.T) {
	repo := &fakeBuildRepository{}
	svc := NewBuildService(repo, nil, nil)

	build, err := svc.CreateBuild(context.Background(), CreateBuildInput{
		ProjectID: "project-1",
		Source: &CreateBuildSourceInput{
			RepositoryURL: "https://github.com/acme/repo.git",
			Ref:           "main",
			CommitSHA:     "abc123",
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if build.SourceRef == nil || *build.SourceRef != "main" {
		t.Fatalf("expected source_ref main, got %v", build.SourceRef)
	}
	if build.SourceSHA == nil || *build.SourceSHA != "abc123" {
		t.Fatalf("expected source_sha abc123, got %v", build.SourceSHA)
	}
	if build.TriggerType != domain.BuildTriggerTypeManual {
		t.Fatalf("expected manual trigger type, got %q", build.TriggerType)
	}
}

func TestBuildService_GetBuild(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name      string
		repo      *fakeBuildRepository
		buildID   string
		expectErr error
	}{
		{
			name: "success",
			repo: &fakeBuildRepository{build: domain.Build{
				ID:        "build-1",
				ProjectID: "project-1",
				Status:    domain.BuildStatusRunning,
				CreatedAt: now,
			}},
			buildID: "build-1",
		},
		{
			name:      "not found",
			repo:      &fakeBuildRepository{getErr: repository.ErrBuildNotFound},
			buildID:   "missing",
			expectErr: ErrBuildNotFound,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewBuildService(tc.repo, nil, nil)
			build, err := svc.GetBuild(context.Background(), tc.buildID)

			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if build.ID != tc.repo.build.ID {
				t.Fatalf("expected build id %q, got %q", tc.repo.build.ID, build.ID)
			}
		})
	}
}

func TestBuildService_ListBuilds(t *testing.T) {
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending}}
	svc := NewBuildService(repo, nil, nil)

	builds, err := svc.ListBuilds(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("expected one build, got %d", len(builds))
	}
	if builds[0].ID != "build-1" {
		t.Fatalf("expected build-1 id, got %q", builds[0].ID)
	}
}

func TestBuildService_ValidTransitions(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name                string
		initialStatus       domain.BuildStatus
		action              func(*BuildService, context.Context, string) (domain.Build, error)
		expectedStatus      domain.BuildStatus
		expectedUpdateCalls int
	}{
		{
			name:                "pending to queued",
			initialStatus:       domain.BuildStatusPending,
			action:              (*BuildService).QueueBuild,
			expectedStatus:      domain.BuildStatusQueued,
			expectedUpdateCalls: 1,
		},
		{
			name:           "queued to running",
			initialStatus:  domain.BuildStatusQueued,
			action:         (*BuildService).StartBuild,
			expectedStatus: domain.BuildStatusRunning,
			// StartBuild now calls PrepareBuildExecution: queued→preparing, preparing→running.
			expectedUpdateCalls: 2,
		},
		{
			name:                "running to success",
			initialStatus:       domain.BuildStatusRunning,
			action:              (*BuildService).CompleteBuild,
			expectedStatus:      domain.BuildStatusSuccess,
			expectedUpdateCalls: 1,
		},
		{
			name:                "running to failed",
			initialStatus:       domain.BuildStatusRunning,
			action:              (*BuildService).FailBuild,
			expectedStatus:      domain.BuildStatusFailed,
			expectedUpdateCalls: 1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeBuildRepository{
				build: domain.Build{
					ID:        "build-1",
					ProjectID: "project-1",
					Status:    tc.initialStatus,
					CreatedAt: now,
				},
			}

			svc := NewBuildService(repo, nil, nil)

			updated, err := tc.action(svc, context.Background(), "build-1")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if updated.Status != tc.expectedStatus {
				t.Fatalf("expected status %q, got %q", tc.expectedStatus, updated.Status)
			}

			if repo.updateCalls != tc.expectedUpdateCalls {
				t.Fatalf("expected UpdateStatus to be called %d times, got %d", tc.expectedUpdateCalls, repo.updateCalls)
			}

			if repo.updatedID != "build-1" {
				t.Fatalf("expected UpdateStatus id %q, got %q", "build-1", repo.updatedID)
			}

			if repo.updatedStatus != tc.expectedStatus {
				t.Fatalf("expected UpdateStatus status %q, got %q", tc.expectedStatus, repo.updatedStatus)
			}
		})
	}
}

func TestBuildService_InvalidTransitions(t *testing.T) {
	tests := []struct {
		name          string
		initialStatus domain.BuildStatus
		action        func(*BuildService, context.Context, string) (domain.Build, error)
	}{
		{
			name:          "running to queued is invalid",
			initialStatus: domain.BuildStatusRunning,
			action:        (*BuildService).QueueBuild,
		},
		{
			name:          "pending to running is invalid",
			initialStatus: domain.BuildStatusPending,
			action:        (*BuildService).StartBuild,
		},
		{
			name:          "pending to success is invalid",
			initialStatus: domain.BuildStatusPending,
			action:        (*BuildService).CompleteBuild,
		},
		{
			name:          "success to failed is invalid",
			initialStatus: domain.BuildStatusSuccess,
			action:        (*BuildService).FailBuild,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeBuildRepository{
				build: domain.Build{
					ID:        "build-1",
					ProjectID: "project-1",
					Status:    tc.initialStatus,
				},
			}

			svc := NewBuildService(repo, nil, nil)

			_, err := tc.action(svc, context.Background(), "build-1")
			if !errors.Is(err, ErrInvalidBuildStatusTransition) {
				t.Fatalf("expected ErrInvalidBuildStatusTransition, got %v", err)
			}

			if repo.updateCalls != 0 {
				t.Fatalf("expected UpdateStatus to not be called, got %d", repo.updateCalls)
			}
		})
	}
}

func TestBuildService_CompleteBuild_GeneratesArtifactVersionAndChannel(t *testing.T) {
	jobID := "job-1"
	pipelineYAML := strings.Join([]string{
		"version: 1",
		"steps:",
		"  - name: build",
		"    run: make build",
		"artifacts:",
		"  - path: dist/app.tgz",
		"    version:",
		"      template: 3.1.{build_number}",
		"      channel: latest",
	}, "\n")
	repo := &fakeBuildRepository{build: domain.Build{
		ID:                 "build-1",
		BuildNumber:        42,
		ProjectID:          "project-1",
		JobID:              &jobID,
		Status:             domain.BuildStatusRunning,
		PipelineConfigYAML: &pipelineYAML,
	}}
	labelRepo := memoryrepo.NewArtifactLabelRepository()
	labelRepo.SeedBuilds(repo.build)
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-1": {{ID: "artifact-1", BuildID: "build-1", LogicalPath: "dist/app.tgz", CreatedAt: time.Now().UTC()}},
	}}
	labelRepo.SeedArtifacts(artifactRepo.artifacts["build-1"]...)

	svc := NewBuildService(repo, nil, nil)
	svc.artifactRepo = artifactRepo
	svc.versionTagger = versiontagsvc.NewService(nil).WithArtifactLabels(labelRepo)

	if _, err := svc.CompleteBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	tags, err := labelRepo.ListByArtifactID(context.Background(), "artifact-1")
	if err != nil {
		t.Fatalf("expected artifact tags, got %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected generated version and channel tags, got %#v", tags)
	}
	kinds := map[domain.VersionTagKind]string{}
	for _, tag := range tags {
		kinds[tag.Kind] = tag.Version
	}
	if kinds[domain.VersionTagKindVersion] != "3.1.42" {
		t.Fatalf("expected generated version 3.1.42, got %#v", kinds)
	}
	if kinds[domain.VersionTagKindChannel] != "latest" {
		t.Fatalf("expected generated channel latest, got %#v", kinds)
	}
}

func TestBuildService_CompleteBuild_GeneratedArtifactVersionConflictReturnsError(t *testing.T) {
	jobID := "job-1"
	pipelineYAML := strings.Join([]string{
		"version: 1",
		"steps:",
		"  - name: build",
		"    run: make build",
		"artifacts:",
		"  - path: dist/app.tgz",
		"    version:",
		"      template: 3.1.{build_number}",
	}, "\n")
	repo := &fakeBuildRepository{build: domain.Build{
		ID:                 "build-2",
		BuildNumber:        42,
		ProjectID:          "project-1",
		JobID:              &jobID,
		Status:             domain.BuildStatusRunning,
		PipelineConfigYAML: &pipelineYAML,
	}}
	previousBuild := domain.Build{ID: "build-old", BuildNumber: 41, ProjectID: "project-1", JobID: &jobID}
	labelRepo := memoryrepo.NewArtifactLabelRepository()
	labelRepo.SeedBuilds(repo.build, previousBuild)
	previousArtifact := domain.BuildArtifact{ID: "artifact-old", BuildID: "build-old", LogicalPath: "dist/app.tgz", CreatedAt: time.Now().UTC().Add(-time.Minute)}
	currentArtifact := domain.BuildArtifact{ID: "artifact-new", BuildID: "build-2", LogicalPath: "dist/app.tgz", CreatedAt: time.Now().UTC()}
	labelRepo.SeedArtifacts(previousArtifact, currentArtifact)
	_, err := labelRepo.CreateForArtifacts(context.Background(), repository.CreateArtifactLabelsParams{
		JobID:       jobID,
		Value:       "3.1.42",
		Kind:        domain.VersionTagKindVersion,
		ArtifactIDs: []string{"artifact-old"},
	})
	if err != nil {
		t.Fatalf("expected seed version tag, got %v", err)
	}
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-2": {currentArtifact},
	}}

	svc := NewBuildService(repo, nil, nil)
	svc.artifactRepo = artifactRepo
	svc.versionTagger = versiontagsvc.NewService(nil).WithArtifactLabels(labelRepo)

	_, err = svc.CompleteBuild(context.Background(), "build-2")
	if !errors.Is(err, repository.ErrVersionTagConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestBuildService_CompleteBuild_GeneratedWildcardArtifactVersionAndChannelPerPackage(t *testing.T) {
	jobID := "job-1"
	pipelineYAML := strings.Join([]string{
		"version: 1",
		"steps:",
		"  - name: build",
		"    run: make build",
		"artifacts:",
		"  - path: artifacts/images/*.tar",
		"    version:",
		"      template: 0.1.{build_number}",
		"      channel: latest",
	}, "\n")
	repo := &fakeBuildRepository{build: domain.Build{
		ID:                 "build-3",
		BuildNumber:        42,
		ProjectID:          "project-1",
		JobID:              &jobID,
		Status:             domain.BuildStatusRunning,
		PipelineConfigYAML: &pipelineYAML,
	}}
	labelRepo := memoryrepo.NewArtifactLabelRepository()
	labelRepo.SeedBuilds(repo.build)
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-3": {
			{ID: "artifact-backend", BuildID: "build-3", LogicalPath: "artifacts/images/backend-image.tar", CreatedAt: time.Now().UTC()},
			{ID: "artifact-frontend", BuildID: "build-3", LogicalPath: "artifacts/images/frontend-image.tar", CreatedAt: time.Now().UTC().Add(time.Second)},
		},
	}}
	labelRepo.SeedArtifacts(artifactRepo.artifacts["build-3"]...)

	svc := NewBuildService(repo, nil, nil)
	svc.artifactRepo = artifactRepo
	svc.versionTagger = versiontagsvc.NewService(nil).WithArtifactLabels(labelRepo)

	if _, err := svc.CompleteBuild(context.Background(), "build-3"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, artifactID := range []string{"artifact-backend", "artifact-frontend"} {
		tags, err := labelRepo.ListByArtifactID(context.Background(), artifactID)
		if err != nil {
			t.Fatalf("expected artifact tags for %s, got %v", artifactID, err)
		}
		if len(tags) != 2 {
			t.Fatalf("expected generated version and channel tags for %s, got %#v", artifactID, tags)
		}
		kinds := map[domain.VersionTagKind]string{}
		for _, tag := range tags {
			kinds[tag.Kind] = tag.Version
		}
		if kinds[domain.VersionTagKindVersion] != "0.1.42" {
			t.Fatalf("expected generated version 0.1.42 for %s, got %#v", artifactID, kinds)
		}
		if kinds[domain.VersionTagKindChannel] != "latest" {
			t.Fatalf("expected generated channel latest for %s, got %#v", artifactID, kinds)
		}
	}
}

func TestBuildService_CompleteBuild_GeneratedWildcardArtifactVersionAndChannelApplyToStepNamedArtifacts(t *testing.T) {
	jobID := "job-1"
	stepBackend := "step-backend"
	stepFrontend := "step-frontend"
	pipelineYAML := strings.Join([]string{
		"version: 1",
		"artifacts:",
		"  - path: artifacts/images/*.tar",
		"    version:",
		"      template: 0.1.{build_number}",
		"      channel: latest",
		"steps:",
		"  - group:",
		"      name: Images",
		"      steps:",
		"        - name: Backend Image",
		"          run: echo backend",
		"          artifacts:",
		"            - path: artifacts/images/backend-image.tar",
		"              name: coyote-ci/backend",
		"              type: docker_image",
		"        - name: Frontend Image",
		"          run: echo frontend",
		"          artifacts:",
		"            - path: artifacts/images/frontend-image.tar",
		"              name: coyote-ci/frontend",
		"              type: docker_image",
	}, "\n")
	repo := &fakeBuildRepository{build: domain.Build{
		ID:                 "build-4",
		BuildNumber:        42,
		ProjectID:          "project-1",
		JobID:              &jobID,
		Status:             domain.BuildStatusRunning,
		PipelineConfigYAML: &pipelineYAML,
	}, steps: []domain.BuildStep{{ID: stepBackend, StepIndex: 0, Name: "Backend Image"}, {ID: stepFrontend, StepIndex: 1, Name: "Frontend Image"}}}
	labelRepo := memoryrepo.NewArtifactLabelRepository()
	labelRepo.SeedBuilds(repo.build)
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-4": {
			{ID: "artifact-backend", BuildID: "build-4", StepID: &stepBackend, Name: "coyote-ci/backend", LogicalPath: "artifacts/images/backend-image.tar", ArtifactType: domain.ArtifactTypeDockerImage, CreatedAt: time.Now().UTC()},
			{ID: "artifact-frontend", BuildID: "build-4", StepID: &stepFrontend, Name: "coyote-ci/frontend", LogicalPath: "artifacts/images/frontend-image.tar", ArtifactType: domain.ArtifactTypeDockerImage, CreatedAt: time.Now().UTC().Add(time.Second)},
		},
	}}
	labelRepo.SeedArtifacts(artifactRepo.artifacts["build-4"]...)

	svc := NewBuildService(repo, nil, nil)
	svc.artifactRepo = artifactRepo
	svc.versionTagger = versiontagsvc.NewService(nil).WithArtifactLabels(labelRepo)

	if _, err := svc.CompleteBuild(context.Background(), "build-4"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for _, artifactID := range []string{"artifact-backend", "artifact-frontend"} {
		tags, err := labelRepo.ListByArtifactID(context.Background(), artifactID)
		if err != nil {
			t.Fatalf("expected artifact tags for %s, got %v", artifactID, err)
		}
		if len(tags) != 2 {
			t.Fatalf("expected generated version and channel tags for %s, got %#v", artifactID, tags)
		}
		kinds := map[domain.VersionTagKind]string{}
		for _, tag := range tags {
			kinds[tag.Kind] = tag.Version
		}
		if kinds[domain.VersionTagKindVersion] != "0.1.42" {
			t.Fatalf("expected generated version 0.1.42 for %s, got %#v", artifactID, kinds)
		}
		if kinds[domain.VersionTagKindChannel] != "latest" {
			t.Fatalf("expected generated channel latest for %s, got %#v", artifactID, kinds)
		}
	}
}

func TestNewBuildServiceFromConfig_WiresArtifactLabelRepositoryForGeneratedChannelTagging(t *testing.T) {
	jobID := "job-1"
	stepBackend := "step-backend"
	stepFrontend := "step-frontend"
	pipelineYAML := strings.Join([]string{
		"version: 1",
		"artifacts:",
		"  - path: artifacts/images/*.tar",
		"    version:",
		"      template: 0.1.{build_number}",
		"      channel: latest",
		"steps:",
		"  - group:",
		"      name: Images",
		"      steps:",
		"        - name: Backend Image",
		"          run: echo backend",
		"          artifacts:",
		"            - path: artifacts/images/backend-image.tar",
		"              name: coyote-ci/backend",
		"              type: docker_image",
		"        - name: Frontend Image",
		"          run: echo frontend",
		"          artifacts:",
		"            - path: artifacts/images/frontend-image.tar",
		"              name: coyote-ci/frontend",
		"              type: docker_image",
	}, "\n")
	repo := &fakeBuildRepository{build: domain.Build{
		ID:                 "build-config-wiring",
		BuildNumber:        42,
		ProjectID:          "project-1",
		JobID:              &jobID,
		Status:             domain.BuildStatusRunning,
		PipelineConfigYAML: &pipelineYAML,
	}, steps: []domain.BuildStep{{ID: stepBackend, StepIndex: 0, Name: "Backend Image"}, {ID: stepFrontend, StepIndex: 1, Name: "Frontend Image"}}}
	labelRepo := memoryrepo.NewArtifactLabelRepository()
	labelRepo.SeedBuilds(repo.build)
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-config-wiring": {
			{ID: "artifact-backend", BuildID: "build-config-wiring", StepID: &stepBackend, Name: "coyote-ci/backend", LogicalPath: "artifacts/images/backend-image.tar", ArtifactType: domain.ArtifactTypeDockerImage, CreatedAt: time.Now().UTC()},
			{ID: "artifact-frontend", BuildID: "build-config-wiring", StepID: &stepFrontend, Name: "coyote-ci/frontend", LogicalPath: "artifacts/images/frontend-image.tar", ArtifactType: domain.ArtifactTypeDockerImage, CreatedAt: time.Now().UTC().Add(time.Second)},
		},
	}}
	labelRepo.SeedArtifacts(artifactRepo.artifacts["build-config-wiring"]...)

	tagger := versiontagsvc.NewService(nil)
	svc := NewBuildServiceFromConfig(repo, nil, nil, BuildServiceConfig{
		VersionTagger:     tagger,
		ArtifactRepo:      artifactRepo,
		ArtifactLabelRepo: labelRepo,
	})

	if _, err := svc.CompleteBuild(context.Background(), "build-config-wiring"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, artifactID := range []string{"artifact-backend", "artifact-frontend"} {
		tags, err := labelRepo.ListByArtifactID(context.Background(), artifactID)
		if err != nil {
			t.Fatalf("expected artifact tags for %s, got %v", artifactID, err)
		}
		if len(tags) != 2 {
			t.Fatalf("expected generated version and channel tags for %s, got %#v", artifactID, tags)
		}
		kinds := map[domain.VersionTagKind]string{}
		for _, tag := range tags {
			kinds[tag.Kind] = tag.Version
		}
		if kinds[domain.VersionTagKindVersion] != "0.1.42" {
			t.Fatalf("expected generated version 0.1.42 for %s, got %#v", artifactID, kinds)
		}
		if kinds[domain.VersionTagKindChannel] != "latest" {
			t.Fatalf("expected generated channel latest for %s, got %#v", artifactID, kinds)
		}
	}
}

func TestBuildService_TransitionBuildStatus_NotFound(t *testing.T) {
	repo := &fakeBuildRepository{getErr: repository.ErrBuildNotFound}
	svc := NewBuildService(repo, nil, nil)

	_, err := svc.StartBuild(context.Background(), "missing-build")
	if !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound, got %v", err)
	}

	if repo.updateCalls != 0 {
		t.Fatalf("expected UpdateStatus to not be called, got %d", repo.updateCalls)
	}
}

func TestBuildService_TransitionBuildStatus_UpdateError(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{
			ID:        "build-1",
			ProjectID: "project-1",
			Status:    domain.BuildStatusQueued,
		},
		updateErr: errors.New("update failed"),
	}

	svc := NewBuildService(repo, nil, nil)

	_, err := svc.StartBuild(context.Background(), "build-1")
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("expected update error, got %v", err)
	}

	if repo.updateCalls != 1 {
		t.Fatalf("expected UpdateStatus to be called once, got %d", repo.updateCalls)
	}
}

func TestBuildService_CancelBuild_TerminalizesBuildAndNonTerminalSteps(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now},
		steps: []domain.BuildStep{
			{ID: "step-0", BuildID: "build-1", StepIndex: 0, Name: "setup", Status: domain.BuildStepStatusSuccess},
			{ID: "step-1", BuildID: "build-1", StepIndex: 1, Name: "test", Status: domain.BuildStepStatusRunning},
			{ID: "step-2", BuildID: "build-1", StepIndex: 2, Name: "lint", Status: domain.BuildStepStatusPending},
		},
	}
	svc := NewBuildService(repo, nil, nil)

	canceled, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}
	if canceled.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build status canceled, got %q", canceled.Status)
	}

	if repo.steps[0].Status != domain.BuildStepStatusSuccess {
		t.Fatalf("expected already terminal step to remain success, got %q", repo.steps[0].Status)
	}
	if repo.steps[1].Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected running step to be terminalized as canceled, got %q", repo.steps[1].Status)
	}
	if repo.steps[2].Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected pending step to be terminalized as canceled, got %q", repo.steps[2].Status)
	}
}

func TestBuildService_CancelBuild_QueuedBuildSucceeds(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: now},
		steps: []domain.BuildStep{{ID: "step-0", BuildID: "build-1", StepIndex: 0, Name: "setup", Status: domain.BuildStepStatusPending}},
	}
	svc := NewBuildService(repo, nil, nil)

	canceled, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}
	if canceled.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build status, got %q", canceled.Status)
	}
	if repo.steps[0].Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected pending step to be canceled, got %q", repo.steps[0].Status)
	}
}

func TestBuildService_CancelBuild_PreparingBuildSucceeds(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPreparing, CreatedAt: now},
		steps: []domain.BuildStep{{ID: "step-0", BuildID: "build-1", StepIndex: 0, Name: "setup", Status: domain.BuildStepStatusPending}},
	}
	svc := NewBuildService(repo, nil, nil)

	canceled, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}
	if canceled.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build status, got %q", canceled.Status)
	}
	if repo.steps[0].Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected pending step to be canceled, got %q", repo.steps[0].Status)
	}
}

func TestBuildService_CancelBuild_AtomicRepoWithExecutionJobsSkipsFallback(t *testing.T) {
	now := time.Now().UTC()
	buildRepo := &fakeAtomicCancelBuildRepository{
		fakeBuildRepository:   &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}},
		updatedSteps:          1,
		includesExecutionJobs: true,
	}
	jobRepo := &fakeExecutionJobCancelRepository{updatedJobs: 2}
	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{ExecutionJobRepo: jobRepo})

	canceled, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}
	if canceled.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build status, got %q", canceled.Status)
	}
	if jobRepo.cancelCalls != 0 {
		t.Fatalf("expected execution job fallback to be skipped, got %d calls", jobRepo.cancelCalls)
	}
}

func TestBuildService_CancelBuild_AtomicRepoWithoutExecutionJobsUsesFallback(t *testing.T) {
	now := time.Now().UTC()
	buildRepo := &fakeAtomicCancelBuildRepository{
		fakeBuildRepository:   &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}},
		updatedSteps:          1,
		includesExecutionJobs: false,
	}
	jobRepo := &fakeExecutionJobCancelRepository{updatedJobs: 2}
	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{ExecutionJobRepo: jobRepo})

	canceled, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}
	if canceled.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build status, got %q", canceled.Status)
	}
	if jobRepo.cancelCalls != 1 {
		t.Fatalf("expected execution job fallback to run once, got %d calls", jobRepo.cancelCalls)
	}
}

func TestBuildService_CancelBuild_AtomicRepoInvokesNotifier(t *testing.T) {
	now := time.Now().UTC()
	buildRepo := &fakeAtomicCancelBuildRepository{
		fakeBuildRepository:   &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}},
		updatedSteps:          1,
		includesExecutionJobs: true,
	}
	notifier := &recordingBuildNotifier{}
	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})

	canceled, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build failed: %v", err)
	}
	if canceled.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build status, got %q", canceled.Status)
	}
	if len(notifier.builds) != 1 {
		t.Fatalf("expected one notifier call, got %d", len(notifier.builds))
	}
	if notifier.builds[0].ID != "build-1" || notifier.builds[0].Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build notification for build-1, got %#v", notifier.builds[0])
	}
}

func TestBuildService_LateWorkerReportAfterCancelDoesNotResurrectState(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	buildRepo := memoryrepo.NewBuildRepository()
	jobRepo := memoryrepo.NewExecutionJobRepository()
	jobRepo.SetBuildRepository(buildRepo)
	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{ExecutionJobRepo: jobRepo})

	if _, err := buildRepo.Create(ctx, domain.Build{ID: "build-race", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: now}); err != nil {
		t.Fatalf("create build failed: %v", err)
	}
	if _, err := buildRepo.QueueBuild(ctx, "build-race", []domain.BuildStep{{ID: "step-race", StepIndex: 0, Name: "test", Command: "go test ./...", Status: domain.BuildStepStatusPending}}); err != nil {
		t.Fatalf("queue build failed: %v", err)
	}
	if _, err := buildRepo.UpdateStatus(ctx, "build-race", domain.BuildStatusRunning, nil); err != nil {
		t.Fatalf("start build failed: %v", err)
	}
	if _, err := jobRepo.CreateJobsForBuild(ctx, []domain.ExecutionJob{{ID: "job-race", BuildID: "build-race", StepID: "step-race", NodeID: "step-race", Name: "test", StepIndex: 0, AttemptNumber: 1, Status: domain.ExecutionJobStatusQueued, Image: "golang:1.24", WorkingDir: ".", Command: []string{"go", "test", "./..."}, ResolvedSpecJSON: "{}", CreatedAt: now}}); err != nil {
		t.Fatalf("create jobs failed: %v", err)
	}

	claim := repository.StepClaim{WorkerID: "worker-1", ClaimToken: "claim-1", ClaimedAt: now.Add(time.Minute), LeaseExpiresAt: now.Add(2 * time.Minute)}
	step, stepClaimed, err := buildRepo.ClaimPendingStep(ctx, "build-race", 0, claim)
	if err != nil {
		t.Fatalf("claim step failed: %v", err)
	}
	if !stepClaimed {
		t.Fatal("expected step claim to succeed")
	}
	job, jobClaimed, err := jobRepo.ClaimJobByStepID(ctx, step.ID, claim)
	if err != nil {
		t.Fatalf("claim job failed: %v", err)
	}
	if !jobClaimed {
		t.Fatal("expected job claim to succeed")
	}

	if _, cancelErr := svc.CancelBuild(ctx, "build-race"); cancelErr != nil {
		t.Fatalf("cancel build failed: %v", cancelErr)
	}

	result := steprunner.RunStepResult{Status: steprunner.RunStepStatusSuccess, ExitCode: 0, Stdout: "ok", StartedAt: claim.ClaimedAt, FinishedAt: now.Add(3 * time.Minute)}
	report, err := svc.HandleStepResult(ctx, steprunner.RunStepRequest{BuildID: "build-race", JobID: job.ID, StepID: step.ID, StepIndex: step.StepIndex, StepName: step.Name, WorkerID: claim.WorkerID, ClaimToken: claim.ClaimToken}, result)
	if err != nil {
		t.Fatalf("late handle step result failed: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionDuplicateTerminal {
		t.Fatalf("expected duplicate terminal outcome, got %q", report.CompletionOutcome)
	}

	lateJob, outcome, err := jobRepo.CompleteJobSuccess(ctx, job.ID, claim.ClaimToken, result.FinishedAt, result.ExitCode, []domain.ArtifactRef{{Name: "dist/app", URI: "s3://bucket/dist/app"}})
	if err != nil {
		t.Fatalf("late complete job failed: %v", err)
	}
	if outcome != repository.StepCompletionDuplicateTerminal {
		t.Fatalf("expected duplicate terminal job outcome, got %q", outcome)
	}
	if lateJob.Status != domain.ExecutionJobStatusCanceled {
		t.Fatalf("expected job to remain canceled, got %q", lateJob.Status)
	}

	build, err := buildRepo.GetByID(ctx, "build-race")
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if build.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected build to remain canceled, got %q", build.Status)
	}
	steps, err := buildRepo.GetStepsByBuildID(ctx, "build-race")
	if err != nil {
		t.Fatalf("get steps failed: %v", err)
	}
	if steps[0].Status != domain.BuildStepStatusCanceled {
		t.Fatalf("expected step to remain canceled, got %q", steps[0].Status)
	}
	jobs, err := jobRepo.GetJobsByBuildID(ctx, "build-race")
	if err != nil {
		t.Fatalf("get jobs failed: %v", err)
	}
	if jobs[0].Status != domain.ExecutionJobStatusCanceled {
		t.Fatalf("expected persisted job to remain canceled, got %q", jobs[0].Status)
	}
}

func TestBuildService_CancelBuild_NonCancelableBuildIsRejected(t *testing.T) {
	tests := []struct {
		name   string
		status domain.BuildStatus
	}{
		{name: "pending", status: domain.BuildStatusPending},
		{name: "success", status: domain.BuildStatusSuccess},
		{name: "failed", status: domain.BuildStatusFailed},
		{name: "canceled", status: domain.BuildStatusCanceled},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			repo := &fakeBuildRepository{
				build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: tc.status, CreatedAt: now},
				steps: []domain.BuildStep{{ID: "step-0", BuildID: "build-1", StepIndex: 0, Name: "setup", Status: domain.BuildStepStatusSuccess}},
			}
			svc := NewBuildService(repo, nil, nil)

			_, err := svc.CancelBuild(context.Background(), "build-1")
			if !errors.Is(err, ErrInvalidBuildStatusTransition) {
				t.Fatalf("expected invalid transition, got %v", err)
			}
			if repo.updateCalls != 0 {
				t.Fatalf("expected no update status call for terminal build, got %d", repo.updateCalls)
			}
		})
	}
}

func TestBuildService_QueueBuildWithTemplate(t *testing.T) {
	tests := []struct {
		name          string
		template      string
		expectedNames []string
	}{
		{name: "default when omitted", template: "", expectedNames: []string{"default"}},
		{name: "default explicit", template: BuildTemplateDefault, expectedNames: []string{"default"}},
		{name: "test template", template: BuildTemplateTest, expectedNames: []string{"setup", "test", "teardown"}},
		{name: "build template", template: BuildTemplateBuild, expectedNames: []string{"install", "compile"}},
		{name: "fail template", template: BuildTemplateFail, expectedNames: []string{"setup", "verify"}},
		{name: "unknown falls back", template: "unknown", expectedNames: []string{"default"}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeBuildRepository{
				build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
			}
			svc := NewBuildService(repo, nil, nil)

			if _, err := svc.QueueBuildWithTemplate(context.Background(), "build-1", tc.template); err != nil {
				t.Fatalf("queue with template returned error: %v", err)
			}

			if len(repo.steps) != len(tc.expectedNames) {
				t.Fatalf("expected %d steps, got %d", len(tc.expectedNames), len(repo.steps))
			}

			for idx, expectedName := range tc.expectedNames {
				if repo.steps[idx].StepIndex != idx {
					t.Fatalf("expected step index %d, got %d", idx, repo.steps[idx].StepIndex)
				}
				if repo.steps[idx].Name != expectedName {
					t.Fatalf("expected step name %q at index %d, got %q", expectedName, idx, repo.steps[idx].Name)
				}
			}
		})
	}
}

func TestBuildService_QueueBuildWithTemplate_FailTemplateCommands(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
	}
	svc := NewBuildService(repo, nil, nil)

	if _, err := svc.QueueBuildWithTemplate(context.Background(), "build-1", BuildTemplateFail); err != nil {
		t.Fatalf("queue with fail template returned error: %v", err)
	}

	if len(repo.steps) != 2 {
		t.Fatalf("expected 2 fail-template steps, got %d", len(repo.steps))
	}
	if len(repo.steps[0].Args) < 2 || !strings.Contains(repo.steps[0].Args[1], "exit 0") {
		t.Fatalf("expected first step script to include exit 0, got %+v", repo.steps[0].Args)
	}
	if len(repo.steps[1].Args) < 2 || !strings.Contains(repo.steps[1].Args[1], "exit 1") {
		t.Fatalf("expected second step script to include exit 1, got %+v", repo.steps[1].Args)
	}
}

func TestBuildService_QueueBuildWithTemplate_CustomTemplateCommands(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
	}
	svc := NewBuildService(repo, nil, nil)

	customSteps := []QueueBuildCustomStepInput{
		{Command: "echo ok && exit 0"},
		{Name: "failure", Command: "echo fail && exit 1"},
	}

	if _, err := svc.QueueBuildWithTemplateAndCustomSteps(context.Background(), "build-1", BuildTemplateCustom, customSteps); err != nil {
		t.Fatalf("queue with custom template returned error: %v", err)
	}

	if len(repo.steps) != 2 {
		t.Fatalf("expected 2 custom steps, got %d", len(repo.steps))
	}
	if repo.steps[0].Name != "step-1" {
		t.Fatalf("expected generated first step name step-1, got %q", repo.steps[0].Name)
	}
	if len(repo.steps[0].Args) < 2 || repo.steps[0].Args[0] != "-c" || repo.steps[0].Args[1] != "echo ok && exit 0" {
		t.Fatalf("expected first step to run via sh -c with command, got %+v", repo.steps[0].Args)
	}
	if repo.steps[1].Name != "failure" {
		t.Fatalf("expected explicit step name to persist, got %q", repo.steps[1].Name)
	}
	if len(repo.steps[1].Args) < 2 || repo.steps[1].Args[1] != "echo fail && exit 1" {
		t.Fatalf("expected second step command to persist, got %+v", repo.steps[1].Args)
	}
}

func TestBuildService_QueueBuildWithTemplate_CustomTemplateValidation(t *testing.T) {
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusPending, CreatedAt: time.Now().UTC()},
	}
	svc := NewBuildService(repo, nil, nil)

	if _, err := svc.QueueBuildWithTemplateAndCustomSteps(context.Background(), "build-1", BuildTemplateCustom, nil); !errors.Is(err, ErrCustomTemplateStepsRequired) {
		t.Fatalf("expected ErrCustomTemplateStepsRequired, got %v", err)
	}

	if _, err := svc.QueueBuildWithTemplateAndCustomSteps(context.Background(), "build-1", BuildTemplateCustom, []QueueBuildCustomStepInput{{Name: "bad", Command: "  "}}); !errors.Is(err, ErrCustomTemplateStepCommandRequired) {
		t.Fatalf("expected ErrCustomTemplateStepCommandRequired, got %v", err)
	}
}

func TestBuildService_GetBuildSteps_NotFound(t *testing.T) {
	repo := &fakeBuildRepository{getErr: repository.ErrBuildNotFound}
	svc := NewBuildService(repo, nil, nil)

	_, err := svc.GetBuildSteps(context.Background(), "missing")
	if !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestBuildService_GetBuildLogs_NotFound(t *testing.T) {
	repo := &fakeBuildRepository{getErr: repository.ErrBuildNotFound}
	svc := NewBuildService(repo, nil, nil)

	_, err := svc.GetBuildLogs(context.Background(), "missing")
	if !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
