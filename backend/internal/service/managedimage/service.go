package managedimage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

const defaultBotBranchPrefix = "coyote/managed-image-refresh"

type RepoFetcher interface {
	Fetch(ctx context.Context, repoURL string, ref string) (localPath string, commitSHA string, err error)
}

type WritebackConfigLookup interface {
	GetByJobID(ctx context.Context, jobID string) (domain.JobManagedImageConfig, error)
}

type CredentialLookup interface {
	GetByID(ctx context.Context, id string) (domain.SourceCredential, error)
}

type ManagedImageCatalog interface {
	EnsureManagedImage(ctx context.Context, projectID string, name string) (domain.ManagedImage, error)
	FindVersionByFingerprint(ctx context.Context, managedImageID string, dependencyFingerprint string) (domain.ManagedImageVersion, bool, error)
	CreateVersion(ctx context.Context, version domain.ManagedImageVersion) (domain.ManagedImageVersion, error)
}

type ImagePublisher interface {
	Publish(ctx context.Context, req PublishRequest) (PublishedImage, error)
}

type PublishRequest struct {
	ProjectID             string
	RepositoryURL         string
	ManagedImageName      string
	DependencyFingerprint string
	RepoRoot              string
	PipelinePath          string
	BaseRef               string
}

type PublishedImage struct {
	ImageRef     string
	ImageDigest  string
	VersionLabel string
}

type GitWriteBack interface {
	CommitAndPushPipelineUpdate(ctx context.Context, req source.GitWriteBackRequest) (source.GitWriteBackResult, error)
}

type PullRequestCreator interface {
	CreateOrGetPullRequest(ctx context.Context, req source.GitHubPullRequestRequest) (source.GitHubPullRequestResult, error)
}

type Service struct {
	fetcher            RepoFetcher
	writebacks         WritebackConfigLookup
	credentials        CredentialLookup
	catalog            ManagedImageCatalog
	publisher          ImagePublisher
	writer             GitWriteBack
	pullRequests       PullRequestCreator
	clock              func() time.Time
	computeFingerprint func(repoRoot string, pipelinePath string) (string, []string, error)
}

func NewService(fetcher RepoFetcher, writebacks WritebackConfigLookup, credentials CredentialLookup, catalog ManagedImageCatalog, publisher ImagePublisher, writer GitWriteBack, pullRequests PullRequestCreator) *Service {
	return &Service{
		fetcher:            fetcher,
		writebacks:         writebacks,
		credentials:        credentials,
		catalog:            catalog,
		publisher:          publisher,
		writer:             writer,
		pullRequests:       pullRequests,
		clock:              time.Now,
		computeFingerprint: ComputeDependencyFingerprint,
	}
}

