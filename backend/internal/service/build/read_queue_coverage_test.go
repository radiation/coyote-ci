package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type readQueueExecutionJobRepo struct {
	fakeExecutionJobCancelRepository
	jobsByBuild    []domain.ExecutionJob
	jobsByBuildErr error
	jobByID        domain.ExecutionJob
	jobByIDErr     error
	jobByStep      domain.ExecutionJob
	jobByStepErr   error
	claimNextJob   domain.ExecutionJob
	claimNextOK    bool
	claimNextErr   error
	claimByStepJob domain.ExecutionJob
	claimByStepOK  bool
	claimByStepErr error
	renewJob       domain.ExecutionJob
	renewOutcome   repository.StepCompletionOutcome
	renewErr       error
}

func (r *readQueueExecutionJobRepo) GetJobsByBuildID(_ context.Context, _ string) ([]domain.ExecutionJob, error) {
	if r.jobsByBuildErr != nil {
		return nil, r.jobsByBuildErr
	}
	return append([]domain.ExecutionJob(nil), r.jobsByBuild...), nil
}

func (r *readQueueExecutionJobRepo) GetJobByID(_ context.Context, _ string) (domain.ExecutionJob, error) {
	if r.jobByIDErr != nil {
		return domain.ExecutionJob{}, r.jobByIDErr
	}
	return r.jobByID, nil
}

func (r *readQueueExecutionJobRepo) GetJobByStepID(_ context.Context, _ string) (domain.ExecutionJob, error) {
	if r.jobByStepErr != nil {
		return domain.ExecutionJob{}, r.jobByStepErr
	}
	return r.jobByStep, nil
}

func (r *readQueueExecutionJobRepo) ClaimNextRunnableJob(_ context.Context, _ repository.StepClaim) (domain.ExecutionJob, bool, error) {
	return r.claimNextJob, r.claimNextOK, r.claimNextErr
}

func (r *readQueueExecutionJobRepo) ClaimJobByStepID(_ context.Context, _ string, _ repository.StepClaim) (domain.ExecutionJob, bool, error) {
	return r.claimByStepJob, r.claimByStepOK, r.claimByStepErr
}

func (r *readQueueExecutionJobRepo) RenewJobLease(_ context.Context, _ string, _ string, _ time.Time) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return r.renewJob, r.renewOutcome, r.renewErr
}

type readQueueExecutionOutputRepo struct {
	outputsByBuild []domain.ExecutionJobOutput
	outputsByJob   []domain.ExecutionJobOutput
}

func (r *readQueueExecutionOutputRepo) CreateMany(_ context.Context, outputs []domain.ExecutionJobOutput) ([]domain.ExecutionJobOutput, error) {
	return outputs, nil
}

func (r *readQueueExecutionOutputRepo) ListByBuildID(_ context.Context, _ string) ([]domain.ExecutionJobOutput, error) {
	return append([]domain.ExecutionJobOutput(nil), r.outputsByBuild...), nil
}

func (r *readQueueExecutionOutputRepo) ListByJobID(_ context.Context, _ string) ([]domain.ExecutionJobOutput, error) {
	return append([]domain.ExecutionJobOutput(nil), r.outputsByJob...), nil
}

type readQueueLogReader struct {
	buildLogs []logs.BuildLogLine
	chunks    []logs.StepLogChunk
	err       error
}

func (r *readQueueLogReader) WriteStepLog(context.Context, string, string, string) error {
	return nil
}

func (r *readQueueLogReader) GetBuildLogs(_ context.Context, _ string) ([]logs.BuildLogLine, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]logs.BuildLogLine(nil), r.buildLogs...), nil
}

func (r *readQueueLogReader) ListStepLogChunks(_ context.Context, _ string, _ int, _ int64, _ int) ([]logs.StepLogChunk, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]logs.StepLogChunk(nil), r.chunks...), nil
}

