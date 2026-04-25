package handler

// BuildHandler endpoint-focused tests moved from build_handler_test.go:
// - pipeline/repo create endpoints
// - artifact list/download endpoints

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

// Build creation endpoints beyond the base /builds handler coverage.
func TestCreatePipelineBuild(t *testing.T) {
	t.Run("valid pipeline creates queued build", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		h := NewBuildHandler(svc)

		body := `{
			"project_id": "proj-1",
			"pipeline_yaml": "version: 1\nsteps:\n  - name: Lint\n    run: golangci-lint run\n  - name: Test\n    run: go test ./...\n"
		}`

		req := httptest.NewRequest(http.MethodPost, "/builds/pipeline", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.CreatePipelineBuild(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		data := resp["data"]
		if data["status"] != "queued" {
			t.Errorf("expected queued, got %v", data["status"])
		}
		buildID, ok := data["id"].(string)
		if !ok {
			t.Fatal("expected string build id in response")
		}
		steps := repo.steps[buildID]
		if len(steps) != 2 {
			t.Fatalf("expected 2 steps, got %d", len(steps))
		}
		if steps[0].Name != "Lint" {
			t.Errorf("step 0 name: got %q", steps[0].Name)
		}
	})

	t.Run("missing project_id returns 400", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		h := NewBuildHandler(svc)

		body := `{"pipeline_yaml": "version: 1\nsteps:\n  - name: X\n    run: echo\n"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/pipeline", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.CreatePipelineBuild(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid YAML returns 400", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		h := NewBuildHandler(svc)

		body := `{"project_id": "proj-1", "pipeline_yaml": "version: 2\nsteps:\n  - name: X\n    run: echo\n"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/pipeline", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.CreatePipelineBuild(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// handlerFakeRepoFetcher implements source.RepoFetcher for handler tests.
type handlerFakeRepoFetcher struct {
	localPath string
	commitSHA string
	err       error
}

func (f *handlerFakeRepoFetcher) Fetch(_ context.Context, _ string, _ string) (string, string, error) {
	if f.err != nil {
		return "", "", f.err
	}
	return f.localPath, f.commitSHA, nil
}

func TestCreateRepoBuild(t *testing.T) {
	t.Run("valid repo build", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(tmpDir+"/.coyote", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmpDir+"/.coyote/pipeline.yml", []byte("version: 1\nsteps:\n  - name: test\n    run: echo ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		svc.SetRepoFetcher(&handlerFakeRepoFetcher{localPath: tmpDir, commitSHA: "abc123"})
		h := NewBuildHandler(svc)

		body := `{"project_id":"proj-1","repo_url":"https://github.com/org/repo.git","ref":"main"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/repo", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.CreateRepoBuild(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		data := resp["data"]
		if data["status"] != "queued" {
			t.Errorf("expected queued, got %v", data["status"])
		}
		source, ok := data["source"].(map[string]interface{})
		if !ok || source == nil {
			t.Fatal("expected source object in response")
		}
		if source["repository_url"] != "https://github.com/org/repo.git" {
			t.Errorf("expected source.repository_url in response, got %v", source["repository_url"])
		}
		if source["ref"] != "main" {
			t.Errorf("expected source.ref in response, got %v", source["ref"])
		}
		if source["source_commit_sha"] != "abc123" {
			t.Errorf("expected source.source_commit_sha in response, got %v", source["source_commit_sha"])
		}
		if data["pipeline_source"] != "repo" {
			t.Errorf("expected pipeline_source in response, got %v", data["pipeline_source"])
		}
		if data["pipeline_path"] != ".coyote/pipeline.yml" {
			t.Errorf("expected pipeline_path in response, got %v", data["pipeline_path"])
		}
	})

	t.Run("valid repo build with custom pipeline path", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(tmpDir+"/scenarios/success-basic", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmpDir+"/scenarios/success-basic/coyote.yml", []byte("version: 1\nsteps:\n  - name: test\n    run: echo ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		svc.SetRepoFetcher(&handlerFakeRepoFetcher{localPath: tmpDir, commitSHA: "abc123"})
		h := NewBuildHandler(svc)

		body := `{"project_id":"proj-1","repo_url":"https://github.com/org/repo.git","ref":"main","pipeline_path":"scenarios/success-basic/coyote.yml"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/repo", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.CreateRepoBuild(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		data := resp["data"]
		if data["pipeline_source"] != "repo" {
			t.Errorf("expected pipeline_source in response, got %v", data["pipeline_source"])
		}
		if data["pipeline_path"] != "scenarios/success-basic/coyote.yml" {
			t.Errorf("expected pipeline_path in response, got %v", data["pipeline_path"])
		}
	})

	t.Run("missing project_id returns 400", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		svc.SetRepoFetcher(&handlerFakeRepoFetcher{localPath: "/tmp", commitSHA: "abc"})
		h := NewBuildHandler(svc)

		body := `{"repo_url":"https://github.com/org/repo.git","ref":"main"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/repo", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.CreateRepoBuild(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing ref returns 400", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		svc.SetRepoFetcher(&handlerFakeRepoFetcher{localPath: "/tmp", commitSHA: "abc"})
		h := NewBuildHandler(svc)

		body := `{"project_id":"proj-1","repo_url":"https://github.com/org/repo.git"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/repo", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.CreateRepoBuild(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("commit sha without ref returns 201", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(tmpDir+"/.coyote", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmpDir+"/.coyote/pipeline.yml", []byte("version: 1\nsteps:\n  - name: test\n    run: echo ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		svc.SetRepoFetcher(&handlerFakeRepoFetcher{localPath: tmpDir, commitSHA: "abc123"})
		h := NewBuildHandler(svc)

		body := `{"project_id":"proj-1","repo_url":"https://github.com/org/repo.git","commit_sha":"abc123"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/repo", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.CreateRepoBuild(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("pipeline not found returns 400", func(t *testing.T) {
		tmpDir := t.TempDir() // empty dir, no .coyote/pipeline.yml
		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		svc.SetRepoFetcher(&handlerFakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})
		h := NewBuildHandler(svc)

		body := `{"project_id":"proj-1","repo_url":"https://github.com/org/repo.git","ref":"main"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/repo", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.CreateRepoBuild(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid pipeline path traversal returns 400", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(tmpDir+"/.coyote", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmpDir+"/.coyote/pipeline.yml", []byte("version: 1\nsteps:\n  - name: test\n    run: echo ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		repo := &fakeRepo{}
		svc := buildsvc.NewBuildService(repo, nil, logs.NewNoopSink())
		svc.SetRepoFetcher(&handlerFakeRepoFetcher{localPath: tmpDir, commitSHA: "abc"})
		h := NewBuildHandler(svc)

		body := `{"project_id":"proj-1","repo_url":"https://github.com/org/repo.git","ref":"main","pipeline_path":"../../foo"}`
		req := httptest.NewRequest(http.MethodPost, "/builds/repo", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.CreateRepoBuild(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestBuildHandler_GetBuildArtifacts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	repo := &fakeRepo{
		build: domain.Build{ID: "build-1", JobID: &jobID, ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
	}
	artifactRepo := &fakeArtifactRepo{artifactsByBuild: map[string][]domain.BuildArtifact{
		"build-1": {
			{ID: "artifact-1", BuildID: "build-1", LogicalPath: "dist/app", SizeBytes: 128, CreatedAt: now},
		},
	}}
	versionTagRepo := repositorymemory.NewVersionTagRepository()
	versionTagRepo.SeedBuilds(repo.build)
	versionTagRepo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: "build-1"})
	_, err := versionTagRepo.CreateForTargets(context.Background(), repository.CreateVersionTagsParams{JobID: jobID, Version: "v1", ArtifactIDs: []string{"artifact-1"}})
	if err != nil {
		t.Fatalf("failed to seed version tags: %v", err)
	}

	svc := buildsvc.NewBuildService(repo, nil, nil)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(artifact.NewFilesystemStore(t.TempDir())), t.TempDir())
	h := NewBuildHandler(svc)
	h.SetVersionTagService(versiontagsvc.NewService(versionTagRepo))

	req := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/artifacts", nil), "build-1")
	res := httptest.NewRecorder()
	h.GetBuildArtifacts(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}

	data := decodeDataMap(t, res)
	if data["build_id"] != "build-1" {
		t.Fatalf("expected build_id build-1, got %v", data["build_id"])
	}
	items, ok := data["artifacts"].([]any)
	if !ok {
		t.Fatalf("expected artifacts array, got %T", data["artifacts"])
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(items))
	}
	artifactData, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected artifact object, got %T", items[0])
	}
	if artifactData["path"] != "dist/app" {
		t.Fatalf("expected path dist/app, got %v", artifactData["path"])
	}
	tags, ok := artifactData["version_tags"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("expected 1 version tag, got %v", artifactData["version_tags"])
	}
}

func TestBuildHandler_DownloadBuildArtifact(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	storeRoot := t.TempDir()
	store := artifact.NewFilesystemStore(storeRoot)
	if _, err := store.Save(context.Background(), "build-1/dist/app", bytes.NewBufferString("artifact-content")); err != nil {
		t.Fatalf("failed to seed artifact store: %v", err)
	}

	repo := &fakeRepo{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
	}
	contentType := "application/octet-stream"
	artifactRepo := &fakeArtifactRepo{artifactsByBuild: map[string][]domain.BuildArtifact{
		"build-1": {
			{
				ID:          "artifact-1",
				BuildID:     "build-1",
				LogicalPath: "dist/app",
				StorageKey:  "build-1/dist/app",
				SizeBytes:   int64(len("artifact-content")),
				ContentType: &contentType,
				CreatedAt:   now,
			},
		},
	}}

	svc := buildsvc.NewBuildService(repo, nil, nil)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(store), t.TempDir())
	h := NewBuildHandler(svc)

	req := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/artifacts/artifact-1/download", nil), "build-1")
	rctx := chi.RouteContext(req.Context())
	rctx.URLParams.Add("artifactID", "artifact-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	res := httptest.NewRecorder()
	h.DownloadBuildArtifact(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	if got := res.Body.String(); got != "artifact-content" {
		t.Fatalf("expected artifact body, got %q", got)
	}
}

type downloadMapStore struct {
	objects map[string]string
}

func (s *downloadMapStore) Save(_ context.Context, key string, src io.Reader) (int64, error) {
	body, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}
	if s.objects == nil {
		s.objects = map[string]string{}
	}
	s.objects[key] = string(body)
	return int64(len(body)), nil
}

func (s *downloadMapStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := s.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func TestBuildHandler_DownloadBuildArtifact_GCSProviderNativeStorageKey(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fsStore := artifact.NewFilesystemStore(t.TempDir())
	gcsStore := &downloadMapStore{}
	nativeKey := "prefix-root/builds/build-1/shared/artifact-1-app"
	if _, err := gcsStore.Save(context.Background(), nativeKey, bytes.NewBufferString("artifact-content-gcs")); err != nil {
		t.Fatalf("failed to seed gcs store: %v", err)
	}

	repo := &fakeRepo{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: now},
	}
	contentType := "application/octet-stream"
	artifactRepo := &fakeArtifactRepo{artifactsByBuild: map[string][]domain.BuildArtifact{
		"build-1": {
			{
				ID:              "artifact-1",
				BuildID:         "build-1",
				LogicalPath:     "dist/app",
				StorageKey:      nativeKey,
				StorageProvider: domain.StorageProviderGCS,
				SizeBytes:       int64(len("artifact-content-gcs")),
				ContentType:     &contentType,
				CreatedAt:       now,
			},
		},
	}}

	svc := buildsvc.NewBuildService(repo, nil, nil)
	svc.SetArtifactPersistence(artifactRepo, testStoreResolverWithProviders(domain.StorageProviderFilesystem, map[domain.StorageProvider]artifact.Store{
		domain.StorageProviderFilesystem: fsStore,
		domain.StorageProviderGCS:        gcsStore,
	}), t.TempDir())
	h := NewBuildHandler(svc)

	req := addBuildIDParam(httptest.NewRequest(http.MethodGet, "/builds/build-1/artifacts/artifact-1/download", nil), "build-1")
	rctx := chi.RouteContext(req.Context())
	rctx.URLParams.Add("artifactID", "artifact-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	res := httptest.NewRecorder()
	h.DownloadBuildArtifact(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	if got := res.Body.String(); got != "artifact-content-gcs" {
		t.Fatalf("expected artifact body from gcs-native key, got %q", got)
	}
}

func testStoreResolver(store artifact.Store) *artifact.StoreResolver {
	return testStoreResolverWithProviders(domain.StorageProviderFilesystem, map[domain.StorageProvider]artifact.Store{
		domain.StorageProviderFilesystem: store,
	})
}

func testStoreResolverWithProviders(defaultProvider domain.StorageProvider, stores map[domain.StorageProvider]artifact.Store) *artifact.StoreResolver {
	return artifact.NewStoreResolver(defaultProvider, stores)
}
