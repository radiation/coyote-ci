package build

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

type helperSCMDeliveryRepo struct {
	acquireForDelivery     func(context.Context, repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error)
	markSent               func(context.Context, repository.SCMStatusDeliveryMarkSentInput) (repository.SCMStatusDeliveryUpdateResult, error)
	recordRetryableFailure func(context.Context, repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error)
	recordPermanentFailure func(context.Context, repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error)
	recordExhaustedFailure func(context.Context, repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error)
	markSuperseded         func(context.Context, repository.SCMStatusDeliveryMarkSupersededInput) (repository.SCMStatusDeliveryUpdateResult, error)
	getByKey               func(context.Context, string, string, string, string, string) (domain.SCMStatusDelivery, error)
}

func (r *helperSCMDeliveryRepo) AcquireForDelivery(ctx context.Context, input repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error) {
	if r.acquireForDelivery != nil {
		return r.acquireForDelivery(ctx, input)
	}
	return repository.SCMStatusDeliveryClaimResult{}, nil
}
func (r *helperSCMDeliveryRepo) ListRecoverable(context.Context, repository.SCMStatusDeliveryRecoverableScanInput) ([]domain.SCMStatusDelivery, error) {
	return nil, nil
}
func (r *helperSCMDeliveryRepo) MarkSent(ctx context.Context, input repository.SCMStatusDeliveryMarkSentInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	if r.markSent != nil {
		return r.markSent(ctx, input)
	}
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) RecordRetryableFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	if r.recordRetryableFailure != nil {
		return r.recordRetryableFailure(ctx, input)
	}
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) RecordPermanentFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	if r.recordPermanentFailure != nil {
		return r.recordPermanentFailure(ctx, input)
	}
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) RecordExhaustedFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	if r.recordExhaustedFailure != nil {
		return r.recordExhaustedFailure(ctx, input)
	}
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) MarkSuperseded(ctx context.Context, input repository.SCMStatusDeliveryMarkSupersededInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	if r.markSuperseded != nil {
		return r.markSuperseded(ctx, input)
	}
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) GetByKey(ctx context.Context, provider string, repositoryOwner string, repositoryName string, commitSHA string, contextName string) (domain.SCMStatusDelivery, error) {
	if r.getByKey != nil {
		return r.getByKey(ctx, provider, repositoryOwner, repositoryName, commitSHA, contextName)
	}
	return domain.SCMStatusDelivery{}, repository.ErrSCMStatusDeliveryNotFound
}

type noopSCMStatusReporter struct{}

func (noopSCMStatusReporter) NotifyBuildStatus(context.Context, domain.Build) error { return nil }

type helperPublisher struct {
	reqs []SCMCommitStatusPublishRequest
	err  error
}

func (p *helperPublisher) PublishCommitStatus(_ context.Context, req SCMCommitStatusPublishRequest) error {
	p.reqs = append(p.reqs, req)
	return p.err
}

type scmTimeoutNetError struct{}

func (scmTimeoutNetError) Error() string   { return "timeout" }
func (scmTimeoutNetError) Timeout() bool   { return true }
func (scmTimeoutNetError) Temporary() bool { return true }

type helperPublisherError struct {
	retryable bool
	reason    string
}

func (e helperPublisherError) Error() string   { return e.reason }
func (e helperPublisherError) Retryable() bool { return e.retryable }
func (e helperPublisherError) Reason() string  { return e.reason }

