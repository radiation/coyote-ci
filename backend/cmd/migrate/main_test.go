package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func TestPolicy(t *testing.T) {
	for _, test := range []struct {
		name                           string
		applied                        []int64
		online, maintenance, wantError bool
	}{
		{"fresh", nil, false, false, false},
		{"current retry", []int64{0, 1, 2}, false, false, false},
		{"unknown source", []int64{0, 1}, false, false, true},
		{"online", []int64{0, 1}, true, false, false},
		{"maintenance", []int64{0, 1}, false, true, false},
		{"ahead", []int64{0, 1, 2, 3}, true, true, true},
		{"unknown history", []int64{0, 9}, false, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := policy{target: 2, versions: map[int64]bool{1: true, 2: true}, guard: true, online: test.online, maintenance: test.maintenance}
			if err := p.check(test.applied); (err != nil) != test.wantError {
				t.Fatalf("check = %v, want error %v", err, test.wantError)
			}
		})
	}
}

func TestImageTarget(t *testing.T) {
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	files := fstest.MapFS{"00001_first.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")}}
	for _, expected := range []string{"", "1", "2", "01"} {
		_, providerErr := newProvider(db, files, expected, policy{})
		if (providerErr != nil) != (expected == "2" || expected == "01") {
			t.Fatalf("target %q: %v", expected, providerErr)
		}
	}
}

func TestRunValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_URL_FILE", "")
	t.Setenv("MIGRATION_SCHEMA_TARGET", "")
	t.Setenv("MIGRATION_GUARD_UPGRADE", "")
	for _, args := range [][]string{{"redo"}, {"up", "down"}, {"-timeout=0s"}, {"-bad"}, {"up"}} {
		if err := run(context.Background(), args, &bytes.Buffer{}); err == nil {
			t.Fatalf("expected validation failure for %v", args)
		}
	}
	t.Setenv("DATABASE_URL_FILE", "/nonexistent/coyote-database")
	if err := run(context.Background(), nil, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("file failure = %v", err)
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"-dir=../../db/migrations", "target"}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) == "" {
		t.Fatal("missing bundled target")
	}
}

func migrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("COYOTE_MIGRATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("COYOTE_MIGRATION_TEST_DATABASE_URL is required for disposable PostgreSQL tests")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	if _, createErr := admin.Exec("CREATE DATABASE " + name); createErr != nil {
		t.Fatal(createErr)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec("DROP DATABASE " + name + " WITH (FORCE)")
		_ = admin.Close()
	})
	return db
}

func TestPostgresMigrationLifecycle(t *testing.T) {
	db := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	files := fstest.MapFS{
		"00001_first.sql": {Data: []byte("-- +goose Up\nCREATE TABLE evidence (value integer);\nINSERT INTO evidence VALUES (7);\n-- +goose Down\nDROP TABLE evidence;\n")},
	}
	var workers sync.WaitGroup
	for index := 0; index < 3; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			provider, providerErr := newProvider(db, files, "1", policy{guard: true})
			if providerErr != nil {
				t.Error(providerErr)
				return
			}
			if migrateErr := migrate(ctx, provider, "up", io.Discard); migrateErr != nil {
				t.Error(migrateErr)
			}
		}()
	}
	workers.Wait()
	var count int
	if queryErr := db.QueryRow("SELECT count(*) FROM evidence WHERE value = 7").Scan(&count); queryErr != nil || count != 1 {
		t.Fatalf("count=%d error=%v", count, queryErr)
	}
	files["00002_second.sql"] = &fstest.MapFile{Data: []byte("-- +goose Up\nALTER TABLE evidence ADD COLUMN label text;\n-- +goose Down\nALTER TABLE evidence DROP COLUMN label;\n")}
	blocked, err := newProvider(db, files, "2", policy{guard: true})
	if err != nil {
		t.Fatal(err)
	}
	if migrateErr := migrate(ctx, blocked, "up", io.Discard); migrateErr == nil {
		t.Fatal("unknown-source upgrade should fail")
	}
	approved, err := newProvider(db, files, "2", policy{guard: true, online: true})
	if err != nil {
		t.Fatal(err)
	}
	if migrateErr := migrate(ctx, approved, "up", io.Discard); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	if migrateErr := migrate(ctx, approved, "up", io.Discard); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	oldFiles := fstest.MapFS{"00001_first.sql": files["00001_first.sql"]}
	old, err := newProvider(db, oldFiles, "1", policy{})
	if err != nil {
		t.Fatal(err)
	}
	if migrateErr := migrate(ctx, old, "up", io.Discard); migrateErr == nil {
		t.Fatal("ahead database should fail")
	}
	if migrateErr := migrate(ctx, approved, "down", io.Discard); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	if migrateErr := migrate(ctx, approved, "up", io.Discard); migrateErr != nil {
		t.Fatal(migrateErr)
	}
}

