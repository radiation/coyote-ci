package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestNotificationDeliveryRepository_AcquireConcurrentIdenticalCreatesOneRow_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	buildID := createNotificationIntegrationBuild(t, db, ctx)
	repo := NewNotificationDeliveryRepository(db)
	destinationKey := "email-target:" + uuid.NewString()
	base := domain.NotificationDelivery{
		BuildID:         buildID,
		EventType:       domain.NotificationEventTypeBuildFailed,
		Transport:       domain.NotificationTransportEmail,
		DestinationKind: domain.NotificationDestinationKindSharedTarget,
		DestinationKey:  destinationKey,
		Recipient:       "dev+" + buildID + "@example.com",
	}
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	const workers = 8
	start := make(chan struct{})
	type acquireResult struct {
		ID      string
		Outcome repository.NotificationDeliveryClaimOutcome
	}
	results := make(chan acquireResult, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{
				Delivery:      base,
				ClaimOwner:    uuid.NewString(),
				Now:           now,
				ClaimDuration: time.Minute,
				MaxAttempts:   3,
			})
			if err != nil {
				errs <- err
				return
			}
			results <- acquireResult{ID: strings.TrimSpace(result.Delivery.ID), Outcome: result.Outcome}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		if err != nil {
			t.Fatalf("acquire failed: %v", err)
		}
	}

	created := 0
	claimedByOther := 0
	returnedIDs := make([]string, 0, workers)
	for result := range results {
		if result.ID == "" {
			t.Fatal("expected every acquire caller to receive a non-blank delivery id")
		}
		returnedIDs = append(returnedIDs, result.ID)
		switch result.Outcome {
		case repository.NotificationDeliveryClaimOutcomeCreatedClaimed:
			created++
		case repository.NotificationDeliveryClaimOutcomeClaimedByOther:
			claimedByOther++
		default:
			t.Fatalf("unexpected acquire outcome %q", result.Outcome)
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly one created delivery, got %d", created)
	}
	if claimedByOther != workers-1 {
		t.Fatalf("expected %d claimed-by-other outcomes, got %d", workers-1, claimedByOther)
	}
	canonicalID := returnedIDs[0]
	for _, returnedID := range returnedIDs[1:] {
		if returnedID != canonicalID {
			t.Fatalf("expected every acquire caller to receive the same delivery id, got %q and %q", canonicalID, returnedID)
		}
	}

	var persistedID string
	var count int
	var attempts int
	countErr := db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(id::text), ''), COALESCE(MIN(attempts), 0)
		FROM notification_deliveries
		WHERE build_id = $1 AND event_type = $2 AND transport = $3 AND destination_key = $4
	`, buildID, string(domain.NotificationEventTypeBuildFailed), string(domain.NotificationTransportEmail), destinationKey).Scan(&count, &persistedID, &attempts)
	if countErr != nil {
		t.Fatalf("count deliveries failed: %v", countErr)
	}
	if count != 1 {
		t.Fatalf("expected exactly one persisted logical delivery row, got %d", count)
	}
	if strings.TrimSpace(persistedID) == "" {
		t.Fatal("expected persisted delivery id to be non-blank")
	}
	if persistedID != canonicalID {
		t.Fatalf("expected returned delivery id %q to match persisted delivery id %q", canonicalID, persistedID)
	}
	if attempts != 1 {
		t.Fatalf("expected attempt count 1 after concurrent initial claim, got %d", attempts)
	}
}

func TestNotificationDeliveryRepository_AcquireConcurrentDueRetryClaimsOnce_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	buildID := createNotificationIntegrationBuild(t, db, ctx)
	repo := NewNotificationDeliveryRepository(db)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	deliveryID := uuid.NewString()
	destinationKey := "email-target:" + uuid.NewString()
	createdAt := now.Add(-2 * time.Minute)
	nextAttemptAt := now.Add(-time.Minute)

	_, insertErr := db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, last_attempt_at, next_attempt_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'retry_waiting', 1, 3, $8, $9, $10, $10)
	`, deliveryID, buildID, string(domain.NotificationEventTypeBuildFailed), string(domain.NotificationTransportEmail), string(domain.NotificationDestinationKindSharedTarget), destinationKey, "retry+"+buildID+"@example.com", createdAt, nextAttemptAt, createdAt)
	if insertErr != nil {
		t.Fatalf("insert due retry row failed: %v", insertErr)
	}

	const workers = 8
	start := make(chan struct{})
	type claimResult struct {
		ID      string
		Outcome repository.NotificationDeliveryClaimOutcome
	}
	results := make(chan claimResult, workers)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{
				Delivery:      domain.NotificationDelivery{BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: destinationKey, Recipient: "retry+" + buildID + "@example.com"},
				ClaimOwner:    uuid.NewString(),
				Now:           now,
				ClaimDuration: time.Minute,
				MaxAttempts:   3,
			})
			if err != nil {
				errCh <- err
				return
			}
			results <- claimResult{ID: result.Delivery.ID, Outcome: result.Outcome}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	close(results)

	for err := range errCh {
		if err != nil {
			t.Fatalf("claim due retry failed: %v", err)
		}
	}

	claimed := 0
	blocked := 0
	for result := range results {
		if result.ID != deliveryID {
			t.Fatalf("expected stable delivery id %q, got %q", deliveryID, result.ID)
		}
		switch result.Outcome {
		case repository.NotificationDeliveryClaimOutcomeRetryClaimed:
			claimed++
		case repository.NotificationDeliveryClaimOutcomeClaimedByOther:
			blocked++
		default:
			t.Fatalf("unexpected due retry outcome %q", result.Outcome)
		}
	}
	if claimed != 1 || blocked != workers-1 {
		t.Fatalf("expected one retry claim and %d blocked callers, got claimed=%d blocked=%d", workers-1, claimed, blocked)
	}

	var attempts int
	var status string
	stateErr := db.QueryRowContext(ctx, `SELECT attempts, status FROM notification_deliveries WHERE id = $1`, deliveryID).Scan(&attempts, &status)
	if stateErr != nil {
		t.Fatalf("load due retry state failed: %v", stateErr)
	}
	if attempts != 2 {
		t.Fatalf("expected attempts to increment once to 2, got %d", attempts)
	}
	if status != string(domain.NotificationDeliveryStatusSending) {
		t.Fatalf("expected sending status after retry claim, got %q", status)
	}
}

