package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrJobNotFound = errors.New("job not found")
var ErrJobIDRequired = errors.New("job id is required")
var ErrJobNameRequired = errors.New("job name is required")
var ErrJobProjectIDRequired = errors.New("job project_id is required")
var ErrJobRepositorySourceRequired = errors.New("job repository_id or repository_url is required")
var ErrJobRepositoryAssignmentConflict = errors.New("job repository_id cannot be combined with repository_url")
var ErrJobRepositoryIDEmpty = errors.New("job repository_id cannot be empty")
var ErrJobRegisteredRepositoryDisabled = errors.New("job registered repository is disabled")
var ErrJobRegisteredRepositoryStoreNotConfigured = errors.New("job registered repository store not configured")
var ErrJobSourceTargetRequired = errors.New("job default_ref or default_commit_sha is required")
var ErrJobPipelineDefinitionRequired = errors.New("job pipeline_yaml or pipeline_path is required")
var ErrJobInvalidTriggerMode = errors.New("job trigger_mode must be one of branches, tags, branches_and_tags")
var ErrJobPriorityOutOfRange = errors.New("job priority must be between 1 and 10")
var ErrPushEventRepositoryURLRequired = errors.New("push event repository_url is required")
var ErrWebhookProviderRepositoryIDRequired = errors.New("webhook provider_repository_id is required")
var ErrPushEventRefRequired = errors.New("push event ref is required")
var ErrPushEventCommitSHARequired = errors.New("push event commit_sha is required")
var ErrJobNameAmbiguous = errors.New("job selector matched multiple jobs in project")
var ErrJobDisabled = errors.New("job is disabled")
var ErrJobRunRefRequired = errors.New("job run ref is required")
var ErrJobBuildServiceNotConfigured = errors.New("job build service not configured")
var ErrJobManagedImageConfigNotConfigured = errors.New("job managed image config repository not configured")
var ErrJobManagedImageNameRequired = errors.New("job managed image managed_image_name is required")
var ErrJobManagedImagePipelinePathRequired = errors.New("job managed image pipeline_path is required")
var ErrJobManagedImageWriteCredentialIDRequired = errors.New("job managed image write_credential_id is required")
var ErrJobArtifactTriggerProducerJobIDRequired = errors.New("job artifact_triggers producer_job_id is required")
var ErrJobArtifactTriggerPathRequired = errors.New("job artifact_triggers path is required")
var ErrJobArtifactTriggerSelfReference = errors.New("job artifact_triggers cannot reference the same job")
var ErrArtifactTriggerDeliveryQueuedBuildConflict = errors.New("artifact trigger delivery already references a downstream build")
var ErrArtifactTriggerDeliveryPendingRetryDeferred = errors.New("artifact trigger delivery pending recovery is not retryable in v1.3")
var ErrArtifactTriggerDeliveryRetryNotSupported = errors.New("artifact trigger delivery retry is not supported for this delivery state")

type JobService struct {
	jobRepo                   repository.JobRepository
	artifactTriggerDeliveries repository.ArtifactTriggerDeliveryRepository
	projects                  repository.ProjectRepository
	registeredRepositories    repository.SCMRepositoryRegistrationRepository
	managedImageConfigs       repository.JobManagedImageConfigRepository
	credentials               repository.SourceCredentialRepository
	buildService              *buildsvc.BuildService
}

func NewJobService(jobRepo repository.JobRepository, buildService *buildsvc.BuildService) *JobService {
	return &JobService{jobRepo: jobRepo, buildService: buildService}
}

func (s *JobService) WithProjectRepository(projects repository.ProjectRepository) *JobService {
	s.projects = projects
	return s
}

func (s *JobService) WithManagedImageConfigRepository(configs repository.JobManagedImageConfigRepository, credentials repository.SourceCredentialRepository) *JobService {
	s.managedImageConfigs = configs
	s.credentials = credentials
	return s
}

func (s *JobService) WithSCMRepositoryRegistrationRepository(repositories repository.SCMRepositoryRegistrationRepository) *JobService {
	s.registeredRepositories = repositories
	return s
}

