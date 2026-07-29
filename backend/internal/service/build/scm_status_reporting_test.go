package build

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
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
	mu     sync.RWMutex
	builds map[string]domain.Build
	byJob  map[string][]domain.Build
	err    error
}

func scmMappedBuild(build domain.Build) domain.Build {
	registeredRepositoryID := "registered-repository-1"
	connectionID := "scm-connection-1"
	providerRepositoryID := "provider-repository-1"
	build.RegisteredRepositoryID = &registeredRepositoryID
	build.SCMConnectionID = &connectionID
	build.ProviderRepositoryID = &providerRepositoryID
	return build
}

func (r *multiBuildRepository) GetByID(_ context.Context, id string) (domain.Build, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.err != nil {
		return nil, r.err
	}
	return append([]domain.Build(nil), r.byJob[jobID]...), nil
}

func (r *multiBuildRepository) setBuild(build domain.Build) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.builds[build.ID] = build
}

func (r *multiBuildRepository) setJobBuilds(jobID string, builds []domain.Build) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byJob[jobID] = append([]domain.Build(nil), builds...)
}

func TestSCMStatusReporter_NotifyBuildStatus_PendingAndSuccess(t *testing.T) {
	jobID := "job-1"
	createdAt := time.Date(2026, 7, 16, 16, 55, 0, 0, time.UTC)
	build := scmMappedBuild(domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 1, CreatedAt: createdAt, Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
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
	buildRepo.setBuild(build)
	buildRepo.setJobBuilds(jobID, []domain.Build{build})
	if notifyErr := reporter.NotifyBuildStatus(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify success build failed: %v", notifyErr)
	}
	if len(publisher.reqs) != 2 {
		t.Fatalf("expected second publish request, got %d", len(publisher.reqs))
	}
	if publisher.reqs[1].State != domain.SCMCommitStatusStateSuccess {
		t.Fatalf("expected success state, got %q", publisher.reqs[1].State)
	}
	delivery, getErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.Status != domain.SCMStatusDeliveryStatusSent || delivery.DesiredState != domain.SCMCommitStatusStateSuccess {
		t.Fatalf("expected single sent success stream row, got %+v", delivery)
	}
}

func TestSCMStatusReporter_NotifyBuildStatus_SupersedesOlderAttempt(t *testing.T) {
	jobID := "job-1"
	older := scmMappedBuild(domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 1, CreatedAt: time.Date(2026, 7, 16, 17, 0, 0, 0, time.UTC), Status: domain.BuildStatusFailed, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
	newer := scmMappedBuild(domain.Build{ID: "build-2", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 2, CreatedAt: time.Date(2026, 7, 16, 17, 5, 0, 0, time.UTC), Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
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
	delivery, getErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.Status != domain.SCMStatusDeliveryStatusSuperseded || delivery.BuildID != older.ID {
		t.Fatalf("expected superseded delivery owned by older build, got %+v", delivery)
	}
}

func TestSCMStatusReporter_NotifyBuildStatus_RetryableFailure(t *testing.T) {
	jobID := "job-1"
	createdAt := time.Date(2026, 7, 16, 17, 55, 0, 0, time.UTC)
	build := scmMappedBuild(domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 1, CreatedAt: createdAt, Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
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
	delivery, getErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.Status != domain.SCMStatusDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting, got %q", delivery.Status)
	}
	if delivery.NextAttemptAt == nil || !delivery.NextAttemptAt.After(now) {
		t.Fatalf("expected next attempt after now, got %v", delivery.NextAttemptAt)
	}
	if delivery.LastError == nil || len([]rune(*delivery.LastError)) > maxSCMStatusStoredErrorLength {
		t.Fatalf("expected bounded stored error, got %v", delivery.LastError)
	}
}

func TestSCMStatusReporter_NotifyBuildStatus_TerminalReplacesSameBuildPending(t *testing.T) {
	jobID := "job-1"
	createdAt := time.Date(2026, 7, 16, 18, 10, 0, 0, time.UTC)
	build := scmMappedBuild(domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 1, CreatedAt: createdAt, Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
	buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-1": build}, byJob: map[string][]domain.Build{jobID: {build}}}
	deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
	publisher := &recordingSCMPublisher{}
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: buildRepo, ProjectRepo: &fakeSCMProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, DeliveryRepo: deliveryRepo, Publisher: publisher})
	if err != nil {
		t.Fatalf("new reporter failed: %v", err)
	}
	reporter.now = func() time.Time { return time.Date(2026, 7, 16, 18, 11, 0, 0, time.UTC) }

	if notifyErr := reporter.NotifyBuildStatus(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify pending build failed: %v", notifyErr)
	}
	build.Status = domain.BuildStatusSuccess
	buildRepo.setBuild(build)
	buildRepo.setJobBuilds(jobID, []domain.Build{build})
	if notifyErr := reporter.NotifyBuildStatus(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	if len(publisher.reqs) != 2 || publisher.reqs[0].State != domain.SCMCommitStatusStatePending || publisher.reqs[1].State != domain.SCMCommitStatusStateSuccess {
		t.Fatalf("expected pending then success publishes, got %+v", publisher.reqs)
	}
	delivery, getErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.DesiredState != domain.SCMCommitStatusStateSuccess || delivery.LastSentState == nil || *delivery.LastSentState != domain.SCMCommitStatusStateSuccess {
		t.Fatalf("expected terminal state to replace pending stream owner, got %+v", delivery)
	}
}

func TestSCMStatusReporter_NotifyBuildStatus_NewerAttemptDurablyFencesOlderAttempt(t *testing.T) {
	jobID := "job-1"
	older := scmMappedBuild(domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 1, CreatedAt: time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC), Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
	newer := scmMappedBuild(domain.Build{ID: "build-2", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 2, CreatedAt: time.Date(2026, 7, 16, 18, 5, 0, 0, time.UTC), Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
	buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-1": older, "build-2": newer}, byJob: map[string][]domain.Build{jobID: {newer, older}}}
	deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
	publisher := &recordingSCMPublisher{}
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: buildRepo, ProjectRepo: &fakeSCMProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, DeliveryRepo: deliveryRepo, Publisher: publisher})
	if err != nil {
		t.Fatalf("new reporter failed: %v", err)
	}
	reporter.now = func() time.Time { return time.Date(2026, 7, 16, 18, 6, 0, 0, time.UTC) }

	if notifyErr := reporter.NotifyBuildStatus(context.Background(), newer); notifyErr != nil {
		t.Fatalf("notify newer build failed: %v", notifyErr)
	}
	if notifyErr := reporter.NotifyBuildStatus(context.Background(), older); notifyErr != nil {
		t.Fatalf("notify older build failed: %v", notifyErr)
	}
	if len(publisher.reqs) != 1 || publisher.reqs[0].State != domain.SCMCommitStatusStatePending {
		t.Fatalf("expected only newer attempt to publish, got %+v", publisher.reqs)
	}
	delivery, getErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.BuildID != newer.ID || delivery.BuildAttempt != newer.AttemptNumber {
		t.Fatalf("expected newer build to remain stream owner, got %+v", delivery)
	}
}

func TestSCMStatusReporter_NotifyBuildStatus_ReassertsAuthoritativeStateAfterStaleSendLosesClaim(t *testing.T) {
	jobID := "job-1"
	older := scmMappedBuild(domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 1, CreatedAt: time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC), Status: domain.BuildStatusQueued, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
	newer := scmMappedBuild(domain.Build{ID: "build-2", ProjectID: "project-1", JobID: &jobID, AttemptNumber: 2, CreatedAt: time.Date(2026, 7, 16, 18, 5, 0, 0, time.UTC), Status: domain.BuildStatusSuccess, CommitSHA: strPtr("deadbeef"), RepoURL: strPtr("https://github.com/octo/repo.git")})
	buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-1": older, "build-2": newer}, byJob: map[string][]domain.Build{jobID: {older}}}
	deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
	olderStarted := make(chan struct{})
	releaseOlder := make(chan struct{})
	newerPublished := make(chan struct{})
	publisher := newBlockingSCMPublisher(func(req SCMCommitStatusPublishRequest) {
		if req.State == domain.SCMCommitStatusStatePending && req.Description == "Coyote build is queued" {
			closeOnce(olderStarted)
			<-releaseOlder
		}
	}, func(req SCMCommitStatusPublishRequest) {
		if req.State == domain.SCMCommitStatusStateSuccess {
			closeOnce(newerPublished)
		}
	})
	reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: buildRepo, ProjectRepo: &fakeSCMProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, DeliveryRepo: deliveryRepo, Publisher: publisher})
	if err != nil {
		t.Fatalf("new reporter failed: %v", err)
	}
	now := time.Date(2026, 7, 16, 18, 6, 0, 0, time.UTC)
	reporter.now = func() time.Time { return now }

	olderErrCh := make(chan error, 1)
	go func() {
		olderErrCh <- reporter.NotifyBuildStatus(context.Background(), older)
	}()

	<-olderStarted
	buildRepo.setJobBuilds(jobID, []domain.Build{newer, older})
	if notifyErr := reporter.NotifyBuildStatus(context.Background(), newer); notifyErr != nil {
		t.Fatalf("notify newer build failed: %v", notifyErr)
	}
	<-newerPublished
	close(releaseOlder)
	if olderErr := <-olderErrCh; olderErr != nil {
		t.Fatalf("notify older build failed: %v", olderErr)
	}

	finalRemote, ok := publisher.remoteState("github", "repository-snapshot", "repository-snapshot", "deadbeef", "coyote/payments/job-1")
	if !ok {
		t.Fatal("expected remote state to be recorded")
	}
	if finalRemote.State != domain.SCMCommitStatusStateSuccess {
		t.Fatalf("expected authoritative success state to win remotely, got %+v", finalRemote)
	}
	delivery, getErr := deliveryRepo.GetByRepositoryIdentity(context.Background(), "scm-connection-1", "provider-repository-1", "deadbeef", "coyote/payments/job-1")
	if getErr != nil {
		t.Fatalf("get delivery failed: %v", getErr)
	}
	if delivery.BuildID != newer.ID || delivery.BuildAttempt != newer.AttemptNumber {
		t.Fatalf("expected newer build to remain authoritative, got %+v", delivery)
	}
	if delivery.Status != domain.SCMStatusDeliveryStatusRetryWaiting {
		t.Fatalf("expected persisted reassert backstop after replacement, got %+v", delivery)
	}
	if delivery.FailureReason == nil || *delivery.FailureReason != scmStatusFailureReasonAuthoritativeReassert {
		t.Fatalf("expected authoritative reassert reason, got %+v", delivery)
	}
	if !publisher.sawState(domain.SCMCommitStatusStatePending) || publisher.countState(domain.SCMCommitStatusStateSuccess) < 2 {
		t.Fatalf("expected stale pending send and prompt authoritative success reassert, got %+v", publisher.requests())
	}
}

func TestGitHubCommitStatusClient_Hardening(t *testing.T) {
	t.Run("invalid input rejected", func(t *testing.T) {
		client := NewGitHubCommitStatusClient("", nil, "token")
		err := client.PublishCommitStatus(context.Background(), SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: strings.Repeat("x", maxSCMStatusContextLength+1), State: domain.SCMCommitStatusStatePending, Description: "desc"})
		if err == nil {
			t.Fatal("expected invalid input error")
		}
		var publishErr *GitHubCommitStatusError
		if !errors.As(err, &publishErr) || publishErr.Reason() != "github_status_invalid_input" {
			t.Fatalf("expected invalid input github error, got %v", err)
		}
	})

	t.Run("rate limited 403 is retryable and bounded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Path; got != "/repos/octo/repo/statuses/deadbeef" {
				t.Fatalf("unexpected path %q", got)
			}
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, strings.Repeat("A", maxGitHubCommitStatusResponseBodyBytes+32))
		}))
		defer server.Close()

		client := NewGitHubCommitStatusClient(server.URL, server.Client(), "token")
		err := client.PublishCommitStatus(context.Background(), SCMCommitStatusPublishRequest{RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", State: domain.SCMCommitStatusStatePending, Description: "desc"})
		if err == nil {
			t.Fatal("expected rate-limit error")
		}
		var publishErr *GitHubCommitStatusError
		if !errors.As(err, &publishErr) {
			t.Fatalf("expected github publish error, got %v", err)
		}
		if !publishErr.Retryable() || publishErr.Reason() != "github_status_rate_limited" {
			t.Fatalf("expected retryable rate limit error, got retryable=%v reason=%q", publishErr.Retryable(), publishErr.Reason())
		}
		if len(publishErr.message) > maxGitHubCommitStatusResponseBodyBytes {
			t.Fatalf("expected bounded response body, got %d bytes", len(publishErr.message))
		}
	})
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

	buildRepo.build = domain.Build{ID: "build-2", ProjectID: "project-1", JobID: &jobID, CreatedAt: time.Date(2026, 7, 16, 18, 0, 0, 0, time.UTC), Status: domain.BuildStatusQueued, RepoURL: strPtr("https://github.com/octo/repo.git"), Ref: strPtr("refs/heads/main")}
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

