package build

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestAuthorizedReaderGetBuild(t *testing.T) {
	ctx := context.Background()
	buildRepo := repositorymemory.NewBuildRepository()
	build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: time.Now().UTC()}
	if _, createErr := buildRepo.Create(ctx, build); createErr != nil {
		t.Fatalf("create build failed: %v", createErr)
	}

	reader := NewAuthorizedReader(
		NewBuildService(buildRepo, nil, nil),
		authorization.NewAuthorizer(false, buildMembershipLookup{memberships: map[string]domain.ProjectMembership{
			"project-1": {ProjectID: "project-1", UserID: "viewer-1", Role: domain.ProjectMemberRoleViewer},
		}}),
	)
	viewer := authorization.Principal{User: domain.User{ID: "viewer-1"}}
	if got, getErr := reader.GetBuild(ctx, viewer, build.ID); getErr != nil || got.ID != build.ID {
		t.Fatalf("expected viewer build access, got build=%+v err=%v", got, getErr)
	}

	outsider := authorization.Principal{User: domain.User{ID: "outsider-1"}}
	if _, getErr := reader.GetBuild(ctx, outsider, build.ID); !errors.Is(getErr, authorization.ErrForbidden) {
		t.Fatalf("expected outsider denial, got %v", getErr)
	}

	missingScope := authorization.Principal{
		User: domain.User{ID: "viewer-1"},
		APIToken: &authorization.APITokenIdentity{
			ID:     "token-1",
			Scopes: []domain.APITokenScope{domain.APITokenScopeBuildLogs},
		},
	}
	var scopeErr *authorization.MissingAPITokenScopeError
	if _, getErr := reader.GetBuild(ctx, missingScope, build.ID); !errors.As(getErr, &scopeErr) {
		t.Fatalf("expected missing scope error, got %v", getErr)
	}

	scopedPrincipal := missingScope
	scopedPrincipal.APIToken.Scopes = []domain.APITokenScope{domain.APITokenScopeBuildRead}
	if _, getErr := reader.GetBuild(ctx, scopedPrincipal, build.ID); getErr != nil {
		t.Fatalf("expected scoped token access, got %v", getErr)
	}

	if _, getErr := reader.GetBuild(ctx, viewer, "missing"); !errors.Is(getErr, ErrBuildNotFound) {
		t.Fatalf("expected build not found, got %v", getErr)
	}
}

func TestAuthorizedReaderGetBuildAllowsDisabledAuth(t *testing.T) {
	ctx := context.Background()
	buildRepo := repositorymemory.NewBuildRepository()
	build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, CreatedAt: time.Now().UTC()}
	if _, createErr := buildRepo.Create(ctx, build); createErr != nil {
		t.Fatalf("create build failed: %v", createErr)
	}

	reader := NewAuthorizedReader(NewBuildService(buildRepo, nil, nil), authorization.NewAuthorizer(true, nil))
	if _, getErr := reader.GetBuild(ctx, authorization.Principal{}, build.ID); getErr != nil {
		t.Fatalf("expected disabled auth build access, got %v", getErr)
	}
}

