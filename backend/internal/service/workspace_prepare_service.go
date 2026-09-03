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

type WorkspacePreparePayload struct {
	Archive     io.ReadCloser
	Publication domain.WorkspaceRevisionPublication
}

// WorkspaceSourceArchivePreparer materializes the immutable source baseline as
// an archive. It deliberately has no credential-returning API.
type WorkspaceSourceArchivePreparer interface {
	OpenSourceArchive(ctx context.Context, build domain.Build, job domain.ExecutionJob, spec domain.ExecutionJobSpec) (WorkspacePreparePayload, error)
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

// Open authorizes one prepare helper and returns its immutable workspace input.
func (s *WorkspacePrepareService) Open(ctx context.Context, capabilityToken string, executionJobID string, podUID string) (WorkspacePreparePayload, error) {
	if _, err := s.capabilities.Authorize(ctx, capabilityToken, executionJobID, podUID, domain.WorkspaceHelperRolePrepare); err != nil {
		return WorkspacePreparePayload{}, err
	}
	job, err := s.executionJobs.GetJobByID(ctx, strings.TrimSpace(executionJobID))
	if err != nil {
		return WorkspacePreparePayload{}, err
	}
	var spec domain.ExecutionJobSpec
	if decodeErr := json.Unmarshal([]byte(job.ResolvedSpecJSON), &spec); decodeErr != nil {
		return WorkspacePreparePayload{}, fmt.Errorf("%w: resolved execution job spec", ErrWorkspacePrepareInvalidInput)
	}
	switch spec.WorkspaceInput.Mode {
	case domain.WorkspaceInputModePredecessor:
		return s.openPredecessor(ctx, job, spec.WorkspaceInput)
	case domain.WorkspaceInputModeSource:
		build, buildErr := s.builds.GetByID(ctx, job.BuildID)
		if buildErr != nil {
			return WorkspacePreparePayload{}, buildErr
		}
		return s.sources.OpenSourceArchive(ctx, build, job, spec)
	case domain.WorkspaceInputModeFanIn:
		return WorkspacePreparePayload{}, ErrWorkspacePrepareFanInUnsupported
	default:
		return WorkspacePreparePayload{}, fmt.Errorf("%w: workspace input mode", ErrWorkspacePrepareInvalidInput)
	}
}

func (s *WorkspacePrepareService) openPredecessor(ctx context.Context, job domain.ExecutionJob, input domain.WorkspaceInputPlan) (WorkspacePreparePayload, error) {
	if strings.TrimSpace(job.BuildID) == "" || strings.TrimSpace(input.ProducerNodeID) == "" {
		return WorkspacePreparePayload{}, fmt.Errorf("%w: predecessor build and producer node are required", ErrWorkspacePrepareInvalidInput)
	}
	revision, err := s.revisions.GetPublishedByBuildNode(ctx, job.BuildID, input.ProducerNodeID)
	if err != nil {
		return WorkspacePreparePayload{}, err
	}
	if revision.Status != domain.WorkspaceRevisionStatusPublished || revision.ContentDigest == nil || revision.StorageKey == nil || revision.SizeBytes == nil {
		return WorkspacePreparePayload{}, ErrWorkspacePrepareRevisionIncomplete
	}
	publication := domain.WorkspaceRevisionPublication{ContentDigest: *revision.ContentDigest, StorageKey: *revision.StorageKey, SizeBytes: revision.SizeBytes}
	if publicationErr := publication.Validate(); publicationErr != nil {
		return WorkspacePreparePayload{}, ErrWorkspacePrepareRevisionIncomplete
	}
	archive, openErr := s.archives.Open(ctx, publication)
	if openErr != nil {
		return WorkspacePreparePayload{}, openErr
	}
	return WorkspacePreparePayload{Archive: archive, Publication: publication}, nil
}

var _ WorkspacePrepareCapabilityAuthorizer = (*WorkspaceHelperCapabilityService)(nil)
var _ workspace.WorkspaceRevisionArchiveReader = (*workspace.FilesystemWorkspaceRevisionStore)(nil)
