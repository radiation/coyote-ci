package build

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

type CreateBuildInput struct {
	ProjectID string
	JobID     *string
	Priority  int
	Steps     []CreateBuildStepInput
	Source    *CreateBuildSourceInput
	Trigger   *CreateBuildTriggerInput
}

type CreateBuildSourceInput struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
}

type CreateBuildStepInput struct {
	Name           string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
}

type QueueBuildCustomStepInput struct {
	Name    string
	Command string
}

func (s *BuildService) CreateBuild(ctx context.Context, input CreateBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}
	if err := validateRequestedBuildPriority(input.Priority); err != nil {
		return domain.Build{}, err
	}

	sourceSpec, err := buildSourceSpecFromInput(input.Source)
	if err != nil {
		return domain.Build{}, err
	}

	build := domain.Build{
		ID:               uuid.NewString(),
		ProjectID:        input.ProjectID,
		JobID:            input.JobID,
		Priority:         domain.NormalizePriority(input.Priority),
		Status:           domain.BuildStatusPending,
		AttemptNumber:    1,
		CreatedAt:        time.Now().UTC(),
		CurrentStepIndex: 0,
		Source:           sourceSpec,
		RepoURL:          buildOptionalStringPtr(buildSourceRepositoryURL(sourceSpec)),
		Ref:              buildSourceRef(sourceSpec),
		CommitSHA:        buildSourceCommitSHA(sourceSpec),
		Trigger:          toDomainBuildTrigger(input.Trigger),
		ImageSourceKind:  domain.ImageSourceKindExternal,
	}

	if len(input.Steps) > 0 {
		steps := make([]domain.BuildStep, 0, len(input.Steps))
		for idx, step := range input.Steps {
			normalized := normalizeCreateStepInput(step)
			name := strings.TrimSpace(normalized.Name)
			if name == "" {
				name = "step-" + strconv.Itoa(idx+1)
			}

			steps = append(steps, domain.BuildStep{
				ID:              uuid.NewString(),
				BuildID:         build.ID,
				StepIndex:       idx,
				Name:            name,
				Command:         normalized.Command,
				Args:            normalized.Args,
				Env:             normalized.Env,
				WorkingDir:      normalized.WorkingDir,
				TimeoutSeconds:  normalized.TimeoutSeconds,
				Status:          domain.BuildStepStatusPending,
				ImageSourceKind: domain.ImageSourceKindExternal,
			})
		}

		queuedBuild, err := s.buildRepo.CreateQueuedBuild(ctx, build, steps)
		if err != nil {
			return domain.Build{}, err
		}
		if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
			log.Printf("WARNING: durable job creation failed for build_id=%s (build already persisted): %v", queuedBuild.ID, err)
			return domain.Build{}, fmt.Errorf("create execution jobs for build %s: %w", queuedBuild.ID, err)
		}
		return queuedBuild, nil
	}

	return s.buildRepo.Create(ctx, build)
}

// CreatePipelineBuildInput is the service-level input for creating a build from pipeline YAML.
type CreatePipelineBuildInput struct {
	ProjectID    string
	JobID        *string
	Priority     int
	PipelineYAML string
	Source       *CreateBuildSourceInput
	Trigger      *CreateBuildTriggerInput
}

// CreateBuildFromPipeline parses, validates, and resolves pipeline YAML, then creates
// a queued build with canonical build steps. The raw YAML is snapshot-persisted on the build.
func (s *BuildService) CreateBuildFromPipeline(ctx context.Context, input CreatePipelineBuildInput) (domain.Build, error) {
	if input.ProjectID == "" {
		return domain.Build{}, ErrProjectIDRequired
	}
	if err := validateRequestedBuildPriority(input.Priority); err != nil {
		return domain.Build{}, err
	}
	yamlText := strings.TrimSpace(input.PipelineYAML)
	if yamlText == "" {
		return domain.Build{}, ErrPipelineYAMLRequired
	}

	sourceSpec, err := buildSourceSpecFromInput(input.Source)
	if err != nil {
		return domain.Build{}, err
	}

	resolved, err := pipeline.LoadAndResolve([]byte(yamlText))
	if err != nil {
		return domain.Build{}, err
	}

	buildID := uuid.NewString()
	steps := pipelineStepsToDomain(buildID, resolved.Steps)

	pipelineName := resolved.Name
	var pipelineNamePtr *string
	if pipelineName != "" {
		pipelineNamePtr = &pipelineName
	}
	pipelineSource := pipelineSourceInline

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
		Source:             sourceSpec,
		RepoURL:            buildOptionalStringPtr(buildSourceRepositoryURL(sourceSpec)),
		Ref:                buildSourceRef(sourceSpec),
		CommitSHA:          buildSourceCommitSHA(sourceSpec),
		Trigger:            toDomainBuildTrigger(input.Trigger),
		RequestedImageRef:  buildOptionalStringPtr(strings.TrimSpace(resolved.Image)),
		ImageSourceKind:    domain.ImageSourceKindExternal,
	}

	queuedBuild, err := s.buildRepo.CreateQueuedBuild(ctx, build, steps)
	if err != nil {
		return domain.Build{}, err
	}
	if err := s.createDurableJobsForBuild(ctx, queuedBuild, steps); err != nil {
		log.Printf("WARNING: durable job creation failed for build_id=%s (build already persisted): %v", queuedBuild.ID, err)
		return domain.Build{}, fmt.Errorf("create execution jobs for build %s: %w", queuedBuild.ID, err)
	}
	return queuedBuild, nil
}