func TestAuthorizedReaderBuildFailuresAndStepLogs(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	buildRepo := repositorymemory.NewBuildRepository()
	build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusFailed, CreatedAt: now}
	if _, createErr := buildRepo.CreateQueuedBuild(ctx, build, []domain.BuildStep{
		{ID: "step-0", StepIndex: 0, Name: "failed", Status: domain.BuildStepStatusPending},
		{ID: "step-1", StepIndex: 1, Name: "successful", Status: domain.BuildStepStatusPending},
	}); createErr != nil {
		t.Fatalf("create queued build failed: %v", createErr)
	}
	exitCode := 2
	if _, stepErr := buildRepo.UpdateStepByIndex(ctx, build.ID, 0, repository.StepUpdate{Status: domain.BuildStepStatusFailed, ExitCode: &exitCode, ErrorMessage: authorizedReadStringPointer("runner failure"), StartedAt: &now, FinishedAt: &now}); stepErr != nil {
		t.Fatalf("mark failed step: %v", stepErr)
	}
	if _, stepErr := buildRepo.UpdateStepByIndex(ctx, build.ID, 1, repository.StepUpdate{Status: domain.BuildStepStatusSuccess, StartedAt: &now, FinishedAt: &now}); stepErr != nil {
		t.Fatalf("mark successful step: %v", stepErr)
	}
	if _, statusErr := buildRepo.UpdateStatus(ctx, build.ID, domain.BuildStatusFailed, authorizedReadStringPointer("build failed")); statusErr != nil {
		t.Fatalf("mark build failed: %v", statusErr)
	}

	logSink := logs.NewMemorySink()
	for _, text := range []string{"one", "two", "toolong"} {
		if _, appendErr := logSink.AppendStepLogChunk(ctx, logs.StepLogChunk{BuildID: build.ID, StepID: "step-0", StepIndex: 0, StepName: "failed", Stream: logs.StepLogStreamStderr, ChunkText: text, CreatedAt: now}); appendErr != nil {
			t.Fatalf("append log chunk: %v", appendErr)
		}
	}
	if _, appendErr := logSink.AppendStepLogChunk(ctx, logs.StepLogChunk{BuildID: build.ID, StepID: "step-1", StepIndex: 1, StepName: "successful", Stream: logs.StepLogStreamStdout, ChunkText: strings.Repeat("x", MaxStepLogMaxBytes+1), CreatedAt: now}); appendErr != nil {
		t.Fatalf("append oversized log chunk: %v", appendErr)
	}
	reader := NewAuthorizedReader(
		NewBuildService(buildRepo, nil, logSink),
		authorization.NewAuthorizer(false, buildMembershipLookup{memberships: map[string]domain.ProjectMembership{
			"project-1": {ProjectID: "project-1", UserID: "viewer-1", Role: domain.ProjectMemberRoleViewer},
		}}),
	)
	readPrincipal := authorization.Principal{User: domain.User{ID: "viewer-1"}, APIToken: &authorization.APITokenIdentity{ID: "read", Scopes: []domain.APITokenScope{domain.APITokenScopeBuildRead}}}
	logPrincipal := authorization.Principal{User: domain.User{ID: "viewer-1"}, APIToken: &authorization.APITokenIdentity{ID: "logs", Scopes: []domain.APITokenScope{domain.APITokenScopeBuildRead, domain.APITokenScopeBuildLogs}}}

	failures, failuresErr := reader.GetBuildFailures(ctx, readPrincipal, build.ID)
	if failuresErr != nil {
		t.Fatalf("get failures: %v", failuresErr)
	}
	if failures.Build.Status != domain.BuildStatusFailed || len(failures.FailedSteps) != 1 || failures.FailedSteps[0].Name != "failed" || failures.FailedSteps[0].ExitCode == nil || *failures.FailedSteps[0].ExitCode != exitCode {
		t.Fatalf("unexpected failures: %+v", failures)
	}

	if _, logsErr := reader.GetStepLogs(ctx, readPrincipal, build.ID, 0, StepLogOptions{}); logsErr == nil {
		t.Fatal("expected missing build:logs scope")
	} else {
		var scopeErr *authorization.MissingAPITokenScopeError
		if !errors.As(logsErr, &scopeErr) {
			t.Fatalf("expected missing scope error, got %v", logsErr)
		}
	}

	first, firstErr := reader.GetStepLogs(ctx, logPrincipal, build.ID, 0, StepLogOptions{Limit: 2, MaxBytes: 4})
	if firstErr != nil {
		t.Fatalf("get first page: %v", firstErr)
	}
	if len(first.Chunks) != 1 || first.Chunks[0].ChunkText != "one" || !first.HasMore || first.NextSequence != first.Chunks[0].SequenceNo {
		t.Fatalf("unexpected first page: %+v", first)
	}

	second, secondErr := reader.GetStepLogs(ctx, logPrincipal, build.ID, 0, StepLogOptions{AfterSequence: first.NextSequence, Limit: 2, MaxBytes: 4})
	if secondErr != nil {
		t.Fatalf("get second page: %v", secondErr)
	}
	if len(second.Chunks) != 1 || second.Chunks[0].ChunkText != "two" || !second.HasMore || second.NextSequence != second.Chunks[0].SequenceNo {
		t.Fatalf("unexpected second page: %+v", second)
	}

	if _, oversizedErr := reader.GetStepLogs(ctx, logPrincipal, build.ID, 0, StepLogOptions{AfterSequence: second.NextSequence, MaxBytes: 3}); oversizedErr == nil {
		t.Fatal("expected oversized remaining chunk error")
	} else {
		var chunkTooLargeErr *StepLogChunkTooLargeError
		if !errors.As(oversizedErr, &chunkTooLargeErr) || chunkTooLargeErr.MinimumMaxBytes != len("toolong") {
			t.Fatalf("expected oversized chunk error with minimum budget, got %v", oversizedErr)
		}
	}

	if _, oversizedErr := reader.GetStepLogs(ctx, logPrincipal, build.ID, 1, StepLogOptions{MaxBytes: MaxStepLogMaxBytes}); oversizedErr == nil {
		t.Fatal("expected chunk exceeding server maximum error")
	} else {
		var chunkTooLargeErr *StepLogChunkTooLargeError
		if !errors.As(oversizedErr, &chunkTooLargeErr) || chunkTooLargeErr.MinimumMaxBytes != MaxStepLogMaxBytes+1 {
			t.Fatalf("expected server-oversized chunk error with minimum budget, got %v", oversizedErr)
		}
	}

	empty, emptyErr := reader.GetStepLogs(ctx, logPrincipal, build.ID, 1, StepLogOptions{AfterSequence: 4})
	if emptyErr != nil {
		t.Fatalf("get empty log stream: %v", emptyErr)
	}
	if len(empty.Chunks) != 0 || empty.HasMore || empty.NextSequence != 4 {
		t.Fatalf("unexpected empty log stream: %+v", empty)
	}
}

