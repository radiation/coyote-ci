package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type rowScanner interface {
	Scan(dest ...any) error
}

// buildColumns is the canonical column list for build SELECT/RETURNING clauses (full detail).
const buildColumns = `id, project_id, status, created_at, queued_at, started_at, finished_at, current_step_index, attempt_number, rerun_of_build_id, rerun_from_step_index, error_message, pipeline_config_yaml, pipeline_name, pipeline_source, pipeline_path, repo_url, ref, commit_sha`

// buildListColumns is a minimal column list used for list queries (omits large pipeline YAML).
const buildListColumns = `id, project_id, status, created_at, queued_at, started_at, finished_at, current_step_index, attempt_number, rerun_of_build_id, rerun_from_step_index, error_message, pipeline_name, pipeline_source, pipeline_path, repo_url, ref, commit_sha`

const executionJobColumns = `id, build_id, step_id, name, step_index, attempt_number, retry_of_job_id, lineage_root_job_id, status, queue_name, image, working_dir, command_json, env_json, timeout_seconds, pipeline_file_path, context_dir, source_repo_url, source_commit_sha, source_ref_name, source_archive_uri, source_archive_digest, spec_version, spec_digest, resolved_spec_json, claim_token, claimed_by, claim_expires_at, created_at, started_at, finished_at, error_message, exit_code, output_refs_json`

func scanBuildList(scanner rowScanner) (domain.Build, error) {
	var build domain.Build
	var status string
	var queuedAt sql.NullTime
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var errorMessage sql.NullString
	var pipelineName sql.NullString
	var pipelineSource sql.NullString
	var pipelinePath sql.NullString
	var repoURL sql.NullString
	var ref sql.NullString
	var commitSHA sql.NullString

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
		&pipelineName,
		&pipelineSource,
		&pipelinePath,
		&repoURL,
		&ref,
		&commitSHA,
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
	if pipelineName.Valid {
		v := pipelineName.String
		build.PipelineName = &v
	}
	if pipelineSource.Valid {
		v := pipelineSource.String
		build.PipelineSource = &v
	}
	if pipelinePath.Valid {
		v := pipelinePath.String
		build.PipelinePath = &v
	}
	if repoURL.Valid {
		v := repoURL.String
		build.RepoURL = &v
	}
	if ref.Valid {
		v := ref.String
		build.Ref = &v
	}
	if commitSHA.Valid {
		v := commitSHA.String
		build.CommitSHA = &v
	}
	build.Source = domain.NewSourceSpec(readOptionalString(build.RepoURL), readOptionalString(build.Ref), readOptionalString(build.CommitSHA))

	return build, nil
}

func scanBuild(scanner rowScanner) (domain.Build, error) {
	var build domain.Build
	var status string
	var queuedAt sql.NullTime
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var errorMessage sql.NullString
	var pipelineConfigYAML sql.NullString
	var pipelineName sql.NullString
	var pipelineSource sql.NullString
	var pipelinePath sql.NullString
	var repoURL sql.NullString
	var ref sql.NullString
	var commitSHA sql.NullString

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
		&pipelineConfigYAML,
		&pipelineName,
		&pipelineSource,
		&pipelinePath,
		&repoURL,
		&ref,
		&commitSHA,
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
	if pipelineConfigYAML.Valid {
		v := pipelineConfigYAML.String
		build.PipelineConfigYAML = &v
	}
	if pipelineName.Valid {
		v := pipelineName.String
		build.PipelineName = &v
	}
	if pipelineSource.Valid {
		v := pipelineSource.String
		build.PipelineSource = &v
	}
	if pipelinePath.Valid {
		v := pipelinePath.String
		build.PipelinePath = &v
	}
	if repoURL.Valid {
		v := repoURL.String
		build.RepoURL = &v
	}
	if ref.Valid {
		v := ref.String
		build.Ref = &v
	}
	if commitSHA.Valid {
		v := commitSHA.String
		build.CommitSHA = &v
	}
	build.Source = domain.NewSourceSpec(readOptionalString(build.RepoURL), readOptionalString(build.Ref), readOptionalString(build.CommitSHA))

	return build, nil
}

func readOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

