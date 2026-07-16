package build

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service/execution"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

type recordingSCMPublisher struct {
	reqs []SCMCommitStatusPublishRequest
	err  error
}

func (p *recordingSCMPublisher) PublishCommitStatus(_ context.Context, req SCMCommitStatusPublishRequest) error {
	p.reqs = append(p.reqs, req)
	return p.err
}

type fakeSCMProjectRepository struct {
	project domain.Project
	err     error
}

func (r *fakeSCMProjectRepository) GetByID(_ context.Context, _ string) (domain.Project, error) {
	if r.err != nil {
		return domain.Project{}, r.err
	}
	return r.project, nil
}

type multiBuildRepository struct {
	builds map[string]domain.Build
	byJob  map[string][]domain.Build
	err    error
}

func (r *multiBuildRepository) GetByID(_ context.Context, id string) (domain.Build, error) {
	if r.err != nil {
		return domain.Build{}, r.err
	}
	build, ok := r.builds[id]
	if !ok {
		return domain.Build{}, repository.ErrBuildNotFound
	}
	return build, nil
}

func (r *multiBuildRepository) ListByJobID(_ context.Context, jobID string) ([]domain.Build, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]domain.Build(nil), r.byJob[jobID]...), nil
}

func TestSCMStatusReporter_NotifyBuildStatus_PendingAndSuccess(t *testing.T) {
	jobID := "job-1"
	build := domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")}
	buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-1": build}, byJob: map[string][]domain.Build{jobID: {build}}}
	deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
	publisher := &recordingSCMPublisher{}
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: buildRepo, ProjectRepo: &fakeSCMProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, DeliveryRepo: deliveryRepo, Publisher: publisher, PublicBaseURL: "https://ci.example.com"})
	if err != nil {
		t.Fatalf("new reporter failed: %v", err)
	}
	reporter.now = func() time.Time { return time.Date(2026, 7, 16, 17, 0, 0, 0, time.UTC) }

	if notifyErr := reporter.NotifyBuildStatus(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify queued build failed: %v", notifyErr)
	}
	if len(publisher.reqs) != 1 {
		t.Fatalf("expected one publish request, got %d", len(publisher.reqs))
	}
	if publisher.reqs[0].Context != "coyote/payments/job-1" {
		t.Fatalf("expected context coyote/payments/job-1, got %q", publisher.reqs[0].Context)
	}
	if publisher.reqs[0].State != domain.SCMCommitStatusStatePending {
		t.Fatalf("expected pending state, got %q", publisher.reqs[0].State)
	}

	build.Status = domain.BuildStatusSuccess
	buildRepo.builds[build.ID] = build
	buildRepo.byJob[jobID] = []domain.Build{build}
	if notifyErr := reporter.NotifyBuildStatus(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify success build failed: %v", notifyErr)
	}
	if len(publisher.reqs) != 2 {
		t.Fatalf("expected second publish request, got %d", len(publisher.reqs))
	}
	if publisher.reqs[1].State != domain.SCMCommitStatusStateSuccess {
		t.Fatalf("expected success state, got %q", publisher.reqs[1].State)
	}
}

func TestSCMStatusReporter_NotifyBuildStatus_SupersedesOlderAttempt(t *testing.T) {
	jobID := "job-1"
	older := domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 1, CreatedAt: time.Date(2026, 7, 16, 17, 0, 0, 0, time.UTC), Status: domain.BuildStatusFailed, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")}
	newer := domain.Build{ID: "build-2", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 2, CreatedAt: time.Date(2026, 7, 16, 17, 5, 0, 0, time.UTC), Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")}
	buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-1": older, "build-2": newer}, byJob: map[string][]domain.Build{jobID: {newer, older}}}
	deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
	publisher := &recordingSCMPublisher{}
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: buildRepo, ProjectRepo: &fakeSCMProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, DeliveryRepo: deliveryRepo, Publisher: publisher})
	if err != nil {
		t.Fatalf("new reporter failed: %v", err)
	}
	reporter.now = func() time.Time { return time.Date(2026, 7, 16, 17, 10, 0, 0, time.UTC) }

	if notifyErr := reporter.NotifyBuildStatus(context.Background(), older); notifyErr != nil {
		t.Fatalf("notify older build failed: %v", notifyErr)
	}
	if len(publisher.reqs) != 0 {
		t.Fatalf("expected no publish request for superseded attempt, got %d", len(publisher.reqs))
	}
	delivery, getErr := deliveryRepo.GetByBuildContextState(context.Background(), older.ID, "github", "coyote/payments/job-1", domain.SCMCommitStatusStateFailure)
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.Status != domain.SCMStatusDeliveryStatusSuperseded {
		t.Fatalf("expected superseded delivery, got %q", delivery.Status)
	}
}

