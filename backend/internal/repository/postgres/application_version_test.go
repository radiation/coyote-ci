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

var errSetApplicationVersionUpdate = errors.New("update failed")

func TestBuildRepository_SetApplicationVersionIfUnset(t *testing.T) {
	tests := []struct {
		name        string
		updateRows  *sqlmock.Rows
		updateErr   error
		lookupRows  *sqlmock.Rows
		lookupErr   error
		wantVersion string
		wantErr     error
	}{
		{
			name:        "sets version",
			updateRows:  applicationVersionBuildRows("build-1", "1.2.3"),
			wantVersion: "1.2.3",
		},
		{
			name:      "returns update error",
			updateErr: errSetApplicationVersionUpdate,
			wantErr:   errSetApplicationVersionUpdate,
		},
		{
			name:       "rejects conflicting persisted version",
			updateErr:  sql.ErrNoRows,
			lookupRows: applicationVersionBuildRows("build-1", "1.2.4"),
			wantErr:    repository.ErrApplicationVersionConflict,
		},
		{
			name:      "maps missing build",
			updateErr: sql.ErrNoRows,
			lookupErr: sql.ErrNoRows,
			wantErr:   repository.ErrBuildNotFound,
		},
		{
			name:       "maps conditional miss with same version to missing build",
			updateErr:  sql.ErrNoRows,
			lookupRows: applicationVersionBuildRows("build-1", "1.2.3"),
			wantErr:    repository.ErrBuildNotFound,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			db, mock, mockErr := sqlmock.New()
			if mockErr != nil {
				t.Fatalf("create sql mock: %v", mockErr)
			}

			update := mock.ExpectQuery("UPDATE builds").WithArgs("build-1", "1.2.3")
			if testCase.updateErr != nil {
				update.WillReturnError(testCase.updateErr)
			} else {
				update.WillReturnRows(testCase.updateRows)
			}
			if testCase.updateErr == sql.ErrNoRows {
				lookup := mock.ExpectQuery("SELECT id, build_number").WithArgs("build-1")
				if testCase.lookupErr != nil {
					lookup.WillReturnError(testCase.lookupErr)
				} else {
					lookup.WillReturnRows(testCase.lookupRows)
				}
			}
			mock.ExpectClose()

			build, setErr := NewBuildRepository(db).SetApplicationVersionIfUnset(context.Background(), " build-1 ", " 1.2.3 ")
			if testCase.wantErr != nil {
				if !errors.Is(setErr, testCase.wantErr) {
					t.Fatalf("set application version error=%v, want %v", setErr, testCase.wantErr)
				}
			} else {
				if setErr != nil {
					t.Fatalf("set application version: %v", setErr)
				}
				if build.ApplicationVersion == nil || *build.ApplicationVersion != testCase.wantVersion {
					t.Fatalf("application version=%v, want %q", build.ApplicationVersion, testCase.wantVersion)
				}
			}
			if closeErr := db.Close(); closeErr != nil {
				t.Fatalf("close sql mock: %v", closeErr)
			}
			if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
				t.Fatalf("unmet sql expectations: %v", expectationsErr)
			}
		})
	}
}

func applicationVersionBuildRows(id string, version string) *sqlmock.Rows {
	now := time.Now().UTC()
	row := make([]driver.Value, len(buildMockColumns))
	row[0] = id
	row[1] = int64(1)
	row[2] = "project-1"
	row[4] = domain.DefaultPriority
	row[5] = string(domain.BuildStatusQueued)
	row[6] = now
	row[10] = 0
	row[11] = 1
	row[15] = version
	row[30] = string(domain.BuildTriggerKindManual)
	row[54] = string(domain.ImageSourceKindExternal)
	return sqlmock.NewRows(buildMockColumns).AddRow(row...)
}