func TestAuthorizedReaderStepLogsValidationAndBuildStatus(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	buildRepo := repositorymemory.NewBuildRepository()
	reader := NewAuthorizedReader(
		NewBuildService(buildRepo, nil, logs.NewMemorySink()),
		authorization.NewAuthorizer(true, nil),
	)
	for _, status := range []domain.BuildStatus{
		domain.BuildStatusPending,
		domain.BuildStatusQueued,
		domain.BuildStatusPreparing,
		domain.BuildStatusRunning,
		domain.BuildStatusSuccess,
		domain.BuildStatusFailed,
		domain.BuildStatusCanceled,
	} {
		build := domain.Build{ID: string(status), ProjectID: "project-1", Status: status, CreatedAt: now}
		if _, createErr := buildRepo.Create(ctx, build); createErr != nil {
			t.Fatalf("create %s build: %v", status, createErr)
		}
		failures, failuresErr := reader.GetBuildFailures(ctx, authorization.Principal{}, build.ID)
		if failuresErr != nil {
			t.Fatalf("get %s failures: %v", status, failuresErr)
		}
		if failures.Build.Status != status || len(failures.FailedSteps) != 0 {
			t.Fatalf("unexpected %s result: %+v", status, failures)
		}
	}

	for _, options := range []struct {
		stepIndex int
		options   StepLogOptions
		want      error
	}{
		{stepIndex: -1, want: ErrStepLogIndexInvalid},
		{stepIndex: 0, options: StepLogOptions{AfterSequence: -1}, want: ErrStepLogCursorInvalid},
		{stepIndex: 0, options: StepLogOptions{Limit: MaxStepLogLimit + 1}, want: ErrStepLogLimitInvalid},
		{stepIndex: 0, options: StepLogOptions{MaxBytes: MaxStepLogMaxBytes + 1}, want: ErrStepLogMaxBytesInvalid},
	} {
		if _, logsErr := reader.GetStepLogs(ctx, authorization.Principal{}, "failed", options.stepIndex, options.options); !errors.Is(logsErr, options.want) {
			t.Fatalf("expected %v, got %v", options.want, logsErr)
		}
	}
}

func authorizedReadStringPointer(value string) *string {
	return &value
}

type buildMembershipLookup struct {
	memberships map[string]domain.ProjectMembership
}

func (s buildMembershipLookup) GetProjectMembership(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	membership, ok := s.memberships[projectID]
	if !ok || membership.UserID != userID {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return membership, nil
}
