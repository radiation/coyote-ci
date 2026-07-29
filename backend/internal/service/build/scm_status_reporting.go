package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type SCMCommitStatusPublishRequest struct {
	Provider               string
	RepositoryOwner        string
	RepositoryName         string
	RegisteredRepositoryID *string
	SCMConnectionID        *string
	ProviderRepositoryID   *string
	CommitSHA              string
	Context                string
	State                  domain.SCMCommitStatusState
	Description            string
	DetailsURL             *string
}

type SCMCommitStatusPublisher interface {
	PublishCommitStatus(ctx context.Context, req SCMCommitStatusPublishRequest) error
}

type scmStatusBuildRepository interface {
	GetByID(ctx context.Context, id string) (domain.Build, error)
	ListByJobID(ctx context.Context, jobID string) ([]domain.Build, error)
}

type scmStatusProjectRepository interface {
	GetByID(ctx context.Context, id string) (domain.Project, error)
}

type scmStatusDeliveryRepository interface {
	AcquireForDelivery(ctx context.Context, input repository.SCMStatusDeliveryClaimInput) (repository.SCMStatusDeliveryClaimResult, error)
	ListRecoverable(ctx context.Context, input repository.SCMStatusDeliveryRecoverableScanInput) ([]domain.SCMStatusDelivery, error)
	MarkSent(ctx context.Context, input repository.SCMStatusDeliveryMarkSentInput) (repository.SCMStatusDeliveryUpdateResult, error)
	RecordRetryableFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error)
	RecordPermanentFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error)
	RecordExhaustedFailure(ctx context.Context, input repository.SCMStatusDeliveryRecordFailureInput) (repository.SCMStatusDeliveryUpdateResult, error)
	MarkSuperseded(ctx context.Context, input repository.SCMStatusDeliveryMarkSupersededInput) (repository.SCMStatusDeliveryUpdateResult, error)
	GetByKey(ctx context.Context, provider string, repositoryOwner string, repositoryName string, commitSHA string, contextName string) (domain.SCMStatusDelivery, error)
	GetByRepositoryIdentity(ctx context.Context, connectionID string, providerRepositoryID string, commitSHA string, contextName string) (domain.SCMStatusDelivery, error)
}

type SCMStatusReporterConfig struct {
	BuildRepo     scmStatusBuildRepository
	ProjectRepo   scmStatusProjectRepository
	DeliveryRepo  scmStatusDeliveryRepository
	Publisher     SCMCommitStatusPublisher
	PublicBaseURL string
	ClaimOwner    string
	ClaimDuration time.Duration
}

type SCMStatusReporter struct {
	buildRepo     scmStatusBuildRepository
	projectRepo   scmStatusProjectRepository
	deliveryRepo  scmStatusDeliveryRepository
	publisher     SCMCommitStatusPublisher
	publicBaseURL string
	claimOwner    string
	claimDuration time.Duration
	retryPolicy   scmStatusRetryPolicy
	now           func() time.Time
}

type scmStatusFailureDecision struct {
	category  domain.SCMStatusDeliveryFailureCategory
	reason    string
	retryable bool
}

type scmStatusPublisherError interface {
	error
	Retryable() bool
	Reason() string
}

type scmStatusExecutionOutcome int

const (
	scmStatusExecutionOutcomeNone scmStatusExecutionOutcome = iota
	scmStatusExecutionOutcomeSent
	scmStatusExecutionOutcomeReassertScheduled
	scmStatusExecutionOutcomeRetryScheduled
	scmStatusExecutionOutcomePermanentlyFailed
	scmStatusExecutionOutcomeAttemptsExhausted
	scmStatusExecutionOutcomeLostClaim
	scmStatusExecutionOutcomeSuperseded
)

