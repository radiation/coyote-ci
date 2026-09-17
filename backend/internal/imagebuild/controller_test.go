package imagebuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	artifactpkg "github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/service"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
)

func TestControllerStagesSubmitsRenewsAndCompletesImageBuild(t *testing.T) {
	execution := newExecutionFake(t)
	records := memoryrepo.NewExternalImageBuildRepository()
	builder := &builderFake{result: domain.ImageBuildResult{Status: domain.ImageBuildStatusRunning}}
	stager := &stagerFake{source: domain.ImageBuildSource{Bucket: "sources", Object: "job-1.tar.gz", Generation: "1"}}
	controller, newErr := NewController(execution, records, builder, stager, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || !active {
		t.Fatalf("first reconcile active=%t err=%v", active, reconcileErr)
	}
	if stager.calls != 1 || builder.submitCalls != 1 || execution.renewCalls != 1 {
		t.Fatalf("stage=%d submit=%d renew=%d", stager.calls, builder.submitCalls, execution.renewCalls)
	}
	record, getErr := records.GetByExecutionJobID(context.Background(), "job-1")
	if getErr != nil || record.SubmissionState != domain.ExternalImageBuildSubmissionSubmitted || record.ExternalBuildID != "provider-1" {
		t.Fatalf("record=%+v err=%v", record, getErr)
	}

	builder.result = domain.ImageBuildResult{Status: domain.ImageBuildStatusSuccess, ImageDigest: "sha256:abc"}
	active, reconcileErr = controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || execution.completions != 1 || execution.result.Status != runner.RunStepStatusSuccess {
		t.Fatalf("terminal reconcile active=%t err=%v completions=%d result=%+v", active, reconcileErr, execution.completions, execution.result)
	}
	record, getErr = records.GetByExecutionJobID(context.Background(), "job-1")
	if getErr != nil || record.SubmissionState != domain.ExternalImageBuildSubmissionTerminal || record.ImageDigest != "sha256:abc" {
		t.Fatalf("terminal record=%+v err=%v", record, getErr)
	}
}

func TestControllerCompletesWhenTimingPersistenceFails(t *testing.T) {
	execution := newExecutionFake(t)
	execution.timingErr = errors.New("timing store unavailable")
	createdAt := time.Date(2026, time.September, 14, 10, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(time.Minute)
	finishedAt := startedAt.Add(time.Minute)
	builder := &builderFake{result: domain.ImageBuildResult{
		Status:      domain.ImageBuildStatusSuccess,
		ImageDigest: "sha256:abc",
		Timing: &domain.ExecutionTiming{Phases: []domain.ExecutionPhaseTiming{
			{Name: "queue", StartedAt: &createdAt, FinishedAt: &startedAt},
			{Name: "command", StartedAt: &startedAt, FinishedAt: &finishedAt},
		}},
	}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || execution.completions != 1 || execution.result.Status != runner.RunStepStatusSuccess {
		t.Fatalf("active=%t err=%v completions=%d result=%+v", active, reconcileErr, execution.completions, execution.result)
	}
}

func TestControllerCompletesWithAuthoritativeRemoteCommandTiming(t *testing.T) {
	execution := newExecutionFake(t)
	startedAt := time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(7*time.Minute + 29*time.Second)
	builder := &builderFake{result: domain.ImageBuildResult{Status: domain.ImageBuildStatusSuccess, ImageDigest: "sha256:abc", Timing: &domain.ExecutionTiming{Phases: []domain.ExecutionPhaseTiming{{Name: "command", StartedAt: &startedAt, FinishedAt: &finishedAt}}}}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}
	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || !execution.result.StartedAt.Equal(startedAt) || !execution.result.FinishedAt.Equal(finishedAt) || execution.result.FinishedAt.Sub(execution.result.StartedAt) != 7*time.Minute+29*time.Second {
		t.Fatalf("active=%t err=%v result=%+v", active, reconcileErr, execution.result)
	}
}
func TestControllerAdoptsExistingProviderBuildAndCancelsCanceledJob(t *testing.T) {
	execution := newExecutionFake(t)
	records := memoryrepo.NewExternalImageBuildRepository()
	builder := &builderFake{found: true, handle: domain.ImageBuildHandle{ID: "adopted"}, result: domain.ImageBuildResult{Status: domain.ImageBuildStatusRunning}}
	controller, newErr := NewController(execution, records, builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}
	if active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step); reconcileErr != nil || !active {
		t.Fatalf("reconcile active=%t err=%v", active, reconcileErr)
	}
	if builder.submitCalls != 0 {
		t.Fatalf("submit calls=%d, want adoption only", builder.submitCalls)
	}
	execution.job.Status = domain.ExecutionJobStatusCanceled
	if active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step); reconcileErr != nil || active || builder.cancelCalls != 1 {
		t.Fatalf("cancel reconcile active=%t err=%v cancel=%d", active, reconcileErr, builder.cancelCalls)
	}
}

func TestControllerRejectsInvalidOrUnsafeTerminalStates(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*executionFake, *builderFake, *stagerFake)
		wantActive bool
		wantStatus runner.RunStepStatus
	}{
		{name: "invalid spec", configure: func(execution *executionFake, _ *builderFake, _ *stagerFake) { execution.job.ResolvedSpecJSON = "{}" }, wantStatus: runner.RunStepStatusFailed},
		{name: "stage error", configure: func(_ *executionFake, _ *builderFake, stager *stagerFake) { stager.err = errors.New("stage failed") }, wantActive: true},
		{name: "successful provider result without digest", configure: func(_ *executionFake, builder *builderFake, _ *stagerFake) {
			builder.result = domain.ImageBuildResult{Status: domain.ImageBuildStatusSuccess}
		}, wantStatus: runner.RunStepStatusFailed},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			execution := newExecutionFake(t)
			builder := &builderFake{result: domain.ImageBuildResult{Status: domain.ImageBuildStatusRunning}}
			stager := &stagerFake{}
			testCase.configure(execution, builder, stager)
			controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, stager, &sourceFake{})
			if newErr != nil {
				t.Fatalf("new controller: %v", newErr)
			}
			active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
			if testCase.name == "stage error" {
				if !active || !errors.Is(reconcileErr, stager.err) {
					t.Fatalf("active=%t err=%v", active, reconcileErr)
				}
				return
			}
			if reconcileErr != nil || active != testCase.wantActive || execution.result.Status != testCase.wantStatus {
				t.Fatalf("active=%t err=%v result=%+v", active, reconcileErr, execution.result)
			}
		})
	}
}

