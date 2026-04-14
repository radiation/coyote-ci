package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildService_RetryJob_CreatesNewAttemptAndPreservesHistory(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	execRepo := memoryrepo.NewExecutionJobRepository()
	outputRepo := memoryrepo.NewExecutionJobOutputRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)
	svc.SetExecutionJobOutputRepository(outputRepo)

	now := time.Now().UTC()
	sourceBuild := domain.Build{
		ID:            "build-1",
		ProjectID:     "project-1",
		Status:        domain.BuildStatusFailed,
		AttemptNumber: 1,
		CreatedAt:     now,
		RepoURL:       stringPtr("https://github.com/acme/repo.git"),
		Ref:           stringPtr("main"),
		CommitSHA:     stringPtr("abc123"),
	}
	steps := []domain.BuildStep{{
		ID:             "step-1",
		BuildID:        sourceBuild.ID,
		StepIndex:      0,
		Name:           "verify",
		Command:        "sh",
		Args:           []string{"-c", "go test ./..."},
		Env:            map[string]string{"GOFLAGS": "-mod=readonly"},
		WorkingDir:     "backend",
		TimeoutSeconds: 120,
		Status:         domain.BuildStepStatusFailed,
	}}
	if _, err := buildRepo.CreateQueuedBuild(context.Background(), sourceBuild, steps); err != nil {
		t.Fatalf("create source build failed: %v", err)
	}

	lineageRoot := "job-1"
	timeout := 120
	failedJob := domain.ExecutionJob{
		ID:               "job-1",
		BuildID:          sourceBuild.ID,
		StepID:           "step-1",
		Name:             "verify",
		StepIndex:        0,
		AttemptNumber:    1,
		LineageRootJobID: &lineageRoot,
		Status:           domain.ExecutionJobStatusFailed,
		Image:            "golang:1.24",
		WorkingDir:       "backend",
		Command:          []string{"sh", "-c", "go test ./..."},
		Environment:      map[string]string{"GOFLAGS": "-mod=readonly"},
		TimeoutSeconds:   &timeout,
		Source: domain.SourceSnapshotRef{
			RepositoryURL: "https://github.com/acme/repo.git",
			CommitSHA:     "abc123",
			RefName:       stringPtr("main"),
		},
		SpecVersion:      1,
		ResolvedSpecJSON: `{"version":1}`,
		CreatedAt:        now,
		FinishedAt:       timePtr(now.Add(time.Minute)),
		ErrorMessage:     stringPtr("failed"),
		ExitCode:         intPtr(1),
	}
	if _, err := execRepo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{failedJob}); err != nil {
		t.Fatalf("seed failed job failed: %v", err)
	}

	retryResult, err := svc.RetryJob(context.Background(), failedJob.ID)
	if err != nil {
		t.Fatalf("retry job failed: %v", err)
	}

	if retryResult.Build.ID == sourceBuild.ID {
		t.Fatal("expected retry to create a new build attempt")
	}
	if retryResult.Build.RerunOfBuildID == nil || *retryResult.Build.RerunOfBuildID != sourceBuild.ID {
		t.Fatalf("expected rerun_of_build_id to reference source build, got %v", retryResult.Build.RerunOfBuildID)
	}
	if retryResult.Build.AttemptNumber != 2 {
		t.Fatalf("expected build attempt number 2, got %d", retryResult.Build.AttemptNumber)
	}

	createdJob := retryResult.Job
	if createdJob.AttemptNumber != 2 {
		t.Fatalf("expected retry attempt number 2, got %d", createdJob.AttemptNumber)
	}
	if createdJob.RetryOfJobID == nil || *createdJob.RetryOfJobID != failedJob.ID {
		t.Fatalf("expected retry_of_job_id=%s, got %v", failedJob.ID, createdJob.RetryOfJobID)
	}
	if createdJob.LineageRootJobID == nil || *createdJob.LineageRootJobID != lineageRoot {
		t.Fatalf("expected lineage root %s, got %v", lineageRoot, createdJob.LineageRootJobID)
	}
	if createdJob.Source.CommitSHA != failedJob.Source.CommitSHA || createdJob.ResolvedSpecJSON != failedJob.ResolvedSpecJSON {
		t.Fatal("expected retry attempt to preserve source identity and resolved spec")
	}

	storedOld, err := execRepo.GetJobByID(context.Background(), failedJob.ID)
	if err != nil {
		t.Fatalf("reload old job failed: %v", err)
	}
	if storedOld.Status != domain.ExecutionJobStatusFailed {
		t.Fatalf("expected old failed job history unchanged, got %q", storedOld.Status)
	}
}

