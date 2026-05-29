package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
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
	_, createBuildErr := buildRepo.CreateQueuedBuild(context.Background(), sourceBuild, steps)
	if createBuildErr != nil {
		t.Fatalf("create source build failed: %v", createBuildErr)
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
	_, createJobsErr := execRepo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{failedJob})
	if createJobsErr != nil {
		t.Fatalf("seed failed job failed: %v", createJobsErr)
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
	_, createJobsErr := execRepo.CreateJobsForBuild(context.Background(), []domain.ExecutionJob{queuedJob})
	if createJobsErr != nil {
		t.Fatalf("seed queued job failed: %v", createJobsErr)
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
	_, createBuildErr := buildRepo.CreateQueuedBuild(context.Background(), sourceBuild, sourceSteps)
	if createBuildErr != nil {
		t.Fatalf("create source build failed: %v", createBuildErr)
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
	_, createJobsErr := execRepo.CreateJobsForBuild(context.Background(), jobs)
	if createJobsErr != nil {
		t.Fatalf("seed jobs failed: %v", createJobsErr)
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
	_, createBuildErr := buildRepo.CreateQueuedBuild(context.Background(), build, steps)
	if createBuildErr != nil {
		t.Fatalf("create build failed: %v", createBuildErr)
	}

	_, err := svc.RerunBuildFromStep(context.Background(), build.ID, 5)
	if !errors.Is(err, ErrInvalidRerunStepIndex) {
		t.Fatalf("expected ErrInvalidRerunStepIndex, got %v", err)
	}
}

func TestBuildService_RerunBuild_CreatesNewQueuedBuildForTerminalBuilds(t *testing.T) {
	terminalStatuses := []domain.BuildStatus{
		domain.BuildStatusSuccess,
		domain.BuildStatusFailed,
		domain.BuildStatusCanceled,
	}

	for _, terminalStatus := range terminalStatuses {
		terminalStatus := terminalStatus
		t.Run(string(terminalStatus), func(t *testing.T) {
			buildRepo := memoryrepo.NewBuildRepository()
			execRepo := memoryrepo.NewExecutionJobRepository()
			svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
			svc.SetExecutionJobRepository(execRepo)

			sourceBuild, sourceSteps := seedRerunnableBuild(t, buildRepo, terminalStatus)

			newBuild, err := svc.RerunBuild(context.Background(), sourceBuild.ID)
			if err != nil {
				t.Fatalf("rerun build failed: %v", err)
			}

			if newBuild.ID == sourceBuild.ID {
				t.Fatal("expected rerun to create a new build id")
			}
			if newBuild.Status != domain.BuildStatusQueued {
				t.Fatalf("expected new build status queued, got %q", newBuild.Status)
			}
			if newBuild.QueuedAt == nil {
				t.Fatal("expected new build to have queued_at")
			}
			if newBuild.StartedAt != nil || newBuild.FinishedAt != nil {
				t.Fatalf("expected fresh timestamps without start/finish, got started=%v finished=%v", newBuild.StartedAt, newBuild.FinishedAt)
			}
			if newBuild.RerunOfBuildID == nil || *newBuild.RerunOfBuildID != sourceBuild.ID {
				t.Fatalf("expected rerun_of_build_id=%s, got %v", sourceBuild.ID, newBuild.RerunOfBuildID)
			}
			if newBuild.RerunFromStepIdx != nil {
				t.Fatalf("expected whole-build rerun to leave rerun_from_step_index empty, got %v", newBuild.RerunFromStepIdx)
			}
			if newBuild.AttemptNumber != 2 {
				t.Fatalf("expected attempt number 2, got %d", newBuild.AttemptNumber)
			}
			if newBuild.ProjectID != sourceBuild.ProjectID || newBuild.JobID == nil || *newBuild.JobID != *sourceBuild.JobID {
				t.Fatalf("expected project/job context to be preserved, got project=%q job=%v", newBuild.ProjectID, newBuild.JobID)
			}
			if newBuild.Source == nil || newBuild.Source.CommitSHA == nil || *newBuild.Source.CommitSHA != "abc123" {
				t.Fatalf("expected source commit to be preserved, got %+v", newBuild.Source)
			}
			if newBuild.Trigger.Kind != domain.BuildTriggerKindWebhook || newBuild.Trigger.Ref == nil || *newBuild.Trigger.Ref != "refs/heads/main" {
				t.Fatalf("expected trigger metadata to be preserved, got %+v", newBuild.Trigger)
			}

			newSteps, err := buildRepo.GetStepsByBuildID(context.Background(), newBuild.ID)
			if err != nil {
				t.Fatalf("get new steps failed: %v", err)
			}
			if len(newSteps) != len(sourceSteps) {
				t.Fatalf("expected %d new steps, got %d", len(sourceSteps), len(newSteps))
			}
			for index, newStep := range newSteps {
				if newStep.ID == sourceSteps[index].ID {
					t.Fatalf("expected step %d to get a new id", index)
				}
				if newStep.Status != domain.BuildStepStatusPending {
					t.Fatalf("expected step %d pending, got %q", index, newStep.Status)
				}
				if newStep.StartedAt != nil || newStep.FinishedAt != nil || newStep.ExitCode != nil || newStep.Stdout != nil || newStep.Stderr != nil || newStep.ErrorMessage != nil {
					t.Fatalf("expected step %d execution fields to be fresh, got %+v", index, newStep)
				}
			}

			newJobs, err := execRepo.GetJobsByBuildID(context.Background(), newBuild.ID)
			if err != nil {
				t.Fatalf("get new jobs failed: %v", err)
			}
			if len(newJobs) != len(sourceSteps) {
				t.Fatalf("expected durable jobs for rerun build, got %d", len(newJobs))
			}
			for _, newJob := range newJobs {
				if newJob.Status != domain.ExecutionJobStatusQueued {
					t.Fatalf("expected rerun job queued, got %q", newJob.Status)
				}
				if newJob.FinishedAt != nil || newJob.ExitCode != nil || newJob.ErrorMessage != nil || len(newJob.OutputRefs) != 0 {
					t.Fatalf("expected rerun job execution result fields to be fresh, got %+v", newJob)
				}
			}

			reloadedSource, err := buildRepo.GetByID(context.Background(), sourceBuild.ID)
			if err != nil {
				t.Fatalf("reload source build failed: %v", err)
			}
			if reloadedSource.Status != terminalStatus || reloadedSource.ID != sourceBuild.ID {
				t.Fatalf("expected source build unchanged, got %+v", reloadedSource)
			}
			reloadedSourceSteps, err := buildRepo.GetStepsByBuildID(context.Background(), sourceBuild.ID)
			if err != nil {
				t.Fatalf("reload source steps failed: %v", err)
			}
			if reloadedSourceSteps[1].Status != sourceSteps[1].Status || reloadedSourceSteps[1].ExitCode == nil || *reloadedSourceSteps[1].ExitCode != 1 {
				t.Fatalf("expected source step execution result unchanged, got %+v", reloadedSourceSteps[1])
			}
		})
	}
}

func TestBuildService_RerunBuild_DoesNotCopyArtifactsOrLogs(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	artifactRepo := &fakeArtifactRepository{}
	logSink := logs.NewMemorySink()
	svc := NewBuildService(buildRepo, nil, logSink)
	svc.SetArtifactPersistence(artifactRepo, nil, "")

	sourceBuild, _ := seedRerunnableBuild(t, buildRepo, domain.BuildStatusSuccess)
	createdArtifact, err := artifactRepo.Create(context.Background(), domain.BuildArtifact{
		ID:              "artifact-1",
		BuildID:         sourceBuild.ID,
		Name:            "dist",
		LogicalPath:     "dist/app.tar.gz",
		StorageKey:      "build-1/dist/app.tar.gz",
		StorageProvider: domain.StorageProviderFilesystem,
		CreatedAt:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed artifact failed: %v", err)
	}
	if createdArtifact.ID == "" {
		t.Fatal("expected seeded artifact id")
	}
	writeErr := logSink.WriteStepLog(context.Background(), sourceBuild.ID, "test", "original log line")
	if writeErr != nil {
		t.Fatalf("seed log failed: %v", writeErr)
	}

	newBuild, err := svc.RerunBuild(context.Background(), sourceBuild.ID)
	if err != nil {
		t.Fatalf("rerun build failed: %v", err)
	}

	originalArtifacts, err := svc.GetBuildArtifacts(context.Background(), sourceBuild.ID)
	if err != nil {
		t.Fatalf("get original artifacts failed: %v", err)
	}
	if len(originalArtifacts) != 1 {
		t.Fatalf("expected original artifact to remain, got %d", len(originalArtifacts))
	}
	newArtifacts, err := svc.GetBuildArtifacts(context.Background(), newBuild.ID)
	if err != nil {
		t.Fatalf("get new artifacts failed: %v", err)
	}
	if len(newArtifacts) != 0 {
		t.Fatalf("expected rerun build to start without copied artifacts, got %d", len(newArtifacts))
	}

	originalLogs, err := svc.GetBuildLogs(context.Background(), sourceBuild.ID)
	if err != nil {
		t.Fatalf("get original logs failed: %v", err)
	}
	if len(originalLogs) != 1 {
		t.Fatalf("expected original log to remain, got %d", len(originalLogs))
	}
	newLogs, err := svc.GetBuildLogs(context.Background(), newBuild.ID)
	if err != nil {
		t.Fatalf("get new logs failed: %v", err)
	}
	if len(newLogs) != 0 {
		t.Fatalf("expected rerun build to start without copied logs, got %d", len(newLogs))
	}
}

func TestBuildService_RerunBuild_AssignsFreshPerJobNumbersAndImmediateLineage(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	execRepo := memoryrepo.NewExecutionJobRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
	svc.SetExecutionJobRepository(execRepo)

	sourceBuild, sourceSteps := seedRerunnableBuild(t, buildRepo, domain.BuildStatusSuccess)
	if sourceBuild.BuildNumber != 1 {
		t.Fatalf("expected source build number 1, got %d", sourceBuild.BuildNumber)
	}

	firstRerun, err := svc.RerunBuild(context.Background(), sourceBuild.ID)
	if err != nil {
		t.Fatalf("first rerun failed: %v", err)
	}
	secondRerun, err := svc.RerunBuild(context.Background(), sourceBuild.ID)
	if err != nil {
		t.Fatalf("second rerun failed: %v", err)
	}
	terminalizedFirstRerun, err := buildRepo.UpdateStatus(context.Background(), firstRerun.ID, domain.BuildStatusSuccess, nil)
	if err != nil {
		t.Fatalf("terminalize first rerun failed: %v", err)
	}
	chainedRerun, err := svc.RerunBuild(context.Background(), firstRerun.ID)
	if err != nil {
		t.Fatalf("rerun of rerun failed: %v", err)
	}

	if firstRerun.BuildNumber != 2 {
		t.Fatalf("expected first rerun build number 2, got %d", firstRerun.BuildNumber)
	}
	if secondRerun.BuildNumber != 3 {
		t.Fatalf("expected second rerun build number 3, got %d", secondRerun.BuildNumber)
	}
	if chainedRerun.BuildNumber != 4 {
		t.Fatalf("expected chained rerun build number 4, got %d", chainedRerun.BuildNumber)
	}
	if firstRerun.RerunOfBuildID == nil || *firstRerun.RerunOfBuildID != sourceBuild.ID {
		t.Fatalf("expected first rerun_of_build_id=%s, got %v", sourceBuild.ID, firstRerun.RerunOfBuildID)
	}
	if secondRerun.RerunOfBuildID == nil || *secondRerun.RerunOfBuildID != sourceBuild.ID {
		t.Fatalf("expected second rerun_of_build_id=%s, got %v", sourceBuild.ID, secondRerun.RerunOfBuildID)
	}
	if chainedRerun.RerunOfBuildID == nil || *chainedRerun.RerunOfBuildID != terminalizedFirstRerun.ID {
		t.Fatalf("expected chained rerun_of_build_id=%s, got %v", terminalizedFirstRerun.ID, chainedRerun.RerunOfBuildID)
	}
	if sourceBuild.BuildNumber == firstRerun.BuildNumber {
		t.Fatalf("expected rerun to use a new build number, both were %d", firstRerun.BuildNumber)
	}

	reloadedSource, err := buildRepo.GetByID(context.Background(), sourceBuild.ID)
	if err != nil {
		t.Fatalf("reload source build failed: %v", err)
	}
	if reloadedSource.BuildNumber != sourceBuild.BuildNumber {
		t.Fatalf("expected source build number to remain %d, got %d", sourceBuild.BuildNumber, reloadedSource.BuildNumber)
	}
	if reloadedSource.RerunOfBuildID != nil {
		t.Fatalf("expected source rerun_of_build_id to remain nil, got %v", reloadedSource.RerunOfBuildID)
	}

	reloadedSteps, err := buildRepo.GetStepsByBuildID(context.Background(), sourceBuild.ID)
	if err != nil {
		t.Fatalf("reload source steps failed: %v", err)
	}
	if len(reloadedSteps) != len(sourceSteps) {
		t.Fatalf("expected %d source steps, got %d", len(sourceSteps), len(reloadedSteps))
	}
}

func TestBuildService_RerunBuild_RejectsActiveBuilds(t *testing.T) {
	activeStatuses := []domain.BuildStatus{
		domain.BuildStatusPending,
		domain.BuildStatusQueued,
		domain.BuildStatusPreparing,
		domain.BuildStatusRunning,
	}

	for _, activeStatus := range activeStatuses {
		activeStatus := activeStatus
		t.Run(string(activeStatus), func(t *testing.T) {
			buildRepo := memoryrepo.NewBuildRepository()
			svc := NewBuildService(buildRepo, nil, &fakeLogSink{})
			sourceBuild, _ := seedRerunnableBuild(t, buildRepo, domain.BuildStatusSuccess)

			updatedBuild, err := buildRepo.UpdateStatus(context.Background(), sourceBuild.ID, activeStatus, nil)
			if err != nil {
				t.Fatalf("set active build status failed: %v", err)
			}
			if updatedBuild.Status != activeStatus {
				t.Fatalf("expected active status %q, got %q", activeStatus, updatedBuild.Status)
			}

			_, err = svc.RerunBuild(context.Background(), sourceBuild.ID)
			if !errors.Is(err, ErrInvalidBuildStatusTransition) {
				t.Fatalf("expected ErrInvalidBuildStatusTransition, got %v", err)
			}
		})
	}
}

func TestBuildService_RerunBuild_RejectsMissingBuildContext(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	svc := NewBuildService(buildRepo, nil, &fakeLogSink{})

	_, err := svc.RerunBuild(context.Background(), "missing")
	if !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound, got %v", err)
	}

	sourceBuild := domain.Build{ID: "build-empty", ProjectID: "project-1", Status: domain.BuildStatusSuccess, CreatedAt: time.Now().UTC()}
	_, createErr := buildRepo.Create(context.Background(), sourceBuild)
	if createErr != nil {
		t.Fatalf("seed empty build failed: %v", createErr)
	}
	_, err = svc.RerunBuild(context.Background(), sourceBuild.ID)
	if !errors.Is(err, ErrBuildRerunUnavailable) {
		t.Fatalf("expected ErrBuildRerunUnavailable, got %v", err)
	}
}

func seedRerunnableBuild(t *testing.T, buildRepo *memoryrepo.BuildRepository, status domain.BuildStatus) (domain.Build, []domain.BuildStep) {
	t.Helper()
	now := time.Now().UTC()
	jobID := "job-1"
	pipelineConfig := "version: 1\nimage: golang:1.24\nsteps:\n  - name: setup\n    run: echo setup\n  - name: test\n    run: go test ./...\n"
	pipelineName := "ci"
	pipelineSource := "repo"
	pipelinePath := ".coyote/pipeline.yml"
	sourceBuild := domain.Build{
		ID:                 "build-" + string(status),
		ProjectID:          "project-1",
		JobID:              &jobID,
		Priority:           7,
		Status:             domain.BuildStatusQueued,
		AttemptNumber:      1,
		CreatedAt:          now,
		PipelineConfigYAML: &pipelineConfig,
		PipelineName:       &pipelineName,
		PipelineSource:     &pipelineSource,
		PipelinePath:       &pipelinePath,
		Source:             domain.NewSourceSpec("https://github.com/acme/repo.git", "refs/heads/main", "abc123"),
		RepoURL:            stringPtr("https://github.com/acme/repo.git"),
		Ref:                stringPtr("refs/heads/main"),
		CommitSHA:          stringPtr("abc123"),
		Trigger: domain.BuildTrigger{
			Kind:          domain.BuildTriggerKindWebhook,
			SCMProvider:   stringPtr("github"),
			EventType:     stringPtr("push"),
			RepositoryURL: stringPtr("https://github.com/acme/repo.git"),
			Ref:           stringPtr("refs/heads/main"),
			CommitSHA:     stringPtr("abc123"),
			Actor:         stringPtr("octocat"),
		},
		RequestedImageRef: stringPtr("golang:1.24"),
		ImageSourceKind:   domain.ImageSourceKindExternal,
	}
	sourceSteps := []domain.BuildStep{
		{ID: "step-0", BuildID: sourceBuild.ID, StepIndex: 0, NodeID: "setup", Name: "setup", Command: "sh", Args: []string{"-c", "echo setup"}, Env: map[string]string{}, WorkingDir: ".", TimeoutSeconds: 60, Status: domain.BuildStepStatusSuccess},
		{ID: "step-1", BuildID: sourceBuild.ID, StepIndex: 1, NodeID: "test", DependsOnNodes: []string{"setup"}, Name: "test", Command: "sh", Args: []string{"-c", "go test ./..."}, Env: map[string]string{"GOFLAGS": "-mod=readonly"}, WorkingDir: "backend", TimeoutSeconds: 120, ArtifactPaths: []string{"dist/*"}, Status: domain.BuildStepStatusFailed, StartedAt: timePtr(now.Add(time.Minute)), FinishedAt: timePtr(now.Add(2 * time.Minute)), ExitCode: intPtr(1), Stdout: stringPtr("test output"), Stderr: stringPtr("test failure"), ErrorMessage: stringPtr("failed")},
	}
	createdBuild, err := buildRepo.CreateQueuedBuild(context.Background(), sourceBuild, sourceSteps)
	if err != nil {
		t.Fatalf("seed build failed: %v", err)
	}
	errorMessage := stringPtr("terminal source")
	updatedBuild, err := buildRepo.UpdateStatus(context.Background(), createdBuild.ID, status, errorMessage)
	if err != nil {
		t.Fatalf("terminalize build failed: %v", err)
	}
	createdSteps, err := buildRepo.GetStepsByBuildID(context.Background(), createdBuild.ID)
	if err != nil {
		t.Fatalf("reload source steps failed: %v", err)
	}
	return updatedBuild, createdSteps
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