func (s *JobService) WithArtifactTriggerDeliveryRepository(deliveries repository.ArtifactTriggerDeliveryRepository) *JobService {
	s.artifactTriggerDeliveries = deliveries
	return s
}

type ManagedImageConfigInput struct {
	Enabled           bool
	ManagedImageName  string
	PipelinePath      string
	WriteCredentialID string
	BotBranchPrefix   *string
	CommitAuthorName  *string
	CommitAuthorEmail *string
}

type ManagedImageConfigPatch struct {
	Enabled           *bool
	ManagedImageName  *string
	PipelinePath      *string
	WriteCredentialID *string
	BotBranchPrefix   *string
	CommitAuthorName  *string
	CommitAuthorEmail *string
}

type CreateJobInput struct {
	ProjectID        string
	ProjectSlug      string
	Name             string
	Priority         *int
	RepositoryID     string
	RepositoryURL    string
	DefaultRef       string
	DefaultCommitSHA string
	PushEnabled      *bool
	PushBranch       *string
	TriggerMode      *string
	BranchAllowlist  []string
	TagAllowlist     []string
	ArtifactTriggers []domain.JobArtifactTrigger
	PipelineYAML     string
	PipelinePath     string
	ManagedImage     *ManagedImageConfigInput
	Enabled          *bool
}

type UpdateJobInput struct {
	Name             *string
	Priority         *int
	RepositoryIDSet  bool
	RepositoryID     *string
	RepositoryURL    *string
	DefaultRef       *string
	DefaultCommitSHA *string
	PushEnabled      *bool
	PushBranch       *string
	TriggerMode      *string
	BranchAllowlist  *[]string
	TagAllowlist     *[]string
	ArtifactTriggers *[]domain.JobArtifactTrigger
	PipelineYAML     *string
	PipelinePath     *string
	ManagedImageSet  bool
	ManagedImage     *ManagedImageConfigPatch
	Enabled          *bool
}

