package handler

import (
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
)

func toBuildResponse(build domain.Build) api.BuildResponse {
	trigger := domain.NormalizeBuildTrigger(build.Trigger)
	sourceCommitSHA := buildSourceCommitSHA(build)
	triggerCommitSHA := buildTriggerCommitSHA(build, trigger)
	return api.BuildResponse{
		ID:                 build.ID,
		BuildNumber:        build.BuildNumber,
		ProjectID:          build.ProjectID,
		JobID:              build.JobID,
		Status:             string(build.Status),
		CreatedAt:          build.CreatedAt.Format(time.RFC3339),
		QueuedAt:           formatOptionalTime(build.QueuedAt),
		StartedAt:          formatOptionalTime(build.StartedAt),
		FinishedAt:         formatOptionalTime(build.FinishedAt),
		CurrentStepIndex:   build.CurrentStepIndex,
		AttemptNumber:      max(build.AttemptNumber, 1),
		RerunOfBuildID:     build.RerunOfBuildID,
		RerunFromStepIndex: build.RerunFromStepIdx,
		ErrorMessage:       build.ErrorMessage,
		PipelineConfigYAML: build.PipelineConfigYAML,
		PipelineName:       build.PipelineName,
		PipelineSource:     build.PipelineSource,
		PipelinePath:       build.PipelinePath,
		TriggerKind:        string(trigger.Kind),
		SCMProvider:        trigger.SCMProvider,
		EventType:          trigger.EventType,
		RepositoryOwner:    trigger.RepositoryOwner,
		RepositoryName:     trigger.RepositoryName,
		RepositoryURL:      trigger.RepositoryURL,
		TriggerRef:         trigger.Ref,
		RefType:            trigger.RefType,
		SourceCommitSHA:    sourceCommitSHA,
		TriggerCommitSHA:   triggerCommitSHA,
		DeliveryID:         trigger.DeliveryID,
		Actor:              trigger.Actor,
		Source:             toBuildSourceResponse(build),
		Image:              toImageExecutionResponse(build.RequestedImageRef, build.ResolvedImageRef, build.ImageSourceKind, build.ManagedImageID, build.ManagedImageVersionID),
	}
}

func toBuildSourceResponse(build domain.Build) *api.BuildSourceResponse {
	if build.Source != nil {
		return &api.BuildSourceResponse{
			RepositoryURL:   strings.TrimSpace(build.Source.RepositoryURL),
			Ref:             build.Source.Ref,
			SourceCommitSHA: build.Source.CommitSHA,
		}
	}

	if build.RepoURL == nil {
		return nil
	}

	return &api.BuildSourceResponse{
		RepositoryURL:   strings.TrimSpace(*build.RepoURL),
		Ref:             build.Ref,
		SourceCommitSHA: build.CommitSHA,
	}
}

func buildSourceCommitSHA(build domain.Build) *string {
	if build.Source != nil {
		return build.Source.CommitSHA
	}
	return build.CommitSHA
}

func buildTriggerCommitSHA(build domain.Build, trigger domain.BuildTrigger) *string {
	if trigger.CommitSHA != nil {
		return trigger.CommitSHA
	}
	if trigger.Kind != domain.BuildTriggerKindWebhook {
		return nil
	}
	if build.Source != nil {
		return build.Source.CommitSHA
	}
	return build.CommitSHA
}

func toBuildStepResponse(step domain.BuildStep, job *domain.ExecutionJob, outputs []domain.ExecutionJobOutput) api.BuildStepResponse {
	resp := api.BuildStepResponse{
		ID:           step.ID,
		BuildID:      step.BuildID,
		StepIndex:    step.StepIndex,
		GroupName:    step.GroupName,
		Name:         step.Name,
		Command:      displayCommand(step),
		Status:       string(step.Status),
		Image:        toImageExecutionResponse(step.RequestedImageRef, step.ResolvedImageRef, step.ImageSourceKind, step.ManagedImageID, step.ManagedImageVersionID),
		Job:          toExecutionJobResponse(job, outputs),
		WorkerID:     step.WorkerID,
		ExitCode:     step.ExitCode,
		Stdout:       step.Stdout,
		Stderr:       step.Stderr,
		ErrorMessage: step.ErrorMessage,
	}

	if step.StartedAt != nil {
		startedAt := step.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &startedAt
	}

	if step.FinishedAt != nil {
		finishedAt := step.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &finishedAt
	}

	return resp
}