func TestNotificationDeliveryRepository_AcquireConcurrentStaleClaimReclaimsOnce_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	buildID := createNotificationIntegrationBuild(t, db, ctx)
	repo := NewNotificationDeliveryRepository(db)
	now := time.Date(2026, 7, 2, 13, 0, 0, 0, time.UTC)
	deliveryID := uuid.NewString()
	destinationKey := "email-target:" + uuid.NewString()
	createdAt := now.Add(-3 * time.Minute)
	claimedAt := now.Add(-2 * time.Minute)
	claimExpiresAt := now.Add(-time.Minute)
	oldOwner := "worker-old"

	_, insertErr := db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, last_attempt_at, claimed_at, claim_expires_at, claimed_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'sending', 1, 3, $8, $9, $10, $11, $12, $12)
	`, deliveryID, buildID, string(domain.NotificationEventTypeBuildFailed), string(domain.NotificationTransportEmail), string(domain.NotificationDestinationKindSharedTarget), destinationKey, "stale+"+buildID+"@example.com", claimedAt, claimedAt, claimExpiresAt, oldOwner, createdAt)
	if insertErr != nil {
		t.Fatalf("insert stale claim row failed: %v", insertErr)
	}

	const workers = 8
	start := make(chan struct{})
	type claimResult struct {
		ID         string
		Outcome    repository.NotificationDeliveryClaimOutcome
		ClaimOwner string
		ClaimedAt  time.Time
	}
	results := make(chan claimResult, workers)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimOwner := uuid.NewString()
			result, err := repo.AcquireForDelivery(context.Background(), repository.NotificationDeliveryClaimInput{
				Delivery:      domain.NotificationDelivery{BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: destinationKey, Recipient: "stale+" + buildID + "@example.com"},
				ClaimOwner:    claimOwner,
				Now:           now,
				ClaimDuration: time.Minute,
				MaxAttempts:   3,
			})
			if err != nil {
				errCh <- err
				return
			}
			claimedAtValue := time.Time{}
			if result.Delivery.ClaimedAt != nil {
				claimedAtValue = *result.Delivery.ClaimedAt
			}
			results <- claimResult{ID: result.Delivery.ID, Outcome: result.Outcome, ClaimOwner: claimOwner, ClaimedAt: claimedAtValue}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	close(results)

	for err := range errCh {
		if err != nil {
			t.Fatalf("stale reclaim failed: %v", err)
		}
	}

	claimed := 0
	blocked := 0
	winningOwner := ""
	winningClaimedAt := time.Time{}
	for result := range results {
		if result.ID != deliveryID {
			t.Fatalf("expected stable delivery id %q, got %q", deliveryID, result.ID)
		}
		switch result.Outcome {
		case repository.NotificationDeliveryClaimOutcomeStaleClaimReclaimed:
			claimed++
			winningOwner = result.ClaimOwner
			winningClaimedAt = result.ClaimedAt
		case repository.NotificationDeliveryClaimOutcomeClaimedByOther:
			blocked++
		default:
			t.Fatalf("unexpected stale reclaim outcome %q", result.Outcome)
		}
	}
	if claimed != 1 || blocked != workers-1 {
		t.Fatalf("expected one stale reclaim and %d blocked callers, got claimed=%d blocked=%d", workers-1, claimed, blocked)
	}

	var attempts int
	var claimedBy string
	stateErr := db.QueryRowContext(ctx, `SELECT attempts, claimed_by FROM notification_deliveries WHERE id = $1`, deliveryID).Scan(&attempts, &claimedBy)
	if stateErr != nil {
		t.Fatalf("load stale reclaim state failed: %v", stateErr)
	}
	if attempts != 2 {
		t.Fatalf("expected attempts to increment once to 2, got %d", attempts)
	}
	if claimedBy == oldOwner || claimedBy == "" {
		t.Fatalf("expected a new claim owner, got %q", claimedBy)
	}

	lostSent, lostSentErr := repo.MarkSent(ctx, repository.NotificationDeliveryMarkSentInput{DeliveryID: deliveryID, ClaimOwner: oldOwner, ClaimedAt: claimedAt, SentAt: now})
	if lostSentErr != nil {
		t.Fatalf("old owner mark sent failed: %v", lostSentErr)
	}
	if lostSent.Outcome != repository.NotificationDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost_claim for old owner mark sent, got %q", lostSent.Outcome)
	}

	lostFailure, lostFailureErr := repo.RecordPermanentFailure(ctx, repository.NotificationDeliveryRecordFailureInput{DeliveryID: deliveryID, ClaimOwner: oldOwner, ClaimedAt: claimedAt, FailedAt: now, FailureCategory: domain.NotificationDeliveryFailureCategoryPermanent, FailureReason: "stale_owner", LastError: strPtr("stale owner")})
	if lostFailureErr != nil {
		t.Fatalf("old owner record failure failed: %v", lostFailureErr)
	}
	if lostFailure.Outcome != repository.NotificationDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost_claim for old owner failure update, got %q", lostFailure.Outcome)
	}

	winningSent, winningSentErr := repo.MarkSent(ctx, repository.NotificationDeliveryMarkSentInput{DeliveryID: deliveryID, ClaimOwner: winningOwner, ClaimedAt: winningClaimedAt, SentAt: now})
	if winningSentErr != nil {
		t.Fatalf("winning owner mark sent failed: %v", winningSentErr)
	}
	if winningSent.Outcome != repository.NotificationDeliveryUpdateOutcomeUpdated {
		t.Fatalf("expected updated outcome for winning owner, got %q", winningSent.Outcome)
	}
}

func TestNotificationDeliveryRepository_ListRecoverable_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	buildID := createNotificationIntegrationBuild(t, db, ctx)
	repo := NewNotificationDeliveryRepository(db)
	now := time.Date(2026, 7, 2, 14, 0, 0, 0, time.UTC)
	retryDueA := uuid.NewString()
	retryDueB := uuid.NewString()
	retryFuture := uuid.NewString()
	staleClaim := uuid.NewString()
	activeClaim := uuid.NewString()
	sentID := uuid.NewString()
	createdAt := now.Add(-5 * time.Minute)
	retryDueAtA := now.Add(-3 * time.Minute)
	retryDueAtB := now.Add(-2 * time.Minute)
	retryFutureAt := now.Add(time.Minute)
	claimedAt := now.Add(-2 * time.Minute)
	staleExpiresAt := now.Add(-time.Minute)
	activeExpiresAt := now.Add(time.Minute)

	_, insertErr := db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, last_attempt_at, next_attempt_at, claimed_at, claim_expires_at, claimed_by,
			failure_category, failure_reason, created_at, updated_at, sent_at
		) VALUES
		($1, $7, 'build_failed', 'email', 'shared_target', $2, $3, 'retry_waiting', 1, 3, $8, $4, NULL, NULL, NULL, 'retryable', 'email_send_failed', $9, $9, NULL),
		($5, $7, 'build_failed', 'email', 'shared_target', $6, $10, 'retry_waiting', 1, 3, $8, $11, NULL, NULL, NULL, 'retryable', 'email_send_failed', $9, $9, NULL),
		($12, $7, 'build_failed', 'email', 'shared_target', $13, $14, 'retry_waiting', 1, 3, $8, $15, NULL, NULL, NULL, 'retryable', 'email_send_failed', $9, $9, NULL),
		($16, $7, 'build_failed', 'email', 'shared_target', $17, $18, 'sending', 1, 3, $19, NULL, $19, $20, 'worker-a', NULL, NULL, $9, $9, NULL),
		($21, $7, 'build_failed', 'email', 'shared_target', $22, $23, 'sending', 1, 3, $19, NULL, $19, $24, 'worker-a', NULL, NULL, $9, $9, NULL),
		($25, $7, 'build_failed', 'email', 'shared_target', $26, $27, 'sent', 1, 1, $8, NULL, NULL, NULL, NULL, NULL, NULL, $9, $9, $8)
	`, retryDueA, "email-target:a", "a@example.com", retryDueAtA,
		retryDueB, "email-target:b", buildID, createdAt,
		"b@example.com", retryDueAtB,
		retryFuture, "email-target:c", "c@example.com", retryFutureAt,
		staleClaim, "email-target:d", "d@example.com", claimedAt, staleExpiresAt,
		activeClaim, "email-target:e", "e@example.com", activeExpiresAt,
		sentID, "email-target:f", "f@example.com",
	)
	if insertErr != nil {
		t.Fatalf("insert recoverable rows failed: %v", insertErr)
	}

	result, err := repo.ListRecoverable(ctx, repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 10})
	if err != nil {
		t.Fatalf("list recoverable failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 recoverable rows, got %d", len(result))
	}
	got := []string{result[0].ID, result[1].ID, result[2].ID}
	want := []string{retryDueA, retryDueB, staleClaim}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected ordered ids %v, got %v", want, got)
	}

	limited, limitErr := repo.ListRecoverable(ctx, repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 2})
	if limitErr != nil {
		t.Fatalf("list recoverable limit failed: %v", limitErr)
	}
	if len(limited) != 2 || limited[0].ID != retryDueA || limited[1].ID != retryDueB {
		t.Fatalf("unexpected limited recoverable result: %+v", limited)
	}

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.ListRecoverable(canceledCtx, repository.NotificationDeliveryRecoverableScanInput{Now: now, Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled scan error, got %v", err)
	}
}

