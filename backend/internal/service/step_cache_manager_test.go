package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/runner"
)

type stubCacheStore struct {
	restoredKeys []string
	savedKeys    []string
	hits         map[string]bool
}

func (s *stubCacheStore) Restore(_ context.Context, key string, _ string) (bool, error) {
	s.restoredKeys = append(s.restoredKeys, key)
	return s.hits[key], nil
}

func (s *stubCacheStore) Save(_ context.Context, key string, _ string) error {
	s.savedKeys = append(s.savedKeys, key)
	return nil
}

func TestStepCacheManager_PrepareAndSave(t *testing.T) {
	workspaceRoot := t.TempDir()
	buildID := "build-1"
	buildWorkspace := filepath.Join(workspaceRoot, buildID)
	if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir build workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildWorkspace, "go.mod"), []byte("module example"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildWorkspace, "go.sum"), []byte("sum"), 0o644); err != nil {
		t.Fatalf("write go.sum: %v", err)
	}

	store := &stubCacheStore{hits: map[string]bool{}}
	manager := NewStepCacheManager(store, workspaceRoot)
	svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})
	logManager := NewExecutionLogManager(svc, StepExecutionContext{ExecutionRequest: runner.RunStepRequest{BuildID: buildID, StepID: "step-1", StepName: "test", StepIndex: 0}})

	executionContext := StepExecutionContext{
		Build: domain.Build{ID: buildID, ProjectID: "project-1"},
		Step: &domain.BuildStep{Cache: &domain.StepCacheConfig{
			Scope:    domain.CacheScopeJob,
			Paths:    []string{"/go/pkg/mod", "/root/.cache/go-build"},
			KeyFiles: []string{"go.mod", "go.sum"},
		}},
		ExecutionImage: "golang:1.26",
		ExecutionRequest: runner.RunStepRequest{
			BuildID:   buildID,
			JobID:     "job-1",
			StepID:    "step-1",
			StepIndex: 0,
			StepName:  "test",
		},
	}

	prepared, err := manager.Prepare(context.Background(), executionContext, logManager)
	if err != nil {
		t.Fatalf("prepare cache: %v", err)
	}
	if !prepared.Enabled {
		t.Fatal("expected prepared cache to be enabled")
	}
	if len(prepared.Mounts) != 2 {
		t.Fatalf("expected 2 cache mounts, got %d", len(prepared.Mounts))
	}
	if len(store.restoredKeys) != 1 {
		t.Fatalf("expected one restore lookup, got %d", len(store.restoredKeys))
	}

	if err := manager.Save(context.Background(), logManager, prepared, runner.RunStepResult{Status: runner.RunStepStatusSuccess}); err != nil {
		t.Fatalf("save cache: %v", err)
	}
	if len(store.savedKeys) != 1 {
		t.Fatalf("expected one save call, got %d", len(store.savedKeys))
	}
	if store.savedKeys[0] != store.restoredKeys[0] {
		t.Fatalf("expected restore/save keys to match, restore=%q save=%q", store.restoredKeys[0], store.savedKeys[0])
	}
}

func TestStepCacheManager_SaveSkippedOnFailedStep(t *testing.T) {
	workspaceRoot := t.TempDir()
	store := &stubCacheStore{hits: map[string]bool{}}
	manager := NewStepCacheManager(store, workspaceRoot)
	svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})
	logManager := NewExecutionLogManager(svc, StepExecutionContext{ExecutionRequest: runner.RunStepRequest{BuildID: "build-1", StepName: "test"}})

	err := manager.Save(context.Background(), logManager, preparedStepCache{Enabled: true, Key: "v1/job/key", RuntimeDir: workspaceRoot}, runner.RunStepResult{Status: runner.RunStepStatusFailed})
	if err != nil {
		t.Fatalf("save should be skipped without error, got %v", err)
	}
	if len(store.savedKeys) != 0 {
		t.Fatalf("expected no cache save calls for failed step, got %d", len(store.savedKeys))
	}

	sink, ok := svc.logSink.(*fakeLogSink)
	if !ok {
		t.Fatalf("expected fakeLogSink, got %T", svc.logSink)
	}

	foundSkipLine := false
	for _, line := range sink.lines {
		if strings.Contains(line, "cache save skipped: step not successful") {
			foundSkipLine = true
			break
		}
	}
	if !foundSkipLine {
		t.Fatalf("expected skip log line, got %#v", sink.lines)
	}
}