func NewSCMStatusReporter(cfg SCMStatusReporterConfig) (*SCMStatusReporter, error) {
	if cfg.BuildRepo == nil {
		return nil, errors.New("scm status reporter requires a build repository")
	}
	if cfg.ProjectRepo == nil {
		return nil, errors.New("scm status reporter requires a project repository")
	}
	if cfg.DeliveryRepo == nil {
		return nil, errors.New("scm status reporter requires a delivery repository")
	}
	if cfg.Publisher == nil {
		return nil, errors.New("scm status reporter requires a publisher")
	}
	claimDuration := scmStatusClaimDuration(cfg.ClaimDuration)
	if err := validateSCMStatusClaimDuration(claimDuration); err != nil {
		return nil, err
	}
	return &SCMStatusReporter{
		buildRepo:     cfg.BuildRepo,
		projectRepo:   cfg.ProjectRepo,
		deliveryRepo:  cfg.DeliveryRepo,
		publisher:     cfg.Publisher,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
		claimOwner:    scmStatusClaimOwner(cfg.ClaimOwner),
		claimDuration: claimDuration,
		retryPolicy:   defaultSCMStatusRetryPolicy(),
		now:           func() time.Time { return time.Now().UTC() },
	}, nil
}

func (r *SCMStatusReporter) NotifyBuildStatus(ctx context.Context, build domain.Build) error {
	if r == nil {
		return nil
	}
	result, shouldSend, err := r.acquireDelivery(ctx, build, scmStatusRecoveryReasonInline)
	if err != nil {
		return err
	}
	if !shouldSend {
		return nil
	}
	_, executeErr := r.executeClaimedDelivery(ctx, result.Delivery, result.ReassertAfter, scmStatusRecoveryReasonInline)
	return executeErr
}

func (r *SCMStatusReporter) acquireDelivery(ctx context.Context, build domain.Build, recoveryReason string) (repository.SCMStatusDeliveryClaimResult, bool, error) {
	delivery, ok, err := r.planDelivery(ctx, build)
	if err != nil || !ok {
		return repository.SCMStatusDeliveryClaimResult{}, false, err
	}
	result, err := r.deliveryRepo.AcquireForDelivery(ctx, repository.SCMStatusDeliveryClaimInput{
		Delivery:      delivery,
		ClaimOwner:    r.claimOwner,
		Now:           r.now().UTC(),
		ClaimDuration: r.claimDuration,
		MaxAttempts:   r.retryPolicy.maxAttempts,
	})
	if err != nil {
		return repository.SCMStatusDeliveryClaimResult{}, false, err
	}
	switch result.Outcome {
	case repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed, repository.SCMStatusDeliveryClaimOutcomeRetryClaimed, repository.SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed:
		log.Printf("scm status delivery claimed: build_id=%s provider=%s state=%s outcome=%s reason=%s", result.Delivery.BuildID, result.Delivery.Provider, result.Delivery.DesiredState, result.Outcome, recoveryReason)
		return result, true, nil
	default:
		log.Printf("scm status delivery skipped: build_id=%s provider=%s state=%s outcome=%s reason=%s", result.Delivery.BuildID, result.Delivery.Provider, result.Delivery.DesiredState, result.Outcome, recoveryReason)
		return result, false, nil
	}
}

func (r *SCMStatusReporter) planDelivery(ctx context.Context, build domain.Build) (domain.SCMStatusDelivery, bool, error) {
	if build.ValidateRepositoryIdentitySnapshot() != nil || build.RegisteredRepositoryID == nil || build.SCMConnectionID == nil || build.ProviderRepositoryID == nil {
		return domain.SCMStatusDelivery{}, false, nil
	}
	commitSHA := scmStatusCommitSHA(build)
	if commitSHA == "" {
		return domain.SCMStatusDelivery{}, false, nil
	}
	contextName, ok, err := r.buildContextName(ctx, build)
	if err != nil || !ok {
		return domain.SCMStatusDelivery{}, false, err
	}
	state, description, ok := scmStatusStateForBuild(build)
	if !ok {
		return domain.SCMStatusDelivery{}, false, nil
	}
	delivery := domain.SCMStatusDelivery{
		BuildID:                strings.TrimSpace(build.ID),
		BuildAttempt:           build.AttemptNumber,
		BuildCreatedAt:         scmStatusBuildCreatedAt(build, r.now().UTC()),
		Provider:               "github",
		RepositoryOwner:        "repository-snapshot",
		RepositoryName:         "repository-snapshot",
		RegisteredRepositoryID: cloneStringPtr(build.RegisteredRepositoryID),
		SCMConnectionID:        cloneStringPtr(build.SCMConnectionID),
		ProviderRepositoryID:   cloneStringPtr(build.ProviderRepositoryID),
		CommitSHA:              commitSHA,
		Context:                contextName,
		DesiredState:           state,
		Description:            truncateSCMStatusText(description, maxSCMStatusDescriptionLength),
		DetailsURL:             scmStatusDetailsURL(r.publicBaseURL, build.ID),
	}
	return delivery.Normalize(), true, nil
}