func TestNotificationRecoveryDrain_ConcurrentDrainsSendOnce_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	buildID := createNotificationIntegrationBuild(t, db, ctx)
	_, updateErr := db.ExecContext(ctx, `UPDATE builds SET status = 'failed', updated_at = $2 WHERE id = $1`, buildID, time.Now().UTC())
	if updateErr != nil {
		t.Fatalf("update build status failed: %v", updateErr)
	}

	deliveryRepo := NewNotificationDeliveryRepository(db)
	buildRepo := NewBuildRepository(db)
	subscriptionRepo := NewNotificationSubscriptionRepository(db)
	target, targetErr := subscriptionRepo.CreateTarget(ctx, domain.NotificationTarget{Type: domain.NotificationTargetTypeEmail, Origin: domain.NotificationTargetOriginManual, Name: "alerts", Recipient: "alerts@example.com", Enabled: true})
	if targetErr != nil {
		t.Fatalf("create notification target failed: %v", targetErr)
	}
	kind, destinationKey, keyErr := domain.NotificationSharedEmailTargetKey(target.ID)
	if keyErr != nil {
		t.Fatalf("build shared target key failed: %v", keyErr)
	}
	deliveryID := uuid.NewString()
	now := time.Now().UTC()
	nextAttemptAt := now.Add(-time.Minute)
	createdAt := now.Add(-2 * time.Minute)
	_, insertErr := db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, notification_target_id, recipient, status,
			attempts, max_attempts, last_attempt_at, next_attempt_at, failure_category, failure_reason, created_at, updated_at
		) VALUES ($1, $2, 'build_failed', 'email', $3, $4, $5, $6, 'retry_waiting', 1, 3, $7, $8, 'retryable', 'email_send_failed', $9, $9)
	`, deliveryID, buildID, string(kind), destinationKey, target.ID, target.Recipient, createdAt, nextAttemptAt, createdAt)
	if insertErr != nil {
		t.Fatalf("insert due retry delivery failed: %v", insertErr)
	}

	sender := &countingEmailSender{}
	newDrain := func(claimOwner string) *buildsvc.NotificationRecoveryDrain {
		notifier, err := buildsvc.NewBuildNotificationService(buildsvc.BuildNotificationConfig{
			Enabled:          true,
			Recipients:       "dev@example.com",
			Sender:           sender,
			BuildRepo:        buildRepo,
			DeliveryRepo:     deliveryRepo,
			SubscriptionRepo: subscriptionRepo,
			ClaimOwner:       claimOwner,
			DeliveryMetrics:  observability.NewNoopNotificationDeliveryMetrics(),
		})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		drain, drainErr := buildsvc.NewNotificationRecoveryDrain(buildsvc.NotificationRecoveryDrainConfig{Notifier: notifier, Interval: time.Millisecond, BatchSize: 5})
		if drainErr != nil {
			t.Fatalf("create drain failed: %v", drainErr)
		}
		return drain
	}

	drainA := newDrain("server-a")
	drainB := newDrain("server-b")
	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for _, drain := range []*buildsvc.NotificationRecoveryDrain{drainA, drainB} {
		wg.Add(1)
		go func(d *buildsvc.NotificationRecoveryDrain) {
			defer wg.Done()
			<-start
			_, err := d.RunIteration(context.Background())
			errCh <- err
		}(drain)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent drain iteration failed: %v", err)
		}
	}
	if sender.Count() != 1 {
		t.Fatalf("expected one provider send under concurrent drains, got %d", sender.Count())
	}

	var attempts int
	var status string
	var persistedID string
	stateErr := db.QueryRowContext(ctx, `SELECT id::text, attempts, status FROM notification_deliveries WHERE build_id = $1 AND event_type = 'build_failed' AND transport = 'email' AND destination_key = $2`, buildID, destinationKey).Scan(&persistedID, &attempts, &status)
	if stateErr != nil {
		t.Fatalf("load recovered delivery state failed: %v", stateErr)
	}
	if persistedID != deliveryID {
		t.Fatalf("expected canonical delivery id %q, got %q", deliveryID, persistedID)
	}
	if attempts != 2 {
		t.Fatalf("expected one attempt increment to 2, got %d", attempts)
	}
	if status != string(domain.NotificationDeliveryStatusSent) {
		t.Fatalf("expected sent status after concurrent recovery, got %q", status)
	}
}

type countingEmailSender struct {
	mu    sync.Mutex
	count int
}

func (s *countingEmailSender) SendText(_ context.Context, _ platformemail.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	return nil
}

func (s *countingEmailSender) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func TestNotificationDeliveryRepository_AcquireRejectsTerminalAndBlockedStates_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	buildID := createNotificationIntegrationBuild(t, db, ctx)
	repo := NewNotificationDeliveryRepository(db)
	now := time.Date(2026, 7, 2, 14, 0, 0, 0, time.UTC)
	createdAt := now.Add(-time.Minute)

	tests := []struct {
		name          string
		status        string
		attempts      int
		maxAttempts   int
		nextAttemptAt *time.Time
		claimedAt     *time.Time
		claimExpires  *time.Time
		claimedBy     *string
		wantOutcome   repository.NotificationDeliveryClaimOutcome
	}{
		{name: "sent", status: "sent", attempts: 1, maxAttempts: 3, wantOutcome: repository.NotificationDeliveryClaimOutcomeAlreadySent},
		{name: "permanent", status: "failed_permanent", attempts: 1, maxAttempts: 3, wantOutcome: repository.NotificationDeliveryClaimOutcomePermanentlyFailed},
		{name: "exhausted", status: "failed_exhausted", attempts: 3, maxAttempts: 3, wantOutcome: repository.NotificationDeliveryClaimOutcomeAttemptsExhausted},
		{name: "retry not due", status: "retry_waiting", attempts: 1, maxAttempts: 3, nextAttemptAt: timePtr(now.Add(time.Minute)), wantOutcome: repository.NotificationDeliveryClaimOutcomeRetryNotDue},
		{name: "active claim", status: "sending", attempts: 1, maxAttempts: 3, claimedAt: timePtr(now.Add(-time.Minute)), claimExpires: timePtr(now.Add(time.Minute)), claimedBy: strPtr("worker-a"), wantOutcome: repository.NotificationDeliveryClaimOutcomeClaimedByOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deliveryID := uuid.NewString()
			destinationKey := "email-target:" + uuid.NewString()
			sentAt := nullableTimeValue(nil)
			if tc.status == "sent" {
				sentAt = createdAt
			}
			_, insertErr := db.ExecContext(ctx, `
				INSERT INTO notification_deliveries (
					id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
					attempts, max_attempts, last_attempt_at, next_attempt_at, claimed_at, claim_expires_at, claimed_by, created_at, updated_at, sent_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16, $17)
			`, deliveryID, buildID, string(domain.NotificationEventTypeBuildFailed), string(domain.NotificationTransportEmail), string(domain.NotificationDestinationKindSharedTarget), destinationKey, tc.name+"@example.com", tc.status, tc.attempts, tc.maxAttempts, createdAt, nullableTimeValue(tc.nextAttemptAt), nullableTimeValue(tc.claimedAt), nullableTimeValue(tc.claimExpires), nullableStringValue(tc.claimedBy), createdAt, sentAt)
			if insertErr != nil {
				t.Fatalf("insert state row failed: %v", insertErr)
			}

			result, claimErr := repo.AcquireForDelivery(ctx, repository.NotificationDeliveryClaimInput{
				Delivery:      domain.NotificationDelivery{BuildID: buildID, EventType: domain.NotificationEventTypeBuildFailed, Transport: domain.NotificationTransportEmail, DestinationKind: domain.NotificationDestinationKindSharedTarget, DestinationKey: destinationKey, Recipient: tc.name + "@example.com"},
				ClaimOwner:    uuid.NewString(),
				Now:           now,
				ClaimDuration: time.Minute,
				MaxAttempts:   3,
			})
			if claimErr != nil {
				t.Fatalf("claim state %s failed: %v", tc.name, claimErr)
			}
			if result.Outcome != tc.wantOutcome {
				t.Fatalf("expected outcome %q, got %q", tc.wantOutcome, result.Outcome)
			}
		})
	}
}

func TestNotificationSubscriptionRepository_EnsureConfigEmailTargetConcurrentReturnsOneCanonicalRow_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	repo := NewNotificationSubscriptionRepository(db)
	recipient := "Build.Alerts+" + uuid.NewString() + "@Example.com"
	variants := []string{recipient, strings.ToLower(recipient)}

	const workers = 8
	start := make(chan struct{})
	targets := make(chan domain.NotificationTarget, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			target, err := repo.EnsureConfigEmailTarget(context.Background(), repository.EnsureConfigNotificationEmailTargetInput{
				ID:        uuid.NewString(),
				Name:      "Build alerts",
				Recipient: variants[idx%len(variants)],
			})
			if err != nil {
				errs <- err
				return
			}
			targets <- target
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(targets)

	for err := range errs {
		if err != nil {
			t.Fatalf("ensure config email target failed: %v", err)
		}
	}

	var canonicalID string
	for target := range targets {
		if canonicalID == "" {
			canonicalID = target.ID
			recipient = target.Recipient
		}
		if target.ID != canonicalID {
			t.Fatalf("expected all callers to observe the same canonical target id, got %q and %q", canonicalID, target.ID)
		}
		if target.Origin != domain.NotificationTargetOriginConfigDefault {
			t.Fatalf("expected config-default origin, got %q", target.Origin)
		}
		if target.OwnerUserID != nil {
			t.Fatalf("expected ownerless config target, got owner %v", *target.OwnerUserID)
		}
	}

	var count int
	countErr := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM notification_targets
		WHERE type = 'email'
		  AND origin = 'config_default'
		  AND owner_user_id IS NULL
		  AND lower(recipient) = lower($1)
	`, recipient).Scan(&count)
	if countErr != nil {
		t.Fatalf("count config targets failed: %v", countErr)
	}
	if count != 1 {
		t.Fatalf("expected one canonical config-default target, got %d", count)
	}
}

