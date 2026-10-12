package release

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type releaseRepositoryFake struct {
	created       domain.ApplicationRelease
	promoteErr    error
	pending       []domain.ApplicationRelease
	reconcileErr  error
	reconcileErrs map[string]error
	reconciledIDs []string
	failErr       error
	failedBuildID string
	failedReason  string
	getByID       domain.ApplicationRelease
	getByIDErr    error
}

func (f *releaseRepositoryFake) Create(_ context.Context, release domain.ApplicationRelease) (domain.ApplicationRelease, error) {
	f.created = release
	return release, nil
}
func (f *releaseRepositoryFake) GetByBuildID(context.Context, string) (domain.ApplicationRelease, error) {
	return domain.ApplicationRelease{}, repository.ErrApplicationReleaseNotFound
}
func (f *releaseRepositoryFake) GetByID(context.Context, string) (domain.ApplicationRelease, error) {
	if f.getByIDErr != nil {
		return domain.ApplicationRelease{}, f.getByIDErr
	}
	return f.getByID, nil
}
func (f *releaseRepositoryFake) ListPending(context.Context, int) ([]domain.ApplicationRelease, error) {
	return f.pending, nil
}
func (f *releaseRepositoryFake) Reconcile(_ context.Context, id string) (domain.ApplicationRelease, error) {
	f.reconciledIDs = append(f.reconciledIDs, id)
	if err, found := f.reconcileErrs[id]; found {
		return domain.ApplicationRelease{}, err
	}
	return domain.ApplicationRelease{}, f.reconcileErr
}
func (f *releaseRepositoryFake) FailBuildRelease(_ context.Context, buildID string, reason string) (domain.ApplicationRelease, error) {
	f.failedBuildID, f.failedReason = buildID, reason
	return domain.ApplicationRelease{}, f.failErr
}

func TestServiceReconcilePending_ReconcilesEveryPendingRelease(t *testing.T) {
	repo := &releaseRepositoryFake{pending: []domain.ApplicationRelease{{ID: "release-1"}, {ID: "release-2"}}}
	if err := NewService(repo).ReconcilePending(context.Background(), 10); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if got := strings.Join(repo.reconciledIDs, ","); got != "release-1,release-2" {
		t.Fatalf("reconciled IDs = %q", got)
	}
}

func TestServiceReconcilePending_ReturnsReconcileError(t *testing.T) {
	repo := &releaseRepositoryFake{pending: []domain.ApplicationRelease{{ID: "release-1"}}, reconcileErr: errors.New("database unavailable")}
	if err := NewService(repo).ReconcilePending(context.Background(), 10); err == nil {
		t.Fatal("ReconcilePending() error = nil, want error")
	}
}

func TestServiceReconcilePending_ContinuesAfterErrors(t *testing.T) {
	firstErr := errors.New("first unavailable")
	secondErr := errors.New("second unavailable")
	repo := &releaseRepositoryFake{
		pending:       []domain.ApplicationRelease{{ID: "release-1"}, {ID: "release-2"}, {ID: "release-3"}},
		reconcileErrs: map[string]error{"release-1": firstErr, "release-2": secondErr},
	}
	reconcileErr := NewService(repo).ReconcilePending(context.Background(), 10)
	if !errors.Is(reconcileErr, firstErr) || !errors.Is(reconcileErr, secondErr) {
		t.Fatalf("ReconcilePending() error = %v, want both errors", reconcileErr)
	}
	if got := strings.Join(repo.reconciledIDs, ","); got != "release-1,release-2,release-3" {
		t.Fatalf("reconciled IDs = %q, want all pending releases", got)
	}
}

func TestServiceFailBuildRelease_OnlyTerminalFailuresReachRepository(t *testing.T) {
	repo := &releaseRepositoryFake{}
	service := NewService(repo)
	if err := service.FailBuildRelease(context.Background(), domain.Build{ID: "build-running", Status: domain.BuildStatusRunning}); err != nil {
		t.Fatalf("running build error = %v", err)
	}
	if repo.failedBuildID != "" {
		t.Fatalf("failed build ID = %q, want no call", repo.failedBuildID)
	}
	reason := "canceled by user"
	if err := service.FailBuildRelease(context.Background(), domain.Build{ID: "build-canceled", Status: domain.BuildStatusCanceled, ErrorMessage: &reason}); err != nil {
		t.Fatalf("canceled build error = %v", err)
	}
	if repo.failedBuildID != "build-canceled" || repo.failedReason != reason {
		t.Fatalf("failure call = (%q, %q)", repo.failedBuildID, repo.failedReason)
	}
}