func (s *JobService) CreateJob(ctx context.Context, input CreateJobInput) (domain.Job, error) {
	normalized, err := normalizeCreateJobInput(input)
	if err != nil {
		return domain.Job{}, err
	}

	projectID, err := s.resolveProjectID(ctx, normalized.ProjectID, normalized.ProjectSlug)
	if err != nil {
		return domain.Job{}, err
	}

	repositoryID, repositoryURL, err := s.resolveJobRepositorySource(ctx, normalized.RepositoryID, normalized.RepositoryURL)
	if err != nil {
		return domain.Job{}, err
	}
	normalized.RepositoryID = readStringPtr(repositoryID)
	normalized.RepositoryURL = repositoryURL

	if strings.TrimSpace(normalized.PipelineYAML) != "" {
		if validateErr := validatePipelineYAML(normalized.PipelineYAML); validateErr != nil {
			return domain.Job{}, validateErr
		}
	}

	var defaultCommitSHA *string
	if strings.TrimSpace(normalized.DefaultCommitSHA) != "" {
		v := strings.TrimSpace(normalized.DefaultCommitSHA)
		defaultCommitSHA = &v
	}
	var pipelinePath *string
	if strings.TrimSpace(normalized.PipelinePath) != "" {
		v := strings.TrimSpace(normalized.PipelinePath)
		pipelinePath = &v
	}

	if validateErr := validatePipelineDefinition(normalized.PipelineYAML, pipelinePath); validateErr != nil {
		return domain.Job{}, validateErr
	}

	enabled := true
	if normalized.Enabled != nil {
		enabled = *normalized.Enabled
	}
	pushEnabled := false
	if normalized.PushEnabled != nil {
		pushEnabled = *normalized.PushEnabled
	}
	var pushBranch *string
	if pushEnabled && normalized.PushBranch != nil {
		branch := normalizePushRef(*normalized.PushBranch)
		if branch != "" {
			pushBranch = &branch
		}
	}

	triggerMode := webhooksvc.NormalizeWebhookFilterMode(domain.JobTriggerMode(readStringPtr(normalized.TriggerMode)))
	branchAllowlist := normalizeBranchAllowlist(normalized.BranchAllowlist)
	if len(branchAllowlist) == 0 && pushBranch != nil {
		branchAllowlist = []string{*pushBranch}
	}
	tagAllowlist := normalizeTagAllowlist(normalized.TagAllowlist)

	now := time.Now().UTC()
	job := domain.Job{
		ID:               uuid.NewString(),
		ProjectID:        projectID,
		Name:             normalized.Name,
		Priority:         normalizedPriority(normalized.Priority),
		RepositoryID:     repositoryID,
		RepositoryURL:    normalized.RepositoryURL,
		DefaultRef:       normalized.DefaultRef,
		DefaultCommitSHA: defaultCommitSHA,
		PushEnabled:      pushEnabled,
		PushBranch:       pushBranch,
		TriggerMode:      triggerMode,
		BranchAllowlist:  branchAllowlist,
		TagAllowlist:     tagAllowlist,
		ArtifactTriggers: domain.NormalizeJobArtifactTriggers(normalized.ArtifactTriggers),
		PipelineYAML:     normalized.PipelineYAML,
		PipelinePath:     pipelinePath,
		Enabled:          enabled,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	for _, trigger := range job.ArtifactTriggers {
		if trigger.ProducerJobID == job.ID {
			return domain.Job{}, ErrJobArtifactTriggerSelfReference
		}
	}

	created, err := s.jobRepo.Create(ctx, job)
	if err != nil {
		return domain.Job{}, err
	}
	if input.ManagedImage != nil && input.ManagedImage.Enabled {
		config, err := s.upsertManagedImageConfig(ctx, created, input.ManagedImage)
		if err != nil {
			return domain.Job{}, err
		}
		created.ManagedImageConfig = &config
	}

	if !created.Enabled {
		return created, nil
	}

	_, createErr := s.createBuildForJob(ctx, created)
	if createErr != nil {
		if rollbackErr := s.rollbackCreatedJob(ctx, created); rollbackErr != nil {
			return domain.Job{}, fmt.Errorf("creating initial build: %w; rollback failed: %v", createErr, rollbackErr)
		}
		return domain.Job{}, createErr
	}

	return created, nil
}

func (s *JobService) rollbackCreatedJob(ctx context.Context, job domain.Job) error {
	if job.ManagedImageConfig != nil && s.managedImageConfigs != nil {
		if err := s.managedImageConfigs.DeleteByJobID(ctx, job.ID); err != nil && !errors.Is(err, repository.ErrJobManagedImageConfigNotFound) {
			return err
		}
	}
	if err := s.jobRepo.Delete(ctx, job.ID); err != nil && !errors.Is(err, repository.ErrJobNotFound) {
		return err
	}
	return nil
}

func (s *JobService) ListJobs(ctx context.Context) ([]domain.Job, error) {
	jobs, err := s.jobRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	if enrichErr := s.attachLatestBuilds(ctx, jobs); enrichErr != nil {
		return nil, enrichErr
	}
	return jobs, nil
}

func (s *JobService) GetJobsByIDs(ctx context.Context, ids []string) ([]domain.Job, error) {
	return s.jobRepo.GetByIDs(ctx, ids)
}

func (s *JobService) GetRegisteredRepositoriesByIDs(ctx context.Context, ids []string) (map[string]domain.SCMRepositoryRegistration, error) {
	if s.registeredRepositories == nil || len(ids) == 0 {
		return map[string]domain.SCMRepositoryRegistration{}, nil
	}
	items, err := s.registeredRepositories.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	indexed := make(map[string]domain.SCMRepositoryRegistration, len(items))
	for _, item := range items {
		indexed[item.ID] = item
	}
	return indexed, nil
}

func (s *JobService) ListJobsPaged(ctx context.Context, params repository.ListParams) ([]domain.Job, error) {
	jobs, err := s.jobRepo.ListPaged(ctx, params)
	if err != nil {
		return nil, err
	}
	if enrichErr := s.attachLatestBuilds(ctx, jobs); enrichErr != nil {
		return nil, enrichErr
	}
	return jobs, nil
}

func (s *JobService) ListJobsByProject(ctx context.Context, projectID string) ([]domain.Job, error) {
	trimmedID := strings.TrimSpace(projectID)
	if trimmedID == "" {
		return nil, ErrProjectIDRequired
	}
	resolvedProjectID := trimmedID
	if s.projects != nil {
		var err error
		resolvedProjectID, err = s.resolveProjectID(ctx, trimmedID, "")
		if err != nil {
			return nil, err
		}
	}
	jobs, err := s.jobRepo.ListByProjectID(ctx, resolvedProjectID)
	if err != nil {
		return nil, err
	}
	if enrichErr := s.attachLatestBuilds(ctx, jobs); enrichErr != nil {
		return nil, enrichErr
	}
	return jobs, nil
}

func (s *JobService) attachLatestBuilds(ctx context.Context, jobs []domain.Job) error {
	if s.buildService == nil {
		return nil
	}
	if len(jobs) == 0 {
		return nil
	}

	jobIDs := make([]string, 0, len(jobs))
	for i := range jobs {
		jobIDs = append(jobIDs, jobs[i].ID)
	}

	latestBuilds, err := s.buildService.ListLatestBuildsByJobIDs(ctx, jobIDs)
	if err != nil {
		return err
	}

	for i := range jobs {
		build, ok := latestBuilds[jobs[i].ID]
		if !ok {
			jobs[i].LatestBuild = nil
			continue
		}
		jobs[i].LatestBuild = &domain.JobBuildSummary{
			ID:           build.ID,
			BuildNumber:  build.BuildNumber,
			Status:       build.Status,
			CreatedAt:    build.CreatedAt,
			FinishedAt:   build.FinishedAt,
			ErrorMessage: build.ErrorMessage,
		}
	}

	return nil
}

func (s *JobService) ListBuildsByJobID(ctx context.Context, jobID string) ([]domain.Build, error) {
	if s.buildService == nil {
		return nil, ErrJobBuildServiceNotConfigured
	}
	return s.buildService.ListBuildsByJobID(ctx, jobID)
}

func (s *JobService) ResolveProjectID(ctx context.Context, projectID string, projectSlug string) (string, error) {
	return s.resolveProjectID(ctx, projectID, projectSlug)
}

func (s *JobService) GetJob(ctx context.Context, id string) (domain.Job, error) {
	if strings.TrimSpace(id) == "" {
		return domain.Job{}, ErrJobIDRequired
	}

	job, err := s.jobRepo.GetByID(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return domain.Job{}, ErrJobNotFound
		}
		return domain.Job{}, err
	}
	if err := s.attachManagedImageConfig(ctx, &job); err != nil {
		return domain.Job{}, err
	}

	return job, nil
}

