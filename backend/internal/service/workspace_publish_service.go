package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

var ErrWorkspacePublishInvalidArchive = errors.New("invalid workspace publish archive")
var ErrWorkspacePublishArchiveTooLarge = errors.New("workspace publish archive exceeds maximum size")

const defaultWorkspacePublishMaxUploadBytes int64 = 1024 * 1024 * 1024
const defaultWorkspacePublishMaxUncompressedBytes int64 = 1024 * 1024 * 1024
const defaultWorkspacePublishMaxArchiveEntries = 10000

var workspacePublishCreateTemp = os.CreateTemp

type workspacePublishExecutionJobRepository interface {
	GetJobByID(context.Context, string) (domain.ExecutionJob, error)
}

type WorkspacePublishServiceConfig struct {
	CapabilityAuthorizer WorkspacePrepareCapabilityAuthorizer
	ExecutionJobs        workspacePublishExecutionJobRepository
	WorkspaceRevisions   repository.WorkspaceRevisionRepository
	RevisionStore        workspace.WorkspaceRevisionStore
	MaxUploadBytes       int64
	MaxUncompressedBytes int64
	MaxArchiveEntries    int
}

type WorkspacePublishService struct {
	capabilities         WorkspacePrepareCapabilityAuthorizer
	executionJobs        workspacePublishExecutionJobRepository
	revisions            repository.WorkspaceRevisionRepository
	store                workspace.WorkspaceRevisionStore
	maxUploadBytes       int64
	maxUncompressedBytes int64
	maxArchiveEntries    int
}

func NewWorkspacePublishService(config WorkspacePublishServiceConfig) (*WorkspacePublishService, error) {
	if config.CapabilityAuthorizer == nil || config.ExecutionJobs == nil || config.WorkspaceRevisions == nil || config.RevisionStore == nil {
		return nil, errors.New("workspace publish service requires capability, execution job, revision repository, and revision store")
	}
	maxUploadBytes := config.MaxUploadBytes
	if maxUploadBytes <= 0 {
		maxUploadBytes = defaultWorkspacePublishMaxUploadBytes
	}
	maxUncompressedBytes := config.MaxUncompressedBytes
	if maxUncompressedBytes <= 0 {
		maxUncompressedBytes = defaultWorkspacePublishMaxUncompressedBytes
	}
	maxArchiveEntries := config.MaxArchiveEntries
	if maxArchiveEntries <= 0 {
		maxArchiveEntries = defaultWorkspacePublishMaxArchiveEntries
	}
	return &WorkspacePublishService{capabilities: config.CapabilityAuthorizer, executionJobs: config.ExecutionJobs, revisions: config.WorkspaceRevisions, store: config.RevisionStore, maxUploadBytes: maxUploadBytes, maxUncompressedBytes: maxUncompressedBytes, maxArchiveEntries: maxArchiveEntries}, nil
}