func TestMigration00032_BackfillsNotificationDeliveryIdentityDeterministically_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	schema := createNotificationIntegrationSchema(t, db, ctx)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Fatalf("close dedicated connection: %v", closeErr)
		}
	}()

	setSearchPath(t, ctx, conn, schema)
	applyNotificationMigrationSeries(t, ctx, conn, "00031")

	now := time.Now().UTC()
	projectID := uuid.NewString()
	jobID := uuid.NewString()
	buildEmailA := uuid.NewString()
	buildEmailB := uuid.NewString()
	buildWebhook := uuid.NewString()
	buildDM := uuid.NewString()
	userID := uuid.NewString()
	ownedTargetID := uuid.NewString()
	webhookTargetID := uuid.NewString()
	workspaceID := uuid.NewString()
	identityID := uuid.NewString()

	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4)
	`, projectID, "Project "+projectID, "project-"+uuid.NewString(), now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO jobs (id, project_id, name, priority, repository_url, push_enabled, trigger_mode, branch_allowlist, tag_allowlist, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, 5, 'https://example.invalid/repo.git', FALSE, 'branches', '[]'::jsonb, '[]'::jsonb, TRUE, $4, $4)
	`, jobID, projectID, "job-"+jobID, now)
	for index, buildID := range []string{buildEmailA, buildEmailB, buildWebhook, buildDM} {
		mustExecNotificationIntegration(t, ctx, conn, `
			INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind)
			VALUES ($1, $2, $3, $4, 5, 'pending', $5, 0, 1, 'manual', 'external')
		`, buildID, index+1, projectID, jobID, now)
	}
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO users (id, email, display_name, global_role, created_at, updated_at)
		VALUES ($1, $2, $3, 'user', $4, $4)
	`, userID, "owner+"+userID+"@example.com", "Owner", now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_targets (id, owner_user_id, type, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, $2, 'email', $3, $4, TRUE, $5, $5)
	`, ownedTargetID, userID, "Personal email", "owner+"+userID+"@example.com", now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_targets (id, type, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, 'slack_webhook', $2, $3, TRUE, $4, $4)
	`, webhookTargetID, "Slack build alerts", "https://hooks.slack.test/services/T/B/X", now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO slack_workspace_integrations (id, workspace_id, workspace_name, workspace_url, bot_id, authed_user_id, app_id, bot_token_secret, enabled, connected_at, created_at, updated_at)
		VALUES ($1, 'T123', 'Workspace', 'https://workspace.example.com', 'B123', 'Ubot', 'A123', 'secret', TRUE, $2, $2, $2)
	`, workspaceID, now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO user_slack_identities (id, user_id, slack_workspace_integration_id, slack_user_id, slack_display_name, slack_real_name, slack_handle, slack_email, profile_image_url, enabled, linked_at, last_verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'U999', 'dev', 'Dev User', 'dev', 'dev@example.com', NULL, TRUE, $4, $4, $4, $4)
	`, identityID, userID, workspaceID, now)

	legacyRecipient := " Shared.Alerts+Migration@Test.example.com "
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_deliveries (id, build_id, event_type, recipient, status, attempts, created_at, updated_at)
		VALUES ($1, $2, 'build_failed', $3, 'pending', 0, $4, $4)
	`, "delivery-email-a", buildEmailA, legacyRecipient, now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_deliveries (id, build_id, event_type, recipient, status, attempts, created_at, updated_at)
		VALUES ($1, $2, 'build_succeeded', $3, 'sent', 1, $4, $4)
	`, "delivery-email-b", buildEmailB, "shared.alerts+migration@test.example.com", now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_deliveries (id, build_id, event_type, recipient, status, attempts, created_at, updated_at)
		VALUES ($1, $2, 'build_failed', $3, 'pending', 0, $4, $4)
	`, "delivery-webhook", buildWebhook, "slack_webhook:"+webhookTargetID, now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_deliveries (id, build_id, event_type, recipient, status, attempts, created_at, updated_at)
		VALUES ($1, $2, 'build_failed', $3, 'failed', 1, $4, $4)
	`, "delivery-dm", buildDM, "slack_dm:"+workspaceID+":U999", now)

	applyNotificationMigrationFile(t, ctx, conn, "00032_refactor_notification_delivery_identity.sql")

	var configTargetID string
	var configOrigin string
	var configOwner sql.NullString
	configErr := conn.QueryRowContext(ctx, `
		SELECT id::text, origin, owner_user_id::text
		FROM notification_targets
		WHERE type = 'email'
		  AND origin = 'config_default'
		  AND owner_user_id IS NULL
		  AND lower(recipient) = lower($1)
	`, strings.TrimSpace(legacyRecipient)).Scan(&configTargetID, &configOrigin, &configOwner)
	if configErr != nil {
		t.Fatalf("lookup config-default target failed: %v", configErr)
	}
	if configOrigin != string(domain.NotificationTargetOriginConfigDefault) {
		t.Fatalf("expected config-default origin, got %q", configOrigin)
	}
	if configOwner.Valid {
		t.Fatalf("expected config-default target to remain ownerless, got owner %q", configOwner.String)
	}

	var ownedOrigin string
	ownedErr := conn.QueryRowContext(ctx, `SELECT origin FROM notification_targets WHERE id = $1`, ownedTargetID).Scan(&ownedOrigin)
	if ownedErr != nil {
		t.Fatalf("lookup owned target origin failed: %v", ownedErr)
	}
	if ownedOrigin != string(domain.NotificationTargetOriginManual) {
		t.Fatalf("expected owned target origin to backfill to manual, got %q", ownedOrigin)
	}

	assertDeliveryIdentity := func(deliveryID string, wantTransport string, wantKind string, wantKey string, wantTargetID string, wantUserID string, wantWorkspaceID string) {
		t.Helper()
		var gotTransport string
		var gotKind string
		var gotKey string
		var gotTargetID sql.NullString
		var gotUserID sql.NullString
		var gotWorkspaceID sql.NullString
		err := conn.QueryRowContext(ctx, `
			SELECT transport, destination_kind, destination_key, notification_target_id::text, recipient_user_id::text, slack_workspace_integration_id::text
			FROM notification_deliveries
			WHERE id = $1
		`, deliveryID).Scan(&gotTransport, &gotKind, &gotKey, &gotTargetID, &gotUserID, &gotWorkspaceID)
		if err != nil {
			t.Fatalf("lookup migrated delivery %s failed: %v", deliveryID, err)
		}
		if gotTransport != wantTransport || gotKind != wantKind || gotKey != wantKey {
			t.Fatalf("unexpected delivery identity for %s: transport=%q kind=%q key=%q", deliveryID, gotTransport, gotKind, gotKey)
		}
		if wantTargetID == "" {
			if gotTargetID.Valid {
				t.Fatalf("expected no target id for %s, got %q", deliveryID, gotTargetID.String)
			}
		} else if !gotTargetID.Valid || gotTargetID.String != wantTargetID {
			t.Fatalf("unexpected target id for %s: got %q want %q", deliveryID, gotTargetID.String, wantTargetID)
		}
		if wantUserID == "" {
			if gotUserID.Valid {
				t.Fatalf("expected no recipient user id for %s, got %q", deliveryID, gotUserID.String)
			}
		} else if !gotUserID.Valid || gotUserID.String != wantUserID {
			t.Fatalf("unexpected recipient user id for %s: got %q want %q", deliveryID, gotUserID.String, wantUserID)
		}
		if wantWorkspaceID == "" {
			if gotWorkspaceID.Valid {
				t.Fatalf("expected no workspace id for %s, got %q", deliveryID, gotWorkspaceID.String)
			}
		} else if !gotWorkspaceID.Valid || gotWorkspaceID.String != wantWorkspaceID {
			t.Fatalf("unexpected workspace id for %s: got %q want %q", deliveryID, gotWorkspaceID.String, wantWorkspaceID)
		}
	}

	assertDeliveryIdentity("delivery-email-a", "email", "shared_target", "email-target:"+configTargetID, configTargetID, "", "")
	assertDeliveryIdentity("delivery-email-b", "email", "shared_target", "email-target:"+configTargetID, configTargetID, "", "")
	assertDeliveryIdentity("delivery-webhook", "slack_webhook", "shared_target", "slack-webhook-target:"+webhookTargetID, webhookTargetID, "", "")
	assertDeliveryIdentity("delivery-dm", "slack_dm", "slack_identity", "slack-dm:"+workspaceID+":U999", "", userID, workspaceID)
}

func TestNotificationTargetOriginConstraintRejectsInvalidCombinations_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	schema := createNotificationIntegrationSchema(t, db, ctx)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Fatalf("close dedicated connection: %v", closeErr)
		}
	}()

	setSearchPath(t, ctx, conn, schema)
	applyNotificationMigrationSeries(t, ctx, conn, "00032")

	now := time.Now().UTC()
	userID := uuid.NewString()
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO users (id, email, display_name, global_role, created_at, updated_at)
		VALUES ($1, $2, $3, 'user', $4, $4)
	`, userID, "user+"+userID+"@example.com", "User", now)

	assertConstraint := func(name string, query string, args ...any) {
		t.Helper()
		_, execErr := conn.ExecContext(ctx, query, args...)
		if execErr == nil {
			t.Fatalf("expected %s insert to fail", name)
		}
		var pgErr *pgconn.PgError
		if !errors.As(execErr, &pgErr) {
			t.Fatalf("expected pg error for %s, got %T: %v", name, execErr, execErr)
		}
		if pgErr.ConstraintName != "notification_targets_origin_semantics_check" && pgErr.ConstraintName != "notification_targets_origin_check" {
			t.Fatalf("expected named origin constraint for %s, got %q", name, pgErr.ConstraintName)
		}
	}

	assertConstraint("config-default slack webhook", `
		INSERT INTO notification_targets (id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, 'slack_webhook', 'config_default', 'Slack invalid', 'https://hooks.slack.example/services/T/B/X', TRUE, $2, $2)
	`, uuid.NewString(), now)
	assertConstraint("owned config-default email", `
		INSERT INTO notification_targets (id, owner_user_id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, $2, 'email', 'config_default', 'Owned invalid', '<owned@example.com>', TRUE, $3, $3)
	`, uuid.NewString(), userID, now)
	assertConstraint("unsupported origin", `
		INSERT INTO notification_targets (id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, 'email', 'legacy', 'Legacy invalid', '<legacy@example.com>', TRUE, $2, $2)
	`, uuid.NewString(), now)

	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_targets (id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, 'email', 'manual', 'Manual shared', '<shared@example.com>', TRUE, $2, $2)
	`, uuid.NewString(), now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_targets (id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, 'slack_webhook', 'manual', 'Manual Slack', 'https://hooks.slack.example/services/T/B/Y', TRUE, $2, $2)
	`, uuid.NewString(), now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_targets (id, owner_user_id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, $2, 'email', 'manual', 'Manual personal', '<personal@example.com>', TRUE, $3, $3)
	`, uuid.NewString(), userID, now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_targets (id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, 'email', 'config_default', 'Config default', '<config@example.com>', TRUE, $2, $2)
	`, uuid.NewString(), now)
}