func TestBuildService_ReadQueue_ListAndClaimWrappers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 19, 0, 0, 0, time.UTC)
	jobID := "job-1"
	repo := &fakeBuildRepository{
		build: domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, Status: domain.BuildStatusRunning, CreatedAt: now},
		steps: []domain.BuildStep{
			{BuildID: "build-1", StepIndex: 0, Status: domain.BuildStepStatusPending},
			{BuildID: "build-1", StepIndex: 1, Status: domain.BuildStepStatusPending},
			{BuildID: "build-1", StepIndex: 2, Status: domain.BuildStepStatusRunning, LeaseExpiresAt: timePtr(now.Add(-time.Minute))},
		},
	}
	svc := NewBuildService(repo, nil, nil)

	activeBuilds, activeErr := svc.ListActiveBuilds(ctx)
	if activeErr != nil || len(activeBuilds) != 1 || activeBuilds[0].ID != "build-1" {
		t.Fatalf("unexpected active builds result: builds=%+v err=%v", activeBuilds, activeErr)
	}

	pagedBuilds, pagedErr := svc.ListBuildsPaged(ctx, repository.ListParams{})
	if pagedErr != nil || len(pagedBuilds) != 1 {
		t.Fatalf("unexpected paged builds result: builds=%+v err=%v", pagedBuilds, pagedErr)
	}

	queueEntries, queueErr := svc.ListQueue(ctx, repository.QueueListParams{})
	if queueErr != nil || len(queueEntries) != 1 || queueEntries[0].Build.ID != "build-1" {
		t.Fatalf("unexpected queue result: entries=%+v err=%v", queueEntries, queueErr)
	}

	jobBuilds, jobBuildsErr := svc.ListBuildsByJobID(ctx, jobID)
	if jobBuildsErr != nil || len(jobBuilds) != 1 || jobBuilds[0].ID != "build-1" {
		t.Fatalf("unexpected builds by job result: builds=%+v err=%v", jobBuilds, jobBuildsErr)
	}

	latestBuilds, latestErr := svc.ListLatestBuildsByJobIDs(ctx, []string{jobID, "other"})
	if latestErr != nil || len(latestBuilds) != 1 || latestBuilds[jobID].ID != "build-1" {
		t.Fatalf("unexpected latest builds result: latest=%+v err=%v", latestBuilds, latestErr)
	}

	startedAt := now.Add(time.Minute)
	claimedStep, claimed, claimErr := svc.ClaimStepIfPending(ctx, "build-1", 0, nil, startedAt)
	if claimErr != nil || !claimed || claimedStep.Status != domain.BuildStepStatusRunning || claimedStep.StartedAt == nil || !claimedStep.StartedAt.Equal(startedAt) {
		t.Fatalf("unexpected ClaimStepIfPending result: step=%+v claimed=%v err=%v", claimedStep, claimed, claimErr)
	}

	claim := repository.StepClaim{WorkerID: "worker-1", ClaimToken: "claim-1", ClaimedAt: now.Add(2 * time.Minute), LeaseExpiresAt: now.Add(3 * time.Minute)}
	claimedPendingStep, pendingClaimed, pendingClaimErr := svc.ClaimPendingStep(ctx, "build-1", 1, claim)
	if pendingClaimErr != nil || !pendingClaimed || claimedPendingStep.ClaimToken == nil || *claimedPendingStep.ClaimToken != "claim-1" {
		t.Fatalf("unexpected ClaimPendingStep result: step=%+v claimed=%v err=%v", claimedPendingStep, pendingClaimed, pendingClaimErr)
	}

	reclaimClaim := repository.StepClaim{WorkerID: "worker-2", ClaimToken: "claim-2", ClaimedAt: now.Add(4 * time.Minute), LeaseExpiresAt: now.Add(5 * time.Minute)}
	reclaimedStep, reclaimed, reclaimErr := svc.ReclaimExpiredStep(ctx, "build-1", 2, now, reclaimClaim)
	if reclaimErr != nil || !reclaimed || reclaimedStep.ClaimToken == nil || *reclaimedStep.ClaimToken != "claim-2" {
		t.Fatalf("unexpected ReclaimExpiredStep result: step=%+v claimed=%v err=%v", reclaimedStep, reclaimed, reclaimErr)
	}

	notFoundSvc := NewBuildService(&fakeBuildRepository{getErr: repository.ErrBuildNotFound}, nil, nil)
	_, _, notFoundErr := notFoundSvc.ClaimStepIfPending(ctx, "missing", 0, nil, startedAt)
	if !errors.Is(notFoundErr, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound from claim wrapper, got %v", notFoundErr)
	}
	_, _, notFoundErr = notFoundSvc.ClaimPendingStep(ctx, "missing", 0, claim)
	if !errors.Is(notFoundErr, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound from pending claim wrapper, got %v", notFoundErr)
	}
	_, _, notFoundErr = notFoundSvc.ReclaimExpiredStep(ctx, "missing", 0, now, reclaimClaim)
	if !errors.Is(notFoundErr, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound from reclaim wrapper, got %v", notFoundErr)
	}
}