func (r *SCMStatusReporter) buildContextName(ctx context.Context, build domain.Build) (string, bool, error) {
	if build.JobID == nil || strings.TrimSpace(*build.JobID) == "" {
		return "", false, nil
	}
	project, err := r.projectRepo.GetByID(ctx, strings.TrimSpace(build.ProjectID))
	if err != nil {
		return "", false, err
	}
	projectSlug := strings.TrimSpace(project.Slug)
	if projectSlug == "" {
		return "", false, nil
	}
	return scmStatusContextName(projectSlug, strings.TrimSpace(*build.JobID)), true, nil
}

func (r *SCMStatusReporter) executeClaimedDelivery(ctx context.Context, delivery domain.SCMStatusDelivery, reassertAfter *time.Time, recoveryReason string) (scmStatusExecutionOutcome, error) {
	build, err := r.buildRepo.GetByID(ctx, delivery.BuildID)
	if err != nil {
		outcome, markErr := r.markDeliveryFailed(ctx, delivery, err, r.now().UTC(), recoveryReason)
		if markErr != nil {
			return scmStatusExecutionOutcomeNone, errors.Join(err, markErr)
		}
		return outcome, err
	}
	superseded, supersedeErr := r.isSuperseded(ctx, build, delivery)
	if supersedeErr != nil {
		outcome, markErr := r.markDeliveryFailed(ctx, delivery, supersedeErr, r.now().UTC(), recoveryReason)
		if markErr != nil {
			return scmStatusExecutionOutcomeNone, errors.Join(supersedeErr, markErr)
		}
		return outcome, supersedeErr
	}
	if superseded {
		claimedAt, claimErr := claimedSCMStatusTimestamp(delivery)
		if claimErr != nil {
			return scmStatusExecutionOutcomeNone, claimErr
		}
		claimOwner := r.claimOwner
		result, err := r.deliveryRepo.MarkSuperseded(ctx, repository.SCMStatusDeliveryMarkSupersededInput{
			DeliveryID:   delivery.ID,
			ClaimOwner:   &claimOwner,
			ClaimedAt:    &claimedAt,
			SupersededAt: r.now().UTC(),
			Reason:       "newer_build_attempt_exists",
		})
		if err != nil {
			return scmStatusExecutionOutcomeNone, err
		}
		if result.Outcome == repository.SCMStatusDeliveryUpdateOutcomeLostClaim {
			return scmStatusExecutionOutcomeLostClaim, nil
		}
		return scmStatusExecutionOutcomeSuperseded, nil
	}
	publishErr := r.publisher.PublishCommitStatus(ctx, SCMCommitStatusPublishRequest{
		Provider:               delivery.Provider,
		RepositoryOwner:        delivery.RepositoryOwner,
		RepositoryName:         delivery.RepositoryName,
		RegisteredRepositoryID: cloneStringPtr(delivery.RegisteredRepositoryID),
		SCMConnectionID:        cloneStringPtr(delivery.SCMConnectionID),
		ProviderRepositoryID:   cloneStringPtr(delivery.ProviderRepositoryID),
		CommitSHA:              delivery.CommitSHA,
		Context:                delivery.Context,
		State:                  delivery.DesiredState,
		Description:            delivery.Description,
		DetailsURL:             delivery.DetailsURL,
	})
	if publishErr != nil {
		if errors.Is(publishErr, context.Canceled) || errors.Is(publishErr, context.DeadlineExceeded) {
			return scmStatusExecutionOutcomeNone, publishErr
		}
		outcome, markErr := r.markDeliveryFailed(ctx, delivery, publishErr, r.now().UTC(), recoveryReason)
		if markErr != nil {
			return scmStatusExecutionOutcomeNone, errors.Join(publishErr, markErr)
		}
		return outcome, publishErr
	}
	attemptedAt := r.now().UTC()
	if reassertAfter != nil && reassertAfter.After(attemptedAt) {
		outcome, scheduleErr := r.markDeliveryReassertPending(ctx, delivery, attemptedAt, *reassertAfter)
		if scheduleErr != nil {
			return scmStatusExecutionOutcomeNone, scheduleErr
		}
		if outcome == scmStatusExecutionOutcomeLostClaim {
			return outcome, r.reassertAuthoritativeDelivery(ctx, delivery)
		}
		return outcome, nil
	}
	outcome, markErr := r.markDeliverySent(ctx, delivery, attemptedAt)
	if markErr != nil {
		return scmStatusExecutionOutcomeNone, markErr
	}
	if outcome == scmStatusExecutionOutcomeLostClaim {
		return outcome, r.reassertAuthoritativeDelivery(ctx, delivery)
	}
	return outcome, nil
}

