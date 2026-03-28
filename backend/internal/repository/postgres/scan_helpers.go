package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func scanBuild(scanner rowScanner) (domain.Build, error) {
	var build domain.Build
	var status string
	var queuedAt sql.NullTime
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var errorMessage sql.NullString

	err := scanner.Scan(
		&build.ID,
		&build.ProjectID,
		&status,
		&build.CreatedAt,
		&queuedAt,
		&startedAt,
		&finishedAt,
		&build.CurrentStepIndex,
		&errorMessage,
	)
	if err != nil {
		return domain.Build{}, err
	}

	build.Status = domain.BuildStatus(status)
	if queuedAt.Valid {
		queued := queuedAt.Time
		build.QueuedAt = &queued
	}
	if startedAt.Valid {
		started := startedAt.Time
		build.StartedAt = &started
	}
	if finishedAt.Valid {
		finished := finishedAt.Time
		build.FinishedAt = &finished
	}
	if errorMessage.Valid {
		errMsg := errorMessage.String
		build.ErrorMessage = &errMsg
	}

	return build, nil
}

func scanStep(scanner rowScanner) (domain.BuildStep, error) {
	var step domain.BuildStep
	var status string
	var command string
	var argsRaw []byte
	var envRaw []byte
	var workingDir string
	var timeoutSeconds int
	var workerID sql.NullString
	var claimToken sql.NullString
	var claimedAt sql.NullTime
	var leaseExpiresAt sql.NullTime
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var exitCode sql.NullInt64
	var stdout sql.NullString
	var stderr sql.NullString
	var errorMessage sql.NullString

	err := scanner.Scan(
		&step.ID,
		&step.BuildID,
		&step.StepIndex,
		&step.Name,
		&command,
		&argsRaw,
		&envRaw,
		&workingDir,
		&timeoutSeconds,
		&status,
		&workerID,
		&claimToken,
		&claimedAt,
		&leaseExpiresAt,
		&startedAt,
		&finishedAt,
		&exitCode,
		&stdout,
		&stderr,
		&errorMessage,
	)
	if err != nil {
		return domain.BuildStep{}, err
	}

	step.Command = command
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &step.Args); err != nil {
			return domain.BuildStep{}, err
		}
	} else {
		step.Args = []string{}
	}
	if len(envRaw) > 0 {
		if err := json.Unmarshal(envRaw, &step.Env); err != nil {
			return domain.BuildStep{}, err
		}
	} else {
		step.Env = map[string]string{}
	}
	step.WorkingDir = workingDir
	step.TimeoutSeconds = timeoutSeconds
	step.Status = domain.BuildStepStatus(status)
	if workerID.Valid {
		worker := workerID.String
		step.WorkerID = &worker
	}
	if claimToken.Valid {
		token := claimToken.String
		step.ClaimToken = &token
	}
	if claimedAt.Valid {
		claimed := claimedAt.Time
		step.ClaimedAt = &claimed
	}
	if leaseExpiresAt.Valid {
		lease := leaseExpiresAt.Time
		step.LeaseExpiresAt = &lease
	}
	if startedAt.Valid {
		started := startedAt.Time
		step.StartedAt = &started
	}
	if finishedAt.Valid {
		finished := finishedAt.Time
		step.FinishedAt = &finished
	}
	if exitCode.Valid {
		exit := int(exitCode.Int64)
		step.ExitCode = &exit
	}
	if stdout.Valid {
		stdoutValue := stdout.String
		step.Stdout = &stdoutValue
	}
	if stderr.Valid {
		stderrValue := stderr.String
		step.Stderr = &stderrValue
	}
	if errorMessage.Valid {
		errMsg := errorMessage.String
		step.ErrorMessage = &errMsg
	}

	return step, nil
}