func (s *JobService) ResolveJobByProjectAndName(ctx context.Context, projectID string, name string) (domain.Job, error) {
	trimmedProjectID := strings.TrimSpace(projectID)
	if trimmedProjectID == "" {
		return domain.Job{}, ErrProjectIDRequired
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return domain.Job{}, ErrJobNameRequired
	}
	if s.projects != nil {
		if _, err := s.projects.GetByID(ctx, trimmedProjectID); err != nil {
			return domain.Job{}, err
		}
	}

	matches, err := s.jobRepo.FindByProjectIDAndName(ctx, trimmedProjectID, trimmedName, 2)
	if err != nil {
		return domain.Job{}, err
	}
	if len(matches) == 0 {
		return domain.Job{}, ErrJobNotFound
	}
	if len(matches) > 1 {
		return domain.Job{}, ErrJobNameAmbiguous
	}

	job := matches[0]
	if err := s.attachManagedImageConfig(ctx, &job); err != nil {
		return domain.Job{}, err
	}
	enriched := []domain.Job{job}
	if err := s.attachLatestBuilds(ctx, enriched); err != nil {
		return domain.Job{}, err
	}
	return enriched[0], nil
}

func (s *JobService) UpdateJob(ctx context.Context, id string, input UpdateJobInput) (domain.Job, error) {
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}

	repositoryID, repositoryURL, err := s.resolveUpdatedJobRepositorySource(ctx, job, input)
	if err != nil {
		return domain.Job{}, err
	}

	if input.Name != nil {
		job.Name = strings.TrimSpace(*input.Name)
	}
	if input.Priority != nil {
		if !domain.ValidPriority(*input.Priority) {
			return domain.Job{}, ErrJobPriorityOutOfRange
		}
		job.Priority = *input.Priority
	}
	job.RepositoryID = repositoryID
	job.RepositoryURL = repositoryURL
	if input.DefaultRef != nil {
		job.DefaultRef = strings.TrimSpace(*input.DefaultRef)
	}
	if input.DefaultCommitSHA != nil {
		commit := strings.TrimSpace(*input.DefaultCommitSHA)
		if commit == "" {
			job.DefaultCommitSHA = nil
		} else {
			job.DefaultCommitSHA = &commit
		}
	}
	if input.PushEnabled != nil {
		job.PushEnabled = *input.PushEnabled
	}
	if input.PushBranch != nil {
		branch := normalizePushRef(*input.PushBranch)
		if branch == "" {
			job.PushBranch = nil
		} else {
			job.PushBranch = &branch
		}
	}
	if input.TriggerMode != nil {
		if !isValidTriggerMode(*input.TriggerMode) {
			return domain.Job{}, ErrJobInvalidTriggerMode
		}
		mode := webhooksvc.NormalizeWebhookFilterMode(domain.JobTriggerMode(strings.TrimSpace(*input.TriggerMode)))
		job.TriggerMode = mode
	}
	if input.BranchAllowlist != nil {
		job.BranchAllowlist = normalizeBranchAllowlist(*input.BranchAllowlist)
	}
	if input.TagAllowlist != nil {
		job.TagAllowlist = normalizeTagAllowlist(*input.TagAllowlist)
	}
	if input.ArtifactTriggers != nil {
		if validateErr := validateRawArtifactTriggers(*input.ArtifactTriggers); validateErr != nil {
			return domain.Job{}, validateErr
		}
		job.ArtifactTriggers = domain.NormalizeJobArtifactTriggers(*input.ArtifactTriggers)
	}
	// If push has been explicitly disabled and no new push branch was provided,
	// clear any existing branch filter to avoid leaving stale configuration.
	if input.PushEnabled != nil && !*input.PushEnabled && input.PushBranch == nil {
		job.PushBranch = nil
	}
	if len(job.BranchAllowlist) == 0 && job.PushBranch != nil {
		job.BranchAllowlist = []string{*job.PushBranch}
	}
	if input.PipelineYAML != nil {
		job.PipelineYAML = strings.TrimSpace(*input.PipelineYAML)
	}
	if input.PipelinePath != nil {
		path := strings.TrimSpace(*input.PipelinePath)
		if path == "" {
			job.PipelinePath = nil
		} else {
			job.PipelinePath = &path
		}
	}
	if input.Enabled != nil {
		job.Enabled = *input.Enabled
	}
	for _, trigger := range job.ArtifactTriggers {
		if trigger.ProducerJobID == job.ID {
			return domain.Job{}, ErrJobArtifactTriggerSelfReference
		}
	}

	if validateErr := validateJobRequiredFields(job); validateErr != nil {
		return domain.Job{}, validateErr
	}
	if strings.TrimSpace(job.PipelineYAML) != "" {
		if validateErr := validatePipelineYAML(job.PipelineYAML); validateErr != nil {
			return domain.Job{}, validateErr
		}
	}
	if validateErr := validatePipelineDefinition(job.PipelineYAML, job.PipelinePath); validateErr != nil {
		return domain.Job{}, validateErr
	}

	job.UpdatedAt = time.Now().UTC()
	updated, err := s.jobRepo.Update(ctx, job)
	if err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return domain.Job{}, ErrJobNotFound
		}
		return domain.Job{}, err
	}
	if input.ManagedImageSet {
		config, configErr := s.patchManagedImageConfig(ctx, updated, input.ManagedImage)
		if configErr != nil {
			return domain.Job{}, configErr
		}
		updated.ManagedImageConfig = config
	} else if attachErr := s.attachManagedImageConfig(ctx, &updated); attachErr != nil {
		return domain.Job{}, attachErr
	}

	return updated, nil
}

