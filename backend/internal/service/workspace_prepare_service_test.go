package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestWorkspacePrepareServiceOpenPredecessorStreamsPublishedRevision(t *testing.T) {
	size := int64(7)
	harness := newWorkspacePrepareServiceForTest(t, domain.ExecutionJob{ID: "job-2", BuildID: "build-1", ResolvedSpecJSON: `{"workspace_input":{"mode":"predecessor","producer_node_id":"compile"}}`})
	harness.revisions.revision = domain.WorkspaceRevision{Status: domain.WorkspaceRevisionStatusPublished, ContentDigest: workspacePrepareStringPointer("sha256:abc"), StorageKey: workspacePrepareStringPointer("workspace-revisions/revision-1.tar.gz"), SizeBytes: &size}
	harness.archives.contents = []byte("archive")

	prepared, err := harness.service.Open(context.Background(), "capability", "job-2", "pod-1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	contents, readErr := io.ReadAll(prepared.Archive)
	closeErr := prepared.Archive.Close()
	if readErr != nil || closeErr != nil || string(contents) != "archive" {
		t.Fatalf("stream = %q, %v, %v", contents, readErr, closeErr)
	}
	if prepared.Publication.ContentDigest != "sha256:abc" || prepared.Publication.SizeBytes == nil || *prepared.Publication.SizeBytes != size {
		t.Fatalf("publication = %#v", prepared.Publication)
	}
	if harness.revisions.buildID != "build-1" || harness.revisions.nodeID != "compile" || harness.sources.calls != 0 {
		t.Fatalf("unexpected dependencies: revision=%q/%q source calls=%d", harness.revisions.buildID, harness.revisions.nodeID, harness.sources.calls)
	}
}

func TestWorkspacePrepareServiceOpenRejectsUnauthorizedBeforeLookup(t *testing.T) {
	harness := newWorkspacePrepareServiceForTest(t, domain.ExecutionJob{})
	harness.capabilities.err = errors.New("denied")
	if _, err := harness.service.Open(context.Background(), "bad", "job-1", "pod-1"); !errors.Is(err, harness.capabilities.err) {
		t.Fatalf("open: %v", err)
	}
	if harness.executionJobs.calls != 0 {
		t.Fatalf("job lookups = %d, want 0", harness.executionJobs.calls)
	}
}

func TestWorkspacePrepareServiceOpenRejectsIncompletePredecessorRevision(t *testing.T) {
	harness := newWorkspacePrepareServiceForTest(t, domain.ExecutionJob{ID: "job-2", BuildID: "build-1", ResolvedSpecJSON: `{"workspace_input":{"mode":"predecessor","producer_node_id":"compile"}}`})
	harness.revisions.revision = domain.WorkspaceRevision{Status: domain.WorkspaceRevisionStatusPublished, ContentDigest: workspacePrepareStringPointer("sha256:abc")}
	if _, err := harness.service.Open(context.Background(), "capability", "job-2", "pod-1"); !errors.Is(err, ErrWorkspacePrepareRevisionIncomplete) {
		t.Fatalf("open: %v", err)
	}
	if harness.archives.calls != 0 {
		t.Fatalf("archive opens = %d, want 0", harness.archives.calls)
	}
}

func TestWorkspacePrepareServiceOpenSourceUsesSourceArchivePreparer(t *testing.T) {
	harness := newWorkspacePrepareServiceForTest(t, domain.ExecutionJob{ID: "job-1", BuildID: "build-1", ResolvedSpecJSON: `{"version":1,"workspace_input":{"mode":"source"}}`})
	harness.sources.contents = []byte("source")
	prepared, err := harness.service.Open(context.Background(), "capability", "job-1", "pod-1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = prepared.Archive.Close() }()
	contents, readErr := io.ReadAll(prepared.Archive)
	if readErr != nil || string(contents) != "source" || harness.builds.calls != 1 || harness.sources.calls != 1 {
		t.Fatalf("source stream = %q, %v; build/source calls = %d/%d", contents, readErr, harness.builds.calls, harness.sources.calls)
	}
}

func TestWorkspacePrepareServiceOpenRejectsUnsupportedOrInvalidPlans(t *testing.T) {
	for _, testCase := range []struct {
		name, spec string
		want       error
	}{
		{name: "fan in", spec: `{"workspace_input":{"mode":"fan_in"}}`, want: ErrWorkspacePrepareFanInUnsupported},
		{name: "missing mode", spec: `{"workspace_input":{}}`, want: ErrWorkspacePrepareInvalidInput},
		{name: "invalid json", spec: `{`, want: ErrWorkspacePrepareInvalidInput},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspacePrepareServiceForTest(t, domain.ExecutionJob{ID: "job-1", BuildID: "build-1", ResolvedSpecJSON: testCase.spec})
			if _, err := harness.service.Open(context.Background(), "capability", "job-1", "pod-1"); !errors.Is(err, testCase.want) {
				t.Fatalf("open: %v", err)
			}
		})
	}
}