func TestSCMStatusBuildHelpers(t *testing.T) {
	t.Run("reporter constructor validation", func(t *testing.T) {
		_, err := NewSCMStatusReporter(SCMStatusReporterConfig{})
		if err == nil {
			t.Fatal("expected missing dependency error")
		}
		reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: &multiBuildRepository{}, ProjectRepo: &fakeSCMProjectRepository{}, DeliveryRepo: memoryrepo.NewSCMStatusDeliveryRepository(), Publisher: &recordingSCMPublisher{}, ClaimDuration: -time.Second})
		if err != nil {
			t.Fatalf("expected defaulted negative claim duration to succeed, got %v", err)
		}
		if reporter.claimDuration != defaultSCMStatusDeliveryClaimDuration {
			t.Fatalf("expected default claim duration %v, got %v", defaultSCMStatusDeliveryClaimDuration, reporter.claimDuration)
		}
	})

	t.Run("retry policy and claim helpers", func(t *testing.T) {
		policy := defaultSCMStatusRetryPolicy()
		if policy.delayForAttempt(0) != defaultSCMStatusRetryInitialDelay || policy.delayForAttempt(1) != defaultSCMStatusRetryInitialDelay || policy.delayForAttempt(2) != defaultSCMStatusRetryInitialDelay*2 || policy.delayForAttempt(3) != defaultSCMStatusRetryInitialDelay*4 || policy.delayForAttempt(6) != defaultSCMStatusRetryMaxDelay {
			t.Fatalf("unexpected retry delays: attempt0=%v attempt1=%v attempt2=%v attempt3=%v attempt6=%v", policy.delayForAttempt(0), policy.delayForAttempt(1), policy.delayForAttempt(2), policy.delayForAttempt(3), policy.delayForAttempt(6))
		}
		if scmStatusClaimDuration(0) != defaultSCMStatusDeliveryClaimDuration || scmStatusClaimDuration(time.Second) != time.Second {
			t.Fatal("unexpected claim duration normalization")
		}
		if validateSCMStatusClaimDuration(0) == nil || validateSCMStatusClaimDuration(time.Second) != nil {
			t.Fatal("unexpected claim duration validation result")
		}
		if scmStatusClaimOwner("  ") != "inline-scm-status-reporter" || scmStatusClaimOwner(" worker ") != "worker" {
			t.Fatal("unexpected claim owner normalization")
		}
	})

	t.Run("reporting helpers", func(t *testing.T) {
		triggerProvider := "github"
		triggerOwner := "octo"
		triggerRepo := "repo"
		triggerSHA := "cafebabe"
		build := domain.Build{ID: "build-1", Status: domain.BuildStatusRunning, RepoURL: strPtr("https://github.com/octo/repo.git"), SourceSHA: strPtr("deadbeef")}
		build.Trigger = domain.BuildTrigger{SCMProvider: &triggerProvider, RepositoryOwner: &triggerOwner, RepositoryName: &triggerRepo, CommitSHA: &triggerSHA}
		provider, owner, repo, ok := scmStatusRepositoryIdentity(build)
		if !ok || provider != "github" || owner != "octo" || repo != "repo" {
			t.Fatalf("unexpected scm repository identity: %s %s %s %v", provider, owner, repo, ok)
		}
		if scmStatusCommitSHA(build) != "deadbeef" {
			t.Fatalf("expected source sha precedence, got %q", scmStatusCommitSHA(build))
		}
		if state, _, stateOK := scmStatusStateForBuild(domain.Build{Status: domain.BuildStatusCanceled}); !stateOK || state != domain.SCMCommitStatusStateError {
			t.Fatalf("unexpected canceled state mapping: %s %v", state, stateOK)
		}
		if _, _, emptyStateOK := scmStatusStateForBuild(domain.Build{}); emptyStateOK {
			t.Fatal("expected unknown build status to be skipped")
		}
		if scmStatusBuildCreatedAt(domain.Build{}, time.Unix(123, 0).UTC()) != time.Unix(123, 0).UTC() {
			t.Fatal("expected fallback build creation time")
		}
		repoURLOnly := domain.Build{RepoURL: strPtr("https://github.com/octo/repo.git")}
		fallbackProvider, fallbackOwner, fallbackRepo, fallbackOK := scmStatusRepositoryIdentity(repoURLOnly)
		if !fallbackOK || fallbackProvider != "github" || fallbackOwner != "octo" || fallbackRepo != "repo" {
			t.Fatalf("unexpected repo-url fallback identity: %s %s %s %v", fallbackProvider, fallbackOwner, fallbackRepo, fallbackOK)
		}
		if got := scmStatusContextName(strings.Repeat("project", 30), "job-1"); len([]rune(got)) > maxSCMStatusContextLength {
			t.Fatalf("expected bounded context name, got %q", got)
		}
		if got := scmStatusDetailsURL("", "build-1"); got != nil {
			t.Fatalf("expected nil details url without public base, got %v", *got)
		}
		if got := scmStatusDetailsURL("https://ci.example.com", "build-1"); got == nil || *got != "https://ci.example.com/builds/build-1" {
			t.Fatalf("unexpected details url: %v", got)
		}
		detailsURL := "https://ci.example.com/builds/1"
		left := domain.SCMStatusDelivery{Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: domain.SCMCommitStatusStatePending, Description: "desc", DetailsURL: &detailsURL}
		publishRequest := scmStatusPublishRequest(left)
		if publishRequest.Provider != left.Provider || publishRequest.RepositoryOwner != left.RepositoryOwner || publishRequest.RepositoryName != left.RepositoryName || publishRequest.CommitSHA != left.CommitSHA || publishRequest.Context != left.Context || publishRequest.State != left.DesiredState || publishRequest.Description != left.Description || publishRequest.DetailsURL != left.DetailsURL {
			t.Fatalf("unexpected publish request: %+v", publishRequest)
		}
		right := left
		if !scmStatusPublishEquivalent(left, right) {
			t.Fatal("expected equal publish payloads to match")
		}
		right.Description = "other"
		if scmStatusPublishEquivalent(left, right) {
			t.Fatal("did not expect different publish payloads to match")
		}
		if truncateSCMStatusText("  hello  ", 0) != "hello" {
			t.Fatal("unexpected truncation result")
		}
		if got := truncateSCMStatusText("  alphabet  ", 5); got != "alpha" {
			t.Fatalf("expected truncated text, got %q", got)
		}
		if _, err := claimedSCMStatusTimestamp(domain.SCMStatusDelivery{}); err == nil {
			t.Fatal("expected missing claimed timestamp error")
		}
		claimedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.FixedZone("UTC-4", -4*60*60))
		claimedTime, claimedErr := claimedSCMStatusTimestamp(domain.SCMStatusDelivery{ClaimedAt: &claimedAt})
		if claimedErr != nil || !claimedTime.Equal(claimedAt.UTC()) {
			t.Fatalf("expected claimed timestamp in UTC, got %v err=%v", claimedTime, claimedErr)
		}
		if parsedOwner, parsedRepo, parsedOK := parseGitHubRepositoryURL("https://gitlab.com/octo/repo"); parsedOK || parsedOwner != "" || parsedRepo != "" {
			t.Fatalf("expected invalid github url to fail, got %q %q %v", parsedOwner, parsedRepo, parsedOK)
		}
		owner, repo, ok = parseGitHubRepositoryURL("https://github.com/octo/repo.git")
		if !ok || owner != "octo" || repo != "repo" {
			t.Fatalf("unexpected github url parse result: %q %q %v", owner, repo, ok)
		}
	})

	t.Run("failure classification", func(t *testing.T) {
		if decision := classifySCMStatusDeliveryFailure(context.Canceled); !decision.retryable || decision.reason != "context_canceled" {
			t.Fatalf("unexpected cancellation decision: %+v", decision)
		}
		if decision := classifySCMStatusDeliveryFailure(scmTimeoutNetError{}); !decision.retryable || decision.reason != "network_timeout" {
			t.Fatalf("unexpected timeout decision: %+v", decision)
		}
		if decision := classifySCMStatusDeliveryFailure(helperPublisherError{retryable: false, reason: "bad_request"}); decision.retryable || decision.reason != "bad_request" {
			t.Fatalf("unexpected publisher decision: %+v", decision)
		}
		if decision := classifySCMStatusDeliveryFailure(errors.New("boom")); !decision.retryable || decision.reason != "github_status_send_failed" {
			t.Fatalf("unexpected generic decision: %+v", decision)
		}
	})
}