func (s *JobService) resolveJobRepositorySource(ctx context.Context, repositoryID string, repositoryURL string) (*string, string, error) {
	trimmedRepositoryID := strings.TrimSpace(repositoryID)
	trimmedRepositoryURL := strings.TrimSpace(repositoryURL)
	if trimmedRepositoryID != "" && trimmedRepositoryURL != "" {
		return nil, "", ErrJobRepositoryAssignmentConflict
	}
	if trimmedRepositoryID != "" {
		registration, err := s.resolveAssignableRegisteredRepository(ctx, trimmedRepositoryID)
		if err != nil {
			return nil, "", err
		}
		return trimmedStringPtr(registration.ID), registration.CloneURL, nil
	}
	if trimmedRepositoryURL == "" {
		return nil, "", ErrJobRepositorySourceRequired
	}
	return nil, trimmedRepositoryURL, nil
}

func (s *JobService) resolveUpdatedJobRepositorySource(ctx context.Context, job domain.Job, input UpdateJobInput) (*string, string, error) {
	if input.RepositoryIDSet {
		if input.RepositoryID == nil {
			if input.RepositoryURL == nil {
				return nil, "", ErrJobRepositorySourceRequired
			}
			trimmedRepositoryURL := strings.TrimSpace(*input.RepositoryURL)
			if trimmedRepositoryURL == "" {
				return nil, "", ErrJobRepositorySourceRequired
			}
			return nil, trimmedRepositoryURL, nil
		}
		if input.RepositoryURL != nil {
			return nil, "", ErrJobRepositoryAssignmentConflict
		}
		trimmedRepositoryID := strings.TrimSpace(*input.RepositoryID)
		if trimmedRepositoryID == "" {
			return nil, "", ErrJobRepositoryIDEmpty
		}
		registration, err := s.resolveAssignableRegisteredRepository(ctx, trimmedRepositoryID)
		if err != nil {
			return nil, "", err
		}
		return trimmedStringPtr(registration.ID), registration.CloneURL, nil
	}

	if input.RepositoryURL != nil {
		if job.RepositoryID != nil {
			return nil, "", ErrJobRepositoryAssignmentConflict
		}
		return nil, strings.TrimSpace(*input.RepositoryURL), nil
	}

	return job.RepositoryID, job.RepositoryURL, nil
}

