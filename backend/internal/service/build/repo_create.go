package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

// CreateRepoBuildInput is the service-level input for creating a build from a repository checkout.
type CreateRepoBuildInput struct {
	ProjectID          string
	JobID              *string
	Priority           int
	RepoURL            string
	Ref                string
	CommitSHA          string
	PipelinePath       string
	Trigger            *CreateBuildTriggerInput
	RepositoryIdentity *domain.RepositoryIdentitySnapshot
}

const pipelineFilePath = ".coyote/pipeline.yml"
const pipelineSourceRepo = "repo"
const pipelineSourceInline = "inline"

// CreateBuildFromRepo clones the repo, loads .coyote/pipeline.yml, parses/validates/resolves
// it, then creates a queued build with canonical build steps and repo source metadata.
func (s *BuildService) CreateBuildFromRepo(ctx context.Context, input CreateRepoBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}
	if err := validateRequestedBuildPriority(input.Priority); err != nil {
		return domain.Build{}, err
	}
	if strings.TrimSpace(input.RepoURL) == "" {
		return domain.Build{}, ErrRepoURLRequired
	}
	if strings.TrimSpace(input.Ref) == "" && strings.TrimSpace(input.CommitSHA) == "" {
		return domain.Build{}, ErrSourceTargetRequired
	}
	if s.repoFetcher == nil {
		return domain.Build{}, ErrRepoFetcherNotConfigured
	}

	fetchTarget := strings.TrimSpace(input.CommitSHA)
	if fetchTarget == "" {
		fetchTarget = strings.TrimSpace(input.Ref)
	}

	localPath, commitSHA, err := s.repoFetcher.Fetch(ctx, input.RepoURL, fetchTarget)
	if err != nil {
		return domain.Build{}, fmt.Errorf("fetching repo: %w", err)
	}
	defer func() {
		if localPath != "" {
			_ = os.RemoveAll(localPath)
		}
	}()

	absPipelinePath, effectivePipelinePath, err := resolveRepoPipelinePath(localPath, input.PipelinePath)
	if err != nil {
		return domain.Build{}, err
	}

	src := pipeline.FileSource{Path: absPipelinePath}
	yamlData, _, err := src.Load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Build{}, fmt.Errorf("%w: %s", ErrPipelineFileNotFound, effectivePipelinePath)
		}
		return domain.Build{}, fmt.Errorf("loading pipeline file: %w", err)
	}

	resolved, err := pipeline.LoadAndResolve(yamlData)
	if err != nil {
		return domain.Build{}, err
	}

	resolved, err = resolveRepoStepWorkingDirs(effectivePipelinePath, resolved)
	if err != nil {
		return domain.Build{}, err
	}

	buildID := uuid.NewString()
	steps := pipelineStepsToDomain(buildID, resolved.Steps)

	yamlText := string(yamlData)
	var pipelineNamePtr *string
	if resolved.Name != "" {
		pipelineNamePtr = &resolved.Name
	}
	pipelineSource := pipelineSourceRepo
	pipelinePath := effectivePipelinePath

	repoURL := strings.TrimSpace(input.RepoURL)
	ref := strings.TrimSpace(input.Ref)
	requestedCommitSHA := strings.TrimSpace(input.CommitSHA)
	var commitSHAPtr *string
	if commitSHA != "" {
		commitSHAPtr = &commitSHA
	}

	sourceCommitValue := requestedCommitSHA
	if commitSHA != "" {
		sourceCommitValue = commitSHA
	}
	domainSource := domain.NewSourceSpec(repoURL, ref, sourceCommitValue)

	build := domain.Build{
		ID:                 buildID,
		ProjectID:          input.ProjectID,
		JobID:              input.JobID,
		Priority:           domain.NormalizePriority(input.Priority),
		Status:             domain.BuildStatusQueued,
		AttemptNumber:      1,
		CreatedAt:          time.Now().UTC(),
		CurrentStepIndex:   0,
		PipelineConfigYAML: &yamlText,
		PipelineName:       pipelineNamePtr,
		PipelineSource:     &pipelineSource,
		PipelinePath:       &pipelinePath,
		Source:             domainSource,
		RepoURL:            &repoURL,
		Ref:                buildOptionalStringPtr(ref),
		CommitSHA:          commitSHAPtr,
		Trigger:            toDomainBuildTrigger(input.Trigger),
		RequestedImageRef:  buildOptionalStringPtr(strings.TrimSpace(resolved.Image)),
		ImageSourceKind:    domain.ImageSourceKindExternal,
	}
	if identityErr := applyBuildRepositoryIdentity(&build, input.RepositoryIdentity); identityErr != nil {
		return domain.Build{}, identityErr
	}
	build = domain.NormalizeBuildMetadata(build)

	queuedBuild, err := s.buildRepo.CreateQueuedBuild(ctx, build, steps)
	if err != nil {
		return domain.Build{}, err
	}
	if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
		log.Printf("WARNING: durable job creation failed for build_id=%s (build already persisted): %v", queuedBuild.ID, err)
		return domain.Build{}, fmt.Errorf("create execution jobs for build %s: %w", queuedBuild.ID, err)
	}
	s.notifySCMBuildStatus(ctx, queuedBuild)
	if s.managedImageRefresher != nil {
		refreshRef := strings.TrimSpace(input.CommitSHA)
		if refreshRef == "" {
			refreshRef = strings.TrimSpace(input.Ref)
		}
		if refreshRef != "" {
			jobID := ""
			if input.JobID != nil {
				jobID = strings.TrimSpace(*input.JobID)
			}
			refreshResult, refreshErr := s.managedImageRefresher.RefreshManagedPipelineImage(ctx, ManagedImageRefreshInput{
				JobID:         jobID,
				ProjectID:     input.ProjectID,
				RepositoryURL: input.RepoURL,
				Ref:           refreshRef,
				BaseBranch:    strings.TrimSpace(input.Ref),
				PipelinePath:  effectivePipelinePath,
			})
			if refreshErr != nil {
				log.Printf("WARNING: managed image refresh write-back failed for build_id=%s repo=%s: %v", queuedBuild.ID, input.RepoURL, refreshErr)
			} else if refreshResult.ManagedImageID != "" && refreshResult.ManagedImageVersionID != "" && strings.TrimSpace(refreshResult.PinnedImageRef) != "" {
				requestedRef := buildOptionalStringPtr(strings.TrimSpace(resolved.Image))
				resolvedRef := buildOptionalStringPtr(strings.TrimSpace(refreshResult.PinnedImageRef))
				managedImageID := buildOptionalStringPtr(refreshResult.ManagedImageID)
				managedImageVersionID := buildOptionalStringPtr(refreshResult.ManagedImageVersionID)
				updatedBuild, updateImageErr := s.buildRepo.UpdateImageExecution(ctx, queuedBuild.ID, requestedRef, resolvedRef, domain.ImageSourceKindManaged, managedImageID, managedImageVersionID)
				if updateImageErr != nil {
					log.Printf("WARNING: managed image provenance update failed for build_id=%s: %v", queuedBuild.ID, updateImageErr)
				} else {
					log.Printf("INFO: managed image provenance updated build_id=%s source_kind=%s managed_image_id=%s managed_image_version_id=%s", updatedBuild.ID, updatedBuild.ImageSourceKind, buildReadOptionalString(updatedBuild.ManagedImageID), buildReadOptionalString(updatedBuild.ManagedImageVersionID))
					queuedBuild = updatedBuild
				}
			}
		}
	}
	return queuedBuild, nil
}
