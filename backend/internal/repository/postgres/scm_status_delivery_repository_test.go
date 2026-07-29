package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNormalizePostgresSCMStatusDeliveryClaimInputRejectsWhitespaceIdentitySnapshot(t *testing.T) {
	whitespace := "  "
	connectionID := "connection-1"
	providerRepositoryID := "repository-1"
	delivery := domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: time.Now().UTC(), Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", RegisteredRepositoryID: &whitespace, SCMConnectionID: &connectionID, ProviderRepositoryID: &providerRepositoryID, CommitSHA: "deadbeef", Context: "coyote/build", DesiredState: domain.SCMCommitStatusStatePending}
	if _, _, _, _, err := normalizePostgresSCMStatusDeliveryClaimInput(repository.SCMStatusDeliveryClaimInput{Delivery: delivery, ClaimOwner: "worker-1", Now: time.Now().UTC(), ClaimDuration: time.Minute, MaxAttempts: 1}); err == nil {
		t.Fatal("expected whitespace identity snapshot to be rejected")
	}
}

func TestSCMStatusDeliveryRepository_UsesRepositoryIdentityForConflictAndLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMStatusDeliveryRepository(db)
	now := time.Now().UTC()
	registrationID := "registration-1"
	connectionID := "connection-1"
	providerRepositoryID := "repository-1"
	row := scmStatusDeliveryTestColumns()
	stored := []driver.Value{"delivery-1", "build-1", 1, now, "github", "octo", "repo", registrationID, connectionID, providerRepositoryID, "deadbeef", "coyote/build", "pending", "pending", "pending", nil, "sent", 1, 2, now, nil, nil, nil, nil, nil, nil, nil, now, nil, now, now}
	delivery := domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", RegisteredRepositoryID: &registrationID, SCMConnectionID: &connectionID, ProviderRepositoryID: &providerRepositoryID, CommitSHA: "deadbeef", Context: "coyote/build", DesiredState: domain.SCMCommitStatusStatePending, Description: "pending"}

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO scm_status_deliveries").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("WHERE scm_connection_id").WithArgs(connectionID, providerRepositoryID, "deadbeef", "coyote/build").WillReturnRows(sqlmock.NewRows(row).AddRow(stored...))
	result, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{Delivery: delivery, ClaimOwner: "worker", Now: now, ClaimDuration: time.Minute, MaxAttempts: 2})
	if claimErr != nil || result.Outcome != repository.SCMStatusDeliveryClaimOutcomeAlreadySent {
		t.Fatalf("expected identity conflict to resolve existing delivery, result=%+v err=%v", result, claimErr)
	}

	mock.ExpectQuery("WHERE scm_connection_id").WithArgs(connectionID, providerRepositoryID, "deadbeef", "coyote/build").WillReturnRows(sqlmock.NewRows(row).AddRow(stored...))
	fetched, fetchErr := repo.GetByRepositoryIdentity(context.Background(), " "+connectionID+" ", " "+providerRepositoryID+" ", " deadbeef ", " coyote/build ")
	if fetchErr != nil || fetched.SCMConnectionID == nil || *fetched.SCMConnectionID != connectionID {
		t.Fatalf("expected identity lookup delivery, got %+v err=%v", fetched, fetchErr)
	}

	mock.ExpectQuery("WHERE scm_connection_id").WithArgs(connectionID, providerRepositoryID, "missing", "coyote/build").WillReturnError(sql.ErrNoRows)
	if _, missingErr := repo.GetByRepositoryIdentity(context.Background(), connectionID, providerRepositoryID, "missing", "coyote/build"); !errors.Is(missingErr, repository.ErrSCMStatusDeliveryNotFound) {
		t.Fatalf("expected missing identity delivery error, got %v", missingErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet sql expectations: %v", expectationsErr)
	}
}

