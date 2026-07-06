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
var ErrJobRepositoryURLRequired = errors.New("job repository_url is required")
var ErrJobSourceTargetRequired = errors.New("job default_ref or default_commit_sha is required")
var ErrJobPipelineDefinitionRequired = errors.New("job pipeline_yaml or pipeline_path is required")
var ErrJobInvalidTriggerMode = errors.New("job trigger_mode must be one of branches, tags, branches_and_tags")
var ErrJobPriorityOutOfRange = errors.New("job priority must be between 1 and 10")
var ErrPushEventRepositoryURLRequired = errors.New("push event repository_url is required")
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

type JobService struct {
	jobRepo             repository.JobRepository
	projects            repository.ProjectRepository
	managedImageConfigs repository.JobManagedImageConfigRepository
	credentials         repository.SourceCredentialRepository
	buildService        *buildsvc.BuildService
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
	RepositoryURL    string
	DefaultRef       string
	DefaultCommitSHA string
	PushEnabled      *bool
	PushBranch       *string
	TriggerMode      *string
	BranchAllowlist  []string
	TagAllowlist     []string
	PipelineYAML     string
	PipelinePath     string
	ManagedImage     *ManagedImageConfigInput
	Enabled          *bool
}

type UpdateJobInput struct {
	Name             *string
	Priority         *int
	RepositoryURL    *string
	DefaultRef       *string
	DefaultCommitSHA *string
	PushEnabled      *bool
	PushBranch       *string
	TriggerMode      *string
	BranchAllowlist  *[]string
	TagAllowlist     *[]string
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
		RepositoryURL:    normalized.RepositoryURL,
		DefaultRef:       normalized.DefaultRef,
		DefaultCommitSHA: defaultCommitSHA,
		PushEnabled:      pushEnabled,
		PushBranch:       pushBranch,
		TriggerMode:      triggerMode,
		BranchAllowlist:  branchAllowlist,
		TagAllowlist:     tagAllowlist,
		PipelineYAML:     normalized.PipelineYAML,
		PipelinePath:     pipelinePath,
		Enabled:          enabled,
		CreatedAt:        now,
		UpdatedAt:        now,
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

	if input.Name != nil {
		job.Name = strings.TrimSpace(*input.Name)
	}
	if input.Priority != nil {
		if !domain.ValidPriority(*input.Priority) {
			return domain.Job{}, ErrJobPriorityOutOfRange
		}
		job.Priority = *input.Priority
	}
	if input.RepositoryURL != nil {
		job.RepositoryURL = strings.TrimSpace(*input.RepositoryURL)
	}
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
	if s.buildService == nil {
		return domain.Build{}, ErrJobBuildServiceNotConfigured
	}

	var build domain.Build
	var err error
	if strings.TrimSpace(job.PipelineYAML) != "" {
		build, err = s.buildService.CreateBuildFromPipeline(ctx, buildsvc.CreatePipelineBuildInput{
			ProjectID:    job.ProjectID,
			JobID:        &job.ID,
			Priority:     job.Priority,
			PipelineYAML: job.PipelineYAML,
			PipelinePath: readStringPtr(job.PipelinePath),
			Source: &buildsvc.CreateBuildSourceInput{
				RepositoryURL: job.RepositoryURL,
				Ref:           job.DefaultRef,
				CommitSHA:     readStringPtr(job.DefaultCommitSHA),
			},
		})
	} else if job.PipelinePath != nil && strings.TrimSpace(*job.PipelinePath) != "" {
		build, err = s.buildService.CreateBuildFromRepo(ctx, buildsvc.CreateRepoBuildInput{
			ProjectID:    job.ProjectID,
			JobID:        &job.ID,
			Priority:     job.Priority,
			RepoURL:      job.RepositoryURL,
			Ref:          job.DefaultRef,
			CommitSHA:    readStringPtr(job.DefaultCommitSHA),
			PipelinePath: strings.TrimSpace(*job.PipelinePath),
		})
	} else {
		build, err = s.buildService.CreateBuildFromPipeline(ctx, buildsvc.CreatePipelineBuildInput{
			ProjectID:    job.ProjectID,
			JobID:        &job.ID,
			Priority:     job.Priority,
			PipelineYAML: job.PipelineYAML,
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