func (s *WorkspacePublishService) Publish(ctx context.Context, capabilityToken string, executionJobID string, podUID string, archive io.Reader) (domain.WorkspaceRevision, error) {
	capability, authorizeErr := s.capabilities.Authorize(ctx, capabilityToken, executionJobID, podUID, domain.WorkspaceHelperRolePublish)
	if authorizeErr != nil {
		return domain.WorkspaceRevision{}, authorizeErr
	}
	job, jobErr := s.executionJobs.GetJobByID(ctx, strings.TrimSpace(executionJobID))
	if jobErr != nil {
		return domain.WorkspaceRevision{}, jobErr
	}
	if job.ID != strings.TrimSpace(executionJobID) || job.ClaimToken == nil {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionStaleClaim
	}
	claimToken := *job.ClaimToken
	if !hmac.Equal([]byte(capability.ClaimDigest), []byte(domain.ExecutionJobClaimDigest(claimToken))) {
		return domain.WorkspaceRevision{}, repository.ErrWorkspaceRevisionStaleClaim
	}
	revisionID := domain.WorkspaceRevisionIDForExecutionJob(job.ID)
	_, createErr := s.revisions.CreatePublishing(ctx, domain.WorkspaceRevision{ID: revisionID, ProducingExecutionJobID: job.ID, BuildID: job.BuildID, NodeID: job.NodeID, AttemptNumber: job.AttemptNumber, Status: domain.WorkspaceRevisionStatusPublishing, CreatedAt: time.Now().UTC()})
	if createErr != nil {
		return domain.WorkspaceRevision{}, fmt.Errorf("creating workspace revision: %w", createErr)
	}
	archivePath, digest, size, spoolErr := spoolWorkspacePublishArchive(ctx, archive, s.maxUploadBytes)
	if spoolErr != nil {
		return domain.WorkspaceRevision{}, spoolErr
	}
	defer func() { _ = os.Remove(archivePath) }()
	stagingRoot, stagingErr := os.MkdirTemp("", "coyote-workspace-publish-*")
	if stagingErr != nil {
		return domain.WorkspaceRevision{}, stagingErr
	}
	defer func() { _ = os.RemoveAll(stagingRoot) }()
	restoredRoot := filepath.Join(stagingRoot, "workspace")
	archiveFile, openErr := os.Open(archivePath)
	if openErr != nil {
		return domain.WorkspaceRevision{}, openErr
	}
	publication := domain.WorkspaceRevisionPublication{ContentDigest: digest, StorageKey: "workspace-revisions/upload.tar.gz", SizeBytes: &size}
	restoreErr := workspace.RestoreArchiveWithLimits(ctx, archiveFile, publication, restoredRoot, workspace.WorkspaceRevisionRestoreLimits{MaxUncompressedBytes: s.maxUncompressedBytes, MaxEntries: s.maxArchiveEntries})
	closeErr := archiveFile.Close()
	if restoreErr != nil {
		if errors.Is(restoreErr, workspace.ErrWorkspaceRevisionTooLarge) || errors.Is(restoreErr, workspace.ErrWorkspaceRevisionTooManyEntries) {
			return domain.WorkspaceRevision{}, ErrWorkspacePublishArchiveTooLarge
		}
		return domain.WorkspaceRevision{}, fmt.Errorf("%w: %v", ErrWorkspacePublishInvalidArchive, restoreErr)
	}
	if closeErr != nil {
		return domain.WorkspaceRevision{}, closeErr
	}
	durablePublication, publishErr := s.store.Publish(ctx, workspacePublishObjectID(revisionID, capability.ClaimDigest), restoredRoot)
	if publishErr != nil {
		return domain.WorkspaceRevision{}, fmt.Errorf("publishing workspace revision: %w", publishErr)
	}
	published, markErr := s.revisions.MarkPublishedIfClaimed(ctx, revisionID, claimToken, durablePublication, time.Now().UTC())
	if markErr != nil {
		return domain.WorkspaceRevision{}, fmt.Errorf("marking workspace revision published: %w", markErr)
	}
	return published, nil
}

func workspacePublishObjectID(revisionID string, claimDigest string) string {
	return revisionID + "-" + claimDigest
}

func spoolWorkspacePublishArchive(ctx context.Context, archive io.Reader, maxBytes int64) (string, string, int64, error) {
	if archive == nil {
		return "", "", 0, ErrWorkspacePublishInvalidArchive
	}
	file, createErr := workspacePublishCreateTemp("", "coyote-workspace-upload-*.tar.gz")
	if createErr != nil {
		return "", "", 0, createErr
	}
	path := file.Name()
	hasher := sha256.New()
	size, copyErr := copyWorkspacePublishArchive(ctx, io.MultiWriter(file, hasher), archive, maxBytes)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if copyErr != nil {
			return "", "", 0, copyErr
		}
		return "", "", 0, closeErr
	}
	return path, "sha256:" + hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func copyWorkspacePublishArchive(ctx context.Context, destination io.Writer, source io.Reader, maxBytes int64) (int64, error) {
	buffer := make([]byte, 32*1024)
	var copied int64
	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return copied, contextErr
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			if copied+int64(read) > maxBytes {
				return copied, ErrWorkspacePublishArchiveTooLarge
			}
			written, writeErr := destination.Write(buffer[:read])
			copied += int64(written)
			if writeErr != nil {
				return copied, writeErr
			}
			if written != read {
				return copied, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return copied, nil
		}
		if readErr != nil {
			return copied, readErr
		}
	}
}
