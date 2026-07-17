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