func TestMigration00032_DownFailsIntentionally_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	schema := createNotificationIntegrationSchema(t, db, ctx)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Fatalf("close dedicated connection: %v", closeErr)
		}
	}()

	setSearchPath(t, ctx, conn, schema)
	applyNotificationMigrationSeries(t, ctx, conn, "00032")

	err = applyNotificationMigrationDownExpectError(ctx, conn, "00032_refactor_notification_delivery_identity.sql")
	if err == nil {
		t.Fatal("expected migration 00032 down to fail intentionally")
	}
	message := err.Error()
	for _, want := range []string{
		"intentionally irreversible",
		"legacy notification_targets UNIQUE (type, recipient) model cannot represent valid post-migration data",
		"manual data migration",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected rollback error to contain %q, got %v", want, err)
		}
	}
}

func TestMigration00033_BackfillsClaimableLedgerState_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	schema := createNotificationIntegrationSchema(t, db, ctx)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Fatalf("close dedicated connection: %v", closeErr)
		}
	}()

	setSearchPath(t, ctx, conn, schema)
	applyNotificationMigrationSeries(t, ctx, conn, "00032")

	now := time.Now().UTC()
	projectID := uuid.NewString()
	jobID := uuid.NewString()
	buildSent := uuid.NewString()
	buildFailed := uuid.NewString()
	buildPending := uuid.NewString()

	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4)
	`, projectID, "Project "+projectID, "project-"+uuid.NewString(), now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO jobs (id, project_id, name, priority, repository_url, push_enabled, trigger_mode, branch_allowlist, tag_allowlist, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, 5, 'https://example.invalid/repo.git', FALSE, 'branches', '[]'::jsonb, '[]'::jsonb, TRUE, $4, $4)
	`, jobID, projectID, "job-"+jobID, now)
	for index, buildID := range []string{buildSent, buildFailed, buildPending} {
		mustExecNotificationIntegration(t, ctx, conn, `
			INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind)
			VALUES ($1, $2, $3, $4, 5, 'pending', $5, 0, 1, 'manual', 'external')
		`, buildID, index+1, projectID, jobID, now)
	}

	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status, attempts, created_at, updated_at, sent_at
		) VALUES
			('delivery-sent', $1, 'build_succeeded', 'email', 'shared_target', 'email-target:sent', '<sent@example.com>', 'sent', 1, $4, $4, $4),
			('delivery-failed', $2, 'build_failed', 'email', 'shared_target', 'email-target:failed', '<failed@example.com>', 'failed', 2, $4, $4, NULL),
			('delivery-pending', $3, 'build_failed', 'slack_webhook', 'shared_target', 'slack-webhook-target:pending', 'https://hooks.slack.example/services/T/B/X', 'pending', 0, $4, $4, NULL)
	`, buildSent, buildFailed, buildPending, now)

	applyNotificationMigrationFile(t, ctx, conn, "00033_add_claimable_notification_delivery_ledger.sql")

	assertRow := func(id string, wantStatus string, wantAttempts int, wantMaxAttempts int, wantCategory string, wantReason string, wantSent bool) {
		t.Helper()
		var status string
		var attempts int
		var maxAttempts int
		var failureCategory sql.NullString
		var failureReason sql.NullString
		var nextAttemptAt sql.NullTime
		var claimedAt sql.NullTime
		var claimExpiresAt sql.NullTime
		var claimedBy sql.NullString
		var sentAt sql.NullTime
		rowErr := conn.QueryRowContext(ctx, `
			SELECT status, attempts, max_attempts, failure_category, failure_reason, next_attempt_at, claimed_at, claim_expires_at, claimed_by, sent_at
			FROM notification_deliveries
			WHERE id = $1
		`, id).Scan(&status, &attempts, &maxAttempts, &failureCategory, &failureReason, &nextAttemptAt, &claimedAt, &claimExpiresAt, &claimedBy, &sentAt)
		if rowErr != nil {
			t.Fatalf("lookup migrated row %s failed: %v", id, rowErr)
		}
		if status != wantStatus || attempts != wantAttempts || maxAttempts != wantMaxAttempts {
			t.Fatalf("unexpected migrated row %s: status=%q attempts=%d max_attempts=%d", id, status, attempts, maxAttempts)
		}
		if wantCategory == "" {
			if failureCategory.Valid {
				t.Fatalf("expected no failure category for %s, got %q", id, failureCategory.String)
			}
		} else if !failureCategory.Valid || failureCategory.String != wantCategory {
			t.Fatalf("unexpected failure category for %s: got %q want %q", id, failureCategory.String, wantCategory)
		}
		if wantReason == "" {
			if failureReason.Valid {
				t.Fatalf("expected no failure reason for %s, got %q", id, failureReason.String)
			}
		} else if !failureReason.Valid || failureReason.String != wantReason {
			t.Fatalf("unexpected failure reason for %s: got %q want %q", id, failureReason.String, wantReason)
		}
		if nextAttemptAt.Valid || claimedAt.Valid || claimExpiresAt.Valid || claimedBy.Valid {
			t.Fatalf("expected migrated row %s to clear retry scheduling and claim metadata", id)
		}
		if wantSent != sentAt.Valid {
			t.Fatalf("unexpected sent_at presence for %s: wantSent=%t gotValid=%t", id, wantSent, sentAt.Valid)
		}
	}

	assertRow("delivery-sent", string(domain.NotificationDeliveryStatusSent), 1, 1, "", "", true)
	assertRow("delivery-failed", string(domain.NotificationDeliveryStatusFailedPermanent), 2, 2, string(domain.NotificationDeliveryFailureCategoryPermanent), "legacy_failed_no_retry", false)
	assertRow("delivery-pending", string(domain.NotificationDeliveryStatusFailedExhausted), 1, 1, string(domain.NotificationDeliveryFailureCategoryRetryable), "legacy_pending_no_automatic_retry", false)
}

func TestMigration00034_AddsRecoveryScanIndexes_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	schema := createNotificationIntegrationSchema(t, db, ctx)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Fatalf("close dedicated connection: %v", closeErr)
		}
	}()

	setSearchPath(t, ctx, conn, schema)
	applyNotificationMigrationSeries(t, ctx, conn, "00033")

	assertIndexExists := func(indexName string, wantColumns string, wantPredicate string) {
		t.Helper()
		var definition string
		var predicate sql.NullString
		queryErr := conn.QueryRowContext(ctx, `
			SELECT pg_get_indexdef(i.indexrelid), pg_get_expr(i.indpred, i.indrelid)
			FROM pg_index i
			JOIN pg_class c ON c.oid = i.indexrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema()
			  AND c.relname = $1
		`, indexName).Scan(&definition, &predicate)
		if queryErr != nil {
			t.Fatalf("lookup index %s failed: %v", indexName, queryErr)
		}
		if !strings.Contains(definition, wantColumns) {
			t.Fatalf("expected index %s definition %q to contain %q", indexName, definition, wantColumns)
		}
		if !predicate.Valid || !strings.Contains(predicate.String, wantPredicate) {
			t.Fatalf("expected index %s predicate %q to contain %q", indexName, predicate.String, wantPredicate)
		}
	}
	assertIndexMissing := func(indexName string) {
		t.Helper()
		var exists bool
		queryErr := conn.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = current_schema()
				  AND c.relname = $1
			)
		`, indexName).Scan(&exists)
		if queryErr != nil {
			t.Fatalf("lookup index presence %s failed: %v", indexName, queryErr)
		}
		if exists {
			t.Fatalf("expected index %s to be absent", indexName)
		}
	}

	assertIndexExists("idx_notification_deliveries_retry_waiting_next_attempt_at", "(next_attempt_at)", "status = 'retry_waiting'::text")
	assertIndexExists("idx_notification_deliveries_sending_claim_expires_at", "(claim_expires_at)", "status = 'sending'::text")

	applyNotificationMigrationFile(t, ctx, conn, "00034_add_notification_recovery_scan_indexes.sql")

	assertIndexExists("idx_notification_deliveries_retry_waiting_next_attempt_at_id", "(next_attempt_at, id)", "status = 'retry_waiting'::text")
	assertIndexExists("idx_notification_deliveries_sending_claim_expires_at_id", "(claim_expires_at, id)", "status = 'sending'::text")
	assertIndexMissing("idx_notification_deliveries_retry_waiting_next_attempt_at")
	assertIndexMissing("idx_notification_deliveries_sending_claim_expires_at")

	if downErr := applyNotificationMigrationDownExpectError(ctx, conn, "00034_add_notification_recovery_scan_indexes.sql"); downErr != nil {
		t.Fatalf("expected migration 00034 down to succeed, got %v", downErr)
	}

	assertIndexExists("idx_notification_deliveries_retry_waiting_next_attempt_at", "(next_attempt_at)", "status = 'retry_waiting'::text")
	assertIndexExists("idx_notification_deliveries_sending_claim_expires_at", "(claim_expires_at)", "status = 'sending'::text")
	assertIndexMissing("idx_notification_deliveries_retry_waiting_next_attempt_at_id")
	assertIndexMissing("idx_notification_deliveries_sending_claim_expires_at_id")
}

