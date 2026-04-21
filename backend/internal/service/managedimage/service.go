package managedimage

import (
	"context"
	"errors"
	"fmt"
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
	GetByProjectAndRepo(ctx context.Context, projectID string, repositoryURL string) (domain.RepoWritebackConfig, error)
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

type Service struct {
	fetcher            RepoFetcher
	writebacks         WritebackConfigLookup
	credentials        CredentialLookup
	catalog            ManagedImageCatalog
	publisher          ImagePublisher
	writer             GitWriteBack
	clock              func() time.Time
	computeFingerprint func(repoRoot string, pipelinePath string) (string, []string, error)
}

func NewService(fetcher RepoFetcher, writebacks WritebackConfigLookup, credentials CredentialLookup, catalog ManagedImageCatalog, publisher ImagePublisher, writer GitWriteBack) *Service {
	return &Service{
		fetcher:            fetcher,
		writebacks:         writebacks,
		credentials:        credentials,
		catalog:            catalog,
		publisher:          publisher,
		writer:             writer,
		clock:              time.Now,
		computeFingerprint: ComputeDependencyFingerprint,
	}
}

func (s *Service) RefreshManagedPipelineImage(ctx context.Context, req buildsvc.ManagedImageRefreshInput) (buildsvc.ManagedImageRefreshResult, error) {
	if s.fetcher == nil || s.writebacks == nil || s.credentials == nil || s.catalog == nil || s.publisher == nil || s.writer == nil {
		return buildsvc.ManagedImageRefreshResult{}, fmt.Errorf("managed image refresh service is not fully configured")
	}

	cfg, err := s.lookupWritebackConfig(ctx, strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.RepositoryURL))
	if err != nil {
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

	managedImage, err := s.catalog.EnsureManagedImage(ctx, cfg.ProjectID, cfg.ManagedImageName)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}

	candidateVersion, found, err := s.catalog.FindVersionByFingerprint(ctx, managedImage.ID, dependencyFingerprint)
	if err != nil {
		return buildsvc.ManagedImageRefreshResult{}, err
	}
	if !found {
		published, publishErr := s.publisher.Publish(ctx, PublishRequest{
			ProjectID:             cfg.ProjectID,
			RepositoryURL:         cfg.RepositoryURL,
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
		repoURLValue := strings.TrimSpace(cfg.RepositoryURL)
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

	writeResult, err := s.writer.CommitAndPushPipelineUpdate(ctx, source.GitWriteBackRequest{
		RepositoryURL: cfg.RepositoryURL,
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

func (s *Service) lookupWritebackConfig(ctx context.Context, projectID string, repositoryURL string) (domain.RepoWritebackConfig, error) {
	urlCandidates := repoURLCandidates(repositoryURL)
	var lastErr error
	for _, candidate := range urlCandidates {
		cfg, err := s.writebacks.GetByProjectAndRepo(ctx, projectID, candidate)
		if err == nil {
			return cfg, nil
		}
		if !errors.Is(err, repository.ErrRepoWritebackConfigNotFound) {
			return domain.RepoWritebackConfig{}, err
		}
		lastErr = err
	}
	if lastErr != nil {
		return domain.RepoWritebackConfig{}, lastErr
	}
	return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
}

func repoURLCandidates(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return []string{trimmed}
	}

	base := strings.TrimSuffix(strings.TrimSuffix(trimmed, "/"), ".git")
	candidates := []string{trimmed, strings.TrimSuffix(trimmed, "/"), base, base + ".git"}
	seen := map[string]bool{}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		result = append(result, candidate)
	}
	return result
}

func isImmutableImageRef(value string) bool {
	return strings.Contains(strings.TrimSpace(value), "@sha256:")
}

func deterministicCommitMessage(imageRef string) string {
	return "chore(coyote): refresh managed build image to " + strings.TrimSpace(imageRef)
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
