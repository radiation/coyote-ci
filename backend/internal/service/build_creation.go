package service

import (
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

// pipelineStepsToDomain converts resolved pipeline steps into canonical domain build steps.
func pipelineStepsToDomain(buildID string, steps []pipeline.ResolvedStep) []domain.BuildStep {
	out := make([]domain.BuildStep, 0, len(steps))
	for idx, rs := range steps {
		env := rs.Env
		if env == nil {
			env = map[string]string{}
		}
		workingDir := rs.WorkingDir
		if workingDir == "" {
			workingDir = "."
		}
		out = append(out, domain.BuildStep{
			ID:             uuid.NewString(),
			BuildID:        buildID,
			StepIndex:      idx,
			Name:           rs.Name,
			Image:          rs.Image,
			Command:        "sh",
			Args:           []string{"-c", rs.Run},
			Env:            env,
			WorkingDir:     workingDir,
			TimeoutSeconds: rs.TimeoutSeconds,
			Status:         domain.BuildStepStatusPending,
		})
	}
	return out
}

func defaultBuildSteps(buildID string) []domain.BuildStep {
	return []domain.BuildStep{
		{
			ID:             uuid.NewString(),
			BuildID:        buildID,
			StepIndex:      0,
			Name:           "default",
			Command:        "sh",
			Args:           []string{"-c", "echo coyote-ci worker default step && exit 0"},
			Env:            map[string]string{},
			WorkingDir:     ".",
			TimeoutSeconds: 0,
			Status:         domain.BuildStepStatusPending,
		},
	}
}

func buildStepsForTemplate(buildID string, template string) []domain.BuildStep {
	normalizedTemplate := strings.ToLower(strings.TrimSpace(template))

	stepInputs := []CreateBuildStepInput{
		{
			Name:       "default",
			Command:    "sh",
			Args:       []string{"-c", "echo coyote-ci worker default step && exit 0"},
			Env:        map[string]string{},
			WorkingDir: ".",
		},
	}

	switch normalizedTemplate {
	case "", BuildTemplateDefault:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "default",
				Command:    "sh",
				Args:       []string{"-c", "echo coyote-ci worker default step && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	case BuildTemplateTest:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "setup",
				Command:    "sh",
				Args:       []string{"-c", "echo running setup && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "test",
				Command:    "sh",
				Args:       []string{"-c", "echo running tests && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "teardown",
				Command:    "sh",
				Args:       []string{"-c", "echo running teardown && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	case BuildTemplateBuild:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "install",
				Command:    "sh",
				Args:       []string{"-c", "echo installing dependencies && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "compile",
				Command:    "sh",
				Args:       []string{"-c", "echo compiling project && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	case BuildTemplateFail:
		stepInputs = []CreateBuildStepInput{
			{
				Name:       "setup",
				Command:    "sh",
				Args:       []string{"-c", "echo success && exit 0"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
			{
				Name:       "verify",
				Command:    "sh",
				Args:       []string{"-c", "echo failure 1>&2 && exit 1"},
				Env:        map[string]string{},
				WorkingDir: ".",
			},
		}
	}

	return domainStepsFromInputs(buildID, stepInputs)
}

func buildStepsForCustomTemplate(buildID string, customSteps []QueueBuildCustomStepInput) ([]domain.BuildStep, error) {
	if len(customSteps) == 0 {
		return nil, ErrCustomTemplateStepsRequired
	}

	stepInputs := make([]CreateBuildStepInput, 0, len(customSteps))
	for idx, step := range customSteps {
		command := strings.TrimSpace(step.Command)
		if command == "" {
			return nil, ErrCustomTemplateStepCommandRequired
		}

		name := strings.TrimSpace(step.Name)
		if name == "" {
			name = "step-" + strconv.Itoa(idx+1)
		}

		stepInputs = append(stepInputs, CreateBuildStepInput{
			Name:       name,
			Command:    "sh",
			Args:       []string{"-c", command},
			Env:        map[string]string{},
			WorkingDir: ".",
		})
	}

	return domainStepsFromInputs(buildID, stepInputs), nil
}

func domainStepsFromInputs(buildID string, stepInputs []CreateBuildStepInput) []domain.BuildStep {
	steps := make([]domain.BuildStep, 0, len(stepInputs))
	for idx, input := range stepInputs {
		normalized := normalizeCreateStepInput(input)
		steps = append(steps, domain.BuildStep{
			ID:             uuid.NewString(),
			BuildID:        buildID,
			StepIndex:      idx,
			Name:           normalized.Name,
			Command:        normalized.Command,
			Args:           normalized.Args,
			Env:            normalized.Env,
			WorkingDir:     normalized.WorkingDir,
			TimeoutSeconds: normalized.TimeoutSeconds,
			Status:         domain.BuildStepStatusPending,
		})
	}

	return steps
}

func normalizeCreateStepInput(in CreateBuildStepInput) CreateBuildStepInput {
	out := in

	if strings.TrimSpace(out.Command) == "" {
		out.Command = "sh"
	}
	if len(out.Args) == 0 {
		out.Args = []string{"-c", "echo coyote-ci worker default step && exit 0"}
	}
	if out.Env == nil {
		out.Env = map[string]string{}
	}
	if strings.TrimSpace(out.WorkingDir) == "" {
		out.WorkingDir = "."
	}
	if out.TimeoutSeconds < 0 {
		out.TimeoutSeconds = 0
	}

	return out
}