func toExecutionJobResponse(job *domain.ExecutionJob, outputs []domain.ExecutionJobOutput) *api.ExecutionJobResponse {
	if job == nil {
		return nil
	}
	resp := &api.ExecutionJobResponse{
		ID:               job.ID,
		BuildID:          job.BuildID,
		StepID:           job.StepID,
		Name:             job.Name,
		StepIndex:        job.StepIndex,
		AttemptNumber:    max(job.AttemptNumber, 1),
		RetryOfJobID:     job.RetryOfJobID,
		LineageRootJobID: job.LineageRootJobID,
		Status:           string(job.Status),
		Image:            job.Image,
		WorkingDir:       job.WorkingDir,
		Command:          append([]string(nil), job.Command...),
		CommandPreview:   strings.Join(job.Command, " "),
		Environment:      map[string]string{},
		TimeoutSeconds:   job.TimeoutSeconds,
		PipelineFilePath: job.PipelineFilePath,
		ContextDir:       job.ContextDir,
		SourceRepoURL:    job.Source.RepositoryURL,
		SourceCommitSHA:  job.Source.CommitSHA,
		SourceRefName:    job.Source.RefName,
		SpecVersion:      job.SpecVersion,
		SpecDigest:       job.SpecDigest,
		CreatedAt:        job.CreatedAt.Format(time.RFC3339),
		ErrorMessage:     job.ErrorMessage,
		Outputs:          make([]api.ExecutionJobOutputResponse, 0, len(outputs)),
	}
	for key, value := range job.Environment {
		resp.Environment[key] = value
	}
	if job.StartedAt != nil {
		v := job.StartedAt.Format(time.RFC3339)
		resp.StartedAt = &v
	}
	if job.FinishedAt != nil {
		v := job.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &v
	}
	for _, output := range outputs {
		resp.Outputs = append(resp.Outputs, api.ExecutionJobOutputResponse{
			ID:             output.ID,
			JobID:          output.JobID,
			BuildID:        output.BuildID,
			Name:           output.Name,
			Kind:           output.Kind,
			DeclaredPath:   output.DeclaredPath,
			DestinationURI: output.DestinationURI,
			ContentType:    output.ContentType,
			SizeBytes:      output.SizeBytes,
			Digest:         output.Digest,
			Status:         string(output.Status),
			CreatedAt:      output.CreatedAt.Format(time.RFC3339),
		})
	}
	return resp
}

func toImageExecutionResponse(requestedRef *string, resolvedRef *string, sourceKind domain.ImageSourceKind, managedImageID *string, managedImageVersionID *string) api.ImageExecutionResponse {
	kind := strings.TrimSpace(string(sourceKind))
	if kind == "" {
		kind = string(domain.ImageSourceKindExternal)
	}

	return api.ImageExecutionResponse{
		RequestedRef:          requestedRef,
		ResolvedRef:           resolvedRef,
		SourceKind:            kind,
		ManagedImageID:        managedImageID,
		ManagedImageVersionID: managedImageVersionID,
	}
}

func toBuildArtifactResponse(item domain.BuildArtifact) api.BuildArtifactResponse {
	provider := string(item.StorageProvider)
	if provider == "" {
		provider = string(domain.StorageProviderFilesystem)
	}
	return api.BuildArtifactResponse{
		ID:              item.ID,
		BuildID:         item.BuildID,
		StepID:          item.StepID,
		Path:            item.LogicalPath,
		SizeBytes:       item.SizeBytes,
		ContentType:     item.ContentType,
		ChecksumSHA256:  item.ChecksumSHA256,
		StorageProvider: provider,
		DownloadURLPath: "/api/builds/" + item.BuildID + "/artifacts/" + item.ID + "/download",
		VersionTags:     toVersionTagResponses(item.VersionTags),
		CreatedAt:       item.CreatedAt.Format(time.RFC3339),
	}
}

func toVersionTagResponses(tags []domain.VersionTag) []api.VersionTagResponse {
	if len(tags) == 0 {
		return nil
	}
	resp := make([]api.VersionTagResponse, 0, len(tags))
	for _, tag := range tags {
		resp = append(resp, api.VersionTagResponse{
			ID:                    tag.ID,
			JobID:                 tag.JobID,
			Version:               tag.Version,
			TargetType:            string(tag.TargetType),
			ArtifactID:            tag.ArtifactID,
			ManagedImageVersionID: tag.ManagedImageVersionID,
			CreatedAt:             tag.CreatedAt.Format(time.RFC3339),
		})
	}
	return resp
}

func filterVersionTagsForJob(tags []domain.VersionTag, jobID string) []domain.VersionTag {
	if jobID == "" || len(tags) == 0 {
		return tags
	}
	filtered := make([]domain.VersionTag, 0, len(tags))
	for _, tag := range tags {
		if tag.JobID == jobID {
			filtered = append(filtered, tag)
		}
	}
	return filtered
}

func toStepLogChunkResponse(chunk logs.StepLogChunk) api.StepLogChunkResponse {
	return api.StepLogChunkResponse{
		SequenceNo: chunk.SequenceNo,
		BuildID:    chunk.BuildID,
		StepID:     chunk.StepID,
		StepIndex:  chunk.StepIndex,
		StepName:   chunk.StepName,
		Stream:     string(chunk.Stream),
		ChunkText:  chunk.ChunkText,
		CreatedAt:  chunk.CreatedAt.Format(time.RFC3339),
	}
}

func displayCommand(step domain.BuildStep) string {
	command := strings.TrimSpace(step.Command)
	if command == "" {
		return ""
	}

	if isShellCommand(command) && len(step.Args) >= 2 && strings.TrimSpace(step.Args[0]) == "-c" {
		script := strings.TrimSpace(step.Args[1])
		if script != "" {
			return script
		}
	}

	if len(step.Args) == 0 {
		return command
	}

	parts := make([]string, 0, len(step.Args)+1)
	parts = append(parts, command)
	for _, arg := range step.Args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		parts = append(parts, trimmed)
	}

	return strings.Join(parts, " ")
}

func isShellCommand(command string) bool {
	switch command {
	case "sh", "bash", "zsh", "/bin/sh", "/bin/bash", "/bin/zsh":
		return true
	default:
		return false
	}
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}

	formatted := value.Format(time.RFC3339)
	return &formatted
}