func scanExecutionJob(scanner rowScanner) (domain.ExecutionJob, error) {
	var job domain.ExecutionJob
	var status string
	var retryOfJobID sql.NullString
	var lineageRootJobID sql.NullString
	var queueName sql.NullString
	var commandRaw []byte
	var envRaw []byte
	var timeoutSeconds sql.NullInt64
	var pipelineFilePath sql.NullString
	var contextDir sql.NullString
	var sourceRepoURL sql.NullString
	var sourceRefName sql.NullString
	var sourceArchiveURI sql.NullString
	var sourceArchiveDigest sql.NullString
	var specDigest sql.NullString
	var claimToken sql.NullString
	var claimedBy sql.NullString
	var claimExpiresAt sql.NullTime
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	var errorMessage sql.NullString
	var exitCode sql.NullInt64
	var outputRefsRaw []byte

	err := scanner.Scan(
		&job.ID,
		&job.BuildID,
		&job.StepID,
		&job.Name,
		&job.StepIndex,
		&job.AttemptNumber,
		&retryOfJobID,
		&lineageRootJobID,
		&status,
		&queueName,
		&job.Image,
		&job.WorkingDir,
		&commandRaw,
		&envRaw,
		&timeoutSeconds,
		&pipelineFilePath,
		&contextDir,
		&sourceRepoURL,
		&job.Source.CommitSHA,
		&sourceRefName,
		&sourceArchiveURI,
		&sourceArchiveDigest,
		&job.SpecVersion,
		&specDigest,
		&job.ResolvedSpecJSON,
		&claimToken,
		&claimedBy,
		&claimExpiresAt,
		&job.CreatedAt,
		&startedAt,
		&finishedAt,
		&errorMessage,
		&exitCode,
		&outputRefsRaw,
	)
	if err != nil {
		return domain.ExecutionJob{}, err
	}

	job.Status = domain.ExecutionJobStatus(status)
	if retryOfJobID.Valid {
		v := retryOfJobID.String
		job.RetryOfJobID = &v
	}
	if lineageRootJobID.Valid {
		v := lineageRootJobID.String
		job.LineageRootJobID = &v
	}
	if queueName.Valid {
		v := queueName.String
		job.QueueName = &v
	}
	if err := json.Unmarshal(commandRaw, &job.Command); err != nil {
		return domain.ExecutionJob{}, err
	}
	if len(envRaw) > 0 {
		if err := json.Unmarshal(envRaw, &job.Environment); err != nil {
			return domain.ExecutionJob{}, err
		}
	} else {
		job.Environment = map[string]string{}
	}
	if timeoutSeconds.Valid {
		v := int(timeoutSeconds.Int64)
		job.TimeoutSeconds = &v
	}
	if pipelineFilePath.Valid {
		v := pipelineFilePath.String
		job.PipelineFilePath = &v
	}
	if contextDir.Valid {
		v := contextDir.String
		job.ContextDir = &v
	}
	if sourceRepoURL.Valid {
		job.Source.RepositoryURL = sourceRepoURL.String
	}
	if sourceRefName.Valid {
		v := sourceRefName.String
		job.Source.RefName = &v
	}
	if sourceArchiveURI.Valid {
		v := sourceArchiveURI.String
		job.Source.ArchiveURI = &v
	}
	if sourceArchiveDigest.Valid {
		v := sourceArchiveDigest.String
		job.Source.ArchiveDigest = &v
	}
	if specDigest.Valid {
		v := specDigest.String
		job.SpecDigest = &v
	}
	if claimToken.Valid {
		v := claimToken.String
		job.ClaimToken = &v
	}
	if claimedBy.Valid {
		v := claimedBy.String
		job.ClaimedBy = &v
	}
	if claimExpiresAt.Valid {
		v := claimExpiresAt.Time
		job.ClaimExpiresAt = &v
	}
	if startedAt.Valid {
		v := startedAt.Time
		job.StartedAt = &v
	}
	if finishedAt.Valid {
		v := finishedAt.Time
		job.FinishedAt = &v
	}
	if errorMessage.Valid {
		v := errorMessage.String
		job.ErrorMessage = &v
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		job.ExitCode = &v
	}
	if len(outputRefsRaw) > 0 {
		if err := json.Unmarshal(outputRefsRaw, &job.OutputRefs); err != nil {
			return domain.ExecutionJob{}, err
		}
	} else {
		job.OutputRefs = []domain.ArtifactRef{}
	}

	return job, nil
}
