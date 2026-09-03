package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const workspaceHelperCapabilityIssuer = "coyote-ci-workspace-helper"
const workspaceHelperCapabilityVersion = 1
const workspaceHelperCapabilityLifetime = 10 * time.Minute

var (
	ErrWorkspaceHelperUnauthorized        = errors.New("workspace helper authorization failed")
	ErrWorkspaceHelperCapabilityMalformed = errors.New("workspace helper capability is malformed")
)

type VerifiedWorkloadIdentity struct {
	ExecutionJobID string
	PodUID         string
}

// WorkloadIdentityVerifier verifies a platform workload identity and binds it
// to Coyote's execution identity. Platform-specific types stay behind this seam.
type WorkloadIdentityVerifier interface {
	VerifyWorkspaceHelper(ctx context.Context, token string, executionJobID string, podUID string, role domain.WorkspaceHelperRole) (VerifiedWorkloadIdentity, error)
}

type WorkspaceHelperCapabilityService struct {
	executionJobs repository.ExecutionJobRepository
	identities    WorkloadIdentityVerifier
	signingKey    []byte
	now           func() time.Time
}

func NewWorkspaceHelperCapabilityService(executionJobs repository.ExecutionJobRepository, identities WorkloadIdentityVerifier, signingSecret string) (*WorkspaceHelperCapabilityService, error) {
	if executionJobs == nil || identities == nil {
		return nil, errors.New("workspace helper capability service requires execution job repository and workload identity verifier")
	}
	key := []byte(strings.TrimSpace(signingSecret))
	if len(key) < 32 {
		return nil, errors.New("workspace helper capability signing secret must be at least 32 bytes")
	}
	return &WorkspaceHelperCapabilityService{executionJobs: executionJobs, identities: identities, signingKey: key, now: time.Now}, nil
}

func (s *WorkspaceHelperCapabilityService) Exchange(ctx context.Context, projectedToken string, requested domain.WorkspaceHelperCapability) (string, domain.WorkspaceHelperCapability, error) {
	if err := validateWorkspaceHelperCapability(requested, false); err != nil {
		return "", domain.WorkspaceHelperCapability{}, err
	}
	identity, err := s.identities.VerifyWorkspaceHelper(ctx, strings.TrimSpace(projectedToken), requested.ExecutionJobID, requested.PodUID, requested.Role)
	if err != nil || identity.ExecutionJobID != requested.ExecutionJobID || identity.PodUID != requested.PodUID {
		return "", domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperUnauthorized
	}
	job, activeExecutionErr := s.validateActiveExecution(ctx, requested.ExecutionJobID)
	if activeExecutionErr != nil {
		return "", domain.WorkspaceHelperCapability{}, activeExecutionErr
	}
	capability := requested
	capability.ClaimDigest = workspaceHelperClaimDigest(*job.ClaimToken)
	capability.ExpiresAt = s.now().UTC().Add(workspaceHelperCapabilityLifetime)
	token, err := s.sign(capability)
	if err != nil {
		return "", domain.WorkspaceHelperCapability{}, err
	}
	return token, capability, nil
}

func (s *WorkspaceHelperCapabilityService) Authorize(ctx context.Context, token string, executionJobID string, podUID string, requiredRole domain.WorkspaceHelperRole) (domain.WorkspaceHelperCapability, error) {
	if !requiredRole.Valid() || strings.TrimSpace(executionJobID) == "" || strings.TrimSpace(podUID) == "" {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperUnauthorized
	}
	capability, err := s.verify(token)
	if err != nil || capability.ExecutionJobID != strings.TrimSpace(executionJobID) || capability.PodUID != strings.TrimSpace(podUID) || capability.Role != requiredRole {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperUnauthorized
	}
	job, activeExecutionErr := s.validateActiveExecution(ctx, capability.ExecutionJobID)
	if activeExecutionErr != nil {
		return domain.WorkspaceHelperCapability{}, activeExecutionErr
	}
	if !hmac.Equal([]byte(capability.ClaimDigest), []byte(workspaceHelperClaimDigest(*job.ClaimToken))) {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperUnauthorized
	}
	return capability, nil
}

func (s *WorkspaceHelperCapabilityService) validateActiveExecution(ctx context.Context, executionJobID string) (domain.ExecutionJob, error) {
	job, err := s.executionJobs.GetJobByID(ctx, executionJobID)
	if err != nil {
		if errors.Is(err, repository.ErrExecutionJobNotFound) {
			return domain.ExecutionJob{}, ErrWorkspaceHelperUnauthorized
		}
		return domain.ExecutionJob{}, err
	}
	if job.Status != domain.ExecutionJobStatusRunning || job.ClaimToken == nil || job.ClaimExpiresAt == nil || !job.ClaimExpiresAt.After(s.now().UTC()) {
		return domain.ExecutionJob{}, ErrWorkspaceHelperUnauthorized
	}
	return job, nil
}

type workspaceHelperCapabilityClaims struct {
	Version        int                        `json:"v"`
	Issuer         string                     `json:"iss"`
	ExecutionJobID string                     `json:"execution_job_id"`
	PodUID         string                     `json:"pod_uid"`
	Role           domain.WorkspaceHelperRole `json:"role"`
	ClaimDigest    string                     `json:"claim_digest"`
	ExpiresAt      int64                      `json:"exp"`
}

func (s *WorkspaceHelperCapabilityService) sign(capability domain.WorkspaceHelperCapability) (string, error) {
	claims, err := json.Marshal(workspaceHelperCapabilityClaims{Version: workspaceHelperCapabilityVersion, Issuer: workspaceHelperCapabilityIssuer, ExecutionJobID: capability.ExecutionJobID, PodUID: capability.PodUID, Role: capability.Role, ClaimDigest: capability.ClaimDigest, ExpiresAt: capability.ExpiresAt.UTC().Unix()})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *WorkspaceHelperCapabilityService) verify(token string) (domain.WorkspaceHelperCapability, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperCapabilityMalformed
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperCapabilityMalformed
	}
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperCapabilityMalformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperCapabilityMalformed
	}
	var claims workspaceHelperCapabilityClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperCapabilityMalformed
	}
	capability := domain.WorkspaceHelperCapability{ExecutionJobID: claims.ExecutionJobID, PodUID: claims.PodUID, Role: claims.Role, ClaimDigest: claims.ClaimDigest, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC()}
	if claims.Version != workspaceHelperCapabilityVersion || claims.Issuer != workspaceHelperCapabilityIssuer || !capability.ExpiresAt.After(s.now().UTC()) {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperUnauthorized
	}
	if err := validateWorkspaceHelperCapability(capability, true); err != nil {
		return domain.WorkspaceHelperCapability{}, ErrWorkspaceHelperCapabilityMalformed
	}
	return capability, nil
}

func validateWorkspaceHelperCapability(capability domain.WorkspaceHelperCapability, requireClaimDigest bool) error {
	if strings.TrimSpace(capability.ExecutionJobID) == "" || strings.TrimSpace(capability.PodUID) == "" || !capability.Role.Valid() || requireClaimDigest && strings.TrimSpace(capability.ClaimDigest) == "" {
		return fmt.Errorf("%w: execution job id, pod uid, helper role, and claim digest are required", ErrWorkspaceHelperCapabilityMalformed)
	}
	return nil
}

func workspaceHelperClaimDigest(claimToken string) string {
	digest := sha256.Sum256([]byte(claimToken))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
