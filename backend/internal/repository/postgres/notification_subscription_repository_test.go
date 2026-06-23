package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type notificationTestScanner struct {
	values []any
	err    error
}

func (s notificationTestScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan dest mismatch: got %d want %d", len(dest), len(s.values))
	}
	for index := range dest {
		switch typedDest := dest[index].(type) {
		case *string:
			value, ok := s.values[index].(string)
			if !ok {
				return fmt.Errorf("value %d is not string", index)
			}
			*typedDest = value
		case *bool:
			value, ok := s.values[index].(bool)
			if !ok {
				return fmt.Errorf("value %d is not bool", index)
			}
			*typedDest = value
		case *time.Time:
			value, ok := s.values[index].(time.Time)
			if !ok {
				return fmt.Errorf("value %d is not time", index)
			}
			*typedDest = value
		case *sql.NullString:
			if s.values[index] == nil {
				*typedDest = sql.NullString{}
				continue
			}
			value, ok := s.values[index].(string)
			if !ok {
				return fmt.Errorf("value %d is not nullable string", index)
			}
			*typedDest = sql.NullString{String: value, Valid: true}
		default:
			return fmt.Errorf("unsupported dest type %T", dest[index])
		}
	}
	return nil
}

func TestNotificationSubscriptionRepository_TargetCRUDAndErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationSubscriptionRepository(db)
	now := time.Now().UTC()
	targetColumns := []string{"id", "type", "name", "recipient", "enabled", "created_at", "updated_at"}

	mock.ExpectQuery("INSERT INTO notification_targets").WillReturnRows(sqlmock.NewRows(targetColumns).AddRow(
		"target-1", "email", "Dev Mailbox", "<dev@example.com>", true, now, now,
	))
	created, createErr := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		ID:        "target-1",
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "Dev Mailbox",
		Recipient: "<dev@example.com>",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if createErr != nil {
		t.Fatalf("create target failed: %v", createErr)
	}
	if created.ID != "target-1" || created.Type != domain.NotificationTargetTypeEmail {
		t.Fatalf("unexpected created target %+v", created)
	}

	mock.ExpectQuery("INSERT INTO notification_targets").WillReturnError(errors.New("duplicate key value violates unique constraint notification_targets_type_recipient_key"))
	_, duplicateCreateErr := repo.CreateTarget(context.Background(), domain.NotificationTarget{ID: "target-2", Type: domain.NotificationTargetTypeEmail, Recipient: "<dev@example.com>"})
	if !errors.Is(duplicateCreateErr, repository.ErrNotificationTargetDuplicate) {
		t.Fatalf("expected duplicate target error, got %v", duplicateCreateErr)
	}

	mock.ExpectQuery("INSERT INTO notification_targets").WillReturnError(errors.New("insert failed"))
	_, rawCreateErr := repo.CreateTarget(context.Background(), domain.NotificationTarget{ID: "target-3", Type: domain.NotificationTargetTypeEmail, Recipient: "<qa@example.com>"})
	if rawCreateErr == nil || rawCreateErr.Error() != "insert failed" {
		t.Fatalf("expected raw create error, got %v", rawCreateErr)
	}

	mock.ExpectQuery(`SELECT id, type, name, recipient, enabled, created_at, updated_at\s+FROM notification_targets`).WillReturnRows(
		sqlmock.NewRows(targetColumns).
			AddRow("target-1", "email", "Dev Mailbox", "<dev@example.com>", true, now, now).
			AddRow("target-2", "email", "QA", "<qa@example.com>", false, now.Add(time.Minute), now.Add(time.Minute)),
	)
	targets, listErr := repo.ListTargets(context.Background())
	if listErr != nil {
		t.Fatalf("list targets failed: %v", listErr)
	}
	if len(targets) != 2 || targets[1].ID != "target-2" {
		t.Fatalf("unexpected targets %+v", targets)
	}

	mock.ExpectQuery(`SELECT id, type, name, recipient, enabled, created_at, updated_at\s+FROM notification_targets\s+WHERE id = \$1`).WithArgs("target-1").WillReturnRows(
		sqlmock.NewRows(targetColumns).AddRow("target-1", "email", "Dev Mailbox", "<dev@example.com>", true, now, now),
	)
	fetchedTarget, getErr := repo.GetTargetByID(context.Background(), " target-1 ")
	if getErr != nil {
		t.Fatalf("get target failed: %v", getErr)
	}
	if fetchedTarget.ID != "target-1" {
		t.Fatalf("unexpected target id %q", fetchedTarget.ID)
	}

	mock.ExpectQuery(`SELECT id, type, name, recipient, enabled, created_at, updated_at\s+FROM notification_targets\s+WHERE id = \$1`).WithArgs("missing").WillReturnError(sql.ErrNoRows)
	_, missingTargetErr := repo.GetTargetByID(context.Background(), "missing")
	if !errors.Is(missingTargetErr, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected not found target error, got %v", missingTargetErr)
	}

	mock.ExpectQuery("UPDATE notification_targets").WillReturnRows(sqlmock.NewRows(targetColumns).AddRow(
		"target-1", "email", "Dev Team", "<dev@example.com>", false, now, now.Add(time.Hour),
	))
	updatedTarget, updateErr := repo.UpdateTarget(context.Background(), domain.NotificationTarget{
		ID:        " target-1 ",
		Type:      domain.NotificationTargetTypeEmail,
		Name:      "Dev Team",
		Recipient: "<dev@example.com>",
		Enabled:   false,
		UpdatedAt: now.Add(time.Hour),
	})
	if updateErr != nil {
		t.Fatalf("update target failed: %v", updateErr)
	}
	if updatedTarget.Name != "Dev Team" || updatedTarget.Enabled {
		t.Fatalf("unexpected updated target %+v", updatedTarget)
	}

	mock.ExpectQuery("UPDATE notification_targets").WillReturnError(sql.ErrNoRows)
	_, missingUpdateErr := repo.UpdateTarget(context.Background(), domain.NotificationTarget{ID: "missing"})
	if !errors.Is(missingUpdateErr, repository.ErrNotificationTargetNotFound) {
		t.Fatalf("expected missing target update error, got %v", missingUpdateErr)
	}

	mock.ExpectQuery("UPDATE notification_targets").WillReturnError(errors.New("duplicate key value violates unique constraint notification_targets_type_recipient_key"))
	_, duplicateUpdateErr := repo.UpdateTarget(context.Background(), domain.NotificationTarget{ID: "target-1", Type: domain.NotificationTargetTypeEmail})
	if !errors.Is(duplicateUpdateErr, repository.ErrNotificationTargetDuplicate) {
		t.Fatalf("expected duplicate target update error, got %v", duplicateUpdateErr)
	}

	mock.ExpectQuery("UPDATE notification_targets").WillReturnError(errors.New("update failed"))
	_, rawUpdateErr := repo.UpdateTarget(context.Background(), domain.NotificationTarget{ID: "target-1", Type: domain.NotificationTargetTypeEmail})
	if rawUpdateErr == nil || rawUpdateErr.Error() != "update failed" {
		t.Fatalf("expected raw update error, got %v", rawUpdateErr)
	}

	mock.ExpectQuery("INSERT INTO notification_targets").WillReturnRows(sqlmock.NewRows(targetColumns).AddRow(
		"target-slack", "slack_webhook", "Build Alerts", "https://hooks.slack.example/services/T/B/X", true, now, now,
	))
	createdSlackTarget, createSlackErr := repo.CreateTarget(context.Background(), domain.NotificationTarget{
		ID:        "target-slack",
		Type:      domain.NotificationTargetTypeSlackWebhook,
		Name:      "Build Alerts",
		Recipient: "https://hooks.slack.example/services/T/B/X",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if createSlackErr != nil {
		t.Fatalf("create slack target failed: %v", createSlackErr)
	}
	if createdSlackTarget.Type != domain.NotificationTargetTypeSlackWebhook {
		t.Fatalf("expected slack target type, got %+v", createdSlackTarget)
	}

	mock.ExpectExec(`DELETE FROM notification_targets WHERE id = \$1`).WithArgs("target-slack").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.DeleteTarget(context.Background(), "target-slack"); err != nil {
		t.Fatalf("delete target failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestNotificationSubscriptionRepository_SubscriptionCRUDAndErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationSubscriptionRepository(db)
	now := time.Now().UTC()
	subscriptionColumns := []string{"id", "target_id", "project_id", "job_id", "event_type", "enabled", "created_at", "updated_at"}
	projectID := "project-1"
	jobID := "job-1"

	mock.ExpectQuery("INSERT INTO notification_subscriptions").WillReturnRows(sqlmock.NewRows(subscriptionColumns).AddRow(
		"subscription-1", "target-1", projectID, nil, "build_failed", true, now, now,
	))
	createdProjectSub, createProjectErr := repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		ID:        "subscription-1",
		TargetID:  " target-1 ",
		ProjectID: &projectID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if createProjectErr != nil {
		t.Fatalf("create project subscription failed: %v", createProjectErr)
	}
	if createdProjectSub.ProjectID == nil || *createdProjectSub.ProjectID != projectID {
		t.Fatalf("unexpected project subscription %+v", createdProjectSub)
	}

	mock.ExpectQuery("INSERT INTO notification_subscriptions").WillReturnRows(sqlmock.NewRows(subscriptionColumns).AddRow(
		"subscription-2", "target-1", nil, jobID, "build_succeeded", false, now, now,
	))
	createdJobSub, createJobErr := repo.CreateSubscription(context.Background(), domain.NotificationSubscription{
		ID:        "subscription-2",
		TargetID:  "target-1",
		JobID:     &jobID,
		EventType: domain.NotificationEventTypeBuildSucceeded,
		Enabled:   false,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if createJobErr != nil {
		t.Fatalf("create job subscription failed: %v", createJobErr)
	}
	if createdJobSub.JobID == nil || *createdJobSub.JobID != jobID {
		t.Fatalf("unexpected job subscription %+v", createdJobSub)
	}

	mock.ExpectQuery("INSERT INTO notification_subscriptions").WillReturnError(errors.New("duplicate key value violates unique constraint notification_subscriptions_target_event_project_key"))
	_, duplicateCreateErr := repo.CreateSubscription(context.Background(), domain.NotificationSubscription{ID: "subscription-3", TargetID: "target-1", ProjectID: &projectID, EventType: domain.NotificationEventTypeBuildFailed})
	if !errors.Is(duplicateCreateErr, repository.ErrNotificationSubscriptionDuplicate) {
		t.Fatalf("expected duplicate create error, got %v", duplicateCreateErr)
	}

	mock.ExpectQuery("INSERT INTO notification_subscriptions").WillReturnError(errors.New("insert failed"))
	_, rawCreateErr := repo.CreateSubscription(context.Background(), domain.NotificationSubscription{ID: "subscription-4", TargetID: "target-1", ProjectID: &projectID, EventType: domain.NotificationEventTypeBuildFailed})
	if rawCreateErr == nil || rawCreateErr.Error() != "insert failed" {
		t.Fatalf("expected raw create error, got %v", rawCreateErr)
	}

	mock.ExpectQuery(`SELECT id, target_id, project_id::text, job_id::text, event_type, enabled, created_at, updated_at\s+FROM notification_subscriptions`).WillReturnRows(
		sqlmock.NewRows(subscriptionColumns).
			AddRow("subscription-1", "target-1", projectID, nil, "build_failed", true, now, now).
			AddRow("subscription-2", "target-1", nil, jobID, "build_succeeded", false, now.Add(time.Minute), now.Add(time.Minute)),
	)
	allSubscriptions, listAllErr := repo.ListSubscriptions(context.Background(), repository.NotificationSubscriptionListFilter{})
	if listAllErr != nil {
		t.Fatalf("list subscriptions failed: %v", listAllErr)
	}
	if len(allSubscriptions) != 2 {
		t.Fatalf("expected two subscriptions, got %d", len(allSubscriptions))
	}

	mock.ExpectQuery(`SELECT id, target_id, project_id::text, job_id::text, event_type, enabled, created_at, updated_at\s+FROM notification_subscriptions\s+WHERE id = \$1`).WithArgs("subscription-2").WillReturnRows(
		sqlmock.NewRows(subscriptionColumns).AddRow("subscription-2", "target-1", nil, jobID, "build_succeeded", false, now, now),
	)
	fetchedSub, getSubErr := repo.GetSubscriptionByID(context.Background(), " subscription-2 ")
	if getSubErr != nil {
		t.Fatalf("get subscription failed: %v", getSubErr)
	}
	if fetchedSub.JobID == nil || *fetchedSub.JobID != jobID {
		t.Fatalf("unexpected fetched subscription %+v", fetchedSub)
	}

	mock.ExpectQuery(`SELECT id, target_id, project_id::text, job_id::text, event_type, enabled, created_at, updated_at\s+FROM notification_subscriptions\s+WHERE id = \$1`).WithArgs("missing").WillReturnError(sql.ErrNoRows)
	_, missingSubErr := repo.GetSubscriptionByID(context.Background(), "missing")
	if !errors.Is(missingSubErr, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected missing subscription error, got %v", missingSubErr)
	}

	mock.ExpectQuery("UPDATE notification_subscriptions").WillReturnRows(sqlmock.NewRows(subscriptionColumns).AddRow(
		"subscription-2", "target-1", nil, jobID, "build_succeeded", true, now, now.Add(time.Hour),
	))
	updatedSub, updateSubErr := repo.UpdateSubscription(context.Background(), domain.NotificationSubscription{
		ID:        " subscription-2 ",
		TargetID:  " target-1 ",
		JobID:     &jobID,
		EventType: domain.NotificationEventTypeBuildSucceeded,
		Enabled:   true,
		UpdatedAt: now.Add(time.Hour),
	})
	if updateSubErr != nil {
		t.Fatalf("update subscription failed: %v", updateSubErr)
	}
	if !updatedSub.Enabled {
		t.Fatalf("expected updated subscription to be enabled, got %+v", updatedSub)
	}

	mock.ExpectQuery("UPDATE notification_subscriptions").WillReturnError(sql.ErrNoRows)
	_, missingUpdateErr := repo.UpdateSubscription(context.Background(), domain.NotificationSubscription{ID: "missing"})
	if !errors.Is(missingUpdateErr, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected missing subscription update error, got %v", missingUpdateErr)
	}

	mock.ExpectQuery("UPDATE notification_subscriptions").WillReturnError(errors.New("duplicate key value violates unique constraint notification_subscriptions_target_event_job_key"))
	_, duplicateUpdateErr := repo.UpdateSubscription(context.Background(), domain.NotificationSubscription{ID: "subscription-2", TargetID: "target-1"})
	if !errors.Is(duplicateUpdateErr, repository.ErrNotificationSubscriptionDuplicate) {
		t.Fatalf("expected duplicate subscription update error, got %v", duplicateUpdateErr)
	}

	mock.ExpectQuery("UPDATE notification_subscriptions").WillReturnError(errors.New("update failed"))
	_, rawUpdateErr := repo.UpdateSubscription(context.Background(), domain.NotificationSubscription{ID: "subscription-2", TargetID: "target-1"})
	if rawUpdateErr == nil || rawUpdateErr.Error() != "update failed" {
		t.Fatalf("expected raw update error, got %v", rawUpdateErr)
	}

	mock.ExpectExec(`DELETE FROM notification_subscriptions WHERE id = \$1`).WithArgs("subscription-1").WillReturnResult(sqlmock.NewResult(0, 1))
	deleteErr := repo.DeleteSubscription(context.Background(), " subscription-1 ")
	if deleteErr != nil {
		t.Fatalf("delete subscription failed: %v", deleteErr)
	}

	mock.ExpectExec(`DELETE FROM notification_subscriptions WHERE id = \$1`).WithArgs("missing").WillReturnResult(sqlmock.NewResult(0, 0))
	missingDeleteErr := repo.DeleteSubscription(context.Background(), "missing")
	if !errors.Is(missingDeleteErr, repository.ErrNotificationSubscriptionNotFound) {
		t.Fatalf("expected missing subscription delete error, got %v", missingDeleteErr)
	}

	mock.ExpectExec(`DELETE FROM notification_subscriptions WHERE id = \$1`).WithArgs("broken").WillReturnError(errors.New("delete failed"))
	rawDeleteErr := repo.DeleteSubscription(context.Background(), "broken")
	if rawDeleteErr == nil || rawDeleteErr.Error() != "delete failed" {
		t.Fatalf("expected raw delete error, got %v", rawDeleteErr)
	}

	if nullableOptionalString(nil) != nil {
		t.Fatal("expected nil optional string to stay nil")
	}
	blank := "   "
	if nullableOptionalString(&blank) != nil {
		t.Fatal("expected blank optional string to become nil")
	}
	trimmedValue := " value "
	if got := nullableOptionalString(&trimmedValue); got != "value" {
		t.Fatalf("expected trimmed optional string, got %v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestNotificationSubscriptionRepository_ListAndGetErrorBranches(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationSubscriptionRepository(db)
	now := time.Now().UTC()
	targetColumns := []string{"id", "type", "name", "recipient", "enabled", "created_at", "updated_at"}
	subscriptionColumns := []string{"id", "target_id", "project_id", "job_id", "event_type", "enabled", "created_at", "updated_at"}
	projectID := "project-1"

	mock.ExpectQuery(`SELECT id, type, name, recipient, enabled, created_at, updated_at\s+FROM notification_targets`).WillReturnError(errors.New("list targets failed"))
	_, listTargetsErr := repo.ListTargets(context.Background())
	if listTargetsErr == nil || listTargetsErr.Error() != "list targets failed" {
		t.Fatalf("expected list targets error, got %v", listTargetsErr)
	}

	mock.ExpectQuery(`SELECT id, type, name, recipient, enabled, created_at, updated_at\s+FROM notification_targets`).WillReturnRows(
		sqlmock.NewRows(targetColumns).AddRow("target-1", nil, "Dev", "<dev@example.com>", true, now, now),
	)
	_, targetScanErr := repo.ListTargets(context.Background())
	if targetScanErr == nil {
		t.Fatal("expected target scan error")
	}

	mock.ExpectQuery(`SELECT id, type, name, recipient, enabled, created_at, updated_at\s+FROM notification_targets\s+WHERE id = \$1`).WithArgs("broken").WillReturnError(errors.New("get target failed"))
	_, getTargetErr := repo.GetTargetByID(context.Background(), "broken")
	if getTargetErr == nil || getTargetErr.Error() != "get target failed" {
		t.Fatalf("expected raw get target error, got %v", getTargetErr)
	}

	mock.ExpectQuery(`SELECT id, target_id, project_id::text, job_id::text, event_type, enabled, created_at, updated_at\s+FROM notification_subscriptions`).WillReturnError(errors.New("list subscriptions failed"))
	_, listSubscriptionsErr := repo.ListSubscriptions(context.Background(), repository.NotificationSubscriptionListFilter{})
	if listSubscriptionsErr == nil || listSubscriptionsErr.Error() != "list subscriptions failed" {
		t.Fatalf("expected list subscriptions error, got %v", listSubscriptionsErr)
	}

	mock.ExpectQuery(`SELECT id, target_id, project_id::text, job_id::text, event_type, enabled, created_at, updated_at\s+FROM notification_subscriptions`).WillReturnRows(
		sqlmock.NewRows(subscriptionColumns).AddRow("subscription-1", nil, projectID, nil, "build_failed", true, now, now),
	)
	_, subscriptionScanErr := repo.ListSubscriptions(context.Background(), repository.NotificationSubscriptionListFilter{})
	if subscriptionScanErr == nil {
		t.Fatal("expected subscription scan error")
	}

	mock.ExpectQuery(`SELECT id, target_id, project_id::text, job_id::text, event_type, enabled, created_at, updated_at\s+FROM notification_subscriptions\s+WHERE id = \$1`).WithArgs("broken").WillReturnError(errors.New("get subscription failed"))
	_, getSubscriptionErr := repo.GetSubscriptionByID(context.Background(), "broken")
	if getSubscriptionErr == nil || getSubscriptionErr.Error() != "get subscription failed" {
		t.Fatalf("expected raw get subscription error, got %v", getSubscriptionErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestNotificationSubscriptionRepository_ScanHelpersAndListMatchesErrors(t *testing.T) {
	now := time.Now().UTC()
	scannedTarget, targetScanErr := scanNotificationTarget(notificationTestScanner{values: []any{
		"target-1", "email", "Dev", "<dev@example.com>", true, now, now,
	}})
	if targetScanErr != nil {
		t.Fatalf("scan target failed: %v", targetScanErr)
	}
	if scannedTarget.Type != domain.NotificationTargetTypeEmail {
		t.Fatalf("unexpected scanned target %+v", scannedTarget)
	}

	scannedSubscription, subscriptionScanErr := scanNotificationSubscription(notificationTestScanner{values: []any{
		"subscription-1", "target-1", "project-1", nil, "build_failed", true, now, now,
	}})
	if subscriptionScanErr != nil {
		t.Fatalf("scan subscription failed: %v", subscriptionScanErr)
	}
	if scannedSubscription.ProjectID == nil || *scannedSubscription.ProjectID != "project-1" {
		t.Fatalf("unexpected scanned subscription %+v", scannedSubscription)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := NewNotificationSubscriptionRepository(db)

	matchColumns := []string{
		"id", "target_id", "project_id", "job_id", "event_type", "enabled", "created_at", "updated_at",
		"id", "type", "name", "recipient", "enabled", "created_at", "updated_at",
	}
	mock.ExpectQuery("SELECT .* FROM notification_subscriptions s").WillReturnRows(sqlmock.NewRows(matchColumns).AddRow(
		"subscription-1", "target-1", "project-1", nil, "build_failed", true, now, now,
		nil, "email", "Dev", "<dev@example.com>", true, now, now,
	))
	_, matchScanErr := repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ProjectID: "project-1"}, domain.NotificationEventTypeBuildFailed)
	if matchScanErr == nil {
		t.Fatal("expected scan error for nil target id")
	}

	mock.ExpectQuery("SELECT .* FROM notification_subscriptions s").WillReturnRows(sqlmock.NewRows(matchColumns).
		AddRow(
			"subscription-1", "target-1", "project-1", nil, "build_failed", true, now, now,
			"target-1", "email", "Dev", "<dev@example.com>", true, now, now,
		).
		AddRow(
			"subscription-2", "target-2", "project-1", nil, "build_failed", true, now, now,
			"target-2", "email", "Ops", "<ops@example.com>", true, now, now,
		).
		RowError(1, errors.New("row failed")))
	_, rowErr := repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ProjectID: "project-1"}, domain.NotificationEventTypeBuildFailed)
	if rowErr == nil || rowErr.Error() != "row failed" {
		t.Fatalf("expected row error, got %v", rowErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestNotificationSubscriptionRepository_ListEnabledMatchesForBuildEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewNotificationSubscriptionRepository(db)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"id", "target_id", "project_id", "job_id", "event_type", "enabled", "created_at", "updated_at",
		"id", "type", "name", "recipient", "enabled", "created_at", "updated_at",
	}).AddRow(
		"subscription-1", "target-1", "project-1", nil, "build_failed", true, now, now,
		"target-1", "email", "Dev Mailbox", "<dev@example.com>", true, now, now,
	)

	jobID := "job-1"
	mock.ExpectQuery("SELECT .* FROM notification_subscriptions s").WillReturnRows(rows)
	matches, err := repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID}, domain.NotificationEventTypeBuildFailed)
	if err != nil {
		t.Fatalf("list matches failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one match, got %d", len(matches))
	}
	if matches[0].Target.Recipient != "<dev@example.com>" {
		t.Fatalf("unexpected recipient %q", matches[0].Target.Recipient)
	}
	if matches[0].Subscription.ProjectID == nil || *matches[0].Subscription.ProjectID != "project-1" {
		t.Fatalf("unexpected project scope %+v", matches[0].Subscription)
	}

	mock.ExpectQuery("SELECT .* FROM notification_subscriptions s").WillReturnError(errors.New("query failed"))
	_, err = repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ProjectID: "project-1"}, domain.NotificationEventTypeBuildFailed)
	if err == nil || err.Error() != "query failed" {
		t.Fatalf("expected raw query error, got %v", err)
	}

	mock.ExpectQuery("SELECT .* FROM notification_subscriptions s").WillReturnRows(sqlmock.NewRows([]string{
		"id", "target_id", "project_id", "job_id", "event_type", "enabled", "created_at", "updated_at",
		"id", "type", "name", "recipient", "enabled", "created_at", "updated_at",
	}))
	matches, err = repo.ListEnabledMatchesForBuildEvent(context.Background(), domain.Build{ProjectID: "project-1"}, domain.NotificationEventTypeBuildSucceeded)
	if err != nil {
		t.Fatalf("empty list failed: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected empty matches, got %d", len(matches))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
