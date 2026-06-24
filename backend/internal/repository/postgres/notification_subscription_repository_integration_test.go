package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestNotificationSubscriptionRepository_DeleteTargetCascadesSubscriptions_Postgres(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("COYOTE_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set COYOTE_TEST_DATABASE_URL to run Postgres integration tests")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres connection: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("close postgres connection: %v", closeErr)
		}
	}()

	ctx := context.Background()
	now := time.Now().UTC()
	projectID := uuid.NewString()
	targetID := uuid.NewString()
	subscriptionID := uuid.NewString()

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM notification_subscriptions WHERE id = $1`, subscriptionID)
		_, _ = db.ExecContext(ctx, `DELETE FROM notification_targets WHERE id = $1`, targetID)
		_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
	})

	_, err = db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4)
	`, projectID, "Project "+projectID, "project-"+uuid.NewString(), now)
	if err != nil {
		t.Fatalf("insert project %s: %v", projectID, err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO notification_targets (id, type, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, 'email', $2, $3, TRUE, $4, $4)
	`, targetID, "Dev Mailbox", "dev+"+targetID+"@example.com", now)
	if err != nil {
		t.Fatalf("insert notification target %s: %v", targetID, err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO notification_subscriptions (id, target_id, project_id, job_id, event_type, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, 'build_failed', TRUE, $4, $4)
	`, subscriptionID, targetID, projectID, now)
	if err != nil {
		t.Fatalf("insert notification subscription %s: %v", subscriptionID, err)
	}

	repo := NewNotificationSubscriptionRepository(db)
	deleteErr := repo.DeleteTarget(ctx, targetID)
	if deleteErr != nil {
		t.Fatalf("delete target failed: %v", deleteErr)
	}

	var remaining int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_subscriptions WHERE target_id = $1`, targetID).Scan(&remaining)
	if err != nil {
		t.Fatalf("count subscriptions failed: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected cascaded subscriptions to be removed, got %d remaining", remaining)
	}
}

func TestNotificationSubscriptionRepository_CreateSubscriptionDuplicateConflict_Postgres(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("COYOTE_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set COYOTE_TEST_DATABASE_URL to run Postgres integration tests")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open postgres connection: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("close postgres connection: %v", closeErr)
		}
	}()

	ctx := context.Background()
	now := time.Now().UTC()
	projectID := uuid.NewString()
	targetID := uuid.NewString()
	firstSubscriptionID := uuid.NewString()
	duplicateSubscriptionID := uuid.NewString()

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM notification_subscriptions WHERE id = $1`, firstSubscriptionID)
		_, _ = db.ExecContext(ctx, `DELETE FROM notification_subscriptions WHERE id = $1`, duplicateSubscriptionID)
		_, _ = db.ExecContext(ctx, `DELETE FROM notification_targets WHERE id = $1`, targetID)
		_, _ = db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID)
	})

	_, err = db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, description, created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $4)
	`, projectID, "Project "+projectID, "project-"+uuid.NewString(), now)
	if err != nil {
		t.Fatalf("insert project %s: %v", projectID, err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO notification_targets (id, type, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, 'email', $2, $3, TRUE, $4, $4)
	`, targetID, "Build alerts", "dev+"+targetID+"@example.com", now)
	if err != nil {
		t.Fatalf("insert notification target %s: %v", targetID, err)
	}

	repo := NewNotificationSubscriptionRepository(db)
	projectScopeID := projectID

	_, err = repo.CreateSubscription(ctx, domain.NotificationSubscription{
		ID:        firstSubscriptionID,
		TargetID:  targetID,
		ProjectID: &projectScopeID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create first subscription failed: %v", err)
	}

	_, err = repo.CreateSubscription(ctx, domain.NotificationSubscription{
		ID:        duplicateSubscriptionID,
		TargetID:  targetID,
		ProjectID: &projectScopeID,
		EventType: domain.NotificationEventTypeBuildFailed,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if !errors.Is(err, repository.ErrNotificationSubscriptionDuplicate) {
		t.Fatalf("expected duplicate conflict error, got %v", err)
	}

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_subscriptions WHERE target_id = $1`, targetID).Scan(&count)
	if err != nil {
		t.Fatalf("count subscriptions failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one persisted subscription row after duplicate attempt, got %d", count)
	}
}
