package service

import (
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type BuildExecutionPlanner struct {
	specVersion int
	clock       func() time.Time
}

func NewBuildExecutionPlanner() *BuildExecutionPlanner {
	return &BuildExecutionPlanner{
		specVersion: 1,
		clock:       time.Now,
	}
}

func (p *BuildExecutionPlanner) Plan(build domain.Build, steps []domain.BuildStep, image string) ([]domain.ExecutionJob, error) {
	if len(steps) == 0 {
		return []domain.ExecutionJob{}, nil
	}

	resolvedImage := strings.TrimSpace(image)
	contextDir := plannerContextDirFromPipelinePath(build.PipelinePath)
	pipelinePath := optionalValue(build.PipelinePath)
	sourceRef := plannerSourceRef(build.Source)

	jobs := make([]domain.ExecutionJob, 0, len(steps))
	for _, step := range steps {
		// Step-level image overrides pipeline-level/default image.
		stepImage := strings.TrimSpace(step.Image)
		if stepImage == "" {
			stepImage = resolvedImage
		}

		timeout := step.TimeoutSeconds
		spec := domain.ExecutionJobSpec{
			Version:          p.specVersion,
			Image:            stepImage,
			WorkingDir:       defaultValue(step.WorkingDir, "."),
			Command:          append([]string{defaultValue(step.Command, "sh")}, append([]string(nil), step.Args...)...),
			Environment:      cloneEnv(step.Env),
			TimeoutSeconds:   maxInt(step.TimeoutSeconds, 0),
			PipelineFilePath: pipelinePath,
			ContextDir:       contextDir,
			Source: domain.SourceSnapshotRef{
				RepositoryURL: plannerSourceRepositoryURL(build.Source, build.RepoURL),
				CommitSHA:     plannerSourceCommitSHA(build.Source, build.CommitSHA),
				RefName:       sourceRef,
			},
		}

		specJSON, err := spec.ToJSON()
		if err != nil {
			return nil, err
		}

		jobID := uuid.NewString()
		jobs = append(jobs, domain.ExecutionJob{
			ID:               jobID,
			BuildID:          build.ID,
			StepID:           step.ID,
			Name:             step.Name,
			StepIndex:        step.StepIndex,
			AttemptNumber:    1,
			LineageRootJobID: &jobID,
			Status:           domain.ExecutionJobStatusQueued,
			Image:            stepImage,
			WorkingDir:       spec.WorkingDir,
			Command:          spec.Command,
			Environment:      spec.Environment,
			TimeoutSeconds:   &timeout,
			PipelineFilePath: optionalPointer(pipelinePath),
			ContextDir:       optionalPointer(contextDir),
			Source:           spec.Source,
			SpecVersion:      p.specVersion,
			SpecDigest:       domain.BuildSpecDigest(specJSON),
			ResolvedSpecJSON: specJSON,
			CreatedAt:        p.clock().UTC(),
			OutputRefs:       []domain.ArtifactRef{},
		})
	}

	return jobs, nil
}

func plannerSourceRepositoryURL(spec *domain.SourceSpec, fallback *string) string {
	if spec != nil {
		return strings.TrimSpace(spec.RepositoryURL)
	}
	if fallback == nil {
		return ""
	}
	return strings.TrimSpace(*fallback)
}

func plannerSourceCommitSHA(spec *domain.SourceSpec, fallback *string) string {
	if spec != nil && spec.CommitSHA != nil {
		return strings.TrimSpace(*spec.CommitSHA)
	}
	if fallback == nil {
		return ""
	}
	return strings.TrimSpace(*fallback)
}

func plannerSourceRef(spec *domain.SourceSpec) *string {
	if spec == nil || spec.Ref == nil {
		return nil
	}
	value := strings.TrimSpace(*spec.Ref)
	if value == "" {
		return nil
	}
	return &value
}

func plannerContextDirFromPipelinePath(pipelinePath *string) string {
	if pipelinePath == nil {
		return "."
	}
	normalized := strings.TrimSpace(*pipelinePath)
	if normalized == "" {
		return "."
	}
	dir := path.Clean(path.Dir(strings.ReplaceAll(normalized, "\\", "/")))
	if dir == "" {
		return "."
	}
	return dir
}

func cloneEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	return out
}

func optionalPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func defaultValue(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