func (r *SCMStatusReporter) recoverDelivery(ctx context.Context, candidate domain.SCMStatusDelivery, recoveryReason string) (scmStatusRecoveryAttemptResult, error) {
	result, err := r.deliveryRepo.AcquireForDelivery(ctx, repository.SCMStatusDeliveryClaimInput{
		Delivery:      candidate,
		ClaimOwner:    r.claimOwner,
		Now:           r.now().UTC(),
		ClaimDuration: r.claimDuration,
		MaxAttempts:   r.retryPolicy.maxAttempts,
	})
	if err != nil {
		return scmStatusRecoveryAttemptResult{}, err
	}
	attempt := scmStatusRecoveryAttemptResult{claimOutcome: result.Outcome}
	shouldSend := result.Outcome == repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed || result.Outcome == repository.SCMStatusDeliveryClaimOutcomeRetryClaimed || result.Outcome == repository.SCMStatusDeliveryClaimOutcomeStaleClaimReclaimed
	if !shouldSend {
		return attempt, nil
	}
	_, buildErr := r.buildRepo.GetByID(ctx, candidate.BuildID)
	if buildErr != nil {
		attempt.rehydrationFailed = true
		outcome, markErr := r.markDeliveryFailed(ctx, result.Delivery, buildErr, r.now().UTC(), recoveryReason)
		attempt.executionOutcome = outcome
		if markErr != nil {
			return attempt, markErr
		}
		return attempt, nil
	}
	outcome, executeErr := r.executeClaimedDelivery(ctx, result.Delivery, result.ReassertAfter, recoveryReason)
	attempt.executionOutcome = outcome
	return attempt, executeErr
}

func (r *SCMStatusReporter) markDeliverySent(ctx context.Context, delivery domain.SCMStatusDelivery, sentAt time.Time) (scmStatusExecutionOutcome, error) {
	claimedAt, claimErr := claimedSCMStatusTimestamp(delivery)
	if claimErr != nil {
		return scmStatusExecutionOutcomeNone, claimErr
	}
	result, err := r.deliveryRepo.MarkSent(ctx, repository.SCMStatusDeliveryMarkSentInput{
		DeliveryID: delivery.ID,
		ClaimOwner: r.claimOwner,
		ClaimedAt:  claimedAt,
		SentAt:     sentAt,
		State:      delivery.DesiredState,
	})
	if err != nil {
		return scmStatusExecutionOutcomeNone, err
	}
	if result.Outcome == repository.SCMStatusDeliveryUpdateOutcomeLostClaim {
		return scmStatusExecutionOutcomeLostClaim, nil
	}
	return scmStatusExecutionOutcomeSent, nil
}

func (r *SCMStatusReporter) markDeliveryReassertPending(ctx context.Context, delivery domain.SCMStatusDelivery, attemptedAt time.Time, reassertAt time.Time) (scmStatusExecutionOutcome, error) {
	claimedAt, claimErr := claimedSCMStatusTimestamp(delivery)
	if claimErr != nil {
		return scmStatusExecutionOutcomeNone, claimErr
	}
	retryAt := reassertAt.UTC()
	result, err := r.deliveryRepo.RecordRetryableFailure(ctx, repository.SCMStatusDeliveryRecordFailureInput{
		DeliveryID:      delivery.ID,
		ClaimOwner:      r.claimOwner,
		ClaimedAt:       claimedAt,
		FailedAt:        attemptedAt,
		NextAttemptAt:   &retryAt,
		FailureCategory: domain.SCMStatusDeliveryFailureCategoryRetryable,
		FailureReason:   scmStatusFailureReasonAuthoritativeReassert,
	})
	if err != nil {
		return scmStatusExecutionOutcomeNone, err
	}
	if result.Outcome == repository.SCMStatusDeliveryUpdateOutcomeLostClaim {
		return scmStatusExecutionOutcomeLostClaim, nil
	}
	log.Printf("scm status authoritative reassert scheduled: build_id=%s provider=%s state=%s retry_at=%s", delivery.BuildID, delivery.Provider, delivery.DesiredState, retryAt.Format(time.RFC3339))
	return scmStatusExecutionOutcomeReassertScheduled, nil
}

