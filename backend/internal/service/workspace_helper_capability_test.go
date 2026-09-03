package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestWorkspaceHelperCapabilityServiceExchangeAndAuthorize(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	jobs := &workspaceHelperExecutionJobs{job: activeWorkspaceHelperJob(now)}
	identities := &workspaceHelperIdentityVerifier{identity: VerifiedWorkloadIdentity{ExecutionJobID: "job-1", PodUID: "pod-1"}}
	service, err := NewWorkspaceHelperCapabilityService(jobs, identities, strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	service.now = func() time.Time { return now }

	token, capability, exchangeErr := service.Exchange(context.Background(), "projected-token", domain.WorkspaceHelperCapability{ExecutionJobID: "job-1", PodUID: "pod-1", Role: domain.WorkspaceHelperRolePrepare})
	if exchangeErr != nil {
		t.Fatalf("exchange: %v", exchangeErr)
	}
	if capability.ExpiresAt.Sub(now) != workspaceHelperCapabilityLifetime {
		t.Fatalf("expiration=%s", capability.ExpiresAt)
	}
	if _, authorizeErr := service.Authorize(context.Background(), token, "job-1", "pod-1", domain.WorkspaceHelperRolePrepare); authorizeErr != nil {
		t.Fatalf("authorize: %v", authorizeErr)
	}
	for _, testCase := range []struct {
		name string
		job  string
		pod  string
		role domain.WorkspaceHelperRole
	}{
		{name: "wrong job", job: "job-2", pod: "pod-1", role: domain.WorkspaceHelperRolePrepare},
		{name: "wrong pod", job: "job-1", pod: "pod-2", role: domain.WorkspaceHelperRolePrepare},
		{name: "wrong role", job: "job-1", pod: "pod-1", role: domain.WorkspaceHelperRolePublish},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, authorizeErr := service.Authorize(context.Background(), token, testCase.job, testCase.pod, testCase.role); !errors.Is(authorizeErr, ErrWorkspaceHelperUnauthorized) {
				t.Fatalf("authorize error=%v", authorizeErr)
			}
		})
	}
	if _, authorizeErr := service.Authorize(context.Background(), token+"x", "job-1", "pod-1", domain.WorkspaceHelperRolePrepare); !errors.Is(authorizeErr, ErrWorkspaceHelperUnauthorized) {
		t.Fatalf("tampered token error=%v", authorizeErr)
	}
	service.now = func() time.Time { return capability.ExpiresAt }
	if _, authorizeErr := service.Authorize(context.Background(), token, "job-1", "pod-1", domain.WorkspaceHelperRolePrepare); !errors.Is(authorizeErr, ErrWorkspaceHelperUnauthorized) {
		t.Fatalf("expired token error=%v", authorizeErr)
	}
}