func TestSCMStatusReporter_NotifyBuildStatus_RetryableFailure(t *testing.T) {
	jobID := "job-1"
	build := domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")}
	buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-1": build}, byJob: map[string][]domain.Build{jobID: {build}}}
	deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
	publisher := &recordingSCMPublisher{err: timeoutPublisherError{}}
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: buildRepo, ProjectRepo: &fakeSCMProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, DeliveryRepo: deliveryRepo, Publisher: publisher})
	if err != nil {
		t.Fatalf("new reporter failed: %v", err)
	}
	now := time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC)
	reporter.now = func() time.Time { return now }

	if notifyErr := reporter.NotifyBuildStatus(context.Background(), build); notifyErr == nil {
		t.Fatal("expected retryable publish error")
	}
	delivery, getErr := deliveryRepo.GetByBuildContextState(context.Background(), build.ID, "github", "coyote/payments/job-1", domain.SCMCommitStatusStatePending)
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.Status != domain.SCMStatusDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting, got %q", delivery.Status)
	}
	if delivery.NextAttemptAt == nil || !delivery.NextAttemptAt.After(now) {
		t.Fatalf("expected next attempt after now, got %v", delivery.NextAttemptAt)
	}
}

func TestBuildService_NotifySCMStatusOnCreateAndSourceResolution(t *testing.T) {
	jobID := "job-1"
	buildRepo := &fakeBuildRepository{}
	reporter := &recordingSCMBuildReporter{}
	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{SCMStatusReporter: reporter})
	build, err := svc.CreateBuild(context.Background(), CreateBuildInput{ProjectID: "project-1", JobID: &jobID, Source: &CreateBuildSourceInput{RepositoryURL: "https://github.com/octo/repo.git", CommitSHA: "deadbeef"}})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}
	if len(reporter.builds) != 1 || reporter.builds[0].ID != build.ID {
		t.Fatalf("expected reporter call for created build, got %+v", reporter.builds)
	}

	buildRepo.build = domain.Build{ID: "build-2", ProjectID: "project-1", JobID: &jobID, Status: domain.BuildStatusQueued, RepoURL: strPtr("https://github.com/octo/repo.git"), Ref: strPtr("refs/heads/main")}
	svc.sourceResolver = &stubWorkspaceSourceResolver{resolvedCommit: "cafebabe"}
	svc.executionWorkspaceRoot = t.TempDir()
	if _, resolveErr := svc.resolveBuildSourceInWorkspace(context.Background(), "build-2", executionResolvedBuildSourceSpec("https://github.com/octo/repo.git", "refs/heads/main", "")); resolveErr != nil {
		t.Fatalf("resolve build source failed: %v", resolveErr)
	}
	if len(reporter.builds) != 2 || buildReadOptionalString(reporter.builds[1].CommitSHA) != "cafebabe" {
		t.Fatalf("expected reporter call after source resolution, got %+v", reporter.builds)
	}
}

type recordingSCMBuildReporter struct {
	builds []domain.Build
	err    error
}

func (r *recordingSCMBuildReporter) NotifyBuildStatus(_ context.Context, build domain.Build) error {
	r.builds = append(r.builds, build)
	return r.err
}

type timeoutPublisherError struct{}

func (timeoutPublisherError) Error() string   { return "timeout" }
func (timeoutPublisherError) Timeout() bool   { return true }
func (timeoutPublisherError) Temporary() bool { return true }

type stubWorkspaceSourceResolver struct {
	resolvedCommit string
	err            error
}

func (r *stubWorkspaceSourceResolver) CloneIntoWorkspace(_ context.Context, workspacePath string, _ string) error {
	if r.err != nil {
		return r.err
	}
	return os.MkdirAll(workspacePath, 0o755)
}

func (r *stubWorkspaceSourceResolver) CheckoutWorkspaceSource(_ context.Context, _ string, _ source.WorkspaceSourceSpec) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.resolvedCommit, nil
}

func executionResolvedBuildSourceSpec(repoURL string, ref string, commitSHA string) execution.ResolvedBuildSourceSpec {
	return execution.ResolvedBuildSourceSpec{RepositoryURL: repoURL, Ref: ref, CommitSHA: commitSHA, HasSource: true}
}