func TestServiceFailBuildRelease_IgnoresMissingRelease(t *testing.T) {
	repo := &releaseRepositoryFake{failErr: repository.ErrApplicationReleaseNotFound}
	if err := NewService(repo).FailBuildRelease(context.Background(), domain.Build{ID: "build-failed", Status: domain.BuildStatusFailed}); err != nil {
		t.Fatalf("FailBuildRelease() error = %v", err)
	}
}
func (f *releaseRepositoryFake) PromoteStable(context.Context, string, *string) (domain.ApplicationRelease, error) {
	return domain.ApplicationRelease{}, f.promoteErr
}

func TestServicePromoteStable_JoblessChannelScopeIsRejected(t *testing.T) {
	repo := &releaseRepositoryFake{promoteErr: repository.ErrApplicationReleaseChannelScope}
	_, err := NewService(repo).PromoteStable(context.Background(), "release-1", nil)
	if err != repository.ErrApplicationReleaseChannelScope {
		t.Fatalf("PromoteStable() error = %v, want ErrApplicationReleaseChannelScope", err)
	}
}

func TestServiceCreateSnapshot_ExplicitArtifactAndImageOutputs(t *testing.T) {
	version := "1.4.2"
	pipelineYAML := `version: 1
pipeline:
  application_version:
    value: 1.4.2
  release:
    artifacts: [api]
    images: [image]
    auto_channel: latest
artifacts:
  - name: api
    path: dist/api.tar.gz
steps:
  - name: image
    image_build:
      context: .
      dockerfile: Dockerfile
      image: example/api`
	repo := &releaseRepositoryFake{}
	service := NewService(repo)
	jobID := "job-1"
	_, err := service.CreateSnapshot(context.Background(), domain.Build{
		ID: "build-1", ProjectID: "project-1", JobID: &jobID, ApplicationVersion: &version, PipelineConfigYAML: &pipelineYAML, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if len(repo.created.Outputs) != 2 {
		t.Fatalf("outputs = %d, want 2", len(repo.created.Outputs))
	}
	if repo.created.AutoChannelName != "latest" {
		t.Fatalf("auto channel = %q, want latest", repo.created.AutoChannelName)
	}
	if repo.created.Outputs[0].OutputKey != "artifact:api" || repo.created.Outputs[1].Kind != domain.ApplicationReleaseOutputImage {
		t.Fatalf("unexpected snapshot outputs: %#v", repo.created.Outputs)
	}
}

func TestServiceCreateSnapshot_LegacyPipelineReturnsNotFound(t *testing.T) {
	version := "1.4.2"
	pipelineYAML := "version: 1\npipeline:\n  application_version:\n    value: 1.4.2\nsteps:\n  - name: build\n    run: echo build"
	_, err := NewService(&releaseRepositoryFake{}).CreateSnapshot(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", ApplicationVersion: &version, PipelineConfigYAML: &pipelineYAML})
	if err != repository.ErrApplicationReleaseNotFound {
		t.Fatalf("CreateSnapshot() error = %v, want ErrApplicationReleaseNotFound", err)
	}
}

func TestServiceEnsureSnapshot_LegacyPipelineIsNoOp(t *testing.T) {
	version := "1.4.2"
	pipelineYAML := "version: 1\npipeline:\n  application_version:\n    value: 1.4.2\nsteps:\n  - name: build\n    run: echo build"
	repo := &releaseRepositoryFake{}
	err := NewService(repo).EnsureSnapshot(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", ApplicationVersion: &version, PipelineConfigYAML: &pipelineYAML})
	if err != nil {
		t.Fatalf("EnsureSnapshot() error = %v", err)
	}
	if repo.created.ID != "" {
		t.Fatalf("created release = %#v, want no release", repo.created)
	}
}

func TestServiceCreateSnapshot_JoblessAutoChannelIsRejected(t *testing.T) {
	version := "1.4.2"
	pipelineYAML := "version: 1\npipeline:\n  application_version:\n    value: 1.4.2\n  release:\n    artifacts: [api]\n    auto_channel: latest\nartifacts:\n  - name: api\n    path: dist/api.tar.gz\nsteps:\n  - name: build\n    run: echo build"
	_, err := NewService(&releaseRepositoryFake{}).CreateSnapshot(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", ApplicationVersion: &version, PipelineConfigYAML: &pipelineYAML})
	if err != repository.ErrApplicationReleaseChannelScope {
		t.Fatalf("CreateSnapshot() error = %v, want ErrApplicationReleaseChannelScope", err)
	}
}