func TestWorkspaceHelperCapabilityServiceExchangeRejectsIdentityAndExecutionState(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	request := domain.WorkspaceHelperCapability{ExecutionJobID: "job-1", PodUID: "pod-1", Role: domain.WorkspaceHelperRolePrepare}
	executionLookupErr := errors.New("database unavailable")
	for _, testCase := range []struct {
		name        string
		identity    VerifiedWorkloadIdentity
		identityErr error
		job         domain.ExecutionJob
		jobErr      error
		wantErr     error
	}{
		{name: "identity failure", identityErr: errors.New("invalid identity"), job: activeWorkspaceHelperJob(now), wantErr: ErrWorkspaceHelperUnauthorized},
		{name: "pod mismatch", identity: VerifiedWorkloadIdentity{ExecutionJobID: "job-1", PodUID: "pod-2"}, job: activeWorkspaceHelperJob(now), wantErr: ErrWorkspaceHelperUnauthorized},
		{name: "canceled", identity: VerifiedWorkloadIdentity{ExecutionJobID: "job-1", PodUID: "pod-1"}, job: domain.ExecutionJob{ID: "job-1", Status: domain.ExecutionJobStatusCanceled}, wantErr: ErrWorkspaceHelperUnauthorized},
		{name: "expired claim", identity: VerifiedWorkloadIdentity{ExecutionJobID: "job-1", PodUID: "pod-1"}, job: activeWorkspaceHelperJob(now.Add(-time.Hour)), wantErr: ErrWorkspaceHelperUnauthorized},
		{name: "missing execution", identity: VerifiedWorkloadIdentity{ExecutionJobID: "job-1", PodUID: "pod-1"}, jobErr: repository.ErrExecutionJobNotFound, wantErr: ErrWorkspaceHelperUnauthorized},
		{name: "execution lookup failure", identity: VerifiedWorkloadIdentity{ExecutionJobID: "job-1", PodUID: "pod-1"}, jobErr: executionLookupErr, wantErr: executionLookupErr},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service, err := NewWorkspaceHelperCapabilityService(&workspaceHelperExecutionJobs{job: testCase.job, err: testCase.jobErr}, &workspaceHelperIdentityVerifier{identity: testCase.identity, err: testCase.identityErr}, strings.Repeat("a", 32))
			if err != nil {
				t.Fatalf("new service: %v", err)
			}
			service.now = func() time.Time { return now }
			_, _, exchangeErr := service.Exchange(context.Background(), "projected-token", request)
			if !errors.Is(exchangeErr, testCase.wantErr) {
				t.Fatalf("exchange error=%v", exchangeErr)
			}
		})
	}
}

func TestNewWorkspaceHelperCapabilityServiceValidatesDependenciesAndSecret(t *testing.T) {
	_, nilDependenciesErr := NewWorkspaceHelperCapabilityService(nil, nil, strings.Repeat("a", 32))
	if nilDependenciesErr == nil {
		t.Fatal("expected missing dependencies to fail")
	}
	_, shortSecretErr := NewWorkspaceHelperCapabilityService(&workspaceHelperExecutionJobs{}, &workspaceHelperIdentityVerifier{}, "short")
	if shortSecretErr == nil {
		t.Fatal("expected short signing secret to fail")
	}
}

