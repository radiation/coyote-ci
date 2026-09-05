package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	"github.com/radiation/coyote-ci/backend/internal/source"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

type ServerSourceArchivePreparer struct {
	resolver source.WorkspaceSourceResolver
	checkout *buildsvc.RepositoryAwareCheckoutResolver
}

func NewServerSourceArchivePreparer(resolver source.WorkspaceSourceResolver, checkout *buildsvc.RepositoryAwareCheckoutResolver) (*ServerSourceArchivePreparer, error) {
	if resolver == nil {
		return nil, errors.New("server source archive preparer requires workspace source resolver")
	}
	return &ServerSourceArchivePreparer{resolver: resolver, checkout: checkout}, nil
}

func (p *ServerSourceArchivePreparer) OpenSourceArchive(ctx context.Context, build domain.Build, job domain.ExecutionJob, spec domain.ExecutionJobSpec) (WorkspacePreparePayload, error) {
	checkoutRoot, err := os.MkdirTemp("", "coyote-source-prepare-*")
	if err != nil {
		return WorkspacePreparePayload{}, err
	}
	defer func() { _ = os.RemoveAll(checkoutRoot) }()
	workspacePath := filepath.Join(checkoutRoot, "source")
	repositoryURL := strings.TrimSpace(spec.Source.RepositoryURL)
	if repositoryURL == "" {
		repositoryURL = strings.TrimSpace(job.Source.RepositoryURL)
	}
	if repositoryURL == "" {
		if mkdirErr := os.MkdirAll(workspacePath, 0o755); mkdirErr != nil {
			return WorkspacePreparePayload{}, mkdirErr
		}
		archive, publication, archiveErr := workspace.ArchiveDirectory(ctx, workspacePath)
		if archiveErr != nil {
			return WorkspacePreparePayload{}, archiveErr
		}
		return WorkspacePreparePayload{Archive: archive, Publication: publication}, nil
	}
	if build.RegisteredRepositoryID != nil && build.SCMConnectionID != nil && build.ProviderRepositoryID != nil {
		if p.checkout == nil {
			return WorkspacePreparePayload{}, buildsvc.ErrRepositoryCheckoutConnectionInvalid
		}
		checkout, checkoutErr := p.checkout.Resolve(ctx, domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: *build.RegisteredRepositoryID, SCMConnectionID: *build.SCMConnectionID, ProviderRepositoryID: *build.ProviderRepositoryID})
		if checkoutErr != nil {
			return WorkspacePreparePayload{}, checkoutErr
		}
		authenticatedResolver, ok := p.resolver.(source.AuthenticatedWorkspaceSourceResolver)
		if !ok {
			return WorkspacePreparePayload{}, buildsvc.ErrRepositoryCheckoutConnectionInvalid
		}
		cloneErr := checkout.RunWithCredentialRetry(ctx, func(credential source.HTTPSCredential) error {
			return authenticatedResolver.CloneIntoWorkspaceWithHTTPSCredential(ctx, workspacePath, checkout.RepositoryURL, credential)
		})
		if cloneErr != nil {
			return WorkspacePreparePayload{}, cloneErr
		}
	} else if cloneErr := p.resolver.CloneIntoWorkspace(ctx, workspacePath, repositoryURL); cloneErr != nil {
		return WorkspacePreparePayload{}, cloneErr
	}
	resolvedCommit, checkoutErr := p.resolver.CheckoutWorkspaceSource(ctx, workspacePath, source.WorkspaceSourceSpec{RepositoryURL: repositoryURL, Ref: optionalSourceRef(spec.Source, job.Source), CommitSHA: optionalSourceCommit(spec.Source, job.Source)})
	if checkoutErr != nil {
		return WorkspacePreparePayload{}, checkoutErr
	}
	if expected := optionalSourceCommit(spec.Source, job.Source); expected != "" && strings.TrimSpace(resolvedCommit) != expected {
		return WorkspacePreparePayload{}, fmt.Errorf("%w: pinned commit mismatch", ErrWorkspacePrepareInvalidInput)
	}
	archive, publication, archiveErr := workspace.ArchiveDirectory(ctx, workspacePath)
	if archiveErr != nil {
		return WorkspacePreparePayload{}, archiveErr
	}
	return WorkspacePreparePayload{Archive: archive, Publication: publication}, nil
}

func optionalSourceRef(spec domain.SourceSnapshotRef, fallback domain.SourceSnapshotRef) string {
	if spec.RefName != nil {
		return strings.TrimSpace(*spec.RefName)
	}
	if fallback.RefName != nil {
		return strings.TrimSpace(*fallback.RefName)
	}
	return ""
}

func optionalSourceCommit(spec domain.SourceSnapshotRef, fallback domain.SourceSnapshotRef) string {
	if strings.TrimSpace(spec.CommitSHA) != "" {
		return strings.TrimSpace(spec.CommitSHA)
	}
	return strings.TrimSpace(fallback.CommitSHA)
}

var _ WorkspaceSourceArchivePreparer = (*ServerSourceArchivePreparer)(nil)