func TestNotificationDeliveryLedgerConstraintsRejectInvalidStates_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	schema := createNotificationIntegrationSchema(t, db, ctx)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Fatalf("close dedicated connection: %v", closeErr)
		}
	}()

	setSearchPath(t, ctx, conn, schema)
	applyNotificationMigrationSeries(t, ctx, conn, "00033")

	now := time.Now().UTC()
	projectID := uuid.NewString()
	jobID := uuid.NewString()
	buildID := uuid.NewString()

	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4)
	`, projectID, "Project "+projectID, "project-"+uuid.NewString(), now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO jobs (id, project_id, name, priority, repository_url, push_enabled, trigger_mode, branch_allowlist, tag_allowlist, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, 5, 'https://example.invalid/repo.git', FALSE, 'branches', '[]'::jsonb, '[]'::jsonb, TRUE, $4, $4)
	`, jobID, projectID, "job-"+jobID, now)
	mustExecNotificationIntegration(t, ctx, conn, `
		INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind)
		VALUES ($1, 1, $2, $3, 5, 'pending', $4, 0, 1, 'manual', 'external')
	`, buildID, projectID, jobID, now)

	assertConstraint := func(name string, expected string, query string, args ...any) {
		t.Helper()
		_, execErr := conn.ExecContext(ctx, query, args...)
		if execErr == nil {
			t.Fatalf("expected %s insert to fail", name)
		}
		var pgErr *pgconn.PgError
		if !errors.As(execErr, &pgErr) {
			t.Fatalf("expected pg error for %s, got %T: %v", name, execErr, execErr)
		}
		if pgErr.ConstraintName != expected {
			t.Fatalf("expected constraint %q for %s, got %q", expected, name, pgErr.ConstraintName)
		}
	}

	assertConstraint("attempts above max", "notification_deliveries_attempts_check", `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, created_at, updated_at
		) VALUES ($1, $2, 'build_failed', 'email', 'shared_target', 'email-target:a', '<a@example.com>', 'pending', 2, 1, $3, $3)
	`, uuid.NewString(), buildID, now)

	assertConstraint("retry waiting permanent category", "notification_deliveries_claim_retry_state_check", `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, next_attempt_at, failure_category, created_at, updated_at
		) VALUES ($1, $2, 'build_failed', 'email', 'shared_target', 'email-target:b', '<b@example.com>', 'retry_waiting', 1, 3, $3, 'permanent', $4, $4)
	`, uuid.NewString(), buildID, now.Add(time.Minute), now)

	assertConstraint("retry waiting active claim", "notification_deliveries_claim_retry_state_check", `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, next_attempt_at, claimed_at, claim_expires_at, claimed_by, failure_category, created_at, updated_at
		) VALUES ($1, $2, 'build_failed', 'email', 'shared_target', 'email-target:c', '<c@example.com>', 'retry_waiting', 1, 3, $3, $4, $5, 'worker-a', 'retryable', $6, $6)
	`, uuid.NewString(), buildID, now.Add(time.Minute), now, now.Add(2*time.Minute), now)

	assertConstraint("failed exhausted below max", "notification_deliveries_claim_retry_state_check", `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, failure_category, created_at, updated_at
		) VALUES ($1, $2, 'build_failed', 'email', 'shared_target', 'email-target:d', '<d@example.com>', 'failed_exhausted', 1, 3, 'retryable', $3, $3)
	`, uuid.NewString(), buildID, now)

	assertConstraint("failed permanent retryable category", "notification_deliveries_claim_retry_state_check", `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, failure_category, created_at, updated_at
		) VALUES ($1, $2, 'build_failed', 'email', 'shared_target', 'email-target:e', '<e@example.com>', 'failed_permanent', 1, 3, 'retryable', $3, $3)
	`, uuid.NewString(), buildID, now)

	assertConstraint("sent retains retry schedule", "notification_deliveries_claim_retry_state_check", `
		INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key, recipient, status,
			attempts, max_attempts, next_attempt_at, sent_at, created_at, updated_at
		) VALUES ($1, $2, 'build_failed', 'email', 'shared_target', 'email-target:f', '<f@example.com>', 'sent', 1, 3, $3, $4, $4, $4)
	`, uuid.NewString(), buildID, now.Add(time.Minute), now)
}