func TestWorkspaceHelperCapabilityServiceRejectsMalformedRequestsAndTokens(t *testing.T) {
	service, err := NewWorkspaceHelperCapabilityService(&workspaceHelperExecutionJobs{}, &workspaceHelperIdentityVerifier{}, strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, _, exchangeErr := service.Exchange(context.Background(), "projected-token", domain.WorkspaceHelperCapability{})
	if !errors.Is(exchangeErr, ErrWorkspaceHelperCapabilityMalformed) {
		t.Fatalf("exchange error=%v", exchangeErr)
	}
	if _, authorizeErr := service.Authorize(context.Background(), "not-a-capability", "job-1", "pod-1", domain.WorkspaceHelperRolePrepare); !errors.Is(authorizeErr, ErrWorkspaceHelperUnauthorized) {
		t.Fatalf("authorize error=%v", authorizeErr)
	}
}

func TestWorkspaceHelperCapabilityServiceAuthorizeRevalidatesExecution(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	jobs := &workspaceHelperExecutionJobs{job: activeWorkspaceHelperJob(now)}
	service, err := NewWorkspaceHelperCapabilityService(jobs, &workspaceHelperIdentityVerifier{identity: VerifiedWorkloadIdentity{ExecutionJobID: "job-1", PodUID: "pod-1"}}, strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	service.now = func() time.Time { return now }
	token, _, exchangeErr := service.Exchange(context.Background(), "projected-token", domain.WorkspaceHelperCapability{ExecutionJobID: "job-1", PodUID: "pod-1", Role: domain.WorkspaceHelperRolePrepare})
	if exchangeErr != nil {
		t.Fatalf("exchange: %v", exchangeErr)
	}

	for _, testCase := range []struct {
		name   string
		mutate func(*domain.ExecutionJob)
	}{
		{name: "canceled", mutate: func(job *domain.ExecutionJob) { job.Status = domain.ExecutionJobStatusCanceled }},
		{name: "completed", mutate: func(job *domain.ExecutionJob) { job.Status = domain.ExecutionJobStatusSuccess }},
		{name: "expired claim", mutate: func(job *domain.ExecutionJob) { expired := now.Add(-time.Second); job.ClaimExpiresAt = &expired }},
		{name: "removed claim", mutate: func(job *domain.ExecutionJob) { job.ClaimToken = nil }},
		{name: "superseded claim", mutate: func(job *domain.ExecutionJob) { replacement := "replacement-claim"; job.ClaimToken = &replacement }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			jobs.job = activeWorkspaceHelperJob(now)
			testCase.mutate(&jobs.job)
			if _, authorizeErr := service.Authorize(context.Background(), token, "job-1", "pod-1", domain.WorkspaceHelperRolePrepare); !errors.Is(authorizeErr, ErrWorkspaceHelperUnauthorized) {
				t.Fatalf("authorize error=%v", authorizeErr)
			}
		})
	}
}

func activeWorkspaceHelperJob(now time.Time) domain.ExecutionJob {
	claimToken := "claim"
	claimExpiresAt := now.Add(time.Minute)
	return domain.ExecutionJob{ID: "job-1", Status: domain.ExecutionJobStatusRunning, ClaimToken: &claimToken, ClaimExpiresAt: &claimExpiresAt}
}

type workspaceHelperIdentityVerifier struct {
	identity VerifiedWorkloadIdentity
	err      error
}

func (v *workspaceHelperIdentityVerifier) VerifyWorkspaceHelper(context.Context, string, string, string, domain.WorkspaceHelperRole) (VerifiedWorkloadIdentity, error) {
	return v.identity, v.err
}

type workspaceHelperExecutionJobs struct {
	job domain.ExecutionJob
	err error
}

func (r *workspaceHelperExecutionJobs) GetJobByID(context.Context, string) (domain.ExecutionJob, error) {
	return r.job, r.err
}
func (*workspaceHelperExecutionJobs) CreateJobsForBuild(context.Context, []domain.ExecutionJob) ([]domain.ExecutionJob, error) {
	return nil, nil
}
func (*workspaceHelperExecutionJobs) GetJobsByBuildID(context.Context, string) ([]domain.ExecutionJob, error) {
	return nil, nil
}
func (*workspaceHelperExecutionJobs) GetJobByStepID(context.Context, string) (domain.ExecutionJob, error) {
	return domain.ExecutionJob{}, repository.ErrExecutionJobNotFound
}
func (*workspaceHelperExecutionJobs) ClaimNextRunnableJob(context.Context, repository.StepClaim) (domain.ExecutionJob, bool, error) {
	return domain.ExecutionJob{}, false, nil
}
func (*workspaceHelperExecutionJobs) ClaimJobByStepID(context.Context, string, repository.StepClaim) (domain.ExecutionJob, bool, error) {
	return domain.ExecutionJob{}, false, nil
}
func (*workspaceHelperExecutionJobs) RenewJobLease(context.Context, string, string, time.Time) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return domain.ExecutionJob{}, "", nil
}
func (*workspaceHelperExecutionJobs) CompleteJobSuccess(context.Context, string, string, time.Time, int, []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return domain.ExecutionJob{}, "", nil
}
func (*workspaceHelperExecutionJobs) CompleteJobFailure(context.Context, string, string, time.Time, string, domain.ExecutionFailureKind, *int, []domain.ArtifactRef) (domain.ExecutionJob, repository.StepCompletionOutcome, error) {
	return domain.ExecutionJob{}, "", nil
}

var _ repository.ExecutionJobRepository = (*workspaceHelperExecutionJobs)(nil)