func TestPostgresFailureRetry(t *testing.T) {
	db := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	files := fstest.MapFS{
		"00001_first.sql":  {Data: []byte("-- +goose Up\nCREATE TABLE ready (value integer);\n")},
		"00002_second.sql": {Data: []byte("-- +goose Up\nCREATE TABLE transactional_evidence (value integer);\nSELECT 1 / count(*) FROM ready;\n")},
	}
	provider, err := newProvider(db, files, "2", policy{})
	if err != nil {
		t.Fatal(err)
	}
	if migrateErr := migrate(ctx, provider, "up", io.Discard); migrateErr == nil {
		t.Fatal("migration should fail")
	}
	var exists bool
	if queryErr := db.QueryRow("SELECT to_regclass('transactional_evidence') IS NOT NULL").Scan(&exists); queryErr != nil || exists {
		t.Fatalf("transaction not rolled back: %v %v", exists, queryErr)
	}
	if _, insertErr := db.Exec("INSERT INTO ready VALUES (1)"); insertErr != nil {
		t.Fatal(insertErr)
	}
	if migrateErr := migrate(ctx, provider, "up", io.Discard); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	if migrateErr := migrate(ctx, provider, "status", io.Discard); migrateErr != nil {
		t.Fatal(migrateErr)
	}
}

func TestRunPostgresCredentials(t *testing.T) {
	db := migrationDatabase(t)
	var databaseName string
	if queryErr := db.QueryRow("SELECT current_database()").Scan(&databaseName); queryErr != nil {
		t.Fatal(queryErr)
	}
	parsed, err := url.Parse(os.Getenv("COYOTE_MIGRATION_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	if _, createErr := db.Exec("CREATE SCHEMA alternate"); createErr != nil {
		t.Fatal(createErr)
	}
	parameters := parsed.Query()
	parameters.Set("search_path", "alternate")
	parsed.RawQuery = parameters.Encode()
	directory := t.TempDir()
	credential := filepath.Join(directory, "database-url")
	if writeErr := os.WriteFile(credential, []byte(parsed.String()+"\n"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if writeErr := os.WriteFile(filepath.Join(directory, "00001_init.sql"), []byte("-- +goose Up\nCREATE TABLE evidence (id integer);\n"), 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("DATABASE_URL", "invalid://must-not-be-used")
	t.Setenv("DATABASE_URL_FILE", credential)
	t.Setenv("MIGRATION_GUARD_UPGRADE", "true")
	t.Setenv("MIGRATION_SCHEMA_TARGET", "1")
	t.Setenv("MIGRATION_ONLINE_APPROVED", "false")
	t.Setenv("MIGRATION_MAINTENANCE_ACKNOWLEDGED", "false")
	if runErr := run(context.Background(), []string{"-dir=" + directory}, io.Discard); runErr != nil {
		t.Fatal(runErr)
	}
	var publicOnly bool
	if queryErr := db.QueryRow("SELECT to_regclass('public.evidence') IS NOT NULL AND to_regclass('alternate.evidence') IS NULL").Scan(&publicOnly); queryErr != nil || !publicOnly {
		t.Fatalf("migrations must use public: %v", queryErr)
	}
	if runErr := run(context.Background(), []string{"-dir=" + directory, "down"}, io.Discard); runErr == nil {
		t.Fatal("Helm must reject down")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if runErr := run(ctx, []string{"-dir=" + directory}, io.Discard); runErr == nil {
		t.Fatal("canceled migration must fail")
	}
	if runErr := run(context.Background(), []string{"-dir=" + directory}, io.Discard); runErr != nil {
		t.Fatal(runErr)
	}
	t.Setenv("MIGRATION_SCHEMA_TARGET", "")
	if runErr := run(context.Background(), []string{"-dir=" + directory}, io.Discard); runErr == nil {
		t.Fatal("Helm must require a target")
	}
	t.Setenv("DATABASE_URL_FILE", "")
	t.Setenv("DATABASE_URL", "postgres://postgres:do-not-print@127.0.0.1:1/postgres")
	t.Setenv("MIGRATION_GUARD_UPGRADE", "false")
	if runErr := run(context.Background(), []string{"-dir=" + directory, "-timeout=100ms"}, io.Discard); runErr == nil || strings.Contains(runErr.Error(), "do-not-print") {
		t.Fatalf("unsafe database error: %v", runErr)
	}
}