func TestMigration00033_DownFailsIntentionally_Postgres(t *testing.T) {
	db := openNotificationIntegrationDB(t)
	defer closeNotificationIntegrationDB(t, db)

	ctx := context.Background()
	schema := createNotificationIntegrationSchema(t, db, ctx)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Fatalf("close dedicated connection: %v", closeErr)
		}
	}()

	setSearchPath(t, ctx, conn, schema)
	applyNotificationMigrationSeries(t, ctx, conn, "00033")

	err = applyNotificationMigrationDownExpectError(ctx, conn, "00033_add_claimable_notification_delivery_ledger.sql")
	if err == nil {
		t.Fatal("expected migration 00033 down to fail intentionally")
	}
	message := err.Error()
	for _, want := range []string{
		"intentionally irreversible",
		"claimable notification delivery ledger",
		"cannot be safely collapsed back into the legacy three-status model automatically",
		"manual data migration",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected rollback error to contain %q, got %v", want, err)
		}
	}
}

func openNotificationIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("COYOTE_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set COYOTE_TEST_DATABASE_URL to run Postgres integration tests")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres connection: %v", err)
	}
	return db
}

func closeNotificationIntegrationDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("close postgres connection: %v", err)
	}
}

func createNotificationIntegrationBuild(t *testing.T, db *sql.DB, ctx context.Context) string {
	t.Helper()
	now := time.Now().UTC()
	projectID := uuid.NewString()
	jobID := uuid.NewString()
	buildID := uuid.NewString()

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE build_id = $1`, buildID)
		_, _ = db.ExecContext(ctx, `DELETE FROM builds WHERE id = $1`, buildID)
		_, _ = db.ExecContext(ctx, `DELETE FROM jobs WHERE id = $1`, jobID)
		_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
	})

	_, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4)
	`, projectID, "Project "+projectID, "project-"+uuid.NewString(), now)
	if err != nil {
		t.Fatalf("insert project %s: %v", projectID, err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO jobs (id, project_id, name, priority, repository_url, push_enabled, trigger_mode, branch_allowlist, tag_allowlist, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, 5, 'https://example.invalid/repo.git', FALSE, 'branches', '[]'::jsonb, '[]'::jsonb, TRUE, $4, $4)
	`, jobID, projectID, "job-"+jobID, now)
	if err != nil {
		t.Fatalf("insert job %s: %v", jobID, err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO builds (id, build_number, project_id, job_id, priority, status, created_at, current_step_index, attempt_number, trigger_kind, image_source_kind)
		VALUES ($1, 1, $2, $3, 5, 'pending', $4, 0, 1, 'manual', 'external')
	`, buildID, projectID, jobID, now)
	if err != nil {
		t.Fatalf("insert build %s: %v", buildID, err)
	}

	return buildID
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func nullableTimeValue(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}

func nullableStringValue(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return strings.TrimSpace(*value)
}

func createNotificationIntegrationSchema(t *testing.T, db *sql.DB, ctx context.Context) string {
	t.Helper()
	schema := "notification_identity_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %s", quoteNotificationIdentifier(schema)))
	if err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", quoteNotificationIdentifier(schema)))
	})
	return schema
}

