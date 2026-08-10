package postgres

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type stubRowScanner struct {
	values []any
}

func (s stubRowScanner) Scan(dest ...any) error {
	for i, target := range dest {
		value := reflect.ValueOf(s.values[i])
		targetValue := reflect.ValueOf(target).Elem()
		if !value.IsValid() {
			targetValue.Set(reflect.Zero(targetValue.Type()))
			continue
		}
		switch targetValue.Interface().(type) {
		case sql.NullString:
			switch source := s.values[i].(type) {
			case sql.NullString:
				targetValue.Set(reflect.ValueOf(source))
			case string:
				targetValue.Set(reflect.ValueOf(sql.NullString{String: source, Valid: true}))
			default:
				targetValue.Set(reflect.Zero(targetValue.Type()))
			}
			continue
		case sql.NullInt64:
			switch source := s.values[i].(type) {
			case sql.NullInt64:
				targetValue.Set(reflect.ValueOf(source))
			case int64:
				targetValue.Set(reflect.ValueOf(sql.NullInt64{Int64: source, Valid: true}))
			default:
				targetValue.Set(reflect.Zero(targetValue.Type()))
			}
			continue
		case sql.NullBool:
			switch source := s.values[i].(type) {
			case sql.NullBool:
				targetValue.Set(reflect.ValueOf(source))
			case bool:
				targetValue.Set(reflect.ValueOf(sql.NullBool{Bool: source, Valid: true}))
			default:
				targetValue.Set(reflect.Zero(targetValue.Type()))
			}
			continue
		case sql.NullTime:
			switch source := s.values[i].(type) {
			case sql.NullTime:
				targetValue.Set(reflect.ValueOf(source))
			case time.Time:
				targetValue.Set(reflect.ValueOf(sql.NullTime{Time: source, Valid: true}))
			default:
				targetValue.Set(reflect.Zero(targetValue.Type()))
			}
			continue
		}
		targetValue.Set(value)
	}
	return nil
}

func TestScanBuild_MapsArtifactTriggerFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	now := time.Now().UTC()
	queuedAt := now.Add(time.Minute)
	startedAt := now.Add(2 * time.Minute)
	finishedAt := now.Add(3 * time.Minute)
	deleted := true

	rows := sqlmock.NewRows(buildMockColumns).AddRow(
		"build-1", int64(42), "project-1", "job-1", 0, "success", now, queuedAt, startedAt, finishedAt, 2, 0, "rerun-1", int64(1), "done",
		nil, nil, nil,
		"version: 1", "pipeline", "repo", ".coyote/pipeline.yml", "https://github.com/example/repo.git", "main", "abc123",
		"Author", "author@example.com", "Committer", "committer@example.com",
		"artifact", "github", "release", "example", "repo", "https://github.com/example/repo", "refs/tags/v1.0.0", "v1.0.0", "tag", "v1.0.0", deleted, "def456", "delivery-1", "octocat",
		"project-upstream", "job-upstream", "build-upstream", "artifact-1", "dist/app.tgz", "app.tgz", int64(77), "sha256:abc",
		"golang:1.24", "registry.example.com/build@sha256:123", "managed", "managed-image-1", "managed-version-1",
		int64(42), "synchronize", "https://github.example.com/acme/repo/pull/42", "main", "base-sha", "feature/pr-42", "head-sha", "head",
	)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	row := db.QueryRow("SELECT")
	build, err := scanBuild(row)
	if err != nil {
		t.Fatalf("scanBuild failed: %v", err)
	}
	if build.AttemptNumber != 1 || build.Priority != domain.DefaultPriority {
		t.Fatalf("expected normalized attempt and priority, got attempt=%d priority=%d", build.AttemptNumber, build.Priority)
	}
	if build.Trigger.Kind != domain.BuildTriggerKindArtifact {
		t.Fatalf("expected artifact trigger kind, got %q", build.Trigger.Kind)
	}
	if build.Trigger.ArtifactID == nil || *build.Trigger.ArtifactID != "artifact-1" {
		t.Fatalf("expected artifact id, got %#v", build.Trigger.ArtifactID)
	}
	if build.Trigger.ArtifactSizeBytes == nil || *build.Trigger.ArtifactSizeBytes != 77 {
		t.Fatalf("expected artifact size bytes, got %#v", build.Trigger.ArtifactSizeBytes)
	}
	if build.ManagedImageVersionID == nil || *build.ManagedImageVersionID != "managed-version-1" {
		t.Fatalf("expected managed image version, got %#v", build.ManagedImageVersionID)
	}
	if build.Source == nil || build.Source.RepositoryURL != "https://github.com/example/repo.git" {
		t.Fatalf("expected normalized source spec, got %#v", build.Source)
	}
	if build.Trigger.PullRequest == nil || build.Trigger.PullRequest.Number != 42 || build.Trigger.PullRequest.HeadSHA != "head-sha" {
		t.Fatalf("expected full build snapshot scan, got %+v", build.Trigger.PullRequest)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestScanBuildList_DefaultsExternalImageSourceAndMapsArtifactTriggerFields(t *testing.T) {
	now := time.Now().UTC()
	build, err := scanBuildList(stubRowScanner{values: []any{
		"build-2", int64(7), "project-1", sql.NullString{}, 5, "queued", now, sql.NullTime{}, sql.NullTime{}, sql.NullTime{}, 0, 1, sql.NullString{}, sql.NullInt64{}, sql.NullString{},
		"pipeline", "repo", ".coyote/pipeline.yml", "https://github.com/example/repo.git", "main", "abc123",
		sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
		"artifact", sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullBool{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
		"project-upstream", "job-upstream", "build-upstream", "artifact-2", "dist/lib.tgz", "lib.tgz", sql.NullInt64{Int64: 88, Valid: true}, "sha256:def",
		sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
		int64(42), "opened", "https://github.example.com/acme/repo/pull/42", "main", "base-sha", "feature/pr-42", "head-sha", "head",
	}})
	if err != nil {
		t.Fatalf("scanBuildList failed: %v", err)
	}
	if build.ImageSourceKind != domain.ImageSourceKindExternal {
		t.Fatalf("expected default external image source, got %q", build.ImageSourceKind)
	}
	if build.Trigger.ProducerBuildID == nil || *build.Trigger.ProducerBuildID != "build-upstream" {
		t.Fatalf("expected producer build id, got %#v", build.Trigger.ProducerBuildID)
	}
	if build.Trigger.ArtifactChecksumSHA256 == nil || *build.Trigger.ArtifactChecksumSHA256 != "sha256:def" {
		t.Fatalf("expected artifact checksum, got %#v", build.Trigger.ArtifactChecksumSHA256)
	}
	if build.Source == nil || build.Source.Ref == nil || *build.Source.Ref != "main" {
		t.Fatalf("expected normalized source ref, got %#v", build.Source)
	}
	if build.Trigger.PullRequest == nil || build.Trigger.PullRequest.Number != 42 || build.Trigger.PullRequest.Action != "opened" {
		t.Fatalf("expected list build snapshot scan, got %+v", build.Trigger.PullRequest)
	}
}
