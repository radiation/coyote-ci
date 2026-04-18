package postgres

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type rowScanner interface {
	Scan(dest ...any) error
}

// buildColumns is the canonical column list for build SELECT/RETURNING clauses (full detail).
const buildColumns = `id, project_id, job_id, status, created_at, queued_at, started_at, finished_at, current_step_index, attempt_number, rerun_of_build_id, rerun_from_step_index, error_message, pipeline_config_yaml, pipeline_name, pipeline_source, pipeline_path, repo_url, ref, commit_sha, trigger_kind, scm_provider, event_type, trigger_repository_owner, trigger_repository_name, trigger_repository_url, trigger_raw_ref, trigger_ref, trigger_ref_type, trigger_ref_name, trigger_deleted, trigger_commit_sha, trigger_delivery_id, trigger_actor`

// buildListColumns is a minimal column list used for list queries (omits large pipeline YAML).
const buildListColumns = `id, project_id, job_id, status, created_at, queued_at, started_at, finished_at, current_step_index, attempt_number, rerun_of_build_id, rerun_from_step_index, error_message, pipeline_name, pipeline_source, pipeline_path, repo_url, ref, commit_sha, trigger_kind, scm_provider, event_type, trigger_repository_owner, trigger_repository_name, trigger_repository_url, trigger_raw_ref, trigger_ref, trigger_ref_type, trigger_ref_name, trigger_deleted, trigger_commit_sha, trigger_delivery_id, trigger_actor`

const executionJobColumns = `id, build_id, step_id, node_id, group_name, depends_on_node_ids, name, step_index, attempt_number, retry_of_job_id, lineage_root_job_id, status, queue_name, image, working_dir, command_json, env_json, timeout_seconds, pipeline_file_path, context_dir, source_repo_url, source_commit_sha, source_ref_name, source_archive_uri, source_archive_digest, spec_version, spec_digest, resolved_spec_json, claim_token, claimed_by, claim_expires_at, created_at, started_at, finished_at, error_message, exit_code, output_refs_json`

var executionJobColumnsQualifiedWithJ = qualifyColumns("j", executionJobColumns)

func qualifyColumns(alias string, columns string) string {
	parts := strings.Split(columns, ",")
	qualified := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		qualified = append(qualified, alias+"."+name)
	}
	return strings.Join(qualified, ", ")
}

func scanBuildList(scanner rowScanner) (domain.Build, error) {
	var build domain.Build
	var nf buildNullFields

	err := scanner.Scan(
		&build.ID,
		&build.ProjectID,
		&nf.jobID,
		&nf.status,
		&build.CreatedAt,
		&nf.queuedAt,
		&nf.startedAt,
		&nf.finishedAt,
		&build.CurrentStepIndex,
		&build.AttemptNumber,
		&nf.rerunOfBuildID,
		&nf.rerunFromStepIdx,
		&nf.errorMessage,
		&nf.pipelineName,
		&nf.pipelineSource,
		&nf.pipelinePath,
		&nf.repoURL,
		&nf.ref,
		&nf.commitSHA,
		&nf.triggerKind,
		&nf.scmProvider,
		&nf.eventType,
		&nf.triggerRepositoryOwner,
		&nf.triggerRepositoryName,
		&nf.triggerRepositoryURL,
		&nf.triggerRawRef,
		&nf.triggerRef,
		&nf.triggerRefType,
		&nf.triggerRefName,
		&nf.triggerDeleted,
		&nf.triggerCommitSHA,
		&nf.triggerDeliveryID,
		&nf.triggerActor,
	)
	if err != nil {
		return domain.Build{}, err
	}

	nf.applyTo(&build)
	return build, nil
}

func scanBuild(scanner rowScanner) (domain.Build, error) {
	var build domain.Build
	var nf buildNullFields

	err := scanner.Scan(
		&build.ID,
		&build.ProjectID,
		&nf.jobID,
		&nf.status,
		&build.CreatedAt,
		&nf.queuedAt,
		&nf.startedAt,
		&nf.finishedAt,
		&build.CurrentStepIndex,
		&build.AttemptNumber,
		&nf.rerunOfBuildID,
		&nf.rerunFromStepIdx,
		&nf.errorMessage,
		&nf.pipelineConfigYAML,
		&nf.pipelineName,
		&nf.pipelineSource,
		&nf.pipelinePath,
		&nf.repoURL,
		&nf.ref,
		&nf.commitSHA,
		&nf.triggerKind,
		&nf.scmProvider,
		&nf.eventType,
		&nf.triggerRepositoryOwner,
		&nf.triggerRepositoryName,
		&nf.triggerRepositoryURL,
		&nf.triggerRawRef,
		&nf.triggerRef,
		&nf.triggerRefType,
		&nf.triggerRefName,
		&nf.triggerDeleted,
		&nf.triggerCommitSHA,
		&nf.triggerDeliveryID,
		&nf.triggerActor,
	)
	if err != nil {
		return domain.Build{}, err
	}

	nf.applyTo(&build)
	return build, nil
}