type blockingSCMPublisher struct {
	mu          sync.Mutex
	reqs        []SCMCommitStatusPublishRequest
	remote      map[string]SCMCommitStatusPublishRequest
	beforeWrite func(SCMCommitStatusPublishRequest)
	afterWrite  func(SCMCommitStatusPublishRequest)
}

func newBlockingSCMPublisher(beforeWrite func(SCMCommitStatusPublishRequest), afterWrite func(SCMCommitStatusPublishRequest)) *blockingSCMPublisher {
	return &blockingSCMPublisher{remote: make(map[string]SCMCommitStatusPublishRequest), beforeWrite: beforeWrite, afterWrite: afterWrite}
}

func (p *blockingSCMPublisher) PublishCommitStatus(_ context.Context, req SCMCommitStatusPublishRequest) error {
	if p.beforeWrite != nil {
		p.beforeWrite(req)
	}
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.remote[scmRequestKey(req)] = req
	p.mu.Unlock()
	if p.afterWrite != nil {
		p.afterWrite(req)
	}
	return nil
}

func (p *blockingSCMPublisher) remoteState(provider string, owner string, repo string, sha string, contextName string) (SCMCommitStatusPublishRequest, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	req, ok := p.remote[scmRequestKey(SCMCommitStatusPublishRequest{Provider: provider, RepositoryOwner: owner, RepositoryName: repo, CommitSHA: sha, Context: contextName})]
	return req, ok
}