func TestControllerCompletesPermanentSubmissionError(t *testing.T) {
	execution := newExecutionFake(t)
	records := memoryrepo.NewExternalImageBuildRepository()
	builder := &builderFake{submitErr: retryableBuilderError{err: errors.New("invalid timeout format")}}
	controller, newErr := NewController(execution, records, builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || execution.completions != 1 || execution.result.Status != runner.RunStepStatusFailed || !strings.Contains(execution.result.Stderr, "invalid timeout format") {
		t.Fatalf("active=%t err=%v completions=%d result=%+v", active, reconcileErr, execution.completions, execution.result)
	}
	if builder.submitCalls != 1 {
		t.Fatalf("submit calls=%d", builder.submitCalls)
	}
	execution.job.Status = domain.ExecutionJobStatusFailed
	active, reconcileErr = controller.ReconcileClaimed(context.Background(), execution.step)
	if reconcileErr != nil || active || builder.submitCalls != 1 {
		t.Fatalf("retry active=%t err=%v submit calls=%d", active, reconcileErr, builder.submitCalls)
	}
}

func TestControllerKeepsRetryableSubmissionErrorActive(t *testing.T) {
	execution := newExecutionFake(t)
	builder := &builderFake{submitErr: retryableBuilderError{err: errors.New("service unavailable"), retryable: true}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if !active || !errors.Is(reconcileErr, builder.submitErr) || execution.completions != 0 || execution.renewCalls != 1 {
		t.Fatalf("active=%t err=%v completions=%d renewals=%d", active, reconcileErr, execution.completions, execution.renewCalls)
	}
}

func TestControllerStopsRetryableSubmissionWhenLeaseIsLost(t *testing.T) {
	execution := newExecutionFake(t)
	execution.renewLost = true
	builder := &builderFake{submitErr: retryableBuilderError{err: errors.New("service unavailable"), retryable: true}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if active || reconcileErr != nil || execution.completions != 0 || execution.renewCalls != 1 || builder.submitCalls != 1 {
		t.Fatalf("active=%t err=%v completions=%d renewals=%d submissions=%d", active, reconcileErr, execution.completions, execution.renewCalls, builder.submitCalls)
	}
}

func TestControllerReturnsRetryableSubmissionAndLeaseRenewalErrors(t *testing.T) {
	execution := newExecutionFake(t)
	execution.renewErr = errors.New("lease renewal unavailable")
	builder := &builderFake{submitErr: retryableBuilderError{err: errors.New("service unavailable"), retryable: true}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}

	active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
	if !active || !errors.Is(reconcileErr, builder.submitErr) || !errors.Is(reconcileErr, execution.renewErr) || execution.completions != 0 || execution.renewCalls != 1 {
		t.Fatalf("active=%t err=%v completions=%d renewals=%d", active, reconcileErr, execution.completions, execution.renewCalls)
	}
}

func TestNewControllerRequiresAllDependencies(t *testing.T) {
	if _, err := NewController(nil, nil, nil, nil, nil); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestControllerResolveArtifactInputsUsesKubernetesProducerStepIDs(t *testing.T) {
	context := context.Background()
	execution := newExecutionFake(t)
	execution.job.DependsOnNodeIDs = []string{"node-server", "node-worker"}
	execution.steps = []domain.BuildStep{
		{ID: "build-step-server", BuildID: execution.job.BuildID, NodeID: "node-server"},
		{ID: "build-step-worker", BuildID: execution.job.BuildID, NodeID: "node-worker"},
		{ID: "build-step-other", BuildID: execution.job.BuildID, NodeID: "node-other"},
	}
	store := artifactpkg.NewFilesystemStore(t.TempDir())
	stores := artifactpkg.NewStoreResolver(domain.StorageProviderFilesystem, map[domain.StorageProvider]artifactpkg.Store{domain.StorageProviderFilesystem: store})
	artifacts := &imageBuildArtifactRepositoryFake{}
	serverStepID := "build-step-server"
	workerStepID := "build-step-worker"
	serverContent := []byte("server executable")
	workerContent := []byte("worker executable")
	createImageBuildArtifact(t, context, artifacts, store, domain.BuildArtifact{ID: "artifact-server", BuildID: execution.job.BuildID, StepID: &serverStepID, Name: "coyote-server", LogicalPath: "backend/dist/coyote-server", StorageKey: "artifacts/server", StorageProvider: domain.StorageProviderFilesystem, SizeBytes: int64(len(serverContent)), ChecksumSHA256: checksumPointer(serverContent)}, serverContent)
	createImageBuildArtifact(t, context, artifacts, store, domain.BuildArtifact{ID: "artifact-worker", BuildID: execution.job.BuildID, StepID: &workerStepID, Name: "coyote-worker", LogicalPath: "dist/coyote-worker", StorageKey: "artifacts/worker", StorageProvider: domain.StorageProviderFilesystem, SizeBytes: int64(len(workerContent)), ChecksumSHA256: checksumPointer(workerContent)}, workerContent)
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), &builderFake{}, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}
	controller.WithArtifactInputs(artifacts, stores)
	inputs, resolveErr := controller.resolveArtifactInputs(context, execution.job, domain.RemoteImageBuildSpec{ArtifactInputs: []domain.ImageBuildArtifactInput{{Name: "coyote-server", Destination: "dist/coyote-server"}, {Name: "coyote-worker", Destination: "dist/coyote-worker"}}})
	if resolveErr != nil || len(inputs) != 2 {
		t.Fatalf("inputs=%#v err=%v", inputs, resolveErr)
	}
	for index, wantContent := range [][]byte{serverContent, workerContent} {
		actualContent, readErr := io.ReadAll(inputs[index].Source)
		closeErr := inputs[index].Source.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(actualContent, wantContent) {
			t.Fatalf("input=%d content=%q readErr=%v closeErr=%v", index, actualContent, readErr, closeErr)
		}
	}

	nonUpstreamArtifacts := &imageBuildArtifactRepositoryFake{}
	otherStepID := "build-step-other"
	createImageBuildArtifact(t, context, nonUpstreamArtifacts, store, domain.BuildArtifact{ID: "artifact-other", BuildID: execution.job.BuildID, StepID: &otherStepID, Name: "coyote-server", LogicalPath: "dist/coyote-server", StorageKey: "artifacts/other", StorageProvider: domain.StorageProviderFilesystem, SizeBytes: int64(len(serverContent)), ChecksumSHA256: checksumPointer(serverContent)}, serverContent)
	controller.WithArtifactInputs(nonUpstreamArtifacts, stores)
	_, resolveErr = controller.resolveArtifactInputs(context, execution.job, domain.RemoteImageBuildSpec{ArtifactInputs: []domain.ImageBuildArtifactInput{{Name: "coyote-server", Destination: "dist/coyote-server"}}})
	if resolveErr == nil || resolveErr.Error() != "image build artifact \"coyote-server\" is not available from an upstream dependency" {
		t.Fatalf("resolve non-upstream artifact error=%v", resolveErr)
	}
}

func TestControllerResolveArtifactInputsUsesArtifactStorageProvider(t *testing.T) {
	context := context.Background()
	execution := newExecutionFake(t)
	execution.job.DependsOnNodeIDs = []string{"node-producer"}
	producerStepID := "build-step-producer"
	execution.steps = []domain.BuildStep{{ID: producerStepID, BuildID: execution.job.BuildID, NodeID: "node-producer"}}

	filesystemStore := &recordingArtifactStore{content: map[string][]byte{"artifacts/filesystem": []byte("filesystem executable"), "artifacts/checksum": []byte("corrupt executable")}}
	gcsStore := &recordingArtifactStore{content: map[string][]byte{"artifacts/gcs": []byte("gcs executable")}}
	stores := artifactpkg.NewStoreResolver(domain.StorageProviderFilesystem, map[domain.StorageProvider]artifactpkg.Store{
		domain.StorageProviderFilesystem: filesystemStore,
		domain.StorageProviderGCS:        gcsStore,
	})
	artifacts := &imageBuildArtifactRepositoryFake{items: []domain.BuildArtifact{
		{ID: "artifact-gcs", BuildID: execution.job.BuildID, StepID: &producerStepID, Name: "gcs-artifact", StorageKey: "artifacts/gcs", StorageProvider: domain.StorageProviderGCS, SizeBytes: int64(len("gcs executable")), ChecksumSHA256: checksumPointer([]byte("gcs executable"))},
		{ID: "artifact-filesystem", BuildID: execution.job.BuildID, StepID: &producerStepID, Name: "filesystem-artifact", StorageKey: "artifacts/filesystem", StorageProvider: domain.StorageProviderFilesystem, SizeBytes: int64(len("filesystem executable")), ChecksumSHA256: checksumPointer([]byte("filesystem executable"))},
		{ID: "artifact-checksum", BuildID: execution.job.BuildID, StepID: &producerStepID, Name: "checksum-artifact", StorageKey: "artifacts/checksum", StorageProvider: domain.StorageProviderFilesystem, SizeBytes: int64(len("corrupt executable")), ChecksumSHA256: checksumPointer([]byte("expected executable"))},
		{ID: "artifact-unavailable", BuildID: execution.job.BuildID, StepID: &producerStepID, Name: "unavailable-artifact", StorageKey: "artifacts/unavailable", StorageProvider: domain.StorageProvider("unavailable"), SizeBytes: 1, ChecksumSHA256: checksumPointer([]byte("x"))},
	}}
	controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), &builderFake{}, &stagerFake{}, &sourceFake{})
	if newErr != nil {
		t.Fatalf("new controller: %v", newErr)
	}
	controller.WithArtifactInputs(artifacts, stores)

	for _, testCase := range []struct {
		name             string
		input            string
		wantContent      string
		wantResolveError string
		wantReadError    string
		wantFilesystem   int
		wantGCS          int
	}{
		{name: "gcs uses gcs store", input: "gcs-artifact", wantContent: "gcs executable", wantGCS: 1},
		{name: "filesystem uses filesystem store", input: "filesystem-artifact", wantContent: "filesystem executable", wantFilesystem: 1, wantGCS: 1},
		{name: "checksum validation remains active", input: "checksum-artifact", wantReadError: "checksum or size mismatch", wantFilesystem: 2, wantGCS: 1},
		{name: "unavailable provider does not fall back", input: "unavailable-artifact", wantResolveError: "no store configured for provider \"unavailable\"", wantFilesystem: 2, wantGCS: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			inputs, resolveErr := controller.resolveArtifactInputs(context, execution.job, domain.RemoteImageBuildSpec{ArtifactInputs: []domain.ImageBuildArtifactInput{{Name: testCase.input}}})
			if testCase.wantResolveError != "" {
				if resolveErr == nil || !strings.Contains(resolveErr.Error(), testCase.wantResolveError) {
					t.Fatalf("resolve error=%v, want %q", resolveErr, testCase.wantResolveError)
				}
			} else if resolveErr != nil || len(inputs) != 1 {
				t.Fatalf("inputs=%#v error=%v", inputs, resolveErr)
			} else {
				content, readErr := io.ReadAll(inputs[0].Source)
				closeErr := inputs[0].Source.Close()
				if testCase.wantReadError != "" {
					if readErr == nil || !strings.Contains(readErr.Error(), testCase.wantReadError) {
						t.Fatalf("read error=%v, want %q", readErr, testCase.wantReadError)
					}
				} else if readErr != nil || closeErr != nil || string(content) != testCase.wantContent {
					t.Fatalf("content=%q readErr=%v closeErr=%v", content, readErr, closeErr)
				}
			}
			if len(filesystemStore.opened) != testCase.wantFilesystem || len(gcsStore.opened) != testCase.wantGCS {
				t.Fatalf("filesystem opens=%q gcs opens=%q", filesystemStore.opened, gcsStore.opened)
			}
		})
	}
}