func (r *SCMStatusReporter) markDeliveryFailed(ctx context.Context, delivery domain.SCMStatusDelivery, sendErr error, attemptedAt time.Time, recoveryReason string) (scmStatusExecutionOutcome, error) {
	claimedAt, claimErr := claimedSCMStatusTimestamp(delivery)
	if claimErr != nil {
		return scmStatusExecutionOutcomeNone, claimErr
	}
	decision := classifySCMStatusDeliveryFailure(sendErr)
	message := truncateSCMStatusText(strings.TrimSpace(sendErr.Error()), maxSCMStatusStoredErrorLength)
	logMessage := truncateSCMStatusText(strings.TrimSpace(sendErr.Error()), maxSCMStatusLoggedErrorLength)
	input := repository.SCMStatusDeliveryRecordFailureInput{
		DeliveryID:      delivery.ID,
		ClaimOwner:      r.claimOwner,
		ClaimedAt:       claimedAt,
		FailedAt:        attemptedAt,
		FailureCategory: decision.category,
		FailureReason:   decision.reason,
		LastError:       &message,
	}
	var (
		result repository.SCMStatusDeliveryUpdateResult
		err    error
	)
	if decision.retryable && delivery.Attempts < delivery.MaxAttempts {
		nextAttemptAt := attemptedAt.Add(r.retryPolicy.delayForAttempt(delivery.Attempts))
		input.NextAttemptAt = &nextAttemptAt
		result, err = r.deliveryRepo.RecordRetryableFailure(ctx, input)
	} else if decision.retryable {
		result, err = r.deliveryRepo.RecordExhaustedFailure(ctx, input)
	} else {
		result, err = r.deliveryRepo.RecordPermanentFailure(ctx, input)
	}
	if err != nil {
		return scmStatusExecutionOutcomeNone, err
	}
	if result.Outcome == repository.SCMStatusDeliveryUpdateOutcomeLostClaim {
		return scmStatusExecutionOutcomeLostClaim, nil
	}
	log.Printf("scm status delivery failed: build_id=%s provider=%s state=%s category=%s reason=%s recovery_reason=%s err=%s", delivery.BuildID, delivery.Provider, delivery.DesiredState, decision.category, decision.reason, recoveryReason, logMessage)
	if decision.retryable && delivery.Attempts < delivery.MaxAttempts {
		log.Printf("scm status retry scheduled: build_id=%s provider=%s state=%s reason=%s recovery_reason=%s", delivery.BuildID, delivery.Provider, delivery.DesiredState, decision.reason, recoveryReason)
		return scmStatusExecutionOutcomeRetryScheduled, nil
	}
	if decision.retryable {
		return scmStatusExecutionOutcomeAttemptsExhausted, nil
	}
	return scmStatusExecutionOutcomePermanentlyFailed, nil
}

func (r *SCMStatusReporter) reassertAuthoritativeDelivery(ctx context.Context, staleDelivery domain.SCMStatusDelivery) error {
	authoritative, err := r.getAuthoritativeDelivery(ctx, staleDelivery)
	if err != nil {
		if errors.Is(err, repository.ErrSCMStatusDeliveryNotFound) {
			return nil
		}
		return err
	}
	if scmStatusPublishEquivalent(authoritative, staleDelivery) {
		return nil
	}
	if err := r.publisher.PublishCommitStatus(ctx, scmStatusPublishRequest(authoritative)); err != nil {
		log.Printf("scm status authoritative reassert failed: build_id=%s provider=%s state=%s err=%s", authoritative.BuildID, authoritative.Provider, authoritative.DesiredState, truncateSCMStatusText(strings.TrimSpace(err.Error()), maxSCMStatusLoggedErrorLength))
		return nil
	}
	log.Printf("scm status authoritative state reasserted after lost claim: build_id=%s provider=%s state=%s", authoritative.BuildID, authoritative.Provider, authoritative.DesiredState)
	return nil
}

