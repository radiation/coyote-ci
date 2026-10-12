package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestMoveChannel_LatestDoesNotRegressToOlderBuild(t *testing.T) {
	db, mock, newErr := sqlmock.New()
	if newErr != nil {
		t.Fatalf("new sql mock: %v", newErr)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, current_release_id FROM application_channels").WithArgs("project-1", "job-1", "latest").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_release_id"}).AddRow("channel-1", "release-newer"))
	mock.ExpectQuery("SELECT candidate.build_number > current_build.build_number").WithArgs("build-older", "release-newer").
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(false))
	mock.ExpectCommit()

	tx, beginErr := db.BeginTx(context.Background(), nil)
	if beginErr != nil {
		t.Fatalf("begin transaction: %v", beginErr)
	}
	release := domain.ApplicationRelease{ID: "release-older", BuildID: "build-older", ProjectID: "project-1", JobID: stringPointer("job-1"), CreatedAt: time.Now().UTC()}
	if moveErr := moveChannel(context.Background(), tx, release, "latest", nil, "automatic release completion"); moveErr != nil {
		t.Fatalf("move latest channel: %v", moveErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatalf("commit transaction: %v", commitErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet SQL expectations: %v", expectationsErr)
	}
}

func TestMoveChannel_StablePromotionDoesNotUseBuildOrdering(t *testing.T) {
	db, mock, newErr := sqlmock.New()
	if newErr != nil {
		t.Fatalf("new sql mock: %v", newErr)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, current_release_id FROM application_channels").WithArgs("project-1", "job-1", "stable").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_release_id"}).AddRow("channel-1", "release-newer"))
	mock.ExpectExec("UPDATE application_channels SET current_release_id").WithArgs("channel-1", "release-older").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO application_channel_events").WithArgs(sqlmock.AnyArg(), "channel-1", "release-newer", "release-older", nil, "manual stable promotion").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, beginErr := db.BeginTx(context.Background(), nil)
	if beginErr != nil {
		t.Fatalf("begin transaction: %v", beginErr)
	}
	release := domain.ApplicationRelease{ID: "release-older", BuildID: "build-older", ProjectID: "project-1", JobID: stringPointer("job-1"), CreatedAt: time.Now().UTC()}
	if moveErr := moveChannel(context.Background(), tx, release, "stable", nil, "manual stable promotion"); moveErr != nil {
		t.Fatalf("move stable channel: %v", moveErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatalf("commit transaction: %v", commitErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("unmet SQL expectations: %v", expectationsErr)
	}
}

func stringPointer(value string) *string {
	return &value
}
