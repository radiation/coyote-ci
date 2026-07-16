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
	getByKey func(context.Context, string, string, string, string, string) (domain.SCMStatusDelivery, error)
}

func (r *helperSCMDeliveryRepo) AcquireForDelivery(context.Context, repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error) {
	return repository.SCMStatusDeliveryClaimResult{}, nil
}
func (r *helperSCMDeliveryRepo) ListRecoverable(context.Context, repository.SCMStatusDeliveryRecoverableScanInput) ([]domain.SCMStatusDelivery, error) {
	return nil, nil
}
func (r *helperSCMDeliveryRepo) MarkSent(context.Context, repository.SCMStatusDeliveryMarkSentInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) RecordRetryableFailure(context.Context, repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) RecordPermanentFailure(context.Context, repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) RecordExhaustedFailure(context.Context, repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) MarkSuperseded(context.Context, repository.SCMStatusDeliveryMarkSupersededInput) (repository.SCMStatusDeliveryUpdateResult, error) {
	return repository.SCMStatusDeliveryUpdateResult{}, nil
}
func (r *helperSCMDeliveryRepo) GetByKey(ctx context.Context, provider string, repositoryOwner string, repositoryName string, commitSHA string, contextName string) (domain.SCMStatusDelivery, error) {
	if r.getByKey != nil {
		return r.getByKey(ctx, provider, repositoryOwner, repositoryName, commitSHA, contextName)
	}
	return domain.SCMStatusDelivery{}, repository.ErrSCMStatusDeliveryNotFound
}

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
		if policy.delayForAttempt(1) != defaultSCMStatusRetryInitialDelay || policy.delayForAttempt(6) != defaultSCMStatusRetryMaxDelay {
			t.Fatalf("unexpected retry delays: attempt1=%v attempt6=%v", policy.delayForAttempt(1), policy.delayForAttempt(6))
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