func (s *JobService) resolveAssignableRegisteredRepository(ctx context.Context, repositoryID string) (domain.SCMRepositoryRegistration, error) {
	if s.registeredRepositories == nil {
		return domain.SCMRepositoryRegistration{}, repository.ErrSCMRepositoryRegistrationNotFound
	}
	registration, err := s.registeredRepositories.GetByID(ctx, strings.TrimSpace(repositoryID))
	if err != nil {
		return domain.SCMRepositoryRegistration{}, err
	}
	if registration.Disabled {
		return domain.SCMRepositoryRegistration{}, ErrJobRegisteredRepositoryDisabled
	}
	return registration, nil
}

func trimmedStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *JobService) RunJobNow(ctx context.Context, id string, ref *string) (domain.Build, error) {
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}
	if !job.Enabled {
		return domain.Build{}, ErrJobDisabled
	}
	if ref != nil {
		trimmedRef := strings.TrimSpace(*ref)
		if trimmedRef == "" {
			return domain.Build{}, ErrJobRunRefRequired
		}
		job.DefaultRef = trimmedRef
		job.DefaultCommitSHA = nil
	}

	return s.createBuildForJob(ctx, job)
}

func (s *JobService) createBuildForJob(ctx context.Context, job domain.Job) (domain.Build, error) {
	return s.createBuildForJobWithTrigger(ctx, job, nil)
}