func (r *SCMStatusReporter) getAuthoritativeDelivery(ctx context.Context, delivery domain.SCMStatusDelivery) (domain.SCMStatusDelivery, error) {
	if delivery.SCMConnectionID != nil && delivery.ProviderRepositoryID != nil {
		return r.deliveryRepo.GetByRepositoryIdentity(ctx, *delivery.SCMConnectionID, *delivery.ProviderRepositoryID, delivery.CommitSHA, delivery.Context)
	}
	return r.deliveryRepo.GetByKey(ctx, delivery.Provider, delivery.RepositoryOwner, delivery.RepositoryName, delivery.CommitSHA, delivery.Context)
}

func (r *SCMStatusReporter) isSuperseded(ctx context.Context, build domain.Build, delivery domain.SCMStatusDelivery) (bool, error) {
	if build.JobID == nil || strings.TrimSpace(*build.JobID) == "" {
		return false, nil
	}
	builds, err := r.buildRepo.ListByJobID(ctx, strings.TrimSpace(*build.JobID))
	if err != nil {
		return false, err
	}
	for _, candidate := range builds {
		if strings.TrimSpace(candidate.ID) == strings.TrimSpace(build.ID) {
			continue
		}
		if scmStatusCommitSHA(candidate) != delivery.CommitSHA {
			continue
		}
		if candidate.AttemptNumber > build.AttemptNumber {
			return true, nil
		}
		if candidate.AttemptNumber == build.AttemptNumber && candidate.CreatedAt.After(build.CreatedAt) {
			return true, nil
		}
	}
	return false, nil
}

func scmStatusRepositoryIdentity(build domain.Build) (string, string, string, bool) {
	trigger := domain.NormalizeBuildTrigger(build.Trigger)
	provider := strings.ToLower(strings.TrimSpace(buildReadOptionalString(trigger.SCMProvider)))
	owner := strings.TrimSpace(buildReadOptionalString(trigger.RepositoryOwner))
	repo := strings.TrimSpace(buildReadOptionalString(trigger.RepositoryName))
	if provider == "github" && owner != "" && repo != "" {
		return provider, owner, repo, true
	}
	repoURL := strings.TrimSpace(buildReadOptionalString(trigger.RepositoryURL))
	if repoURL == "" {
		repoURL = buildReadOptionalString(build.RepoURL)
	}
	owner, repo, ok := parseGitHubRepositoryURL(repoURL)
	if !ok {
		return "", "", "", false
	}
	return "github", owner, repo, true
}