func setSearchPath(t *testing.T, ctx context.Context, conn *sql.Conn, schema string) {
	t.Helper()
	_, err := conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", quoteNotificationIdentifier(schema)))
	if err != nil {
		t.Fatalf("set search_path for %s: %v", schema, err)
	}
}

func applyNotificationMigrationSeries(t *testing.T, ctx context.Context, conn *sql.Conn, maxPrefix string) {
	t.Helper()
	entries, err := os.ReadDir("../../../db/migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix := strings.SplitN(name, "_", 2)[0]
		if prefix <= maxPrefix {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	for _, name := range files {
		applyNotificationMigrationFile(t, ctx, conn, name)
	}
}

func applyNotificationMigrationFile(t *testing.T, ctx context.Context, conn *sql.Conn, fileName string) {
	t.Helper()
	upSQL, err := loadNotificationMigrationSection(fileName, true)
	if err != nil {
		t.Fatalf("load up migration %s: %v", fileName, err)
	}
	if strings.TrimSpace(upSQL) == "" {
		return
	}
	_, err = conn.ExecContext(ctx, upSQL)
	if err != nil {
		t.Fatalf("apply migration %s: %v", fileName, err)
	}
}

func applyNotificationMigrationDownExpectError(ctx context.Context, conn *sql.Conn, fileName string) error {
	downSQL, err := loadNotificationMigrationSection(fileName, false)
	if err != nil {
		return err
	}
	if strings.TrimSpace(downSQL) == "" {
		return nil
	}
	_, err = conn.ExecContext(ctx, downSQL)
	return err
}

func loadNotificationMigrationSection(fileName string, up bool) (string, error) {
	path := filepath.Join("../../../db/migrations", fileName)
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	text := string(content)
	downIndex := strings.Index(text, "-- +goose Down")
	if downIndex < 0 {
		if up {
			return text, nil
		}
		return "", nil
	}
	if up {
		return text[:downIndex], nil
	}
	return text[downIndex+len("-- +goose Down"):], nil
}

func mustExecNotificationIntegration(t *testing.T, ctx context.Context, conn *sql.Conn, query string, args ...any) {
	t.Helper()
	_, err := conn.ExecContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
}

func quoteNotificationIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