func TestWorkspacePrepareServiceOpenPropagatesDependencyFailures(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		job    domain.ExecutionJob
		mutate func(workspacePrepareServiceTestHarness)
	}{
		{name: "job lookup", mutate: func(h workspacePrepareServiceTestHarness) { h.executionJobs.err = errors.New("job lookup failed") }},
		{name: "build lookup", job: domain.ExecutionJob{ID: "job-1", BuildID: "build-1", ResolvedSpecJSON: `{"workspace_input":{"mode":"source"}}`}, mutate: func(h workspacePrepareServiceTestHarness) { h.builds.err = errors.New("build lookup failed") }},
		{name: "source archive", job: domain.ExecutionJob{ID: "job-1", BuildID: "build-1", ResolvedSpecJSON: `{"workspace_input":{"mode":"source"}}`}, mutate: func(h workspacePrepareServiceTestHarness) { h.sources.err = errors.New("source archive failed") }},
		{name: "revision lookup", job: domain.ExecutionJob{ID: "job-1", BuildID: "build-1", ResolvedSpecJSON: `{"workspace_input":{"mode":"predecessor","producer_node_id":"compile"}}`}, mutate: func(h workspacePrepareServiceTestHarness) { h.revisions.err = errors.New("revision lookup failed") }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newWorkspacePrepareServiceForTest(t, testCase.job)
			testCase.mutate(harness)
			if _, openErr := harness.service.Open(context.Background(), "capability", "job-1", "pod-1"); openErr == nil {
				t.Fatal("expected open failure")
			}
		})
	}
}

func TestNewWorkspacePrepareServiceRequiresDependencies(t *testing.T) {
	if _, err := NewWorkspacePrepareService(WorkspacePrepareServiceConfig{}); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

type workspacePrepareServiceTestHarness struct {
	service       *WorkspacePrepareService
	capabilities  *workspacePrepareCapabilityFake
	executionJobs *workspacePrepareJobFake
	builds        *workspacePrepareBuildFake
	revisions     *workspacePrepareRevisionFake
	archives      *workspacePrepareArchiveFake
	sources       *workspacePrepareSourceFake
}

func newWorkspacePrepareServiceForTest(t *testing.T, job domain.ExecutionJob) workspacePrepareServiceTestHarness {
	t.Helper()
	capabilities := &workspacePrepareCapabilityFake{}
	executionJobs := &workspacePrepareJobFake{job: job}
	builds := &workspacePrepareBuildFake{}
	revisions := &workspacePrepareRevisionFake{}
	archives := &workspacePrepareArchiveFake{}
	sources := &workspacePrepareSourceFake{}
	service, err := NewWorkspacePrepareService(WorkspacePrepareServiceConfig{CapabilityAuthorizer: capabilities, ExecutionJobs: executionJobs, Builds: builds, WorkspaceRevisions: revisions, RevisionArchives: archives, SourceArchives: sources})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return workspacePrepareServiceTestHarness{service: service, capabilities: capabilities, executionJobs: executionJobs, builds: builds, revisions: revisions, archives: archives, sources: sources}
}

type workspacePrepareCapabilityFake struct{ err error }

func (f *workspacePrepareCapabilityFake) Authorize(context.Context, string, string, string, domain.WorkspaceHelperRole) (domain.WorkspaceHelperCapability, error) {
	return domain.WorkspaceHelperCapability{}, f.err
}

type workspacePrepareJobFake struct {
	job   domain.ExecutionJob
	calls int
	err   error
}

func (f *workspacePrepareJobFake) GetJobByID(context.Context, string) (domain.ExecutionJob, error) {
	f.calls++
	return f.job, f.err
}

type workspacePrepareBuildFake struct {
	calls int
	err   error
}

func (f *workspacePrepareBuildFake) GetByID(context.Context, string) (domain.Build, error) {
	f.calls++
	return domain.Build{}, f.err
}

type workspacePrepareRevisionFake struct {
	revision        domain.WorkspaceRevision
	buildID, nodeID string
	err             error
}

func (f *workspacePrepareRevisionFake) GetPublishedByBuildNode(_ context.Context, buildID string, nodeID string) (domain.WorkspaceRevision, error) {
	f.buildID, f.nodeID = buildID, nodeID
	return f.revision, f.err
}

type workspacePrepareArchiveFake struct {
	contents []byte
	calls    int
}

func (f *workspacePrepareArchiveFake) Open(context.Context, domain.WorkspaceRevisionPublication) (io.ReadCloser, error) {
	f.calls++
	return io.NopCloser(bytes.NewReader(f.contents)), nil
}

type workspacePrepareSourceFake struct {
	contents []byte
	calls    int
	err      error
}

func (f *workspacePrepareSourceFake) OpenSourceArchive(context.Context, domain.Build, domain.ExecutionJob, domain.ExecutionJobSpec) (WorkspacePreparePayload, error) {
	f.calls++
	if f.err != nil {
		return WorkspacePreparePayload{}, f.err
	}
	size := int64(len(f.contents))
	return WorkspacePreparePayload{Archive: io.NopCloser(bytes.NewReader(f.contents)), Publication: domain.WorkspaceRevisionPublication{ContentDigest: "sha256:source", StorageKey: "workspace-revisions/source.tar.gz", SizeBytes: &size}}, nil
}
func workspacePrepareStringPointer(value string) *string { return &value }
