package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

var (
	ErrWorkspacePrepareInvalidInput       = errors.New("invalid workspace prepare input")
	ErrWorkspacePrepareFanInUnsupported   = errors.New("fan-in workspace preparation is not supported")
	ErrWorkspacePrepareRevisionIncomplete = errors.New("published workspace revision metadata is incomplete")
)

type WorkspacePrepareCapabilityAuthorizer interface {
	Authorize(ctx context.Context, token string, executionJobID string, podUID string, requiredRole domain.WorkspaceHelperRole) (domain.WorkspaceHelperCapability, error)
}

type workspacePrepareExecutionJobRepository interface {
	GetJobByID(ctx context.Context, id string) (domain.ExecutionJob, error)
}

type workspacePrepareBuildRepository interface {
	GetByID(ctx context.Context, id string) (domain.Build, error)
}

type workspacePrepareRevisionRepository interface {
	GetPublishedByBuildNode(ctx context.Context, buildID string, nodeID string) (domain.WorkspaceRevision, error)
}

// WorkspaceSourceArchivePreparer materializes the immutable source baseline as
// an archive. It deliberately has no credential-returning API.
type WorkspaceSourceArchivePreparer interface {
	OpenSourceArchive(ctx context.Context, build domain.Build, job domain.ExecutionJob, spec domain.ExecutionJobSpec) (io.ReadCloser, error)
}

type WorkspacePrepareServiceConfig struct {
	CapabilityAuthorizer WorkspacePrepareCapabilityAuthorizer
	ExecutionJobs        workspacePrepareExecutionJobRepository
	Builds               workspacePrepareBuildRepository
	WorkspaceRevisions   workspacePrepareRevisionRepository
	RevisionArchives     workspace.WorkspaceRevisionArchiveReader
	SourceArchives       WorkspaceSourceArchivePreparer
}

type WorkspacePrepareService struct {
	capabilities  WorkspacePrepareCapabilityAuthorizer
	executionJobs workspacePrepareExecutionJobRepository
	builds        workspacePrepareBuildRepository
	revisions     workspacePrepareRevisionRepository
	archives      workspace.WorkspaceRevisionArchiveReader
	sources       WorkspaceSourceArchivePreparer
}

func NewWorkspacePrepareService(config WorkspacePrepareServiceConfig) (*WorkspacePrepareService, error) {
	if config.CapabilityAuthorizer == nil || config.ExecutionJobs == nil || config.Builds == nil || config.WorkspaceRevisions == nil || config.RevisionArchives == nil || config.SourceArchives == nil {
		return nil, errors.New("workspace prepare service requires capability, repository, archive, and source dependencies")
	}
	return &WorkspacePrepareService{capabilities: config.CapabilityAuthorizer, executionJobs: config.ExecutionJobs, builds: config.Builds, revisions: config.WorkspaceRevisions, archives: config.RevisionArchives, sources: config.SourceArchives}, nil
}

// Open authorizes one prepare helper and streams its immutable workspace input.
func (s *WorkspacePrepareService) Open(ctx context.Context, capabilityToken string, executionJobID string, podUID string) (io.ReadCloser, error) {
	if _, err := s.capabilities.Authorize(ctx, capabilityToken, executionJobID, podUID, domain.WorkspaceHelperRolePrepare); err != nil {
		return nil, err
	}
	job, err := s.executionJobs.GetJobByID(ctx, strings.TrimSpace(executionJobID))
	if err != nil {
		return nil, err
	}
	var spec domain.ExecutionJobSpec
	if decodeErr := json.Unmarshal([]byte(job.ResolvedSpecJSON), &spec); decodeErr != nil {
		return nil, fmt.Errorf("%w: resolved execution job spec", ErrWorkspacePrepareInvalidInput)
	}
	switch spec.WorkspaceInput.Mode {
	case domain.WorkspaceInputModePredecessor:
		return s.openPredecessor(ctx, job, spec.WorkspaceInput)
	case domain.WorkspaceInputModeSource:
		build, buildErr := s.builds.GetByID(ctx, job.BuildID)
		if buildErr != nil {
			return nil, buildErr
		}
		return s.sources.OpenSourceArchive(ctx, build, job, spec)
	case domain.WorkspaceInputModeFanIn:
		return nil, ErrWorkspacePrepareFanInUnsupported
	default:
		return nil, fmt.Errorf("%w: workspace input mode", ErrWorkspacePrepareInvalidInput)
	}
}

func (s *WorkspacePrepareService) openPredecessor(ctx context.Context, job domain.ExecutionJob, input domain.WorkspaceInputPlan) (io.ReadCloser, error) {
	if strings.TrimSpace(job.BuildID) == "" || strings.TrimSpace(input.ProducerNodeID) == "" {
		return nil, fmt.Errorf("%w: predecessor build and producer node are required", ErrWorkspacePrepareInvalidInput)
	}
	revision, err := s.revisions.GetPublishedByBuildNode(ctx, job.BuildID, input.ProducerNodeID)
	if err != nil {
		return nil, err
	}
	if revision.Status != domain.WorkspaceRevisionStatusPublished || revision.ContentDigest == nil || revision.StorageKey == nil || revision.SizeBytes == nil {
		return nil, ErrWorkspacePrepareRevisionIncomplete
	}
	publication := domain.WorkspaceRevisionPublication{ContentDigest: *revision.ContentDigest, StorageKey: *revision.StorageKey, SizeBytes: revision.SizeBytes}
	if publicationErr := publication.Validate(); publicationErr != nil {
		return nil, ErrWorkspacePrepareRevisionIncomplete
	}
	return s.archives.Open(ctx, publication)
}

var _ WorkspacePrepareCapabilityAuthorizer = (*WorkspaceHelperCapabilityService)(nil)
var _ workspace.WorkspaceRevisionArchiveReader = (*workspace.FilesystemWorkspaceRevisionStore)(nil)
