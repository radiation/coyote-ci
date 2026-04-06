package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
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
var ErrPushEventRepositoryURLRequired = errors.New("push event repository_url is required")
var ErrPushEventRefRequired = errors.New("push event ref is required")
var ErrPushEventCommitSHARequired = errors.New("push event commit_sha is required")
var ErrJobDisabled = errors.New("job is disabled")
var ErrJobBuildServiceNotConfigured = errors.New("job build service not configured")

type JobService struct {
	jobRepo      repository.JobRepository
	buildService *BuildService
}

func NewJobService(jobRepo repository.JobRepository, buildService *BuildService) *JobService {
	return &JobService{jobRepo: jobRepo, buildService: buildService}
}

type CreateJobInput struct {
	ProjectID        string
	Name             string
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
	Enabled          *bool
}

type UpdateJobInput struct {
	Name             *string
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
	Enabled          *bool
}

func (s *JobService) CreateJob(ctx context.Context, input CreateJobInput) (domain.Job, error) {
	normalized, err := normalizeCreateJobInput(input)
	if err != nil {
		return domain.Job{}, err
	}

	if strings.TrimSpace(normalized.PipelineYAML) != "" {
		if err := validatePipelineYAML(normalized.PipelineYAML); err != nil {
			return domain.Job{}, err
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

	if err := validatePipelineDefinition(normalized.PipelineYAML, pipelinePath); err != nil {
		return domain.Job{}, err
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

	triggerMode := normalizeJobTriggerMode(domain.JobTriggerMode(readStringPtr(normalized.TriggerMode)))
	branchAllowlist := normalizeBranchAllowlist(normalized.BranchAllowlist)
	if len(branchAllowlist) == 0 && pushBranch != nil {
		branchAllowlist = []string{*pushBranch}
	}
	tagAllowlist := normalizeTagAllowlist(normalized.TagAllowlist)

	now := time.Now().UTC()
	job := domain.Job{
		ID:               uuid.NewString(),
		ProjectID:        normalized.ProjectID,
		Name:             normalized.Name,
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

	return s.jobRepo.Create(ctx, job)
}

func (s *JobService) ListJobs(ctx context.Context) ([]domain.Job, error) {
	return s.jobRepo.List(ctx)
}

func (s *JobService) ListJobsPaged(ctx context.Context, params repository.ListParams) ([]domain.Job, error) {
	return s.jobRepo.ListPaged(ctx, params)
}

func (s *JobService) ListBuildsByJobID(ctx context.Context, jobID string) ([]domain.Build, error) {
	if s.buildService == nil {
		return nil, ErrJobBuildServiceNotConfigured
	}
	return s.buildService.ListBuildsByJobID(ctx, jobID)
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

	return job, nil
}

func (s *JobService) UpdateJob(ctx context.Context, id string, input UpdateJobInput) (domain.Job, error) {
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}

	if input.Name != nil {
		job.Name = strings.TrimSpace(*input.Name)
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
		mode := normalizeJobTriggerMode(domain.JobTriggerMode(strings.TrimSpace(*input.TriggerMode)))
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

	return updated, nil
}

func (s *JobService) RunJobNow(ctx context.Context, id string) (domain.Build, error) {
	if s.buildService == nil {
		return domain.Build{}, ErrJobBuildServiceNotConfigured
	}

	job, err := s.GetJob(ctx, id)
	if err != nil {
		return domain.Build{}, err
	}
	if !job.Enabled {
		return domain.Build{}, ErrJobDisabled
	}

	var build domain.Build
	if job.PipelinePath != nil && strings.TrimSpace(*job.PipelinePath) != "" {
		build, err = s.buildService.CreateBuildFromRepo(ctx, CreateRepoBuildInput{
			ProjectID:    job.ProjectID,
			JobID:        &job.ID,
			RepoURL:      job.RepositoryURL,
			Ref:          job.DefaultRef,
			CommitSHA:    readStringPtr(job.DefaultCommitSHA),
			PipelinePath: strings.TrimSpace(*job.PipelinePath),
		})
	} else {
		build, err = s.buildService.CreateBuildFromPipeline(ctx, CreatePipelineBuildInput{
			ProjectID:    job.ProjectID,
			JobID:        &job.ID,
			PipelineYAML: job.PipelineYAML,
			Source: &CreateBuildSourceInput{
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

func normalizeCreateJobInput(input CreateJobInput) (CreateJobInput, error) {
	normalized := input
	normalized.ProjectID = strings.TrimSpace(normalized.ProjectID)
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.RepositoryURL = strings.TrimSpace(normalized.RepositoryURL)
	normalized.DefaultRef = strings.TrimSpace(normalized.DefaultRef)
	normalized.DefaultCommitSHA = strings.TrimSpace(normalized.DefaultCommitSHA)
	if normalized.PushBranch != nil {
		branch := normalizePushRef(*normalized.PushBranch)
		normalized.PushBranch = &branch
	}
	if normalized.TriggerMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*normalized.TriggerMode))
		normalized.TriggerMode = &mode
	}
	normalized.BranchAllowlist = normalizeBranchAllowlist(normalized.BranchAllowlist)
	normalized.TagAllowlist = normalizeTagAllowlist(normalized.TagAllowlist)
	normalized.PipelineYAML = strings.TrimSpace(normalized.PipelineYAML)
	normalized.PipelinePath = strings.TrimSpace(normalized.PipelinePath)

	if err := validateCreateJobRequiredFields(normalized); err != nil {
		return CreateJobInput{}, err
	}

	return normalized, nil
}

func validateCreateJobRequiredFields(input CreateJobInput) error {
	if input.ProjectID == "" {
		return ErrJobProjectIDRequired
	}
	if input.Name == "" {
		return ErrJobNameRequired
	}
	if input.RepositoryURL == "" {
		return ErrJobRepositoryURLRequired
	}
	if input.DefaultRef == "" && input.DefaultCommitSHA == "" {
		return ErrJobSourceTargetRequired
	}
	if input.PipelineYAML == "" && input.PipelinePath == "" {
		return ErrJobPipelineDefinitionRequired
	}
	if input.TriggerMode != nil {
		if !isValidTriggerMode(*input.TriggerMode) {
			return ErrJobInvalidTriggerMode
		}
	}

	return nil
}

func validateJobRequiredFields(job domain.Job) error {
	return validateCreateJobRequiredFields(CreateJobInput{
		ProjectID:        strings.TrimSpace(job.ProjectID),
		Name:             strings.TrimSpace(job.Name),
		RepositoryURL:    strings.TrimSpace(job.RepositoryURL),
		DefaultRef:       strings.TrimSpace(job.DefaultRef),
		DefaultCommitSHA: strings.TrimSpace(readStringPtr(job.DefaultCommitSHA)),
		TriggerMode:      optionalTrimmedStringPtr(string(job.TriggerMode)),
		PipelineYAML:     strings.TrimSpace(job.PipelineYAML),
		PipelinePath:     strings.TrimSpace(readStringPtr(job.PipelinePath)),
	})
}

func validatePipelineDefinition(pipelineYAML string, pipelinePath *string) error {
	if strings.TrimSpace(pipelineYAML) == "" && strings.TrimSpace(readStringPtr(pipelinePath)) == "" {
		return ErrJobPipelineDefinitionRequired
	}
	return nil
}

func validatePipelineYAML(yamlText string) error {
	trimmed := strings.TrimSpace(yamlText)
	if trimmed == "" {
		return ErrJobPipelineDefinitionRequired
	}

	_, err := pipeline.LoadAndResolve([]byte(trimmed))
	if err != nil {
		return err
	}

	return nil
}

func readStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalTrimmedStringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeBranchAllowlist(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, item := range values {
		branch := normalizePushRef(item)
		if branch == "" || seen[branch] {
			continue
		}
		seen[branch] = true
		normalized = append(normalized, branch)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeTagAllowlist(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, item := range values {
		trimmed := strings.TrimSpace(strings.TrimPrefix(item, "refs/tags/"))
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func isValidTriggerMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(domain.JobTriggerModeBranches), string(domain.JobTriggerModeTags), string(domain.JobTriggerModeBranchesAndTags):
		return true
	default:
		return false
	}
}
