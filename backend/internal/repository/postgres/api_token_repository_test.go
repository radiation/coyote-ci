package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var apiTokenMockColumns = []string{"id", "user_id", "name", "token_hash", "token_prefix", "expires_at", "last_used_at", "revoked_at", "created_at", "updated_at"}

func TestNewAPITokenRepository(t *testing.T) {
	repo := NewAPITokenRepository(&sql.DB{})
	if repo == nil {
		t.Fatal("expected repository, got nil")
		return
	}
	if repo.db == nil {
		t.Fatal("expected db to be set")
	}
}

func TestAPITokenRepository_Create(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	lastUsedAt := now.Add(2 * time.Hour)
	revokedAt := now.Add(3 * time.Hour)
	insertErr := errors.New("insert failed")

	tests := []struct {
		name      string
		queryErr  error
		expectErr error
	}{
		{name: "success"},
		{
			name:      "missing user",
			queryErr:  &pgconn.PgError{ConstraintName: "api_tokens_user_id_fkey"},
			expectErr: repository.ErrUserNotFound,
		},
		{
			name:      "query error",
			queryErr:  insertErr,
			expectErr: insertErr,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			repo := NewAPITokenRepository(db)
			token := domain.APIToken{
				ID:          "token-1",
				UserID:      "user-1",
				Name:        "cli",
				TokenHash:   "hash-1",
				TokenPrefix: "coyote_pat_12345678",
				ExpiresAt:   &expiresAt,
				LastUsedAt:  &lastUsedAt,
				RevokedAt:   &revokedAt,
				CreatedAt:   now,
				UpdatedAt:   now,
			}

			expect := mock.ExpectQuery("INSERT INTO api_tokens").
				WithArgs(token.ID, token.UserID, token.Name, token.TokenHash, token.TokenPrefix, token.ExpiresAt, token.LastUsedAt, token.RevokedAt, token.CreatedAt, token.UpdatedAt)
			if tc.queryErr != nil {
				expect.WillReturnError(tc.queryErr)
			} else {
				expect.WillReturnRows(apiTokenRows().AddRow(token.ID, token.UserID, token.Name, token.TokenHash, token.TokenPrefix, expiresAt, lastUsedAt, revokedAt, now, now))
			}

			created, err := repo.Create(context.Background(), token)
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			} else if created.ID != token.ID || created.ExpiresAt == nil || created.LastUsedAt == nil || created.RevokedAt == nil {
				t.Fatalf("unexpected created token: %+v", created)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestAPITokenRepository_ListByUserID(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	repo := NewAPITokenRepository(db)
	mock.ExpectQuery("SELECT id, user_id, name, token_hash, token_prefix").
		WithArgs("user-1").
		WillReturnRows(apiTokenRows().
			AddRow("token-1", "user-1", "cli", "hash-1", "coyote_pat_12345678", expiresAt, nil, nil, now, now).
			AddRow("token-2", "user-1", "release", "hash-2", "coyote_pat_87654321", nil, nil, nil, now.Add(time.Minute), now.Add(time.Minute)))

	tokens, err := repo.ListByUserID(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("ListByUserID failed: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].ExpiresAt == nil || tokens[1].ExpiresAt != nil {
		t.Fatalf("unexpected optional times: %+v", tokens)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestAPITokenRepository_ListByUserIDErrors(t *testing.T) {
	queryErr := errors.New("query failed")
	rowsErr := errors.New("rows failed")
	tests := []struct {
		name     string
		rows     *sqlmock.Rows
		queryErr error
	}{
		{name: "query error", queryErr: queryErr},
		{name: "scan error", rows: sqlmock.NewRows([]string{"id"}).AddRow("token-1")},
		{name: "rows error", rows: apiTokenRows().AddRow("token-1", "user-1", "cli", "hash-1", "prefix", nil, nil, nil, time.Now().UTC(), time.Now().UTC()).RowError(0, rowsErr)},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			repo := NewAPITokenRepository(db)
			expect := mock.ExpectQuery("SELECT id, user_id, name, token_hash, token_prefix").WithArgs("user-1")
			if tc.queryErr != nil {
				expect.WillReturnError(tc.queryErr)
			} else {
				expect.WillReturnRows(tc.rows)
			}

			if _, err := repo.ListByUserID(context.Background(), "user-1"); err == nil {
				t.Fatal("expected error, got nil")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestAPITokenRepository_GetByHash(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	queryErr := errors.New("query failed")
	tests := []struct {
		name      string
		queryErr  error
		expectErr error
	}{
		{name: "success"},
		{name: "not found", queryErr: sql.ErrNoRows, expectErr: repository.ErrAPITokenNotFound},
		{name: "query error", queryErr: queryErr, expectErr: queryErr},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			repo := NewAPITokenRepository(db)
			expect := mock.ExpectQuery("SELECT id, user_id, name, token_hash, token_prefix").WithArgs("hash-1")
			if tc.queryErr != nil {
				expect.WillReturnError(tc.queryErr)
			} else {
				expect.WillReturnRows(apiTokenRows().AddRow("token-1", "user-1", "cli", "hash-1", "prefix", nil, nil, nil, now, now))
			}

			token, err := repo.GetByHash(context.Background(), "hash-1")
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			} else if token.ID != "token-1" {
				t.Fatalf("expected token-1, got %q", token.ID)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestAPITokenRepository_RevokeByID(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	updateErr := errors.New("update failed")
	tests := []struct {
		name         string
		execErr      error
		rowsAffected int64
		expectErr    error
	}{
		{name: "success", rowsAffected: 1},
		{name: "exec error", execErr: updateErr, expectErr: updateErr},
		{name: "not found", rowsAffected: 0, expectErr: repository.ErrAPITokenNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			repo := NewAPITokenRepository(db)
			expect := mock.ExpectExec("UPDATE api_tokens").WithArgs("token-1", "user-1", now)
			if tc.execErr != nil {
				expect.WillReturnError(tc.execErr)
			} else {
				expect.WillReturnResult(sqlmock.NewResult(0, tc.rowsAffected))
			}

			err = repo.RevokeByID(context.Background(), "user-1", "token-1", now)
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func TestAPITokenRepository_TouchLastUsed(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	updateErr := errors.New("update failed")
	tests := []struct {
		name         string
		execErr      error
		rowsAffected int64
		expectErr    error
	}{
		{name: "success", rowsAffected: 1},
		{name: "exec error", execErr: updateErr, expectErr: updateErr},
		{name: "not found", rowsAffected: 0, expectErr: repository.ErrAPITokenNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			repo := NewAPITokenRepository(db)
			expect := mock.ExpectExec("UPDATE api_tokens").WithArgs("token-1", now)
			if tc.execErr != nil {
				expect.WillReturnError(tc.execErr)
			} else {
				expect.WillReturnResult(sqlmock.NewResult(0, tc.rowsAffected))
			}

			err = repo.TouchLastUsed(context.Background(), "token-1", now)
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v", tc.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet sql expectations: %v", err)
			}
		})
	}
}

func apiTokenRows() *sqlmock.Rows {
	return sqlmock.NewRows(apiTokenMockColumns)
}