func TestSCMStatusDeliveryRepository_AcquireForDelivery_CreateAndMarkSent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMStatusDeliveryRepository(db)
	now := time.Now().UTC()
	claimExpiresAt := now.Add(2 * time.Minute)
	row := scmStatusDeliveryTestColumns()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", 1, now, "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "pending", nil, "Coyote build is in progress", "https://coyote.example/builds/build-1", "sending", 1, 3, now, nil, now, claimExpiresAt, "worker-a", nil, nil, nil, nil, nil, now, now,
	))
	mock.ExpectCommit()

	claimed, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{
		Delivery: domain.SCMStatusDelivery{
			BuildID:         "build-1",
			BuildAttempt:    1,
			BuildCreatedAt:  now,
			Provider:        "github",
			RepositoryOwner: "octo",
			RepositoryName:  "repo",
			CommitSHA:       "abcdef",
			Context:         "coyote/default/job-1",
			DesiredState:    domain.SCMCommitStatusStatePending,
			Description:     "Coyote build is in progress",
			DetailsURL:      strPtrPGSCM("https://coyote.example/builds/build-1"),
		},
		ClaimOwner:    "worker-a",
		Now:           now,
		ClaimDuration: 2 * time.Minute,
		MaxAttempts:   3,
	})
	if claimErr != nil {
		t.Fatalf("acquire for delivery failed: %v", claimErr)
	}
	if claimed.Outcome != repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed {
		t.Fatalf("expected created_claimed outcome, got %q", claimed.Outcome)
	}

	mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", 1, now, "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "pending", "pending", "Coyote build is in progress", "https://coyote.example/builds/build-1", "sent", 1, 3, now, nil, nil, nil, nil, nil, nil, nil, now, nil, now, now,
	))

	updated, updateErr := repo.MarkSent(context.Background(), repository.SCMStatusDeliveryMarkSentInput{
		DeliveryID: "delivery-1",
		ClaimOwner: "worker-a",
		ClaimedAt:  now,
		SentAt:     now,
		State:      domain.SCMCommitStatusStatePending,
	})
	if updateErr != nil {
		t.Fatalf("mark sent failed: %v", updateErr)
	}
	if updated.Outcome != repository.SCMStatusDeliveryUpdateOutcomeUpdated {
		t.Fatalf("expected updated outcome, got %q", updated.Outcome)
	}
	if updated.Delivery.Status != domain.SCMStatusDeliveryStatusSent {
		t.Fatalf("expected sent status, got %q", updated.Delivery.Status)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSCMStatusDeliveryRepository_AcquireForDelivery_ExistingOutcomes(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMStatusDeliveryRepository(db)
	now := time.Now().UTC()
	row := scmStatusDeliveryTestColumns()

	t.Run("already sent", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery("INSERT INTO scm_status_deliveries").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT .* FROM scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
			"delivery-1", "build-1", 1, now, "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "pending", "pending", "Coyote build is in progress", nil, "sent", 1, 3, now, nil, nil, nil, nil, nil, nil, nil, now, nil, now, now,
		))

		result, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{
			Delivery:      domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "abcdef", Context: "coyote/default/job-1", DesiredState: domain.SCMCommitStatusStatePending, Description: "Coyote build is in progress"},
			ClaimOwner:    "worker-a",
			Now:           now,
			ClaimDuration: time.Minute,
			MaxAttempts:   3,
		})
		if claimErr != nil {
			t.Fatalf("acquire failed: %v", claimErr)
		}
		if result.Outcome != repository.SCMStatusDeliveryClaimOutcomeAlreadySent {
			t.Fatalf("expected already_sent, got %q", result.Outcome)
		}
	})

	t.Run("retry due reclaimed", func(t *testing.T) {
		nextAttemptAt := now.Add(-time.Minute)
		claimExpiresAt := now.Add(2 * time.Minute)
		claimedAt := now

		mock.ExpectBegin()
		mock.ExpectQuery("INSERT INTO scm_status_deliveries").WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("SELECT .* FROM scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
			"delivery-2", "build-1", 1, now.Add(-2*time.Minute), "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "retry_waiting", 1, 3, now.Add(-time.Minute), nextAttemptAt, nil, nil, nil, "retryable", "github_api_unavailable", "api unavailable", nil, nil, now, now,
		))
		mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
			"delivery-2", "build-1", 1, now.Add(-2*time.Minute), "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "sending", 2, 3, now, nil, claimedAt, claimExpiresAt, "worker-b", "retryable", "github_api_unavailable", "api unavailable", nil, nil, now, now,
		))
		mock.ExpectCommit()

		result, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{
			Delivery:      domain.SCMStatusDelivery{BuildID: "build-1", BuildAttempt: 1, BuildCreatedAt: now.Add(-2 * time.Minute), Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "abcdef", Context: "coyote/default/job-1", DesiredState: domain.SCMCommitStatusStateFailure, Description: "Coyote build failed"},
			ClaimOwner:    "worker-b",
			Now:           now,
			ClaimDuration: 2 * time.Minute,
			MaxAttempts:   3,
		})
		if claimErr != nil {
			t.Fatalf("acquire failed: %v", claimErr)
		}
		if result.Outcome != repository.SCMStatusDeliveryClaimOutcomeRetryClaimed {
			t.Fatalf("expected retry_claimed, got %q", result.Outcome)
		}
		if result.Delivery.Attempts != 2 {
			t.Fatalf("expected attempts to increment to 2, got %d", result.Delivery.Attempts)
		}
	})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSCMStatusDeliveryRepository_AcquireForDelivery_NewerBuildReplacesOlderStreamOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMStatusDeliveryRepository(db)
	now := time.Now().UTC()
	olderCreatedAt := now.Add(-2 * time.Minute)
	row := scmStatusDeliveryTestColumns()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO scm_status_deliveries").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT .* FROM scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", 1, olderCreatedAt, "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "failure", "failure", "Coyote build failed", nil, "sent", 1, 3, olderCreatedAt, nil, nil, nil, nil, nil, nil, nil, olderCreatedAt, nil, olderCreatedAt, olderCreatedAt,
	))
	mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-2", 2, now, "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "pending", nil, "Coyote build is queued", nil, "sending", 1, 3, now, nil, now, now.Add(2*time.Minute), "worker-b", nil, nil, nil, nil, nil, olderCreatedAt, now,
	))
	mock.ExpectCommit()

	result, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{
		Delivery:      domain.SCMStatusDelivery{BuildID: "build-2", BuildAttempt: 2, BuildCreatedAt: now, Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "abcdef", Context: "coyote/default/job-1", DesiredState: domain.SCMCommitStatusStatePending, Description: "Coyote build is queued"},
		ClaimOwner:    "worker-b",
		Now:           now,
		ClaimDuration: 2 * time.Minute,
		MaxAttempts:   3,
	})
	if claimErr != nil {
		t.Fatalf("replace acquire failed: %v", claimErr)
	}
	if result.Outcome != repository.SCMStatusDeliveryClaimOutcomeCreatedClaimed {
		t.Fatalf("expected created_claimed for newer build replacement, got %q", result.Outcome)
	}
	if result.Delivery.BuildID != "build-2" || result.Delivery.BuildAttempt != 2 {
		t.Fatalf("expected newer build to own stream, got %+v", result.Delivery)
	}

	mock.ExpectQuery(`SELECT .* FROM scm_status_deliveries WHERE provider = \$1 AND repository_owner = \$2 AND repository_name = \$3 AND commit_sha = \$4 AND context_name = \$5`).WithArgs("github", "octo", "repo", "abcdef", "coyote/default/job-1").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-2", 2, now, "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "pending", nil, "Coyote build is queued", nil, "sending", 1, 3, now, nil, now, now.Add(2*time.Minute), "worker-b", nil, nil, nil, nil, nil, olderCreatedAt, now,
	))
	lookup, lookupErr := repo.GetByKey(context.Background(), "github", "octo", "repo", "abcdef", "coyote/default/job-1")
	if lookupErr != nil {
		t.Fatalf("lookup failed: %v", lookupErr)
	}
	if lookup.BuildID != "build-2" {
		t.Fatalf("expected stream lookup to return newer build owner, got %q", lookup.BuildID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSCMStatusDeliveryRepository_ListRecoverable_ValidationAndQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMStatusDeliveryRepository(db)
	now := time.Now().UTC()
	row := scmStatusDeliveryTestColumns()

	if _, err := repo.ListRecoverable(context.Background(), repository.SCMStatusDeliveryRecoverableScanInput{}); err == nil {
		t.Fatal("expected missing scan time error")
	}
	if _, err := repo.ListRecoverable(context.Background(), repository.SCMStatusDeliveryRecoverableScanInput{Now: now, Limit: 0}); err == nil {
		t.Fatal("expected missing limit error")
	}

	mock.ExpectQuery("WITH recoverable AS").WithArgs(now, 2).WillReturnRows(sqlmock.NewRows(row).
		AddRow("delivery-1", "build-1", 1, now.Add(-time.Minute), "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "retry_waiting", 1, 3, now, now, nil, nil, nil, "retryable", "github_api_unavailable", "api unavailable", nil, nil, now, now).
		AddRow("delivery-2", "build-1", 1, now.Add(-2*time.Minute), "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "sending", 2, 3, now, nil, now, now, "worker-a", nil, nil, nil, nil, nil, now, now))

	result, listErr := repo.ListRecoverable(context.Background(), repository.SCMStatusDeliveryRecoverableScanInput{Now: now, Limit: 2})
	if listErr != nil {
		t.Fatalf("list recoverable failed: %v", listErr)
	}
	if len(result) != 2 || result[0].ID != "delivery-1" || result[1].ID != "delivery-2" {
		t.Fatalf("unexpected recoverable result: %+v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSCMStatusDeliveryRepository_UpdateConflictAndFailureWrappers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMStatusDeliveryRepository(db)
	now := time.Now().UTC()
	row := scmStatusDeliveryTestColumns()
	claimExpiresAt := now.Add(time.Minute)

	staleRow := sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", 1, now, "github", "octo", "repo", nil, nil, nil, "abcdef", "coyote/default/job-1", "pending", nil, "desc", nil, "sending", 1, 3, now, nil, now, claimExpiresAt, "worker-a", nil, nil, nil, nil, nil, now, now,
	)
	mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT .* FROM scm_status_deliveries WHERE id = \$1`).WithArgs("delivery-1").WillReturnRows(staleRow)
	lostClaim, lostClaimErr := repo.RecordPermanentFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{DeliveryID: "delivery-1", ClaimOwner: "worker-a", ClaimedAt: now, FailedAt: now, FailureCategory: domain.SCMStatusDeliveryFailureCategoryPermanent, FailureReason: "bad_request"})
	if lostClaimErr != nil {
		t.Fatalf("expected lost-claim conflict resolution, got %v", lostClaimErr)
	}
	if lostClaim.Outcome != repository.SCMStatusDeliveryUpdateOutcomeLostClaim {
		t.Fatalf("expected lost claim outcome, got %q", lostClaim.Outcome)
	}

	mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-2", "build-2", 1, now, "github", "octo", "repo", nil, nil, nil, "123456", "coyote/default/job-2", "failure", nil, "desc", nil, "retry_waiting", 2, 3, now, now.Add(time.Minute), nil, nil, nil, "retryable", "temporary", "boom", nil, nil, now, now,
	))
	retryAt := now.Add(time.Minute)
	retryResult, retryErr := repo.RecordRetryableFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{DeliveryID: "delivery-2", ClaimOwner: "worker-b", ClaimedAt: now, FailedAt: now, NextAttemptAt: &retryAt, FailureCategory: domain.SCMStatusDeliveryFailureCategoryRetryable, FailureReason: "temporary", LastError: strPtrPGSCM("boom")})
	if retryErr != nil || retryResult.Delivery.Status != domain.SCMStatusDeliveryStatusRetryWaiting {
		t.Fatalf("unexpected retryable failure result: %+v err=%v", retryResult, retryErr)
	}

	mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-3", "build-3", 1, now, "github", "octo", "repo", nil, nil, nil, "654321", "coyote/default/job-3", "failure", nil, "desc", nil, "failed_exhausted", 3, 3, now, nil, nil, nil, nil, "retryable", "exhausted", "boom", nil, nil, now, now,
	))
	exhaustedResult, exhaustedErr := repo.RecordExhaustedFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{DeliveryID: "delivery-3", ClaimOwner: "worker-c", ClaimedAt: now, FailedAt: now, FailureCategory: domain.SCMStatusDeliveryFailureCategoryRetryable, FailureReason: "exhausted", LastError: strPtrPGSCM("boom")})
	if exhaustedErr != nil || exhaustedResult.Delivery.Status != domain.SCMStatusDeliveryStatusFailedExhausted {
		t.Fatalf("unexpected exhausted failure result: %+v err=%v", exhaustedResult, exhaustedErr)
	}

	claimOwner := "worker-d"
	mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-4", "build-4", 1, now, "github", "octo", "repo", nil, nil, nil, "999999", "coyote/default/job-4", "pending", nil, "desc", nil, "superseded", 1, 3, now, nil, nil, nil, nil, "permanent", "newer_build_attempt_exists", nil, nil, now, now, now,
	))
	supersededResult, supersededErr := repo.MarkSuperseded(context.Background(), repository.SCMStatusDeliveryMarkSupersededInput{DeliveryID: "delivery-4", ClaimOwner: &claimOwner, ClaimedAt: &now, SupersededAt: now, Reason: "newer_build_attempt_exists"})
	if supersededErr != nil || supersededResult.Delivery.Status != domain.SCMStatusDeliveryStatusSuperseded {
		t.Fatalf("unexpected superseded result: %+v err=%v", supersededResult, supersededErr)
	}

	mock.ExpectQuery(`SELECT .* FROM scm_status_deliveries WHERE provider = \$1 AND repository_owner = \$2 AND repository_name = \$3 AND commit_sha = \$4 AND context_name = \$5`).WithArgs("github", "octo", "repo", "missing", "ctx").WillReturnError(sql.ErrNoRows)
	if _, getErr := repo.GetByKey(context.Background(), "github", "octo", "repo", "missing", "ctx"); getErr != repository.ErrSCMStatusDeliveryNotFound {
		t.Fatalf("expected not found, got %v", getErr)
	}

	if nullableSCMCommitStatusStateLocal(nil) != nil {
		t.Fatal("expected nil scm state value")
	}
	state := domain.SCMCommitStatusStatePending
	if nullableSCMCommitStatusStateLocal(&state) != "pending" {
		t.Fatal("expected trimmed scm state string")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func scmStatusDeliveryTestColumns() []string {
	return []string{"id", "build_id", "build_attempt_number", "build_created_at", "provider", "repository_owner", "repository_name", "registered_repository_id", "scm_connection_id", "provider_repository_id", "commit_sha", "context_name", "desired_state", "last_sent_state", "description", "details_url", "status", "attempts", "max_attempts", "last_attempt_at", "next_attempt_at", "claimed_at", "claim_expires_at", "claimed_by", "failure_category", "failure_reason", "last_error", "sent_at", "superseded_at", "created_at", "updated_at"}
}

func strPtrPGSCM(value string) *string {
	return &value
}
