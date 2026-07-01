package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestSlackWorkspaceIntegrationRepository_GetAndMutations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSlackWorkspaceIntegrationRepository(db)
	now := time.Date(2026, 7, 1, 12, 5, 0, 0, time.UTC)
	columns := []string{"id", "workspace_id", "workspace_name", "workspace_url", "bot_id", "authed_user_id", "app_id", "bot_token_secret", "enabled", "connected_at", "last_tested_at", "last_test_succeeded", "created_at", "updated_at", "linked_identity_count"}

	mock.ExpectQuery("SELECT id::text, workspace_id").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-secret", true, now, now, true, now, now, 0))

	integration, getErr := repo.Get(context.Background())
	if getErr != nil {
		t.Fatalf("get integration: %v", getErr)
	}
	if integration.WorkspaceID != "T123" {
		t.Fatalf("unexpected workspace id %q", integration.WorkspaceID)
	}

	mock.ExpectQuery("SELECT id::text, workspace_id").WillReturnError(sql.ErrNoRows)
	_, getMissingErr := repo.Get(context.Background())
	if !errors.Is(getMissingErr, repository.ErrSlackWorkspaceIntegrationNotFound) {
		t.Fatalf("expected not found, got %v", getMissingErr)
	}

	updatedAt := now.Add(time.Minute)
	mock.ExpectQuery("UPDATE slack_workspace_integrations").
		WithArgs(false, updatedAt).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-secret", false, now, now, true, now, updatedAt, 0))

	updated, setErr := repo.SetEnabled(context.Background(), false, updatedAt)
	if setErr != nil {
		t.Fatalf("set enabled: %v", setErr)
	}
	if updated.Enabled {
		t.Fatalf("expected disabled integration")
	}

	testedAt := now.Add(2 * time.Minute)
	mock.ExpectQuery("UPDATE slack_workspace_integrations").
		WithArgs(testedAt, true).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-secret", false, now, testedAt, true, now, testedAt, 0))

	tested, testErr := repo.UpdateLastTestResult(context.Background(), testedAt, true)
	if testErr != nil {
		t.Fatalf("update test result: %v", testErr)
	}
	if tested.LastTestSucceeded == nil || !*tested.LastTestSucceeded {
		t.Fatalf("expected successful test state")
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, workspace_id").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-secret", false, now, testedAt, true, now, testedAt, 0))
	mock.ExpectExec("DELETE FROM slack_workspace_integrations").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if deleteErr := repo.Delete(context.Background()); deleteErr != nil {
		t.Fatalf("delete integration: %v", deleteErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSlackWorkspaceIntegrationRepository_ConnectOrReplace(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := NewSlackWorkspaceIntegrationRepository(db)
	now := time.Date(2026, 7, 1, 12, 10, 0, 0, time.UTC)
	columns := []string{"id", "workspace_id", "workspace_name", "workspace_url", "bot_id", "authed_user_id", "app_id", "bot_token_secret", "enabled", "connected_at", "last_tested_at", "last_test_succeeded", "created_at", "updated_at", "linked_identity_count"}

	integration := domain.SlackWorkspaceIntegration{
		ID:             "int-1",
		WorkspaceID:    "T123",
		WorkspaceName:  strPtr("Coyote"),
		WorkspaceURL:   strPtr("https://example.slack.com/"),
		BotID:          strPtr("B1"),
		AuthedUserID:   strPtr("U1"),
		AppID:          strPtr("A1"),
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		LastTestedAt:   &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, workspace_id").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO slack_workspace_integrations").
		WithArgs("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-secret", true, now, now, nil, now, now).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-secret", true, now, now, nil, now, now, 0))
	mock.ExpectCommit()

	stored, connectErr := repo.ConnectOrReplace(context.Background(), integration, false)
	if connectErr != nil {
		t.Fatalf("connect integration: %v", connectErr)
	}
	if stored.WorkspaceID != "T123" {
		t.Fatalf("unexpected workspace id %q", stored.WorkspaceID)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, workspace_id").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-old", false, now, now, true, now, now, 0))
	mock.ExpectQuery("UPDATE slack_workspace_integrations").
		WithArgs("T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-secret", false, now, now, nil, now).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-secret", false, now, now, nil, now, now, 0))
	mock.ExpectCommit()

	rotated, rotateErr := repo.ConnectOrReplace(context.Background(), integration, false)
	if rotateErr != nil {
		t.Fatalf("rotate integration: %v", rotateErr)
	}
	if rotated.Enabled {
		t.Fatalf("expected existing enabled state to be preserved as false")
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, workspace_id").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-old", true, now, now, true, now, now, 0))
	mock.ExpectRollback()

	_, replaceErr := repo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "int-2",
		WorkspaceID:    "T999",
		BotTokenSecret: "xoxb-new",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, false)
	if !errors.Is(replaceErr, repository.ErrSlackWorkspaceIntegrationReplaceRequired) {
		t.Fatalf("expected replace required error, got %v", replaceErr)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id::text, workspace_id").
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T123", "Coyote", "https://example.slack.com/", "B1", "U1", "A1", "xoxb-old", true, now, now, true, now, now, 0))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM user_slack_identities WHERE slack_workspace_integration_id = \$1`).
		WithArgs("int-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("UPDATE slack_workspace_integrations").
		WithArgs("T999", nil, nil, nil, nil, nil, "xoxb-new", true, now, nil, nil, now).
		WillReturnRows(sqlmock.NewRows(columns).AddRow("int-1", "T999", nil, nil, nil, nil, nil, "xoxb-new", true, now, nil, nil, now, now, 0))
	mock.ExpectCommit()

	replaced, allowReplaceErr := repo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "int-2",
		WorkspaceID:    "T999",
		BotTokenSecret: "xoxb-new",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, true)
	if allowReplaceErr != nil {
		t.Fatalf("replace integration: %v", allowReplaceErr)
	}
	if replaced.WorkspaceID != "T999" {
		t.Fatalf("expected replaced workspace id, got %q", replaced.WorkspaceID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func strPtr(value string) *string {
	v := value
	return &v
}
