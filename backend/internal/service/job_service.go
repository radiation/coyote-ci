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
var ErrJobDefaultRefRequired = errors.New("job default_ref is required")
var ErrJobPipelineYAMLRequired = errors.New("job pipeline_yaml is required")
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
	ProjectID     string
	Name          string
	RepositoryURL string
	DefaultRef    string
	PushEnabled   *bool
	PushBranch    *string
	PipelineYAML  string
	Enabled       *bool
}

type UpdateJobInput struct {
	Name          *string
	RepositoryURL *string
	DefaultRef    *string
	PushEnabled   *bool
	PushBranch    *string
	PipelineYAML  *string
	Enabled       *bool
}

func (s *JobService) CreateJob(ctx context.Context, input CreateJobInput) (domain.Job, error) {
	normalized, err := normalizeCreateJobInput(input)
	if err != nil {
		return domain.Job{}, err
	}

	if err := validatePipelineYAML(normalized.PipelineYAML); err != nil {
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
	if normalized.PushBranch != nil {
		branch := normalizePushRef(*normalized.PushBranch)
		if branch != "" {
			pushBranch = &branch
		}
	}

	now := time.Now().UTC()
	job := domain.Job{
		ID:            uuid.NewString(),
		ProjectID:     normalized.ProjectID,
		Name:          normalized.Name,
		RepositoryURL: normalized.RepositoryURL,
		DefaultRef:    normalized.DefaultRef,
		PushEnabled:   pushEnabled,
		PushBranch:    pushBranch,
		PipelineYAML:  normalized.PipelineYAML,
		Enabled:       enabled,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	return s.jobRepo.Create(ctx, job)
}

func (s *JobService) ListJobs(ctx context.Context) ([]domain.Job, error) {
	return s.jobRepo.List(ctx)
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
	if input.PipelineYAML != nil {
		job.PipelineYAML = strings.TrimSpace(*input.PipelineYAML)
	}
	if input.Enabled != nil {
		job.Enabled = *input.Enabled
	}

	if validateErr := validateJobRequiredFields(job); validateErr != nil {
		return domain.Job{}, validateErr
	}
	if validateErr := validatePipelineYAML(job.PipelineYAML); validateErr != nil {
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

	build, err := s.buildService.CreateBuildFromPipeline(ctx, CreatePipelineBuildInput{
		ProjectID:    job.ProjectID,
		PipelineYAML: job.PipelineYAML,
		SourcePath:   "job:" + job.ID,
		Source: &CreateBuildSourceInput{
			RepositoryURL: job.RepositoryURL,
			Ref:           job.DefaultRef,
		},
	})
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
	if normalized.PushBranch != nil {
		branch := normalizePushRef(*normalized.PushBranch)
		normalized.PushBranch = &branch
	}
	normalized.PipelineYAML = strings.TrimSpace(normalized.PipelineYAML)

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
	if input.DefaultRef == "" {
		return ErrJobDefaultRefRequired
	}
	if input.PipelineYAML == "" {
		return ErrJobPipelineYAMLRequired
	}

	return nil
}

func validateJobRequiredFields(job domain.Job) error {
	return validateCreateJobRequiredFields(CreateJobInput{
		ProjectID:     strings.TrimSpace(job.ProjectID),
		Name:          strings.TrimSpace(job.Name),
		RepositoryURL: strings.TrimSpace(job.RepositoryURL),
		DefaultRef:    strings.TrimSpace(job.DefaultRef),
		PipelineYAML:  strings.TrimSpace(job.PipelineYAML),
	})
}

func validatePipelineYAML(yamlText string) error {
	trimmed := strings.TrimSpace(yamlText)
	if trimmed == "" {
		return ErrJobPipelineYAMLRequired
	}

	_, err := pipeline.LoadAndResolve([]byte(trimmed))
	if err != nil {
		return err
	}

	return nil
}