type recordingArtifactStore struct {
	content map[string][]byte
	opened  []string
}

func (s *recordingArtifactStore) Save(_ context.Context, key string, source io.Reader) (int64, error) {
	content, readErr := io.ReadAll(source)
	if readErr != nil {
		return 0, readErr
	}
	s.content[key] = content
	return int64(len(content)), nil
}

func (s *recordingArtifactStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.opened = append(s.opened, key)
	content, ok := s.content[key]
	if !ok {
		return nil, errors.New("unexpected artifact store key")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func createImageBuildArtifact(t *testing.T, context context.Context, artifacts *imageBuildArtifactRepositoryFake, store artifactpkg.Store, item domain.BuildArtifact, content []byte) {
	t.Helper()
	if _, saveErr := store.Save(context, item.StorageKey, bytes.NewReader(content)); saveErr != nil {
		t.Fatalf("save artifact %q: %v", item.Name, saveErr)
	}
	if _, createErr := artifacts.Create(context, item); createErr != nil {
		t.Fatalf("create artifact %q: %v", item.Name, createErr)
	}
}

func checksumPointer(content []byte) *string {
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	return &checksum
}

type imageBuildArtifactRepositoryFake struct {
	items []domain.BuildArtifact
}

func (r *imageBuildArtifactRepositoryFake) Create(_ context.Context, item domain.BuildArtifact) (domain.BuildArtifact, error) {
	r.items = append(r.items, item)
	return item, nil
}

func (r *imageBuildArtifactRepositoryFake) ListByBuildID(_ context.Context, buildID string) ([]domain.BuildArtifact, error) {
	items := make([]domain.BuildArtifact, 0, len(r.items))
	for _, item := range r.items {
		if item.BuildID == buildID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *imageBuildArtifactRepositoryFake) Browse(context.Context, repository.BrowseArtifactsParams) ([]domain.ArtifactRecord, error) {
	return nil, nil
}

func (r *imageBuildArtifactRepositoryFake) ListCatalog(context.Context, repository.ArtifactCatalogParams) ([]domain.ArtifactRecord, error) {
	return nil, nil
}

func (r *imageBuildArtifactRepositoryFake) GetCatalogByID(context.Context, string) (domain.ArtifactRecord, error) {
	return domain.ArtifactRecord{}, repository.ErrArtifactNotFound
}

func (r *imageBuildArtifactRepositoryFake) GetByID(_ context.Context, buildID, artifactID string) (domain.BuildArtifact, error) {
	for _, item := range r.items {
		if item.BuildID == buildID && item.ID == artifactID {
			return item, nil
		}
	}
	return domain.BuildArtifact{}, repository.ErrArtifactNotFound
}

func (r *imageBuildArtifactRepositoryFake) ListByStepID(_ context.Context, stepID string) ([]domain.BuildArtifact, error) {
	items := make([]domain.BuildArtifact, 0, len(r.items))
	for _, item := range r.items {
		if item.StepID != nil && *item.StepID == stepID {
			items = append(items, item)
		}
	}
	return items, nil
}

type executionFake struct {
	job         domain.ExecutionJob
	step        workersvc.WorkerRunnableStep
	renewCalls  int
	renewLost   bool
	renewErr    error
	completions int
	result      runner.RunStepResult
	timing      domain.ExecutionTiming
	timingErr   error
	steps       []domain.BuildStep
}

func newExecutionFake(t *testing.T) *executionFake {
	t.Helper()
	spec, marshalErr := json.Marshal(domain.ExecutionJobSpec{ExecutionKind: domain.ExecutionKindImageBuild, RemoteImageBuild: &domain.RemoteImageBuildSpec{ContextPath: "backend", DockerfilePath: "backend/Dockerfile", TargetImageReference: "coyote-ci/backend"}, TimeoutSeconds: 60})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	return &executionFake{job: domain.ExecutionJob{ID: "job-1", BuildID: "build-1", Status: domain.ExecutionJobStatusRunning, ResolvedSpecJSON: string(spec)}, step: workersvc.WorkerRunnableStep{BuildID: "build-1", JobID: "job-1", ClaimToken: "claim"}}
}

func (f *executionFake) GetExecutionJob(context.Context, string) (domain.ExecutionJob, error) {
	return f.job, nil
}
func (f *executionFake) GetBuild(context.Context, string) (domain.Build, error) {
	return domain.Build{ID: "build-1"}, nil
}
func (f *executionFake) GetBuildSteps(context.Context, string) ([]domain.BuildStep, error) {
	return f.steps, nil
}
func (f *executionFake) RenewRunnableStepLease(context.Context, workersvc.WorkerRunnableStep) (bool, error) {
	f.renewCalls++
	if f.renewErr != nil {
		return false, f.renewErr
	}
	return !f.renewLost, nil
}
func (f *executionFake) UpdateRunnableStepTiming(_ context.Context, _ workersvc.WorkerRunnableStep, timing domain.ExecutionTiming) (bool, error) {
	f.timing = timing
	return true, f.timingErr
}
func (f *executionFake) CompleteKubernetesRunnableStep(_ context.Context, _ workersvc.WorkerRunnableStep, result runner.RunStepResult) (repository.StepCompletionOutcome, error) {
	f.completions++
	f.result = result
	return repository.StepCompletionCompleted, nil
}

type builderFake struct {
	found                    bool
	handle                   domain.ImageBuildHandle
	result                   domain.ImageBuildResult
	submitErr                error
	submitCalls, cancelCalls int
}

type retryableBuilderError struct {
	err       error
	retryable bool
}

func (e retryableBuilderError) Error() string   { return e.err.Error() }
func (e retryableBuilderError) Unwrap() error   { return e.err }
func (e retryableBuilderError) Retryable() bool { return e.retryable }

func (f *builderFake) Submit(_ context.Context, _ domain.ImageBuildRequest) (domain.ImageBuildHandle, error) {
	f.submitCalls++
	if f.submitErr != nil {
		return domain.ImageBuildHandle{}, f.submitErr
	}
	if f.handle.ID == "" {
		f.handle = domain.ImageBuildHandle{ID: "provider-1"}
	}
	return f.handle, nil
}
func (f *builderFake) Get(context.Context, domain.ImageBuildHandle) (domain.ImageBuildResult, error) {
	return f.result, nil
}
func (f *builderFake) Cancel(context.Context, domain.ImageBuildHandle) error {
	f.cancelCalls++
	return nil
}
func (f *builderFake) FindByExecutionJobID(context.Context, string) (domain.ImageBuildHandle, bool, error) {
	return f.handle, f.found, nil
}

type stagerFake struct {
	calls  int
	source domain.ImageBuildSource
	err    error
}

func (f *stagerFake) Stage(context.Context, string, io.Reader, string, []service.ImageBuildContextArtifact) (domain.ImageBuildSource, error) {
	f.calls++
	if f.err != nil {
		return domain.ImageBuildSource{}, f.err
	}
	if f.source.Bucket == "" {
		return domain.ImageBuildSource{Bucket: "sources", Object: "job-1", Generation: "1"}, nil
	}
	return f.source, nil
}

type sourceFake struct{}

func (*sourceFake) OpenSourceArchive(context.Context, domain.Build, domain.ExecutionJob, domain.ExecutionJobSpec) (service.WorkspacePreparePayload, error) {
	return service.WorkspacePreparePayload{Archive: io.NopCloser(bytes.NewReader([]byte("archive")))}, nil
}

var _ service.ImageBuilder = (*builderFake)(nil)
var _ service.ImageBuildSourceStager = (*stagerFake)(nil)
var _ service.WorkspaceSourceArchivePreparer = (*sourceFake)(nil)