func TestBuildService_RetryJob_RejectsNonTerminalJobs(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)

	now := time.Now().UTC()
	timeout := 30
	queuedJob := domain.ExecutionJob{
		ID:             "job-queued",
		BuildID:        "build-1",
		StepID:         "step-1",
		Name:           "verify",
		StepIndex:      0,
		AttemptNumber:  1,
		Status:         domain.ExecutionJobStatusQueued,
		Image:          "golang:1.24",
		WorkingDir:     ".",
		Command:        []string{"sh", "-c", "go test ./..."},
		Environment:    map[string]string{},
		TimeoutSeconds: &timeout,
		Source: domain.SourceSnapshotRef{
			RepositoryURL: "https://github.com/acme/repo.git",
			CommitSHA:     "abc123",
		},
		SpecVersion:      1,
		ResolvedSpecJSON: `{"version":1}`,
		CreatedAt:        now,
	}
	if _, err := execRepo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{queuedJob}); err != nil {
		t.Fatalf("seed queued job failed: %v", err)
	}

	_, err := svc.RetryJob(context.Background(), queuedJob.ID)
	if !errors.Is(err, ErrExecutionJobNotRetryable) {
		t.Fatalf("expected ErrExecutionJobNotRetryable, got %v", err)
	}
}

func TestBuildService_RerunBuildFromStep_CreatesLinkedBuildAttemptAndPreservesSpec(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)

	now := time.Now().UTC()
	sourceBuild := domain.Build{
		ID:            "build-1",
		ProjectID:     "project-1",
		Status:        domain.BuildStatusFailed,
		AttemptNumber: 1,
		CreatedAt:     now,
		RepoURL:       stringPtr("https://github.com/acme/repo.git"),
		Ref:           stringPtr("main"),
		CommitSHA:     stringPtr("abc123"),
	}
	sourceSteps := []domain.BuildStep{
		{ID: "step-0", BuildID: sourceBuild.ID, StepIndex: 0, Name: "setup", Command: "sh", Args: []string{"-c", "echo setup"}, Env: map[string]string{}, WorkingDir: ".", TimeoutSeconds: 60, Status: domain.BuildStepStatusSuccess},
		{ID: "step-1", BuildID: sourceBuild.ID, StepIndex: 1, Name: "test", Command: "sh", Args: []string{"-c", "go test ./..."}, Env: map[string]string{"A": "1"}, WorkingDir: "backend", TimeoutSeconds: 120, Status: domain.BuildStepStatusFailed},
		{ID: "step-2", BuildID: sourceBuild.ID, StepIndex: 2, Name: "package", Command: "sh", Args: []string{"-c", "go build ./..."}, Env: map[string]string{}, WorkingDir: "backend", TimeoutSeconds: 120, Status: domain.BuildStepStatusPending},
	}
	if _, err := buildRepo.CreateQueuedBuild(context.Background(), sourceBuild, sourceSteps); err != nil {
		t.Fatalf("create source build failed: %v", err)
	}

	timeout := 120
	jobs := []domain.ExecutionJob{
		{ID: "job-1a", BuildID: sourceBuild.ID, StepID: "step-1", Name: "test", StepIndex: 1, AttemptNumber: 1, Status: domain.ExecutionJobStatusFailed, Image: "golang:1.24", WorkingDir: "backend", Command: []string{"sh", "-c", "go test ./..."}, Environment: map[string]string{"A": "1"}, TimeoutSeconds: &timeout, Source: domain.SourceSnapshotRef{RepositoryURL: "https://github.com/acme/repo.git", CommitSHA: "abc123", RefName: stringPtr("main")}, SpecVersion: 1, ResolvedSpecJSON: `{"step":"test","attempt":1}`, CreatedAt: now.Add(time.Minute), FinishedAt: timePtr(now.Add(2 * time.Minute)), ErrorMessage: stringPtr("failed"), ExitCode: intPtr(1)},
		{ID: "job-2a", BuildID: sourceBuild.ID, StepID: "step-2", Name: "package", StepIndex: 2, AttemptNumber: 1, Status: domain.ExecutionJobStatusQueued, Image: "golang:1.24", WorkingDir: "backend", Command: []string{"sh", "-c", "go build ./..."}, Environment: map[string]string{}, TimeoutSeconds: &timeout, Source: domain.SourceSnapshotRef{RepositoryURL: "https://github.com/acme/repo.git", CommitSHA: "abc123", RefName: stringPtr("main")}, SpecVersion: 1, ResolvedSpecJSON: `{"step":"package","attempt":1}`, CreatedAt: now.Add(3 * time.Minute)},
	}
	for i := range jobs {
		root := jobs[i].ID
		jobs[i].LineageRootJobID = &root
	}
	if _, err := execRepo.CreateJobsForBuild(context.Background(), jobs); err != nil {
		t.Fatalf("seed jobs failed: %v", err)
	}

	newBuild, err := svc.RerunBuildFromStep(context.Background(), sourceBuild.ID, 1)
	if err != nil {
		t.Fatalf("rerun build failed: %v", err)
	}
	if newBuild.RerunOfBuildID == nil || *newBuild.RerunOfBuildID != sourceBuild.ID {
		t.Fatalf("expected rerun_of_build_id=%s, got %v", sourceBuild.ID, newBuild.RerunOfBuildID)
	}
	if newBuild.RerunFromStepIdx == nil || *newBuild.RerunFromStepIdx != 1 {
		t.Fatalf("expected rerun_from_step_index=1, got %v", newBuild.RerunFromStepIdx)
	}
	if newBuild.AttemptNumber != 2 {
		t.Fatalf("expected build attempt 2, got %d", newBuild.AttemptNumber)
	}

	newJobs, err := execRepo.GetJobsByBuildID(context.Background(), newBuild.ID)
	if err != nil {
		t.Fatalf("get new build jobs failed: %v", err)
	}
	if len(newJobs) != 2 {
		t.Fatalf("expected two jobs in rerun build, got %d", len(newJobs))
	}
	if newJobs[0].AttemptNumber != 2 || newJobs[0].RetryOfJobID == nil || *newJobs[0].RetryOfJobID != "job-1a" {
		t.Fatalf("expected first rerun job to link to job-1a with attempt 2, got %+v", newJobs[0])
	}
	if newJobs[1].AttemptNumber != 2 || newJobs[1].RetryOfJobID == nil || *newJobs[1].RetryOfJobID != "job-2a" {
		t.Fatalf("expected second rerun job to link to job-2a with attempt 2, got %+v", newJobs[1])
	}
	if newJobs[0].Source.CommitSHA != "abc123" || newJobs[0].ResolvedSpecJSON != `{"step":"test","attempt":1}` {
		t.Fatal("expected rerun job to preserve source identity and resolved spec")
	}
}

func TestBuildService_RerunBuildFromStep_RejectsInvalidStepIndex(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)

	now := time.Now().UTC()
	build := domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusFailed, AttemptNumber: 1, CreatedAt: now}
	steps := []domain.BuildStep{{ID: "step-0", BuildID: build.ID, StepIndex: 0, Name: "only", Command: "sh", Args: []string{"-c", "echo only"}, Env: map[string]string{}, WorkingDir: ".", TimeoutSeconds: 10, Status: domain.BuildStepStatusFailed}}
	if _, err := buildRepo.CreateQueuedBuild(context.Background(), build, steps); err != nil {
		t.Fatalf("create build failed: %v", err)
	}

	_, err := svc.RerunBuildFromStep(context.Background(), build.ID, 5)
	if !errors.Is(err, ErrInvalidRerunStepIndex) {
		t.Fatalf("expected ErrInvalidRerunStepIndex, got %v", err)
	}
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
