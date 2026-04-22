package managedimage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

type fakeFetcher struct {
	repoRoot string
}

func (f fakeFetcher) Fetch(_ context.Context, _ string, _ string) (string, string, error) {
	return f.repoRoot, "abc123", nil
}

type fakeWritebackConfigs struct {
	cfg domain.JobManagedImageConfig
}

func (f fakeWritebackConfigs) GetByJobID(_ context.Context, _ string) (domain.JobManagedImageConfig, error) {
	return f.cfg, nil
}

type lookupWritebackConfigs struct {
	configs map[string]domain.JobManagedImageConfig
}

func (f lookupWritebackConfigs) GetByJobID(_ context.Context, jobID string) (domain.JobManagedImageConfig, error) {
	key := jobID
	cfg, ok := f.configs[key]
	if !ok {
		return domain.JobManagedImageConfig{}, repository.ErrJobManagedImageConfigNotFound
	}
	return cfg, nil
}

type fakeCredentials struct {
	cred domain.SourceCredential
}

func (f fakeCredentials) GetByID(_ context.Context, _ string) (domain.SourceCredential, error) {
	return f.cred, nil
}

type fakeCatalog struct {
	managedImage domain.ManagedImage
	version      domain.ManagedImageVersion
	found        bool
	created      bool
}

func (f *fakeCatalog) EnsureManagedImage(_ context.Context, _ string, _ string) (domain.ManagedImage, error) {
	return f.managedImage, nil
}

func (f *fakeCatalog) FindVersionByFingerprint(_ context.Context, _ string, _ string) (domain.ManagedImageVersion, bool, error) {
	return f.version, f.found, nil
}

func (f *fakeCatalog) CreateVersion(_ context.Context, version domain.ManagedImageVersion) (domain.ManagedImageVersion, error) {
	f.created = true
	f.version = version
	f.found = true
	return version, nil
}

type fakePublisher struct {
	published PublishedImage
	calls     int
}

func (f *fakePublisher) Publish(_ context.Context, _ PublishRequest) (PublishedImage, error) {
	f.calls++
	return f.published, nil
}

type fakeWriter struct {
	calls int
	last  source.GitWriteBackRequest
}

func (f *fakeWriter) CommitAndPushPipelineUpdate(_ context.Context, req source.GitWriteBackRequest) (source.GitWriteBackResult, error) {
	f.calls++
	f.last = req
	return source.GitWriteBackResult{BranchName: req.BranchName, CommitSHA: "def456"}, nil
}

type fakePullRequests struct {
	calls  int
	last   source.GitHubPullRequestRequest
	result source.GitHubPullRequestResult
	err    error
}

func (f *fakePullRequests) CreateOrGetPullRequest(_ context.Context, req source.GitHubPullRequestRequest) (source.GitHubPullRequestResult, error) {
	f.calls++
	f.last = req
	return f.result, f.err
}