func TestStepCacheManager_ScopeAffectsKeySpace(t *testing.T) {
	workspaceRoot := t.TempDir()
	for _, buildID := range []string{"build-a", "build-b"} {
		buildWorkspace := filepath.Join(workspaceRoot, buildID)
		if err := os.MkdirAll(buildWorkspace, 0o755); err != nil {
			t.Fatalf("mkdir build workspace: %v", err)
		}
		if err := os.WriteFile(filepath.Join(buildWorkspace, "go.mod"), []byte("module example"), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		if err := os.WriteFile(filepath.Join(buildWorkspace, "go.sum"), []byte("sum"), 0o644); err != nil {
			t.Fatalf("write go.sum: %v", err)
		}
	}

	store := &stubCacheStore{hits: map[string]bool{}}
	manager := NewStepCacheManager(store, workspaceRoot)
	svc := NewBuildService(&fakeBuildRepository{}, &fakeRunner{}, &fakeLogSink{})

	prepareFor := func(buildID string, scope domain.CacheScope) string {
		logManager := NewExecutionLogManager(svc, StepExecutionContext{ExecutionRequest: runner.RunStepRequest{BuildID: buildID, StepID: "step-1", StepName: "test", StepIndex: 0}})
		prepared, err := manager.Prepare(context.Background(), StepExecutionContext{
			Build: domain.Build{ID: buildID, ProjectID: "project-1", JobID: strPtrCache("job-1")},
			Step: &domain.BuildStep{Cache: &domain.StepCacheConfig{
				Scope:    scope,
				Paths:    []string{"/go/pkg/mod"},
				KeyFiles: []string{"go.mod", "go.sum"},
			}},
			ExecutionImage: "golang:1.26",
			ExecutionRequest: runner.RunStepRequest{
				BuildID: buildID,
				JobID:   "job-1",
				StepID:  "step-1",
			},
		}, logManager)
		if err != nil {
			t.Fatalf("prepare cache for %s: %v", buildID, err)
		}
		return prepared.Key
	}

	jobKeyA := prepareFor("build-a", domain.CacheScopeJob)
	jobKeyB := prepareFor("build-b", domain.CacheScopeJob)
	if jobKeyA != jobKeyB {
		t.Fatalf("expected job-scope keys to match across builds: %q vs %q", jobKeyA, jobKeyB)
	}

	buildKeyA := prepareFor("build-a", domain.CacheScopeBuild)
	buildKeyB := prepareFor("build-b", domain.CacheScopeBuild)
	if buildKeyA == buildKeyB {
		t.Fatalf("expected build-scope keys to differ across builds, both=%q", buildKeyA)
	}

	differentJobKey := func(jobID string) string {
		logManager := NewExecutionLogManager(svc, StepExecutionContext{ExecutionRequest: runner.RunStepRequest{BuildID: "build-c", StepID: "step-1", StepName: "test", StepIndex: 0}})
		prepared, err := manager.Prepare(context.Background(), StepExecutionContext{
			Build: domain.Build{ID: "build-c", ProjectID: "project-1", JobID: strPtrCache(jobID)},
			Step: &domain.BuildStep{Cache: &domain.StepCacheConfig{
				Scope:    domain.CacheScopeJob,
				Paths:    []string{"/go/pkg/mod"},
				KeyFiles: []string{"go.mod", "go.sum"},
			}},
			ExecutionImage: "golang:1.26",
			ExecutionRequest: runner.RunStepRequest{
				BuildID: "build-c",
				JobID:   jobID,
				StepID:  "step-1",
			},
		}, logManager)
		if err != nil {
			t.Fatalf("prepare cache for job %s: %v", jobID, err)
		}
		return prepared.Key
	}

	if differentJobKey("job-1") == differentJobKey("job-2") {
		t.Fatal("expected different jobs to produce different job-scope cache keys")
	}
}

func strPtrCache(value string) *string {
	return &value
}
