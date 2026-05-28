package worker

import (
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func workerTimeoutFromJob(job domain.ExecutionJob) int {
	if job.TimeoutSeconds == nil {
		return 0
	}
	return workerMaxInt(*job.TimeoutSeconds, 0)
}

func workerFirstRunnableStep(steps []domain.BuildStep) (domain.BuildStep, bool) {
	allPreviousSucceeded := true

	for _, step := range steps {
		switch step.Status {
		case domain.BuildStepStatusSuccess:
			continue
		case domain.BuildStepStatusPending:
			if !allPreviousSucceeded {
				return domain.BuildStep{}, false
			}
			return step, true
		case domain.BuildStepStatusRunning, domain.BuildStepStatusFailed:
			allPreviousSucceeded = false
		default:
			allPreviousSucceeded = false
		}
	}

	return domain.BuildStep{}, false
}

func workerFirstReclaimableRunningStep(steps []domain.BuildStep, now time.Time) (domain.BuildStep, bool) {
	for _, step := range steps {
		if step.Status == domain.BuildStepStatusSuccess {
			continue
		}

		if step.Status != domain.BuildStepStatusRunning {
			return domain.BuildStep{}, false
		}
		if step.LeaseExpiresAt == nil || step.LeaseExpiresAt.After(now) {
			return domain.BuildStep{}, false
		}

		return step, true
	}

	return domain.BuildStep{}, false
}

func workerDefaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

func workerDefaultArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"-c", "echo coyote-ci worker default step && exit 0"}
	}

	return args
}

func workerDefaultEnv(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}

	return env
}

func workerMaxInt(value int, minimum int) int {
	if value < minimum {
		return minimum
	}

	return value
}

func workerCommandFromJob(job domain.ExecutionJob) string {
	if len(job.Command) > 0 {
		return workerDefaultString(job.Command[0], "sh")
	}
	return "sh"
}

func workerArgsFromJob(job domain.ExecutionJob) []string {
	if len(job.Command) <= 1 {
		return workerDefaultArgs(nil)
	}
	args := make([]string, len(job.Command)-1)
	copy(args, job.Command[1:])
	return args
}

func workerEnvFromJob(job domain.ExecutionJob) map[string]string {
	if job.Environment == nil {
		return map[string]string{}
	}
	env := make(map[string]string, len(job.Environment))
	for key, value := range job.Environment {
		env[key] = value
	}
	return env
}