func (s *Service) RefreshManagedPipelineImage(ctx context.Context, req buildsvc.ManagedImageRefreshInput) (buildsvc.ManagedImageRefreshResult, error) {
	if s.fetcher == nil || s.writebacks == nil || s.credentials == nil || s.catalog == nil || s.publisher == nil || s.writer == nil {
		return buildsvc.ManagedImageRefreshResult{}, fmt.Errorf("managed image refresh service is not fully configured")
	}

	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		return buildsvc.ManagedImageRefreshResult{Updated: false}, nil
	}

	cfg, err := s.writebacks.GetByJobID(ctx, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrJobManagedImageConfigNotFound) {
			return buildsvc.ManagedImageRefreshResult{Updated: false}, nil
		}
		return buildsvc.ManagedImageRefreshResult{}, err
	}
	if !cfg.Enabled {
		return buildsvc.ManagedImageRefreshResult{Updated: false}, nil
	}

	pipelinePath := strings.TrimSpace(cfg.PipelinePath)
	if pipelinePath == "" {
		pipelinePath = ".coyote/pipeline.yml"
	}

	cloneRef := strings.TrimSpace(req.Ref)
	if cloneRef == "" {
		return buildsvc.ManagedImageRefreshResult{}, fmt.Errorf("source ref is required")
	}

	repoRoot, _, err := s.fetcher.Fetch(ctx, req.RepositoryURL, cloneRef)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}
	defer func() { _ = osRemoveAll(repoRoot) }()

	dependencyFingerprint, _, err := s.computeFingerprint(repoRoot, pipelinePath)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}

	managedImage, err := s.catalog.EnsureManagedImage(ctx, strings.TrimSpace(req.ProjectID), cfg.ManagedImageName)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}

	candidateVersion, found, err := s.catalog.FindVersionByFingerprint(ctx, managedImage.ID, dependencyFingerprint)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}
	if !found {
		published, publishErr := s.publisher.Publish(ctx, PublishRequest{
			ProjectID:             strings.TrimSpace(req.ProjectID),
			RepositoryURL:         strings.TrimSpace(req.RepositoryURL),
			ManagedImageName:      cfg.ManagedImageName,
			DependencyFingerprint: dependencyFingerprint,
			RepoRoot:              repoRoot,
			PipelinePath:          pipelinePath,
			BaseRef:               cloneRef,
		})
		if publishErr != nil {
			return buildsvc.ManagedImageRefreshResult{}, publishErr
		}
		if !isImmutableImageRef(published.ImageRef) {
			return buildsvc.ManagedImageRefreshResult{}, fmt.Errorf("published image ref must be immutable digest form, got %q", published.ImageRef)
		}

		fingerprintValue := dependencyFingerprint
		repoURLValue := strings.TrimSpace(req.RepositoryURL)
		candidateVersion, err = s.catalog.CreateVersion(ctx, domain.ManagedImageVersion{
			ID:                    uuid.NewString(),
			ManagedImageID:        managedImage.ID,
			VersionLabel:          defaultString(strings.TrimSpace(published.VersionLabel), dependencyFingerprint[:12]),
			ImageRef:              strings.TrimSpace(published.ImageRef),
			ImageDigest:           strings.TrimSpace(published.ImageDigest),
			DependencyFingerprint: &fingerprintValue,
			SourceRepositoryURL:   &repoURLValue,
			CreatedAt:             s.clock().UTC(),
		})
		if err != nil {
			return buildsvc.ManagedImageRefreshResult{}, err
		}
	}

	pipelineFilePath := filepath.Join(repoRoot, filepath.FromSlash(filepath.Clean(pipelinePath)))
	rawYAML, err := osReadFile(pipelineFilePath)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}
	updatedYAML, changed, err := pipeline.UpdatePipelineImageRef(rawYAML, candidateVersion.ImageRef)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}
	if !changed {
		return buildsvc.ManagedImageRefreshResult{
			ManagedImageID:        managedImage.ID,
			ManagedImageVersionID: candidateVersion.ID,
			DependencyFingerprint: dependencyFingerprint,
			PinnedImageRef:        candidateVersion.ImageRef,
			Updated:               false,
		}, nil
	}

	credential, err := s.credentials.GetByID(ctx, cfg.WriteCredentialID)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}
	branchPrefix := strings.TrimSpace(cfg.BotBranchPrefix)
	if branchPrefix == "" {
		branchPrefix = defaultBotBranchPrefix
	}
	branchName := branchPrefix + "/" + dependencyFingerprint[:12]
	commitMessage := deterministicCommitMessage(candidateVersion.ImageRef)
	baseBranch := strings.TrimSpace(req.BaseBranch)

	writeResult, err := s.writer.CommitAndPushPipelineUpdate(ctx, source.GitWriteBackRequest{
		RepositoryURL: strings.TrimSpace(req.RepositoryURL),
		RepoRoot:      repoRoot,
		PipelinePath:  pipelinePath,
		BranchName:    branchName,
		CommitMessage: commitMessage,
		Content:       updatedYAML,
		AuthorName:    cfg.CommitAuthorName,
		AuthorEmail:   cfg.CommitAuthorEmail,
		Credential:    credential,
	})
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}
	log.Printf("INFO: managed image refresh wrote pipeline update job_id=%s managed_image_id=%s managed_image_version_id=%s branch=%s commit_sha=%s", jobID, managedImage.ID, candidateVersion.ID, writeResult.BranchName, writeResult.CommitSHA)
	if s.pullRequests != nil {
		if baseBranch == "" {
			log.Printf("WARNING: managed image pull request skipped job_id=%s branch=%s repo=%s: missing base branch", jobID, writeResult.BranchName, strings.TrimSpace(req.RepositoryURL))
		} else {
			prResult, prErr := s.pullRequests.CreateOrGetPullRequest(ctx, source.GitHubPullRequestRequest{
				RepositoryURL: strings.TrimSpace(req.RepositoryURL),
				HeadBranch:    writeResult.BranchName,
				BaseBranch:    baseBranch,
				Title:         commitMessage,
				Body:          managedImagePullRequestBody(candidateVersion.ImageRef, dependencyFingerprint),
				Credential:    credential,
			})
			if prErr != nil {
				log.Printf("WARNING: managed image pull request creation failed job_id=%s branch=%s repo=%s: %v", jobID, writeResult.BranchName, strings.TrimSpace(req.RepositoryURL), prErr)
			} else if strings.TrimSpace(prResult.URL) != "" {
				log.Printf("INFO: managed image pull request ready job_id=%s branch=%s existing=%t url=%s", jobID, writeResult.BranchName, prResult.Existing, prResult.URL)
			}
		}
	}

	return buildsvc.ManagedImageRefreshResult{
		ManagedImageID:        managedImage.ID,
		ManagedImageVersionID: candidateVersion.ID,
		DependencyFingerprint: dependencyFingerprint,
		PinnedImageRef:        candidateVersion.ImageRef,
		Updated:               true,
		BranchName:            writeResult.BranchName,
		CommitSHA:             writeResult.CommitSHA,
	}, nil
}

func isImmutableImageRef(value string) bool {
	return strings.Contains(strings.TrimSpace(value), "@sha256:")
}

func deterministicCommitMessage(imageRef string) string {
	return "chore(coyote): refresh managed build image to " + strings.TrimSpace(imageRef)
}

func managedImagePullRequestBody(imageRef string, dependencyFingerprint string) string {
	return fmt.Sprintf("Automated managed image refresh.\n\n- Image: %s\n- Dependency fingerprint: %s\n", strings.TrimSpace(imageRef), strings.TrimSpace(dependencyFingerprint))
}

func defaultString(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

var osReadFile = func(path string) ([]byte, error) { return os.ReadFile(path) }
var osRemoveAll = func(path string) error { return os.RemoveAll(path) }