func (s *JobService) createBuildForJobWithTrigger(ctx context.Context, job domain.Job, trigger *buildsvc.CreateBuildTriggerInput) (domain.Build, error) {
	if s.buildService == nil {
		return domain.Build{}, ErrJobBuildServiceNotConfigured
	}

	identity, identityErr := s.buildRepositoryIdentity(ctx, job)
	if identityErr != nil {
		return domain.Build{}, identityErr
	}

	var build domain.Build
	var err error
	if strings.TrimSpace(job.PipelineYAML) != "" {
		build, err = s.buildService.CreateBuildFromPipeline(ctx, buildsvc.CreatePipelineBuildInput{
			ProjectID:          job.ProjectID,
			JobID:              &job.ID,
			Priority:           job.Priority,
			PipelineYAML:       job.PipelineYAML,
			PipelinePath:       readStringPtr(job.PipelinePath),
			Trigger:            trigger,
			RepositoryIdentity: identity,
			Source: &buildsvc.CreateBuildSourceInput{
				RepositoryURL: job.RepositoryURL,
				Ref:           job.DefaultRef,
				CommitSHA:     readStringPtr(job.DefaultCommitSHA),
			},
		})
	} else if job.PipelinePath != nil && strings.TrimSpace(*job.PipelinePath) != "" {
		build, err = s.buildService.CreateBuildFromRepo(ctx, buildsvc.CreateRepoBuildInput{
			ProjectID:          job.ProjectID,
			JobID:              &job.ID,
			Priority:           job.Priority,
			RepoURL:            job.RepositoryURL,
			Ref:                job.DefaultRef,
			CommitSHA:          readStringPtr(job.DefaultCommitSHA),
			PipelinePath:       strings.TrimSpace(*job.PipelinePath),
			Trigger:            trigger,
			RepositoryIdentity: identity,
		})
	} else {
		build, err = s.buildService.CreateBuildFromPipeline(ctx, buildsvc.CreatePipelineBuildInput{
			ProjectID:          job.ProjectID,
			JobID:              &job.ID,
			Priority:           job.Priority,
			PipelineYAML:       job.PipelineYAML,
			Trigger:            trigger,
			RepositoryIdentity: identity,
			Source: &buildsvc.CreateBuildSourceInput{
				RepositoryURL: job.RepositoryURL,
				Ref:           job.DefaultRef,
				CommitSHA:     readStringPtr(job.DefaultCommitSHA),
			},
		})
	}
	if err != nil {
		return domain.Build{}, err
	}

	return build, nil
}

func (s *JobService) buildRepositoryIdentity(ctx context.Context, job domain.Job) (*domain.RepositoryIdentitySnapshot, error) {
	if job.RepositoryID == nil || strings.TrimSpace(*job.RepositoryID) == "" {
		return nil, nil
	}
	if s.registeredRepositories == nil {
		return nil, ErrJobRegisteredRepositoryStoreNotConfigured
	}
	registration, err := s.registeredRepositories.GetByID(ctx, strings.TrimSpace(*job.RepositoryID))
	if err != nil {
		return nil, err
	}
	identity := &domain.RepositoryIdentitySnapshot{
		RegisteredRepositoryID: registration.ID,
		SCMConnectionID:        registration.ConnectionID,
		ProviderRepositoryID:   registration.ProviderRepositoryID,
	}
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	return identity, nil
}

func (s *JobService) DispatchArtifactTriggers(ctx context.Context, build domain.Build, artifact domain.BuildArtifact) error {
	if s.artifactTriggerDeliveries == nil || s.buildService == nil || build.Trigger.Kind == domain.BuildTriggerKindArtifact {
		return nil
	}
	if build.JobID == nil || strings.TrimSpace(*build.JobID) == "" {
		return nil
	}

	producerJobID := strings.TrimSpace(*build.JobID)
	jobs, err := s.jobRepo.ListByProjectID(ctx, build.ProjectID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if !job.Enabled || strings.TrimSpace(job.ID) == "" || job.ID == producerJobID {
			continue
		}
		for _, triggerConfig := range domain.NormalizeJobArtifactTriggers(job.ArtifactTriggers) {
			if triggerConfig.ProducerJobID != producerJobID || triggerConfig.Path != artifact.LogicalPath {
				continue
			}
			if dispatchErr := s.dispatchArtifactTriggerToJob(ctx, build, artifact, job); dispatchErr != nil {
				return dispatchErr
			}
			break
		}
	}
	return nil
}