func TestBuildService_ReadQueue_ExecutionJobWrappers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 19, 30, 0, 0, time.UTC)
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}

	nilSvc := NewBuildService(buildRepo, nil, nil)
	nilJobs, nilJobsErr := nilSvc.GetJobsByBuildID(ctx, "build-1")
	if nilJobsErr != nil || len(nilJobs) != 0 {
		t.Fatalf("expected nil job repo fallback, got jobs=%+v err=%v", nilJobs, nilJobsErr)
	}
	nilOutputs, nilOutputsErr := nilSvc.GetJobOutputsByBuildID(ctx, "build-1")
	if nilOutputsErr != nil || len(nilOutputs) != 0 {
		t.Fatalf("expected nil output repo fallback, got outputs=%+v err=%v", nilOutputs, nilOutputsErr)
	}
	nilOutputs, nilOutputsErr = nilSvc.GetJobOutputsByJobID(ctx, "job-1")
	if nilOutputsErr != nil || len(nilOutputs) != 0 {
		t.Fatalf("expected nil output repo fallback by job, got outputs=%+v err=%v", nilOutputs, nilOutputsErr)
	}
	_, getNilErr := nilSvc.GetJobByID(ctx, "job-1")
	if !errors.Is(getNilErr, ErrExecutionJobRepoNotConfigured) {
		t.Fatalf("expected ErrExecutionJobRepoNotConfigured, got %v", getNilErr)
	}
	_, getByStepNilErr := nilSvc.GetJobByStepID(ctx, "step-1")
	if !errors.Is(getByStepNilErr, repository.ErrExecutionJobNotFound) {
		t.Fatalf("expected repository.ErrExecutionJobNotFound, got %v", getByStepNilErr)
	}
	_, nilClaimed, nilClaimErr := nilSvc.ClaimNextRunnableJob(ctx, repository.StepClaim{})
	if nilClaimErr != nil || nilClaimed {
		t.Fatalf("expected nil claim fallback, got claimed=%v err=%v", nilClaimed, nilClaimErr)
	}
	_, nilStepClaimed, nilStepClaimErr := nilSvc.ClaimJobByStepID(ctx, "step-1", repository.StepClaim{})
	if nilStepClaimErr != nil || nilStepClaimed {
		t.Fatalf("expected nil step claim fallback, got claimed=%v err=%v", nilStepClaimed, nilStepClaimErr)
	}
	_, nilRenewed, nilRenewErr := nilSvc.RenewJobLease(ctx, "job-1", "claim", now.Add(time.Minute))
	if nilRenewErr != nil || nilRenewed {
		t.Fatalf("expected nil renew fallback, got renewed=%v err=%v", nilRenewed, nilRenewErr)
	}

	job := domain.ExecutionJob{ID: "job-1", BuildID: "build-1", StepID: "step-1", Status: domain.ExecutionJobStatusRunning}
	output := domain.ExecutionJobOutput{ID: "out-1", JobID: "job-1", BuildID: "build-1", Name: "report.xml", Status: domain.ExecutionJobOutputStatusAvailable, CreatedAt: now}
	jobRepo := &readQueueExecutionJobRepo{
		jobsByBuild:    []domain.ExecutionJob{job},
		jobByID:        job,
		jobByStep:      job,
		claimNextJob:   job,
		claimNextOK:    true,
		claimByStepJob: job,
		claimByStepOK:  true,
		renewJob:       job,
		renewOutcome:   repository.StepCompletionCompleted,
	}
	outputRepo := &readQueueExecutionOutputRepo{outputsByBuild: []domain.ExecutionJobOutput{output}, outputsByJob: []domain.ExecutionJobOutput{output}}
	svc := NewBuildService(buildRepo, nil, nil)
	svc.SetExecutionJobRepository(jobRepo)
	svc.SetExecutionJobOutputRepository(outputRepo)

	jobs, jobsErr := svc.GetJobsByBuildID(ctx, "build-1")
	if jobsErr != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("unexpected jobs by build result: jobs=%+v err=%v", jobs, jobsErr)
	}
	storedJob, storedJobErr := svc.GetJobByID(ctx, "job-1")
	if storedJobErr != nil || storedJob.ID != job.ID {
		t.Fatalf("unexpected GetJobByID result: job=%+v err=%v", storedJob, storedJobErr)
	}
	jobRepo.jobByIDErr = repository.ErrExecutionJobNotFound
	_, missingJobErr := svc.GetJobByID(ctx, "missing")
	if !errors.Is(missingJobErr, ErrExecutionJobNotFound) {
		t.Fatalf("expected ErrExecutionJobNotFound, got %v", missingJobErr)
	}
	jobRepo.jobByIDErr = nil

	outputsByBuild, outputsByBuildErr := svc.GetJobOutputsByBuildID(ctx, "build-1")
	if outputsByBuildErr != nil || len(outputsByBuild) != 1 || outputsByBuild[0].ID != output.ID {
		t.Fatalf("unexpected outputs by build result: outputs=%+v err=%v", outputsByBuild, outputsByBuildErr)
	}
	outputsByJob, outputsByJobErr := svc.GetJobOutputsByJobID(ctx, "job-1")
	if outputsByJobErr != nil || len(outputsByJob) != 1 || outputsByJob[0].ID != output.ID {
		t.Fatalf("unexpected outputs by job result: outputs=%+v err=%v", outputsByJob, outputsByJobErr)
	}
	claimedJob, claimedNext, claimedNextErr := svc.ClaimNextRunnableJob(ctx, repository.StepClaim{WorkerID: "worker-1"})
	if claimedNextErr != nil || !claimedNext || claimedJob.ID != job.ID {
		t.Fatalf("unexpected ClaimNextRunnableJob result: job=%+v claimed=%v err=%v", claimedJob, claimedNext, claimedNextErr)
	}
	jobByStep, jobByStepErr := svc.GetJobByStepID(ctx, "step-1")
	if jobByStepErr != nil || jobByStep.ID != job.ID {
		t.Fatalf("unexpected GetJobByStepID result: job=%+v err=%v", jobByStep, jobByStepErr)
	}
	claimedByStep, claimedStep, claimedByStepErr := svc.ClaimJobByStepID(ctx, "step-1", repository.StepClaim{WorkerID: "worker-2"})
	if claimedByStepErr != nil || !claimedStep || claimedByStep.ID != job.ID {
		t.Fatalf("unexpected ClaimJobByStepID result: job=%+v claimed=%v err=%v", claimedByStep, claimedStep, claimedByStepErr)
	}
	renewedJob, renewed, renewErr := svc.RenewJobLease(ctx, "job-1", "claim", now.Add(time.Minute))
	if renewErr != nil || !renewed || renewedJob.ID != job.ID {
		t.Fatalf("unexpected RenewJobLease success result: job=%+v renewed=%v err=%v", renewedJob, renewed, renewErr)
	}
	jobRepo.renewOutcome = repository.StepCompletionStaleClaim
	_, staleRenewed, staleRenewErr := svc.RenewJobLease(ctx, "job-1", "claim", now.Add(2*time.Minute))
	if staleRenewed || !errors.Is(staleRenewErr, ErrStaleStepClaim) {
		t.Fatalf("expected stale claim error, got renewed=%v err=%v", staleRenewed, staleRenewErr)
	}
	jobRepo.renewOutcome = repository.StepCompletionInvalidTransition
	_, invalidRenewed, invalidRenewErr := svc.RenewJobLease(ctx, "job-1", "claim", now.Add(3*time.Minute))
	if invalidRenewErr != nil || invalidRenewed {
		t.Fatalf("expected non-renewed invalid transition without error, got renewed=%v err=%v", invalidRenewed, invalidRenewErr)
	}
}

