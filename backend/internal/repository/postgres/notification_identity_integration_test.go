package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
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

	const workers = 8
	start := make(chan struct{})
	results := make(chan repository.NotificationDeliveryAcquireOutcome, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := repo.Acquire(context.Background(), base)
			if err != nil {
				errs <- err
				return
			}
			results <- result.Outcome
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
	pending := 0
	for outcome := range results {
		switch outcome {
		case repository.NotificationDeliveryAcquireOutcomeCreated:
			created++
		case repository.NotificationDeliveryAcquireOutcomePending:
			pending++
		default:
			t.Fatalf("unexpected acquire outcome %q", outcome)
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly one created delivery, got %d", created)
	}
	if pending != workers-1 {
		t.Fatalf("expected %d pending duplicate outcomes, got %d", workers-1, pending)
	}

	var count int
	countErr := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM notification_deliveries
		WHERE build_id = $1 AND event_type = $2 AND transport = $3 AND destination_key = $4
	`, buildID, string(domain.NotificationEventTypeBuildFailed), string(domain.NotificationTransportEmail), destinationKey).Scan(&count)
	if countErr != nil {
		t.Fatalf("count deliveries failed: %v", countErr)
	}
	if count != 1 {
		t.Fatalf("expected one persisted logical delivery, got %d", count)
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
	path := filepath.Join("../../../db/migrations", fileName)
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read migration %s: %v", fileName, err)
	}
	upSQL := string(content)
	if idx := strings.Index(upSQL, "-- +goose Down"); idx >= 0 {
		upSQL = upSQL[:idx]
	}
	if strings.TrimSpace(upSQL) == "" {
		return
	}
	_, err = conn.ExecContext(ctx, upSQL)
	if err != nil {
		t.Fatalf("apply migration %s: %v", fileName, err)
	}
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