func (s *JobService) dispatchArtifactTriggerToJob(ctx context.Context, producerBuild domain.Build, artifact domain.BuildArtifact, consumerJob domain.Job) error {
	now := time.Now().UTC()
	producerJobID := strings.TrimSpace(readStringPtr(producerBuild.JobID))
	delivery := domain.ArtifactTriggerDelivery{
		ID:                uuid.NewString(),
		ArtifactID:        artifact.ID,
		ConsumerJobID:     consumerJob.ID,
		ProducerBuildID:   producerBuild.ID,
		ProducerProjectID: producerBuild.ProjectID,
		ProducerJobID:     producerJobID,
		ArtifactPath:      artifact.LogicalPath,
		Status:            domain.ArtifactTriggerDeliveryStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	created, err := s.artifactTriggerDeliveries.Create(ctx, delivery)
	if err != nil {
		if errors.Is(err, repository.ErrArtifactTriggerDeliveryDuplicate) {
			existing, getErr := s.artifactTriggerDeliveries.GetByArtifactIDAndConsumerJobID(ctx, artifact.ID, consumerJob.ID)
			if getErr != nil {
				return getErr
			}
			if existing.Status != domain.ArtifactTriggerDeliveryStatusFailed {
				return nil
			}
			claimed, claimErr := s.artifactTriggerDeliveries.ClaimFailedForRetry(ctx, existing.ID, now)
			if claimErr != nil {
				if errors.Is(claimErr, repository.ErrArtifactTriggerDeliveryRetryNotClaimable) {
					return nil
				}
				return claimErr
			}
			claimed.ProducerBuildID = producerBuild.ID
			claimed.ProducerProjectID = producerBuild.ProjectID
			claimed.ProducerJobID = producerJobID
			claimed.ArtifactPath = artifact.LogicalPath
			claimed.UpdatedAt = now
			created, err = s.artifactTriggerDeliveries.Update(ctx, claimed)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	_, err = s.queueArtifactTriggerDelivery(ctx, created, producerBuild, artifact, consumerJob)
	return err
}

func (s *JobService) queueArtifactTriggerDelivery(ctx context.Context, delivery domain.ArtifactTriggerDelivery, producerBuild domain.Build, artifact domain.BuildArtifact, consumerJob domain.Job) (domain.ArtifactTriggerDelivery, error) {
	artifactSizeBytes := artifact.SizeBytes
	queuedBuild, queueErr := s.createBuildForJobWithTrigger(ctx, consumerJob, &buildsvc.CreateBuildTriggerInput{
		Kind:                   string(domain.BuildTriggerKindArtifact),
		ProducerProjectID:      producerBuild.ProjectID,
		ProducerJobID:          strings.TrimSpace(readStringPtr(producerBuild.JobID)),
		ProducerBuildID:        producerBuild.ID,
		ArtifactID:             artifact.ID,
		ArtifactPath:           artifact.LogicalPath,
		ArtifactName:           artifact.Name,
		ArtifactSizeBytes:      &artifactSizeBytes,
		ArtifactChecksumSHA256: readStringPtr(artifact.ChecksumSHA256),
	})
	if queueErr != nil {
		message := queueErr.Error()
		delivery.Status = domain.ArtifactTriggerDeliveryStatusFailed
		delivery.ErrorMessage = &message
		delivery.UpdatedAt = time.Now().UTC()
		updated, updateErr := s.artifactTriggerDeliveries.Update(ctx, delivery)
		if updateErr != nil {
			return domain.ArtifactTriggerDelivery{}, updateErr
		}
		return updated, queueErr
	}
	delivery.Status = domain.ArtifactTriggerDeliveryStatusQueued
	delivery.QueuedBuildID = &queuedBuild.ID
	delivery.UpdatedAt = time.Now().UTC()
	updated, err := s.artifactTriggerDeliveries.Update(ctx, delivery)
	if err != nil {
		return domain.ArtifactTriggerDelivery{}, err
	}
	return updated, nil
}