func TestBuildService_ReadQueue_ArtifactAndLogWrappers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 16, 20, 0, 0, 0, time.UTC)
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	reader := &readQueueLogReader{
		buildLogs: []logs.BuildLogLine{{StepName: "step-1", Timestamp: now, Message: "hello"}},
		chunks:    []logs.StepLogChunk{{BuildID: "build-1", StepIndex: 0, ChunkText: "chunk"}},
	}
	svc := NewBuildService(repo, nil, reader)

	buildLogs, buildLogsErr := svc.GetBuildLogs(ctx, "build-1")
	if buildLogsErr != nil || len(buildLogs) != 1 || buildLogs[0].Message != "hello" {
		t.Fatalf("unexpected build logs result: logs=%+v err=%v", buildLogs, buildLogsErr)
	}
	logChunks, logChunksErr := svc.GetStepLogChunks(ctx, "build-1", 0, 0, 10)
	if logChunksErr != nil || len(logChunks) != 1 || logChunks[0].ChunkText != "chunk" {
		t.Fatalf("unexpected log chunk result: chunks=%+v err=%v", logChunks, logChunksErr)
	}

	emptyArtifacts, emptyArtifactsErr := svc.GetBuildArtifacts(ctx, "build-1")
	if emptyArtifactsErr != nil || len(emptyArtifacts) != 0 {
		t.Fatalf("expected empty artifact fallback, got artifacts=%+v err=%v", emptyArtifacts, emptyArtifactsErr)
	}

	artifactID := "artifact-1"
	storageKey := "builds/build-1/shared/artifact-1"
	artifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-1": {{ID: artifactID, BuildID: "build-1", StorageKey: storageKey, StorageProvider: domain.StorageProviderFilesystem}},
	}}
	svc.SetArtifactPersistence(artifactRepo, testStoreResolver(&recordingStore{}), t.TempDir())

	storedArtifacts, storedArtifactsErr := svc.GetBuildArtifacts(ctx, "build-1")
	if storedArtifactsErr != nil || len(storedArtifacts) != 1 || storedArtifacts[0].ID != artifactID {
		t.Fatalf("unexpected stored artifact result: artifacts=%+v err=%v", storedArtifacts, storedArtifactsErr)
	}
	artifactMeta, artifactStream, openErr := svc.OpenBuildArtifact(ctx, "build-1", artifactID)
	if openErr != nil || artifactMeta.ID != artifactID {
		t.Fatalf("unexpected OpenBuildArtifact success result: meta=%+v err=%v", artifactMeta, openErr)
	}
	closeErr := artifactStream.Close()
	if closeErr != nil {
		t.Fatalf("close artifact stream: %v", closeErr)
	}

	missingStoreSvc := NewBuildService(repo, nil, nil)
	_, _, missingStoreErr := missingStoreSvc.OpenBuildArtifact(ctx, "build-1", artifactID)
	if !errors.Is(missingStoreErr, ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound without artifact store, got %v", missingStoreErr)
	}

	notFoundStoreSvc := NewBuildService(repo, nil, nil)
	notFoundStoreSvc.SetArtifactPersistence(artifactRepo, testStoreResolver(&fakeArtifactStore{}), t.TempDir())
	_, _, missingArtifactErr := notFoundStoreSvc.OpenBuildArtifact(ctx, "build-1", artifactID)
	if !errors.Is(missingArtifactErr, ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound for missing artifact bytes, got %v", missingArtifactErr)
	}

	providerArtifactRepo := &fakeArtifactRepository{artifacts: map[string][]domain.BuildArtifact{
		"build-1": {{ID: artifactID, BuildID: "build-1", StorageKey: storageKey, StorageProvider: domain.StorageProviderGCS}},
	}}
	providerSvc := NewBuildService(repo, nil, nil)
	providerSvc.SetArtifactPersistence(providerArtifactRepo, testStoreResolver(&recordingStore{}), t.TempDir())
	_, _, providerErr := providerSvc.OpenBuildArtifact(ctx, "build-1", artifactID)
	if !errors.Is(providerErr, ErrArtifactStorageProviderNotConfigured) {
		t.Fatalf("expected ErrArtifactStorageProviderNotConfigured, got %v", providerErr)
	}

	notFoundBuildSvc := NewBuildService(&fakeBuildRepository{getErr: repository.ErrBuildNotFound}, nil, reader)
	_, notFoundLogsErr := notFoundBuildSvc.GetBuildLogs(ctx, "missing")
	if !errors.Is(notFoundLogsErr, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound for logs, got %v", notFoundLogsErr)
	}
	_, notFoundChunksErr := notFoundBuildSvc.GetStepLogChunks(ctx, "missing", 0, 0, 10)
	if !errors.Is(notFoundChunksErr, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound for chunks, got %v", notFoundChunksErr)
	}

	reader.err = errors.New("log read failed")
	_, logReadErr := svc.GetBuildLogs(ctx, "build-1")
	if !errors.Is(logReadErr, reader.err) {
		t.Fatalf("expected log reader error, got %v", logReadErr)
	}
	_, chunkReadErr := svc.GetStepLogChunks(ctx, "build-1", 0, 0, 10)
	if !errors.Is(chunkReadErr, reader.err) {
		t.Fatalf("expected chunk reader error, got %v", chunkReadErr)
	}
}