func TestRefreshManagedPipelineImage_FingerprintChangeCreatesVersionAndWritesBack(t *testing.T) {
	repoRoot := t.TempDir()
	pipelinePath := filepath.Join(repoRoot, ".coyote", "pipeline.yml")
	if err := os.MkdirAll(filepath.Dir(pipelinePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pipelinePath, []byte("version: 1\npipeline:\n  image: golang:1.26.2\n"), 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "backend"), 0o755); err != nil {
		t.Fatalf("mkdir backend: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "backend", "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	catalog := &fakeCatalog{managedImage: domain.ManagedImage{ID: "managed-1"}}
	publisher := &fakePublisher{published: PublishedImage{ImageRef: "registry.example.com/coyote/go@sha256:abcd", ImageDigest: "sha256:abcd", VersionLabel: "v1"}}
	writer := &fakeWriter{}
	pullRequests := &fakePullRequests{result: source.GitHubPullRequestResult{Number: 12, URL: "https://github.com/example/repo/pull/12"}}
	svc := NewService(
		fakeFetcher{repoRoot: repoRoot},
		fakeWritebackConfigs{cfg: domain.JobManagedImageConfig{JobID: "job-1", PipelinePath: ".coyote/pipeline.yml", ManagedImageName: "go", WriteCredentialID: "cred-1", BotBranchPrefix: "coyote/managed-image-refresh", CommitAuthorName: "Coyote Bot", CommitAuthorEmail: "bot@example.com", Enabled: true}},
		fakeCredentials{cred: domain.SourceCredential{ID: "cred-1", Kind: domain.SourceCredentialKindHTTPSToken, SecretRef: "TOKEN"}},
		catalog,
		publisher,
		writer,
		pullRequests,
	)
	svc.clock = func() time.Time { return time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC) }

	res, err := svc.RefreshManagedPipelineImage(context.Background(), buildsvc.ManagedImageRefreshInput{JobID: "job-1", ProjectID: "proj-1", RepositoryURL: "https://example.com/repo.git", Ref: "main"})
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if !res.Updated {
		t.Fatal("expected write-back update")
	}
	if !strings.Contains(res.PinnedImageRef, "@sha256:") {
		t.Fatalf("expected immutable digest pin, got %q", res.PinnedImageRef)
	}
	if publisher.calls != 1 {
		t.Fatalf("expected publisher call, got %d", publisher.calls)
	}
	if !catalog.created {
		t.Fatal("expected managed image version creation")
	}
	if writer.calls != 1 {
		t.Fatalf("expected write-back call, got %d", writer.calls)
	}
	if pullRequests.calls != 1 {
		t.Fatalf("expected pull request call, got %d", pullRequests.calls)
	}
	if pullRequests.last.HeadBranch != writer.last.BranchName {
		t.Fatalf("expected pull request head branch %q, got %q", writer.last.BranchName, pullRequests.last.HeadBranch)
	}
	if !strings.HasPrefix(writer.last.BranchName, "coyote/managed-image-refresh/") {
		t.Fatalf("expected bot branch prefix, got %q", writer.last.BranchName)
	}
	if !strings.Contains(string(writer.last.Content), "@sha256:abcd") {
		t.Fatalf("expected pinned digest in pipeline write-back content: %s", string(writer.last.Content))
	}
}

func TestRefreshManagedPipelineImage_UnchangedFingerprintNoRewrite(t *testing.T) {
	repoRoot := t.TempDir()
	pipelinePath := filepath.Join(repoRoot, ".coyote", "pipeline.yml")
	if err := os.MkdirAll(filepath.Dir(pipelinePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pinned := "registry.example.com/coyote/go@sha256:abcd"
	if err := os.WriteFile(pipelinePath, []byte("version: 1\npipeline:\n  image: "+pinned+"\n"), 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "backend"), 0o755); err != nil {
		t.Fatalf("mkdir backend: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "backend", "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	catalog := &fakeCatalog{
		managedImage: domain.ManagedImage{ID: "managed-1"},
		version:      domain.ManagedImageVersion{ID: "v-1", ManagedImageID: "managed-1", ImageRef: pinned, ImageDigest: "sha256:abcd"},
		found:        true,
	}
	publisher := &fakePublisher{published: PublishedImage{ImageRef: pinned, ImageDigest: "sha256:abcd", VersionLabel: "v1"}}
	writer := &fakeWriter{}
	svc := NewService(
		fakeFetcher{repoRoot: repoRoot},
		fakeWritebackConfigs{cfg: domain.JobManagedImageConfig{JobID: "job-1", PipelinePath: ".coyote/pipeline.yml", ManagedImageName: "go", WriteCredentialID: "cred-1", Enabled: true}},
		fakeCredentials{cred: domain.SourceCredential{ID: "cred-1", Kind: domain.SourceCredentialKindHTTPSToken, SecretRef: "TOKEN"}},
		catalog,
		publisher,
		writer,
		&fakePullRequests{},
	)

	res, err := svc.RefreshManagedPipelineImage(context.Background(), buildsvc.ManagedImageRefreshInput{JobID: "job-1", ProjectID: "proj-1", RepositoryURL: "https://example.com/repo.git", Ref: "main"})
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if res.Updated {
		t.Fatal("expected no write-back when pinned image already matches fingerprint version")
	}
	if publisher.calls != 0 {
		t.Fatalf("expected no publish call, got %d", publisher.calls)
	}
	if writer.calls != 0 {
		t.Fatalf("expected no write-back call, got %d", writer.calls)
	}
}

func TestRefreshManagedPipelineImage_RejectsMutableTagFromPublisher(t *testing.T) {
	repoRoot := t.TempDir()
	pipelinePath := filepath.Join(repoRoot, ".coyote", "pipeline.yml")
	if err := os.MkdirAll(filepath.Dir(pipelinePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pipelinePath, []byte("version: 1\npipeline:\n  image: golang:1.26.2\n"), 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "backend"), 0o755); err != nil {
		t.Fatalf("mkdir backend: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "backend", "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	svc := NewService(
		fakeFetcher{repoRoot: repoRoot},
		fakeWritebackConfigs{cfg: domain.JobManagedImageConfig{JobID: "job-1", PipelinePath: ".coyote/pipeline.yml", ManagedImageName: "go", WriteCredentialID: "cred-1", Enabled: true}},
		fakeCredentials{cred: domain.SourceCredential{ID: "cred-1", Kind: domain.SourceCredentialKindHTTPSToken, SecretRef: "TOKEN"}},
		&fakeCatalog{managedImage: domain.ManagedImage{ID: "managed-1"}},
		&fakePublisher{published: PublishedImage{ImageRef: "registry.example.com/coyote/go:v2", ImageDigest: "sha256:abcd", VersionLabel: "v2"}},
		&fakeWriter{},
		&fakePullRequests{},
	)

	_, err := svc.RefreshManagedPipelineImage(context.Background(), buildsvc.ManagedImageRefreshInput{JobID: "job-1", ProjectID: "proj-1", RepositoryURL: "https://example.com/repo.git", Ref: "main"})
	if err == nil || !strings.Contains(err.Error(), "immutable digest") {
		t.Fatalf("expected immutable digest validation error, got: %v", err)
	}
}

func TestRefreshManagedPipelineImage_DisabledConfig(t *testing.T) {
	svc := NewService(fakeFetcher{repoRoot: t.TempDir()}, fakeWritebackConfigs{cfg: domain.JobManagedImageConfig{JobID: "job-1", Enabled: false}}, fakeCredentials{}, &fakeCatalog{}, &fakePublisher{}, &fakeWriter{}, &fakePullRequests{})
	res, err := svc.RefreshManagedPipelineImage(context.Background(), buildsvc.ManagedImageRefreshInput{JobID: "job-1", ProjectID: "proj", RepositoryURL: "repo", Ref: "main"})
	if err != nil {
		t.Fatalf("expected disabled config to be no-op, got error: %v", err)
	}
	if res.Updated {
		t.Fatal("expected disabled config to skip write-back")
	}
}

func TestRefreshManagedPipelineImage_RepoURLVariantLookup(t *testing.T) {
	repoRoot := t.TempDir()
	pipelinePath := filepath.Join(repoRoot, ".coyote", "pipeline.yml")
	if err := os.MkdirAll(filepath.Dir(pipelinePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pipelinePath, []byte("version: 1\npipeline:\n  image: golang:1.26.2\n"), 0o644); err != nil {
		t.Fatalf("write pipeline: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "backend"), 0o755); err != nil {
		t.Fatalf("mkdir backend: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "backend", "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	lookup := lookupWritebackConfigs{configs: map[string]domain.JobManagedImageConfig{
		"job-1": {
			JobID:             "job-1",
			PipelinePath:      ".coyote/pipeline.yml",
			ManagedImageName:  "go",
			WriteCredentialID: "cred-1",
			Enabled:           true,
		},
	}}

	svc := NewService(
		fakeFetcher{repoRoot: repoRoot},
		lookup,
		fakeCredentials{cred: domain.SourceCredential{ID: "cred-1", Kind: domain.SourceCredentialKindHTTPSToken, SecretRef: "TOKEN"}},
		&fakeCatalog{managedImage: domain.ManagedImage{ID: "managed-1"}},
		&fakePublisher{published: PublishedImage{ImageRef: "registry.example.com/coyote/go@sha256:abcd", ImageDigest: "sha256:abcd", VersionLabel: "v1"}},
		&fakeWriter{},
		&fakePullRequests{},
	)

	_, err := svc.RefreshManagedPipelineImage(context.Background(), buildsvc.ManagedImageRefreshInput{JobID: "job-1", ProjectID: "proj-1", RepositoryURL: "https://example.com/repo", Ref: "main"})
	if err != nil {
		t.Fatalf("expected job-scoped lookup to succeed, got: %v", err)
	}
}

func TestRefreshManagedPipelineImage_MissingJobConfigIsNoOp(t *testing.T) {
	svc := NewService(fakeFetcher{repoRoot: t.TempDir()}, lookupWritebackConfigs{configs: map[string]domain.JobManagedImageConfig{}}, fakeCredentials{}, &fakeCatalog{}, &fakePublisher{}, &fakeWriter{}, &fakePullRequests{})
	res, err := svc.RefreshManagedPipelineImage(context.Background(), buildsvc.ManagedImageRefreshInput{JobID: "job-missing", ProjectID: "proj-1", RepositoryURL: "https://example.com/repo.git", Ref: "main"})
	if err != nil {
		t.Fatalf("expected missing config to be a no-op, got %v", err)
	}
	if res.Updated {
		t.Fatal("expected missing job config to skip refresh")
	}
}