// buildNullFields holds nullable intermediate values used when scanning build
// rows. The applyTo method maps them onto a domain.Build, eliminating duplicate
// null-handling code between scanBuild and scanBuildList.
type buildNullFields struct {
	status                 string
	jobID                  sql.NullString
	queuedAt               sql.NullTime
	startedAt              sql.NullTime
	finishedAt             sql.NullTime
	rerunOfBuildID         sql.NullString
	rerunFromStepIdx       sql.NullInt64
	errorMessage           sql.NullString
	pipelineConfigYAML     sql.NullString
	pipelineName           sql.NullString
	pipelineSource         sql.NullString
	pipelinePath           sql.NullString
	repoURL                sql.NullString
	ref                    sql.NullString
	commitSHA              sql.NullString
	triggerKind            sql.NullString
	scmProvider            sql.NullString
	eventType              sql.NullString
	triggerRepositoryOwner sql.NullString
	triggerRepositoryName  sql.NullString
	triggerRepositoryURL   sql.NullString
	triggerRawRef          sql.NullString
	triggerRef             sql.NullString
	triggerRefType         sql.NullString
	triggerRefName         sql.NullString
	triggerDeleted         sql.NullBool
	triggerCommitSHA       sql.NullString
	triggerDeliveryID      sql.NullString
	triggerActor           sql.NullString
}

func (nf *buildNullFields) applyTo(build *domain.Build) {
	build.Status = domain.BuildStatus(nf.status)
	if nf.jobID.Valid {
		v := nf.jobID.String
		build.JobID = &v
	}
	if nf.queuedAt.Valid {
		v := nf.queuedAt.Time
		build.QueuedAt = &v
	}
	if nf.startedAt.Valid {
		v := nf.startedAt.Time
		build.StartedAt = &v
	}
	if nf.finishedAt.Valid {
		v := nf.finishedAt.Time
		build.FinishedAt = &v
	}
	if build.AttemptNumber <= 0 {
		build.AttemptNumber = 1
	}
	if nf.rerunOfBuildID.Valid {
		v := nf.rerunOfBuildID.String
		build.RerunOfBuildID = &v
	}
	if nf.rerunFromStepIdx.Valid {
		v := int(nf.rerunFromStepIdx.Int64)
		build.RerunFromStepIdx = &v
	}
	if nf.errorMessage.Valid {
		v := nf.errorMessage.String
		build.ErrorMessage = &v
	}
	if nf.pipelineConfigYAML.Valid {
		v := nf.pipelineConfigYAML.String
		build.PipelineConfigYAML = &v
	}
	if nf.pipelineName.Valid {
		v := nf.pipelineName.String
		build.PipelineName = &v
	}
	if nf.pipelineSource.Valid {
		v := nf.pipelineSource.String
		build.PipelineSource = &v
	}
	if nf.pipelinePath.Valid {
		v := nf.pipelinePath.String
		build.PipelinePath = &v
	}
	if nf.repoURL.Valid {
		v := nf.repoURL.String
		build.RepoURL = &v
	}
	if nf.ref.Valid {
		v := nf.ref.String
		build.Ref = &v
	}
	if nf.commitSHA.Valid {
		v := nf.commitSHA.String
		build.CommitSHA = &v
	}
	if nf.triggerKind.Valid {
		build.Trigger.Kind = domain.BuildTriggerKind(nf.triggerKind.String)
	}
	if nf.scmProvider.Valid {
		v := nf.scmProvider.String
		build.Trigger.SCMProvider = &v
	}
	if nf.eventType.Valid {
		v := nf.eventType.String
		build.Trigger.EventType = &v
	}
	if nf.triggerRepositoryOwner.Valid {
		v := nf.triggerRepositoryOwner.String
		build.Trigger.RepositoryOwner = &v
	}
	if nf.triggerRepositoryName.Valid {
		v := nf.triggerRepositoryName.String
		build.Trigger.RepositoryName = &v
	}
	if nf.triggerRepositoryURL.Valid {
		v := nf.triggerRepositoryURL.String
		build.Trigger.RepositoryURL = &v
	}
	if nf.triggerRawRef.Valid {
		v := nf.triggerRawRef.String
		build.Trigger.RawRef = &v
	}
	if nf.triggerRef.Valid {
		v := nf.triggerRef.String
		build.Trigger.Ref = &v
	}
	if nf.triggerRefType.Valid {
		v := nf.triggerRefType.String
		build.Trigger.RefType = &v
	}
	if nf.triggerRefName.Valid {
		v := nf.triggerRefName.String
		build.Trigger.RefName = &v
	}
	if nf.triggerDeleted.Valid {
		v := nf.triggerDeleted.Bool
		build.Trigger.Deleted = &v
	}
	if nf.triggerCommitSHA.Valid {
		v := nf.triggerCommitSHA.String
		build.Trigger.CommitSHA = &v
	}
	if nf.triggerDeliveryID.Valid {
		v := nf.triggerDeliveryID.String
		build.Trigger.DeliveryID = &v
	}
	if nf.triggerActor.Valid {
		v := nf.triggerActor.String
		build.Trigger.Actor = &v
	}
	build.Trigger = domain.NormalizeBuildTrigger(build.Trigger)
	build.Source = domain.NewSourceSpec(readOptionalString(build.RepoURL), readOptionalString(build.Ref), readOptionalString(build.CommitSHA))
}

func readOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func scanStep(scanner rowScanner) (domain.BuildStep, error) {
	var step domain.BuildStep
	var nodeID sql.NullString
	var groupName sql.NullString
	var dependsOnRaw []byte
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
	var artifactPathsRaw []byte
	var cacheConfigRaw []byte

	err := scanner.Scan(
		&step.ID,
		&step.BuildID,
		&step.StepIndex,
		&nodeID,
		&groupName,
		&dependsOnRaw,
		&step.Name,
		&step.Image,
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
		&artifactPathsRaw,
		&cacheConfigRaw,
	)
	if err != nil {
		return domain.BuildStep{}, err
	}
	if nodeID.Valid {
		step.NodeID = nodeID.String
	}
	if groupName.Valid {
		v := groupName.String
		step.GroupName = &v
	}
	if len(dependsOnRaw) > 0 {
		if err := json.Unmarshal(dependsOnRaw, &step.DependsOnNodes); err != nil {
			return domain.BuildStep{}, err
		}
	} else {
		step.DependsOnNodes = []string{}
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
	if len(artifactPathsRaw) > 0 {
		if err := json.Unmarshal(artifactPathsRaw, &step.ArtifactPaths); err != nil {
			return domain.BuildStep{}, err
		}
		if step.ArtifactPaths == nil {
			step.ArtifactPaths = []string{}
		}
	} else {
		step.ArtifactPaths = []string{}
	}
	if len(cacheConfigRaw) > 0 && strings.TrimSpace(string(cacheConfigRaw)) != "null" {
		var cacheConfig domain.StepCacheConfig
		if err := json.Unmarshal(cacheConfigRaw, &cacheConfig); err != nil {
			return domain.BuildStep{}, err
		}
		step.Cache = &cacheConfig
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
	var nodeID sql.NullString
	var groupName sql.NullString
	var dependsOnRaw []byte
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
		&nodeID,
		&groupName,
		&dependsOnRaw,
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
	if nodeID.Valid {
		job.NodeID = nodeID.String
	}
	if groupName.Valid {
		v := groupName.String
		job.GroupName = &v
	}
	if len(dependsOnRaw) > 0 {
		if err := json.Unmarshal(dependsOnRaw, &job.DependsOnNodeIDs); err != nil {
			return domain.ExecutionJob{}, err
		}
	} else {
		job.DependsOnNodeIDs = []string{}
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

func normalizeNodeID(nodeID string, stepIndex int) string {
	trimmed := strings.TrimSpace(nodeID)
	if trimmed == "" {
		return domain.FallbackNodeID(stepIndex)
	}
	return trimmed
}

func normalizeNodeIDSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if out == nil {
		return []string{}
	}
	return out
}