func (p *blockingSCMPublisher) forceRemote(req SCMCommitStatusPublishRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remote[scmRequestKey(req)] = req
}

func (p *blockingSCMPublisher) sawState(state domain.SCMCommitStatusState) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.reqs {
		if req.State == state {
			return true
		}
	}
	return false
}

func (p *blockingSCMPublisher) countState(state domain.SCMCommitStatusState) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, req := range p.reqs {
		if req.State == state {
			count++
		}
	}
	return count
}

func (p *blockingSCMPublisher) requests() []SCMCommitStatusPublishRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]SCMCommitStatusPublishRequest(nil), p.reqs...)
}

var closeOnceMu sync.Mutex
var closeOnceMap = map[chan struct{}]bool{}

func closeOnce(ch chan struct{}) {
	closeOnceMu.Lock()
	defer closeOnceMu.Unlock()
	if closeOnceMap[ch] {
		return
	}
	closeOnceMap[ch] = true
	close(ch)
}

func scmRequestKey(req SCMCommitStatusPublishRequest) string {
	return strings.Join([]string{strings.TrimSpace(req.Provider), strings.TrimSpace(req.RepositoryOwner), strings.TrimSpace(req.RepositoryName), strings.TrimSpace(req.CommitSHA), strings.TrimSpace(req.Context)}, "|")
}