func scmStatusCommitSHA(build domain.Build) string {
	trigger := domain.NormalizeBuildTrigger(build.Trigger)
	for _, candidate := range []string{buildReadOptionalString(build.CommitSHA), buildReadOptionalString(build.SourceSHA), buildReadOptionalString(trigger.CommitSHA)} {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func scmStatusStateForBuild(build domain.Build) (domain.SCMCommitStatusState, string, bool) {
	switch build.Status {
	case domain.BuildStatusPending:
		return domain.SCMCommitStatusStatePending, "Coyote build is pending", true
	case domain.BuildStatusQueued:
		return domain.SCMCommitStatusStatePending, "Coyote build is queued", true
	case domain.BuildStatusPreparing:
		return domain.SCMCommitStatusStatePending, "Coyote build is preparing", true
	case domain.BuildStatusRunning:
		return domain.SCMCommitStatusStatePending, "Coyote build is running", true
	case domain.BuildStatusSuccess:
		return domain.SCMCommitStatusStateSuccess, "Coyote build succeeded", true
	case domain.BuildStatusFailed:
		return domain.SCMCommitStatusStateFailure, "Coyote build failed", true
	case domain.BuildStatusCanceled:
		return domain.SCMCommitStatusStateError, "Coyote build was canceled", true
	default:
		return "", "", false
	}
}

func scmStatusDetailsURL(publicBaseURL string, buildID string) *string {
	urlValue := buildBuildDetailURL(publicBaseURL, buildID)
	if strings.TrimSpace(urlValue) == "" {
		return nil
	}
	return &urlValue
}

func scmStatusBuildCreatedAt(build domain.Build, fallback time.Time) time.Time {
	if !build.CreatedAt.IsZero() {
		return build.CreatedAt.UTC()
	}
	return fallback.UTC()
}

func scmStatusContextName(projectSlug string, jobID string) string {
	prefix := "coyote/"
	trimmedJobID := strings.TrimSpace(jobID)
	trimmedSlug := strings.TrimSpace(projectSlug)
	availableSlugRunes := maxSCMStatusContextLength - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(trimmedJobID) - 1
	if availableSlugRunes <= 0 {
		return truncateSCMStatusText(prefix+trimmedJobID, maxSCMStatusContextLength)
	}
	return prefix + truncateSCMStatusText(trimmedSlug, availableSlugRunes) + "/" + trimmedJobID
}

func scmStatusPublishRequest(delivery domain.SCMStatusDelivery) SCMCommitStatusPublishRequest {
	return SCMCommitStatusPublishRequest{
		Provider:        delivery.Provider,
		RepositoryOwner: delivery.RepositoryOwner,
		RepositoryName:  delivery.RepositoryName,
		CommitSHA:       delivery.CommitSHA,
		Context:         delivery.Context,
		State:           delivery.DesiredState,
		Description:     delivery.Description,
		DetailsURL:      delivery.DetailsURL,
	}
}

func scmStatusPublishEquivalent(left domain.SCMStatusDelivery, right domain.SCMStatusDelivery) bool {
	left = left.Normalize()
	right = right.Normalize()
	if left.Provider != right.Provider || left.RepositoryOwner != right.RepositoryOwner || left.RepositoryName != right.RepositoryName || left.CommitSHA != right.CommitSHA || left.Context != right.Context {
		return false
	}
	if left.DesiredState != right.DesiredState || left.Description != right.Description {
		return false
	}
	return buildReadOptionalString(left.DetailsURL) == buildReadOptionalString(right.DetailsURL)
}

func truncateSCMStatusText(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if maxRunes <= 0 || trimmed == "" {
		return trimmed
	}
	if utf8.RuneCountInString(trimmed) <= maxRunes {
		return trimmed
	}
	runes := []rune(trimmed)
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func claimedSCMStatusTimestamp(delivery domain.SCMStatusDelivery) (time.Time, error) {
	if delivery.ClaimedAt == nil || delivery.ClaimedAt.IsZero() {
		return time.Time{}, fmt.Errorf("scm status delivery claim timestamp is required")
	}
	return delivery.ClaimedAt.UTC(), nil
}

func classifySCMStatusDeliveryFailure(err error) scmStatusFailureDecision {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return scmStatusFailureDecision{category: domain.SCMStatusDeliveryFailureCategoryRetryable, reason: "context_canceled", retryable: true}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return scmStatusFailureDecision{category: domain.SCMStatusDeliveryFailureCategoryRetryable, reason: "network_timeout", retryable: true}
	}
	var publishErr scmStatusPublisherError
	if errors.As(err, &publishErr) {
		category := domain.SCMStatusDeliveryFailureCategoryPermanent
		if publishErr.Retryable() {
			category = domain.SCMStatusDeliveryFailureCategoryRetryable
		}
		return scmStatusFailureDecision{category: category, reason: publishErr.Reason(), retryable: publishErr.Retryable()}
	}
	return scmStatusFailureDecision{category: domain.SCMStatusDeliveryFailureCategoryRetryable, reason: "github_status_send_failed", retryable: true}
}

func parseGitHubRepositoryURL(rawURL string) (string, string, bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", "", false
	}
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", false
	}
	path := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), true
}

const scmStatusRecoveryReasonInline = "inline"
const scmStatusRecoveryReasonDrain = "recovery_drain"

const scmStatusFailureReasonAuthoritativeReassert = "authoritative_reassert_pending"

type scmStatusRecoveryAttemptResult struct {
	claimOutcome      repository.SCMStatusDeliveryClaimOutcome
	executionOutcome  scmStatusExecutionOutcome
	rehydrationFailed bool
}

const maxSCMStatusContextLength = 100
const maxSCMStatusDescriptionLength = 140
const maxSCMStatusStoredErrorLength = 1024
const maxSCMStatusLoggedErrorLength = 256
