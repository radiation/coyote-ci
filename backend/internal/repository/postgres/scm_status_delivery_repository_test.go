package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

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
		"delivery-1", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "pending", nil, "Coyote build is in progress", "https://coyote.example/builds/build-1", "sending", 1, 3, now, nil, now, claimExpiresAt, "worker-a", nil, nil, nil, nil, nil, now, now,
	))
	mock.ExpectCommit()

	claimed, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{
		Delivery: domain.SCMStatusDelivery{
			BuildID:         "build-1",
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
		"delivery-1", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "pending", "pending", "Coyote build is in progress", "https://coyote.example/builds/build-1", "sent", 1, 3, now, nil, nil, nil, nil, nil, nil, nil, now, nil, now, now,
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
			"delivery-1", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "pending", "pending", "Coyote build is in progress", nil, "sent", 1, 3, now, nil, nil, nil, nil, nil, nil, nil, now, nil, now, now,
		))

		result, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{
			Delivery:      domain.SCMStatusDelivery{BuildID: "build-1", Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "abcdef", Context: "coyote/default/job-1", DesiredState: domain.SCMCommitStatusStatePending, Description: "Coyote build is in progress"},
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
			"delivery-2", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "retry_waiting", 1, 3, now.Add(-time.Minute), nextAttemptAt, nil, nil, nil, "retryable", "github_api_unavailable", "api unavailable", nil, nil, now, now,
		))
		mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
			"delivery-2", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "sending", 2, 3, now, nil, claimedAt, claimExpiresAt, "worker-b", "retryable", "github_api_unavailable", "api unavailable", nil, nil, now, now,
		))
		mock.ExpectCommit()

		result, claimErr := repo.AcquireForDelivery(context.Background(), repository.SCMStatusDeliveryClaimInput{
			Delivery:      domain.SCMStatusDelivery{BuildID: "build-1", Provider: "github", RepositoryOwner: "octo", RepositoryName: "repo", CommitSHA: "abcdef", Context: "coyote/default/job-1", DesiredState: domain.SCMCommitStatusStateFailure, Description: "Coyote build failed"},
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

func TestSCMStatusDeliveryRepository_RecordFailureSupersededAndLookup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSCMStatusDeliveryRepository(db)
	now := time.Now().UTC()
	nextAttemptAt := now.Add(30 * time.Second)
	row := scmStatusDeliveryTestColumns()

	mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "retry_waiting", 1, 3, now, nextAttemptAt, nil, nil, nil, "retryable", "github_api_unavailable", "api unavailable", nil, nil, now, now,
	))
	updated, updateErr := repo.RecordRetryableFailure(context.Background(), repository.SCMStatusDeliveryRecordFailureInput{
		DeliveryID:      "delivery-1",
		ClaimOwner:      "worker-a",
		ClaimedAt:       now,
		FailedAt:        now,
		NextAttemptAt:   &nextAttemptAt,
		FailureCategory: domain.SCMStatusDeliveryFailureCategoryRetryable,
		FailureReason:   "github_api_unavailable",
		LastError:       strPtrPGSCM("api unavailable"),
	})
	if updateErr != nil {
		t.Fatalf("record retryable failure failed: %v", updateErr)
	}
	if updated.Delivery.Status != domain.SCMStatusDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry_waiting status, got %q", updated.Delivery.Status)
	}

	mock.ExpectQuery("UPDATE scm_status_deliveries").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-2", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "superseded", 2, 3, now, nil, nil, nil, nil, "permanent", "newer_build_attempt_exists", nil, nil, now, now, now,
	))
	superseded, supersededErr := repo.MarkSuperseded(context.Background(), repository.SCMStatusDeliveryMarkSupersededInput{
		DeliveryID:   "delivery-2",
		SupersededAt: now,
		Reason:       "newer_build_attempt_exists",
	})
	if supersededErr != nil {
		t.Fatalf("mark superseded failed: %v", supersededErr)
	}
	if superseded.Delivery.Status != domain.SCMStatusDeliveryStatusSuperseded {
		t.Fatalf("expected superseded status, got %q", superseded.Delivery.Status)
	}

	mock.ExpectQuery(`SELECT .* FROM scm_status_deliveries WHERE build_id = \$1 AND provider = \$2 AND context_name = \$3 AND desired_state = \$4`).WithArgs("build-1", "github", "coyote/default/job-1", "failure").WillReturnRows(sqlmock.NewRows(row).AddRow(
		"delivery-1", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "retry_waiting", 1, 3, now, nextAttemptAt, nil, nil, nil, "retryable", "github_api_unavailable", "api unavailable", nil, nil, now, now,
	))
	lookup, lookupErr := repo.GetByBuildContextState(context.Background(), "build-1", "github", "coyote/default/job-1", domain.SCMCommitStatusStateFailure)
	if lookupErr != nil {
		t.Fatalf("lookup failed: %v", lookupErr)
	}
	if lookup.ID != "delivery-1" {
		t.Fatalf("expected looked up delivery-1, got %q", lookup.ID)
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
		AddRow("delivery-1", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "retry_waiting", 1, 3, now, now, nil, nil, nil, "retryable", "github_api_unavailable", "api unavailable", nil, nil, now, now).
		AddRow("delivery-2", "build-1", "github", "octo", "repo", "abcdef", "coyote/default/job-1", "failure", nil, "Coyote build failed", nil, "sending", 2, 3, now, nil, now, now, "worker-a", nil, nil, nil, nil, nil, now, now))

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

func scmStatusDeliveryTestColumns() []string {
	return []string{"id", "build_id", "provider", "repository_owner", "repository_name", "commit_sha", "context_name", "desired_state", "last_sent_state", "description", "details_url", "status", "attempts", "max_attempts", "last_attempt_at", "next_attempt_at", "claimed_at", "claim_expires_at", "claimed_by", "failure_category", "failure_reason", "last_error", "sent_at", "superseded_at", "created_at", "updated_at"}
}

func strPtrPGSCM(value string) *string {
	return &value
}