func TestBuildService_GetBuildSCMStatus(t *testing.T) {
	now := time.Date(2026, 7, 17, 14, 0, 0, 0, time.UTC)
	provider := "github"
	repoOwner := "octo"
	repoName := "repo"
	jobID := "job-1"
	project := &domain.Project{ID: "project-1", Slug: "payments"}
	baseBuild := domain.Build{
		ID:            "build-1",
		ProjectID:     project.ID,
		JobID:         &jobID,
		Status:        domain.BuildStatusFailed,
		AttemptNumber: 1,
		CreatedAt:     now,
		Trigger: domain.BuildTrigger{
			SCMProvider:     &provider,
			RepositoryOwner: &repoOwner,
			RepositoryName:  &repoName,
		},
		CommitSHA: stringPtr("deadbeef"),
	}

	t.Run("unlinked returns nil", func(t *testing.T) {
		svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
		view, err := svc.GetBuildSCMStatus(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusQueued, CreatedAt: now}, project)
		if err != nil {
			t.Fatalf("GetBuildSCMStatus failed: %v", err)
		}
		if view != nil {
			t.Fatalf("expected nil scm status for unlinked build, got %+v", view)
		}
	})

	t.Run("linked without authoritative sha is not reportable", func(t *testing.T) {
		svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
		build := baseBuild
		build.CommitSHA = nil
		view, err := svc.GetBuildSCMStatus(context.Background(), build, project)
		if err != nil {
			t.Fatalf("GetBuildSCMStatus failed: %v", err)
		}
		if view == nil || view.Reportable || view.CommitSHA != nil || view.Provider != "github" || view.RepositoryOwner != "octo" || view.RepositoryName != "repo" {
			t.Fatalf("unexpected unresolved-sha view: %+v", view)
		}
	})

	t.Run("reportable but publishing disabled exposes derived fields", func(t *testing.T) {
		svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
		view, err := svc.GetBuildSCMStatus(context.Background(), baseBuild, project)
		if err != nil {
			t.Fatalf("GetBuildSCMStatus failed: %v", err)
		}
		if view == nil || !view.Reportable || view.Configured || view.Context == nil || *view.Context != "coyote/payments/job-1" || view.DesiredState == nil || *view.DesiredState != "failure" {
			t.Fatalf("unexpected disabled view: %+v", view)
		}
	})

	t.Run("reportable build without persisted delivery returns derived fields", func(t *testing.T) {
		svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
		svc.scmStatusReporter = noopSCMStatusReporter{}
		svc.scmStatusDeliveryRepo = &helperSCMDeliveryRepo{}
		view, err := svc.GetBuildSCMStatus(context.Background(), baseBuild, project)
		if err != nil {
			t.Fatalf("GetBuildSCMStatus failed: %v", err)
		}
		if view == nil || !view.Reportable || !view.Configured || view.DeliveryState != nil {
			t.Fatalf("unexpected missing-delivery view: %+v", view)
		}
	})

	t.Run("delivery lookup errors bubble up", func(t *testing.T) {
		svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
		svc.scmStatusReporter = noopSCMStatusReporter{}
		svc.scmStatusDeliveryRepo = &helperSCMDeliveryRepo{getByKey: func(context.Context, string, string, string, string, string) (domain.SCMStatusDelivery, error) {
			return domain.SCMStatusDelivery{}, errors.New("boom")
		}}
		if _, err := svc.GetBuildSCMStatus(context.Background(), baseBuild, project); err == nil || err.Error() != "boom" {
			t.Fatalf("expected boom error, got %v", err)
		}
	})

	t.Run("missing project context keeps linked build non-reportable", func(t *testing.T) {
		svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
		view, err := svc.GetBuildSCMStatus(context.Background(), baseBuild, nil)
		if err != nil {
			t.Fatalf("GetBuildSCMStatus failed: %v", err)
		}
		if view == nil || view.Reportable || view.Context != nil {
			t.Fatalf("expected missing context to keep view non-reportable, got %+v", view)
		}
	})

	configuredService := func(authoritative domain.SCMStatusDelivery) *BuildService {
		svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
		svc.scmStatusReporter = noopSCMStatusReporter{}
		svc.scmStatusDeliveryRepo = &helperSCMDeliveryRepo{
			getByKey: func(context.Context, string, string, string, string, string) (domain.SCMStatusDelivery, error) {
				return authoritative, nil
			},
		}
		return svc
	}

	cases := []struct {
		name                 string
		authoritative        domain.SCMStatusDelivery
		build                domain.Build
		wantDeliveryState    string
		wantAwaitingReassert bool
		wantLastSentState    *string
		wantLastError        *string
		wantOwnerBuildID     *string
		wantOwnerAttempt     *int
	}{
		{
			name:              "pending state maps directly",
			authoritative:     domain.SCMStatusDelivery{ID: "delivery-1", BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", DesiredState: domain.SCMCommitStatusStatePending, Status: domain.SCMStatusDeliveryStatusPending, MaxAttempts: 3},
			build:             baseBuild,
			wantDeliveryState: "pending",
		},
		{
			name:              "sending state maps directly",
			authoritative:     domain.SCMStatusDelivery{ID: "delivery-1", BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", DesiredState: domain.SCMCommitStatusStatePending, Status: domain.SCMStatusDeliveryStatusSending, Attempts: 1, MaxAttempts: 3, ClaimedAt: &now, ClaimExpiresAt: scmTimePtr(now.Add(time.Minute)), ClaimedBy: stringPtr("server-1")},
			build:             baseBuild,
			wantDeliveryState: "sending",
		},
		{
			name:              "sent exposes last sent state",
			authoritative:     domain.SCMStatusDelivery{ID: "delivery-1", BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", DesiredState: domain.SCMCommitStatusStateSuccess, LastSentState: scmStatePtr(domain.SCMCommitStatusStateSuccess), Status: domain.SCMStatusDeliveryStatusSent, Attempts: 1, MaxAttempts: 3, SentAt: &now},
			build:             baseBuild,
			wantDeliveryState: "sent",
			wantLastSentState: stringPtr("success"),
		},
		{
			name:              "retry waiting surfaces retry metadata",
			authoritative:     domain.SCMStatusDelivery{ID: "delivery-1", BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", DesiredState: domain.SCMCommitStatusStateFailure, Status: domain.SCMStatusDeliveryStatusRetryWaiting, Attempts: 2, MaxAttempts: 3, NextAttemptAt: scmTimePtr(now.Add(2 * time.Minute)), FailureCategory: scmDeliveryFailureCategoryPtr(domain.SCMStatusDeliveryFailureCategoryRetryable), FailureReason: stringPtr("github_status_http_retryable"), LastError: stringPtr("GitHub rate limit exceeded")},
			build:             baseBuild,
			wantDeliveryState: "retry_waiting",
			wantLastError:     stringPtr("GitHub rate limit exceeded"),
		},
		{
			name:                 "awaiting reassertion is distinguished structurally",
			authoritative:        domain.SCMStatusDelivery{ID: "delivery-1", BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", DesiredState: domain.SCMCommitStatusStatePending, Status: domain.SCMStatusDeliveryStatusRetryWaiting, Attempts: 1, MaxAttempts: 3, NextAttemptAt: scmTimePtr(now.Add(3 * time.Minute)), FailureCategory: scmDeliveryFailureCategoryPtr(domain.SCMStatusDeliveryFailureCategoryRetryable), FailureReason: stringPtr(scmStatusFailureReasonAuthoritativeReassert)},
			build:                baseBuild,
			wantDeliveryState:    "retry_waiting",
			wantAwaitingReassert: true,
		},
		{
			name:              "permanent failure preserves bounded error",
			authoritative:     domain.SCMStatusDelivery{ID: "delivery-1", BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", DesiredState: domain.SCMCommitStatusStateFailure, Status: domain.SCMStatusDeliveryStatusFailedPermanent, Attempts: 1, MaxAttempts: 3, FailureCategory: scmDeliveryFailureCategoryPtr(domain.SCMStatusDeliveryFailureCategoryPermanent), FailureReason: stringPtr("github_status_invalid_input"), LastError: stringPtr("bad request")},
			build:             baseBuild,
			wantDeliveryState: "failed_permanent",
			wantLastError:     stringPtr("bad request"),
		},
		{
			name:              "exhausted failure maps directly",
			authoritative:     domain.SCMStatusDelivery{ID: "delivery-1", BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", DesiredState: domain.SCMCommitStatusStateFailure, Status: domain.SCMStatusDeliveryStatusFailedExhausted, Attempts: 3, MaxAttempts: 3, FailureCategory: scmDeliveryFailureCategoryPtr(domain.SCMStatusDeliveryFailureCategoryRetryable), FailureReason: stringPtr("github_status_http_retryable"), LastError: stringPtr("still failing")},
			build:             baseBuild,
			wantDeliveryState: "failed_exhausted",
			wantLastError:     stringPtr("still failing"),
		},
		{
			name:              "superseded view includes authoritative owner without leaking active retry state",
			authoritative:     domain.SCMStatusDelivery{ID: "delivery-new", BuildID: "build-2", BuildAttempt: 2, BuildCreatedAt: now.Add(time.Second), Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "coyote/payments/job-1", DesiredState: domain.SCMCommitStatusStateSuccess, Status: domain.SCMStatusDeliveryStatusRetryWaiting, Attempts: 4, MaxAttempts: 5, LastSentState: scmStatePtr(domain.SCMCommitStatusStatePending), NextAttemptAt: scmTimePtr(now.Add(2 * time.Minute)), LastError: stringPtr("owner retrying")},
			build:             baseBuild,
			wantDeliveryState: "superseded",
			wantOwnerBuildID:  stringPtr("build-2"),
			wantOwnerAttempt:  buildSCMIntPtr(2),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := configuredService(tc.authoritative)
			view, err := svc.GetBuildSCMStatus(context.Background(), tc.build, project)
			if err != nil {
				t.Fatalf("GetBuildSCMStatus failed: %v", err)
			}
			if view == nil || !view.Reportable || !view.Configured || view.DeliveryState == nil || *view.DeliveryState != tc.wantDeliveryState {
				t.Fatalf("unexpected scm view: %+v", view)
			}
			if view.AwaitingReassertion != tc.wantAwaitingReassert {
				t.Fatalf("expected awaiting_reassertion=%v, got %+v", tc.wantAwaitingReassert, view)
			}
			if !equalStringPtrs(view.LastSentState, tc.wantLastSentState) {
				t.Fatalf("unexpected last sent state: got=%v want=%v", view.LastSentState, tc.wantLastSentState)
			}
			if !equalStringPtrs(view.LastError, tc.wantLastError) {
				t.Fatalf("unexpected last error: got=%v want=%v", view.LastError, tc.wantLastError)
			}
			if !equalStringPtrs(view.CurrentOwnerBuildID, tc.wantOwnerBuildID) {
				t.Fatalf("unexpected owner build id: got=%v want=%v", view.CurrentOwnerBuildID, tc.wantOwnerBuildID)
			}
			if !equalIntPtrs(view.CurrentOwnerAttempt, tc.wantOwnerAttempt) {
				t.Fatalf("unexpected owner attempt: got=%v want=%v", view.CurrentOwnerAttempt, tc.wantOwnerAttempt)
			}
			if tc.wantDeliveryState == "superseded" && (view.Attempts != nil || view.NextAttemptAt != nil || view.LastError != nil || view.LastSentState != nil) {
				t.Fatalf("superseded view leaked authoritative delivery details: %+v", view)
			}
		})
	}

	t.Run("older attempt resolves shared stream as superseded while current owner remains authoritative", func(t *testing.T) {
		older := baseBuild
		older.ID = "build-1"
		older.AttemptNumber = 1
		older.Status = domain.BuildStatusQueued
		newer := baseBuild
		newer.ID = "build-2"
		newer.AttemptNumber = 2
		newer.CreatedAt = now.Add(time.Minute)
		newer.Status = domain.BuildStatusFailed

		buildRepo := &multiBuildRepository{builds: map[string]domain.Build{"build-1": older, "build-2": newer}, byJob: map[string][]domain.Build{jobID: {newer, older}}}
		deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
		reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{BuildRepo: buildRepo, ProjectRepo: &fakeSCMProjectRepository{project: *project}, DeliveryRepo: deliveryRepo, Publisher: &recordingSCMPublisher{err: timeoutPublisherError{}}})
		if err != nil {
			t.Fatalf("new reporter failed: %v", err)
		}
		reporter.now = func() time.Time { return now }
		if notifyErr := reporter.NotifyBuildStatus(context.Background(), older); notifyErr != nil {
			t.Fatalf("notify older build failed: %v", notifyErr)
		}
		reporter.now = func() time.Time { return now.Add(2 * time.Minute) }
		if notifyErr := reporter.NotifyBuildStatus(context.Background(), newer); notifyErr == nil {
			t.Fatal("expected retryable publish error for newer build")
		}

		svc := NewBuildService(&fakeBuildRepository{}, nil, nil)
		svc.scmStatusReporter = noopSCMStatusReporter{}
		svc.scmStatusDeliveryRepo = deliveryRepo

		olderView, olderErr := svc.GetBuildSCMStatus(context.Background(), older, project)
		if olderErr != nil {
			t.Fatalf("older view failed: %v", olderErr)
		}
		if olderView == nil || olderView.DeliveryState == nil || *olderView.DeliveryState != "superseded" {
			t.Fatalf("expected superseded older view, got %+v", olderView)
		}
		if !equalStringPtrs(olderView.CurrentOwnerBuildID, stringPtr("build-2")) || !equalIntPtrs(olderView.CurrentOwnerAttempt, buildSCMIntPtr(2)) {
			t.Fatalf("unexpected older view owner fields: %+v", olderView)
		}
		if olderView.Attempts != nil || olderView.NextAttemptAt != nil || olderView.LastError != nil || olderView.LastSentState != nil {
			t.Fatalf("older superseded view leaked authoritative delivery details: %+v", olderView)
		}

		newerView, newerErr := svc.GetBuildSCMStatus(context.Background(), newer, project)
		if newerErr != nil {
			t.Fatalf("newer view failed: %v", newerErr)
		}
		if newerView == nil || newerView.DeliveryState == nil || *newerView.DeliveryState != "retry_waiting" {
			t.Fatalf("expected retry_waiting newer view, got %+v", newerView)
		}
		if newerView.Attempts == nil || *newerView.Attempts < 1 || newerView.LastError == nil {
			t.Fatalf("expected active retry metadata on newer view, got %+v", newerView)
		}
	})
}

func scmStatePtr(state domain.SCMCommitStatusState) *domain.SCMCommitStatusState {
	return &state
}

func scmTimePtr(value time.Time) *time.Time {
	copyValue := value.UTC()
	return &copyValue
}

func scmDeliveryFailureCategoryPtr(value domain.SCMStatusDeliveryFailureCategory) *domain.SCMStatusDeliveryFailureCategory {
	return &value
}

func equalStringPtrs(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalIntPtrs(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func TestSCMStatusRecoveryDrainHelpers(t *testing.T) {
	t.Run("constructor validation", func(t *testing.T) {
		if _, err := NewSCMStatusRecoveryDrain(SCMStatusRecoveryDrainConfig{}); err == nil {
			t.Fatal("expected missing reporter error")
		}
		reporter := &SCMStatusReporter{}
		if _, err := NewSCMStatusRecoveryDrain(SCMStatusRecoveryDrainConfig{Reporter: reporter, Interval: time.Second, BatchSize: 1}); err == nil {
			t.Fatal("expected missing delivery repo error")
		}
		reporter.deliveryRepo = memoryrepo.NewSCMStatusDeliveryRepository()
		if _, err := NewSCMStatusRecoveryDrain(SCMStatusRecoveryDrainConfig{Reporter: reporter, Interval: 0, BatchSize: 1}); err == nil {
			t.Fatal("expected invalid interval error")
		}
		if _, err := NewSCMStatusRecoveryDrain(SCMStatusRecoveryDrainConfig{Reporter: reporter, Interval: time.Second, BatchSize: 0}); err == nil {
			t.Fatal("expected invalid batch size error")
		}
	})

	t.Run("apply attempt result", func(t *testing.T) {
		cases := []scmStatusRecoveryAttemptResult{{claimOutcome: repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed, executionOutcome: scmStatusExecutionOutcomeSent}, {claimOutcome: repository.SCMStatusDeliveryClaimOutcomeRetryClaimed, executionOutcome: scmStatusExecutionOutcomeReassertScheduled}, {claimOutcome: repository.SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed, executionOutcome: scmStatusExecutionOutcomeRetryScheduled}, {claimOutcome: repository.SCMStatusDeliveryClaimOutcomeClaimedByOther, executionOutcome: scmStatusExecutionOutcomePermanentlyFailed}, {claimOutcome: repository.SCMStatusDeliveryClaimOutcomeRetryNotDue, executionOutcome: scmStatusExecutionOutcomeAttemptsExhausted}, {claimOutcome: repository.SCMStatusDeliveryClaimOutcomeAlreadySent, executionOutcome: scmStatusExecutionOutcomeLostClaim, rehydrationFailed: true}, {claimOutcome: repository.SCMStatusDeliveryClaimOutcomeSuperseded, executionOutcome: scmStatusExecutionOutcomeSuperseded}, {claimOutcome: repository.SCMStatusDeliveryClaimOutcome("other")}}
		var result SCMStatusRecoveryIterationResult
		drain := &SCMStatusRecoveryDrain{}
		for _, attempt := range cases {
			drain.applyAttemptResult(&result, attempt)
		}
		if result.ClaimAcquired != 1 || result.RetryClaimed != 1 || result.StaleClaimReclaimed != 1 || result.SkippedContention != 1 || result.SkippedNotDue != 1 || result.SkippedTerminal != 2 || result.Sent != 1 || result.ReassertScheduled != 1 || result.RetryScheduled != 1 || result.PermanentlyFailed != 1 || result.AttemptsExhausted != 1 || result.LostClaim != 1 || result.Superseded != 1 || result.RehydrationFailed != 1 {
			t.Fatalf("unexpected recovery counters: %+v", result)
		}
	})

	t.Run("run returns canceled context", func(t *testing.T) {
		reporter := &SCMStatusReporter{deliveryRepo: memoryrepo.NewSCMStatusDeliveryRepository()}
		drain, err := NewSCMStatusRecoveryDrain(SCMStatusRecoveryDrainConfig{Reporter: reporter, Interval: time.Millisecond, BatchSize: 1})
		if err != nil {
			t.Fatalf("new drain failed: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if runErr := drain.Run(ctx); !errors.Is(runErr, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", runErr)
		}
	})
}

func TestSCMStatusReporter_ReassertAuthoritativeDelivery(t *testing.T) {
	stale := domain.SCMStatusDelivery{Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: domain.SCMCommitStatusStatePending, Description: "pending"}

	t.Run("not found ignored", func(t *testing.T) {
		reporter := &SCMStatusReporter{deliveryRepo: &helperSCMDeliveryRepo{getByKey: func(context.Context, string, string, string, string, string) (domain.SCMStatusDelivery, error) {
			return domain.SCMStatusDelivery{}, repository.ErrSCMStatusDeliveryNotFound
		}}, publisher: &helperPublisher{}}
		if err := reporter.reassertAuthoritativeDelivery(context.Background(), stale); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("changed authoritative state republishes", func(t *testing.T) {
		publisher := &helperPublisher{}
		reporter := &SCMStatusReporter{deliveryRepo: &helperSCMDeliveryRepo{getByKey: func(context.Context, string, string, string, string, string) (domain.SCMStatusDelivery, error) {
			return domain.SCMStatusDelivery{Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: domain.SCMCommitStatusStateSuccess, Description: "success"}, nil
		}}, publisher: publisher}
		if err := reporter.reassertAuthoritativeDelivery(context.Background(), stale); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(publisher.reqs) != 1 || publisher.reqs[0].State != domain.SCMCommitStatusStateSuccess {
			t.Fatalf("expected one reassert publish, got %+v", publisher.reqs)
		}
	})

	t.Run("publisher failure swallowed", func(t *testing.T) {
		publisher := &helperPublisher{err: net.UnknownNetworkError("boom")}
		reporter := &SCMStatusReporter{deliveryRepo: &helperSCMDeliveryRepo{getByKey: func(context.Context, string, string, string, string, string) (domain.SCMStatusDelivery, error) {
			return domain.SCMStatusDelivery{Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "deadbeef", Context: "ctx", DesiredState: domain.SCMCommitStatusStateSuccess, Description: "success"}, nil
		}}, publisher: publisher}
		if err := reporter.reassertAuthoritativeDelivery(context.Background(), stale); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
}

func TestSCMStatusReporter_ExecutionHelpers(t *testing.T) {
	claimedAt := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	baseDelivery := domain.SCMStatusDelivery{
		ID:              "delivery-1",
		BuildID:         "build-1",
		Provider:        "github",
		RepositoryOwner: "octo",
		RepositoryName:  "repo",
		CommitSHA:       "deadbeef",
		Context:         "ctx",
		DesiredState:    domain.SCMCommitStatusStatePending,
		Description:     "pending",
		Attempts:        1,
		MaxAttempts:     3,
		ClaimedAt:       &claimedAt,
	}

	t.Run("mark sent and reassert pending outcomes", func(t *testing.T) {
		reporter := &SCMStatusReporter{claimOwner: "worker", deliveryRepo: &helperSCMDeliveryRepo{
			markSent: func(_ context.Context, input repository.SCMStatusDeliveryMarkSentInput) (repository.SCMStatusDeliveryUpdateResult, error) {
				if input.DeliveryID != baseDelivery.ID || input.ClaimOwner != "worker" || input.State != baseDelivery.DesiredState {
					t.Fatalf("unexpected mark sent input: %+v", input)
				}
				return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
			},
			recordRetryableFailure: func(_ context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
				if input.DeliveryID != baseDelivery.ID || input.FailureReason != scmStatusFailureReasonAuthoritativeReassert || input.NextAttemptAt == nil {
					t.Fatalf("unexpected reassert pending input: %+v", input)
				}
				return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
			},
		}}

		outcome, err := reporter.markDeliverySent(context.Background(), baseDelivery, claimedAt.Add(time.Minute))
		if err != nil || outcome != scmStatusExecutionOutcomeSent {
			t.Fatalf("expected sent outcome, got outcome=%v err=%v", outcome, err)
		}

		reassertOutcome, reassertErr := reporter.markDeliveryReassertPending(context.Background(), baseDelivery, claimedAt.Add(2*time.Minute), claimedAt.Add(3*time.Minute))
		if reassertErr != nil || reassertOutcome != scmStatusExecutionOutcomeReassertScheduled {
			t.Fatalf("expected reassert scheduled outcome, got outcome=%v err=%v", reassertOutcome, reassertErr)
		}
	})

	t.Run("mark delivery failed routes retryable permanent exhausted and lost claim", func(t *testing.T) {
		retryableReporter := &SCMStatusReporter{claimOwner: "worker", retryPolicy: defaultSCMStatusRetryPolicy(), deliveryRepo: &helperSCMDeliveryRepo{
			recordRetryableFailure: func(_ context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
				if input.FailureCategory != domain.SCMStatusDeliveryFailureCategoryRetryable || input.NextAttemptAt == nil || input.LastError == nil {
					t.Fatalf("unexpected retryable failure input: %+v", input)
				}
				return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
			},
		}}
		retryableOutcome, retryableErr := retryableReporter.markDeliveryFailed(context.Background(), baseDelivery, scmTimeoutNetError{}, claimedAt.Add(time.Minute), scmStatusRecoveryReasonInline)
		if retryableErr != nil || retryableOutcome != scmStatusExecutionOutcomeRetryScheduled {
			t.Fatalf("expected retry scheduled outcome, got outcome=%v err=%v", retryableOutcome, retryableErr)
		}

		permanentReporter := &SCMStatusReporter{claimOwner: "worker", retryPolicy: defaultSCMStatusRetryPolicy(), deliveryRepo: &helperSCMDeliveryRepo{
			recordPermanentFailure: func(_ context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
				if input.FailureCategory != domain.SCMStatusDeliveryFailureCategoryPermanent || input.NextAttemptAt != nil {
					t.Fatalf("unexpected permanent failure input: %+v", input)
				}
				return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
			},
		}}
		permanentOutcome, permanentErr := permanentReporter.markDeliveryFailed(context.Background(), baseDelivery, helperPublisherError{retryable: false, reason: "bad_request"}, claimedAt.Add(2*time.Minute), scmStatusRecoveryReasonInline)
		if permanentErr != nil || permanentOutcome != scmStatusExecutionOutcomePermanentlyFailed {
			t.Fatalf("expected permanently failed outcome, got outcome=%v err=%v", permanentOutcome, permanentErr)
		}

		exhaustedDelivery := baseDelivery
		exhaustedDelivery.Attempts = exhaustedDelivery.MaxAttempts
		exhaustedReporter := &SCMStatusReporter{claimOwner: "worker", retryPolicy: defaultSCMStatusRetryPolicy(), deliveryRepo: &helperSCMDeliveryRepo{
			recordExhaustedFailure: func(_ context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
				if input.FailureCategory != domain.SCMStatusDeliveryFailureCategoryRetryable || input.NextAttemptAt != nil {
					t.Fatalf("unexpected exhausted failure input: %+v", input)
				}
				return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
			},
		}}
		exhaustedOutcome, exhaustedErr := exhaustedReporter.markDeliveryFailed(context.Background(), exhaustedDelivery, scmTimeoutNetError{}, claimedAt.Add(3*time.Minute), scmStatusRecoveryReasonInline)
		if exhaustedErr != nil || exhaustedOutcome != scmStatusExecutionOutcomeAttemptsExhausted {
			t.Fatalf("expected attempts exhausted outcome, got outcome=%v err=%v", exhaustedOutcome, exhaustedErr)
		}

		lostClaimReporter := &SCMStatusReporter{claimOwner: "worker", retryPolicy: defaultSCMStatusRetryPolicy(), deliveryRepo: &helperSCMDeliveryRepo{
			recordPermanentFailure: func(_ context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
				return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeLostClaim}, nil
			},
		}}
		lostClaimOutcome, lostClaimErr := lostClaimReporter.markDeliveryFailed(context.Background(), baseDelivery, helperPublisherError{retryable: false, reason: "forbidden"}, claimedAt.Add(4*time.Minute), scmStatusRecoveryReasonInline)
		if lostClaimErr != nil || lostClaimOutcome != scmStatusExecutionOutcomeLostClaim {
			t.Fatalf("expected lost claim outcome, got outcome=%v err=%v", lostClaimOutcome, lostClaimErr)
		}
	})

	t.Run("execute claimed delivery build lookup failure and recover rehydration failure", func(t *testing.T) {
		reporter := &SCMStatusReporter{
			buildRepo:   &multiBuildRepository{err: repository.ErrBuildNotFound},
			claimOwner:  "worker",
			retryPolicy: defaultSCMStatusRetryPolicy(),
			publisher:   &helperPublisher{},
			deliveryRepo: &helperSCMDeliveryRepo{
				recordRetryableFailure: func(_ context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
					return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeUpdated}, nil
				},
				acquireForDelivery: func(_ context.Context, input repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error) {
					return repository.SCMStatusDeliveryClaimResult{Delivery: baseDelivery, Outcome: repository.SCMStatusDeliveryClaimOutcomeRetryClaimed}, nil
				},
			},
			now: func() time.Time { return claimedAt.Add(5 * time.Minute) },
		}

		outcome, executeErr := reporter.executeClaimedDelivery(context.Background(), baseDelivery, nil, scmStatusRecoveryReasonInline)
		if !errors.Is(executeErr, repository.ErrBuildNotFound) || outcome != scmStatusExecutionOutcomeRetryScheduled {
			t.Fatalf("expected retry scheduled with original build error, got outcome=%v err=%v", outcome, executeErr)
		}

		attempt, recoverErr := reporter.recoverDelivery(context.Background(), baseDelivery, scmStatusRecoveryReasonDrain)
		if recoverErr != nil {
			t.Fatalf("expected nil recover error, got %v", recoverErr)
		}
		if attempt.claimOutcome != repository.SCMStatusDeliveryClaimOutcomeRetryClaimed || !attempt.rehydrationFailed || attempt.executionOutcome != scmStatusExecutionOutcomeRetryScheduled {
			t.Fatalf("unexpected recover attempt: %+v", attempt)
		}
	})
}

func TestSCMStatusReporter_ControlFlowBranches(t *testing.T) {
	baseNow := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	jobID := "job-1"
	projectID := "project-1"
	build := domain.Build{
		ID:            "build-1",
		ProjectID:     projectID,
		JobID:         &jobID,
		AttemptNumber: 1,
		CreatedAt:     baseNow,
		Status:        domain.BuildStatusQueued,
		CommitSHA:     strPtr("deadbeef"),
		RepoURL:       strPtr("https://github.com/octo/repo.git"),
	}

	t.Run("constructor validates each dependency", func(t *testing.T) {
		projectRepo := &fakeSCMProjectRepository{project: domain.Project{ID: projectID, Slug: "payments"}}
		deliveryRepo := memoryrepo.NewSCMStatusDeliveryRepository()
		publisher := &recordingSCMPublisher{}
		cases := []SCMStatusReporterConfig{
			{ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo, Publisher: publisher},
			{BuildRepo: &multiBuildRepository{}, DeliveryRepo: deliveryRepo, Publisher: publisher},
			{BuildRepo: &multiBuildRepository{}, ProjectRepo: projectRepo, Publisher: publisher},
			{BuildRepo: &multiBuildRepository{}, ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo},
		}
		for idx, cfg := range cases {
			if _, err := NewSCMStatusReporter(cfg); err == nil {
				t.Fatalf("expected constructor error for case %d", idx)
			}
		}
	})

	t.Run("nil reporter and plan-delivery skips", func(t *testing.T) {
		var nilReporter *SCMStatusReporter
		if err := nilReporter.NotifyBuildStatus(context.Background(), build); err != nil {
			t.Fatalf("expected nil reporter to no-op, got %v", err)
		}

		reporter := &SCMStatusReporter{
			projectRepo: &fakeSCMProjectRepository{project: domain.Project{ID: projectID, Slug: "payments"}},
			now:         func() time.Time { return baseNow },
		}
		if _, ok, err := reporter.planDelivery(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusQueued}); err != nil || ok {
			t.Fatalf("expected build without github identity to skip, got ok=%v err=%v", ok, err)
		}
		withoutCommit := build
		withoutCommit.CommitSHA = nil
		withoutCommit.RepoURL = strPtr("https://github.com/octo/repo.git")
		if _, ok, err := reporter.planDelivery(context.Background(), withoutCommit); err != nil || ok {
			t.Fatalf("expected build without commit sha to skip, got ok=%v err=%v", ok, err)
		}
	})

	t.Run("build context and acquire delivery branches", func(t *testing.T) {
		projectErr := errors.New("project lookup failed")
		reporter := &SCMStatusReporter{
			projectRepo:   &fakeSCMProjectRepository{err: projectErr},
			deliveryRepo:  &helperSCMDeliveryRepo{},
			claimOwner:    "worker",
			claimDuration: time.Minute,
			retryPolicy:   defaultSCMStatusRetryPolicy(),
			now:           func() time.Time { return baseNow },
		}
		if _, ok, err := reporter.buildContextName(context.Background(), build); !errors.Is(err, projectErr) || ok {
			t.Fatalf("expected project lookup error, got ok=%v err=%v", ok, err)
		}

		reporter.projectRepo = &fakeSCMProjectRepository{project: domain.Project{ID: projectID, Slug: "   "}}
		if _, ok, err := reporter.buildContextName(context.Background(), build); err != nil || ok {
			t.Fatalf("expected empty slug to skip, got ok=%v err=%v", ok, err)
		}

		acquireErr := errors.New("acquire failed")
		reporter.projectRepo = &fakeSCMProjectRepository{project: domain.Project{ID: projectID, Slug: "payments"}}
		reporter.deliveryRepo = &helperSCMDeliveryRepo{acquireForDelivery: func(_ context.Context, input repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error) {
			if input.ClaimOwner != "worker" || input.MaxAttempts != defaultSCMStatusDeliveryMaxAttempts {
				t.Fatalf("unexpected claim input: %+v", input)
			}
			return repository.SCMStatusDeliveryClaimResult{}, acquireErr
		}}
		if _, shouldSend, err := reporter.acquireDelivery(context.Background(), build, scmStatusRecoveryReasonInline); !errors.Is(err, acquireErr) || shouldSend {
			t.Fatalf("expected acquire error, got shouldSend=%v err=%v", shouldSend, err)
		}

		for _, outcome := range []repository.SCMStatusDeliveryClaimOutcome{
			repository.SCMStatusDeliveryClaimOutcomeAlreadySent,
			repository.SCMStatusDeliveryClaimOutcomeClaimedByOther,
			repository.SCMStatusDeliveryClaimOutcomeRetryNotDue,
		} {
			reporter.deliveryRepo = &helperSCMDeliveryRepo{acquireForDelivery: func(_ context.Context, input repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error) {
				return repository.SCMStatusDeliveryClaimResult{Delivery: input.Delivery, Outcome: outcome}, nil
			}}
			result, shouldSend, err := reporter.acquireDelivery(context.Background(), build, scmStatusRecoveryReasonInline)
			if err != nil || shouldSend || result.Outcome != outcome {
				t.Fatalf("expected skip outcome %q, got result=%+v shouldSend=%v err=%v", outcome, result, shouldSend, err)
			}
		}
	})

	t.Run("execute claimed delivery handles superseded and lost-claim reassert paths", func(t *testing.T) {
		claimedAt := baseNow.Add(time.Minute)
		baseDelivery := domain.SCMStatusDelivery{
			ID:              "delivery-1",
			BuildID:         build.ID,
			BuildAttempt:    build.AttemptNumber,
			BuildCreatedAt:  build.CreatedAt,
			Provider:        "github",
			RepositoryOwner: "octo",
			RepositoryName:  "repo",
			CommitSHA:       "deadbeef",
			Context:         "coyote/payments/job-1",
			DesiredState:    domain.SCMCommitStatusStatePending,
			Description:     "Coyote build is queued",
			Attempts:        1,
			MaxAttempts:     3,
			ClaimedAt:       &claimedAt,
		}

		newer := build
		newer.ID = "build-2"
		newer.AttemptNumber = 2
		newer.CreatedAt = build.CreatedAt.Add(time.Minute)
		buildRepo := &multiBuildRepository{builds: map[string]domain.Build{build.ID: build, newer.ID: newer}, byJob: map[string][]domain.Build{jobID: {newer, build}}}
		supersededReporter := &SCMStatusReporter{
			buildRepo:  buildRepo,
			claimOwner: "worker",
			deliveryRepo: &helperSCMDeliveryRepo{markSuperseded: func(_ context.Context, input repository.SCMStatusDeliveryMarkSupersededInput) (repository.SCMStatusDeliveryUpdateResult, error) {
				if input.Reason != "newer_build_attempt_exists" || input.ClaimOwner == nil || *input.ClaimOwner != "worker" {
					t.Fatalf("unexpected supersede input: %+v", input)
				}
				return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeLostClaim}, nil
			}},
			publisher: &helperPublisher{},
			now:       func() time.Time { return baseNow.Add(2 * time.Minute) },
		}
		outcome, execErr := supersededReporter.executeClaimedDelivery(context.Background(), baseDelivery, nil, scmStatusRecoveryReasonInline)
		if execErr != nil || outcome != scmStatusExecutionOutcomeLostClaim {
			t.Fatalf("expected lost claim on superseded update, got outcome=%v err=%v", outcome, execErr)
		}

		publisher := &helperPublisher{}
		buildRepo = &multiBuildRepository{builds: map[string]domain.Build{build.ID: build}, byJob: map[string][]domain.Build{jobID: {build}}}
		authoritative := baseDelivery
		authoritative.DesiredState = domain.SCMCommitStatusStateSuccess
		authoritative.Description = "Coyote build succeeded"
		reassertReporter := &SCMStatusReporter{
			buildRepo:  buildRepo,
			claimOwner: "worker",
			deliveryRepo: &helperSCMDeliveryRepo{markSent: func(_ context.Context, input repository.SCMStatusDeliveryMarkSentInput) (repository.SCMStatusDeliveryUpdateResult, error) {
				return repository.SCMStatusDeliveryUpdateResult{Outcome: repository.SCMStatusDeliveryUpdateOutcomeLostClaim}, nil
			}, getByKey: func(_ context.Context, provider string, repositoryOwner string, repositoryName string, commitSHA string, contextName string) (domain.SCMStatusDelivery, error) {
				return authoritative, nil
			}},
			publisher: publisher,
			now:       func() time.Time { return baseNow.Add(3 * time.Minute) },
		}
		outcome, execErr = reassertReporter.executeClaimedDelivery(context.Background(), baseDelivery, nil, scmStatusRecoveryReasonInline)
		if execErr != nil || outcome != scmStatusExecutionOutcomeLostClaim {
			t.Fatalf("expected lost claim with successful reassert, got outcome=%v err=%v", outcome, execErr)
		}
		if len(publisher.reqs) != 2 || publisher.reqs[0].State != domain.SCMCommitStatusStatePending || publisher.reqs[1].State != domain.SCMCommitStatusStateSuccess {
			t.Fatalf("expected publish plus authoritative reassert, got %+v", publisher.reqs)
		}
	})

	t.Run("notify build status returns canceled publish errors and isSuperseded checks same-attempt recency", func(t *testing.T) {
		publisher := &helperPublisher{err: context.Canceled}
		buildRepo := &multiBuildRepository{builds: map[string]domain.Build{build.ID: build}, byJob: map[string][]domain.Build{jobID: {build}}}
		reporter, err := NewSCMStatusReporter(SCMStatusReporterConfig{
			BuildRepo:    buildRepo,
			ProjectRepo:  &fakeSCMProjectRepository{project: domain.Project{ID: projectID, Slug: "payments"}},
			DeliveryRepo: memoryrepo.NewSCMStatusDeliveryRepository(),
			Publisher:    publisher,
		})
		if err != nil {
			t.Fatalf("new reporter failed: %v", err)
		}
		reporter.now = func() time.Time { return baseNow }
		if notifyErr := reporter.NotifyBuildStatus(context.Background(), build); !errors.Is(notifyErr, context.Canceled) {
			t.Fatalf("expected canceled publish error, got %v", notifyErr)
		}

		newerSameAttempt := build
		newerSameAttempt.ID = "build-3"
		newerSameAttempt.CreatedAt = build.CreatedAt.Add(time.Second)
		buildRepo.setJobBuilds(jobID, []domain.Build{build, newerSameAttempt})
		isNewer, supersedeErr := reporter.isSuperseded(context.Background(), build, domain.SCMStatusDelivery{CommitSHA: "deadbeef"})
		if supersedeErr != nil || !isNewer {
			t.Fatalf("expected newer same-attempt build to supersede, got superseded=%v err=%v", isNewer, supersedeErr)
		}
	})
}
