package mcp

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

const (
	GetBuildToolName         = "coyote_get_build"
	GetBuildFailuresToolName = "coyote_get_build_failures"
	GetStepLogsToolName      = "coyote_get_step_logs"
)

// GetBuildInput is the stable input schema for coyote_get_build.
type GetBuildInput struct {
	BuildID string `json:"build_id"`
}

// GetBuildOutput is the safe, allow-listed result schema for coyote_get_build.
type GetBuildOutput struct {
	BuildID     string             `json:"build_id"`
	BuildNumber int64              `json:"build_number"`
	ProjectID   string             `json:"project_id"`
	JobID       *string            `json:"job_id,omitempty"`
	Status      domain.BuildStatus `json:"status"`
	CreatedAt   time.Time          `json:"created_at"`
	StartedAt   *time.Time         `json:"started_at,omitempty"`
	FinishedAt  *time.Time         `json:"finished_at,omitempty"`
	SourceRef   *string            `json:"source_ref,omitempty"`
	CommitSHA   *string            `json:"commit_sha,omitempty"`
}

// GetBuildFailuresInput is the stable input schema for coyote_get_build_failures.
type GetBuildFailuresInput struct {
	BuildID string `json:"build_id"`
}

// GetBuildFailuresOutput contains only persisted, non-log failure metadata.
type GetBuildFailuresOutput struct {
	BuildID     string                  `json:"build_id"`
	Status      domain.BuildStatus      `json:"status"`
	CreatedAt   time.Time               `json:"created_at"`
	StartedAt   *time.Time              `json:"started_at,omitempty"`
	FinishedAt  *time.Time              `json:"finished_at,omitempty"`
	FailedSteps []FailedBuildStepOutput `json:"failed_steps"`
}

type FailedBuildStepOutput struct {
	StepIndex  int                    `json:"step_index"`
	Name       string                 `json:"name"`
	Status     domain.BuildStepStatus `json:"status"`
	ExitCode   *int                   `json:"exit_code,omitempty"`
	StartedAt  *time.Time             `json:"started_at,omitempty"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
	LogsTool   bool                   `json:"logs_tool_available"`
}

// GetStepLogsInput is the stable input schema for coyote_get_step_logs.
type GetStepLogsInput struct {
	BuildID       string `json:"build_id"`
	StepIndex     *int   `json:"step_index"`
	AfterSequence int64  `json:"after_sequence,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	MaxBytes      int    `json:"max_bytes,omitempty"`
}

type GetStepLogsOutput struct {
	BuildID       string          `json:"build_id"`
	StepIndex     int             `json:"step_index"`
	Chunks        []StepLogOutput `json:"chunks"`
	NextSequence  int64           `json:"next_sequence"`
	HasMore       bool            `json:"has_more"`
	ResponseBytes int             `json:"response_bytes"`
}

type StepLogOutput struct {
	Sequence  int64              `json:"sequence"`
	Stream    logs.StepLogStream `json:"stream"`
	Timestamp time.Time          `json:"timestamp"`
	Text      string             `json:"text"`
}

type buildTools struct {
	reader *buildsvc.AuthorizedReader
}

func registerBuildTools(server *sdkmcp.Server, reader *buildsvc.AuthorizedReader) {
	tools := buildTools{reader: reader}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        GetBuildToolName,
		Description: "Get safe metadata for a Coyote build by its build_id. Requires build:read for API-token callers.",
	}, tools.getBuild)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        GetBuildFailuresToolName,
		Description: "Get failed-step metadata for a Coyote build. Requires build:read for API-token callers; does not include log text.",
	}, tools.getBuildFailures)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        GetStepLogsToolName,
		Description: "Get cursor-paginated raw log chunks for a Coyote build step. max_bytes limits the combined UTF-8 byte length of returned chunk text, not total MCP response bytes. Requires both build:read and build:logs for API-token callers.",
	}, tools.getStepLogs)
}

func (t buildTools) getBuild(ctx context.Context, _ *sdkmcp.CallToolRequest, input GetBuildInput) (*sdkmcp.CallToolResult, GetBuildOutput, error) {
	buildID := strings.TrimSpace(input.BuildID)
	if buildID == "" {
		return toolError("invalid_request"), GetBuildOutput{}, nil
	}
	principal, ok := currentPrincipal(ctx)
	if !ok {
		return toolError("authentication_required"), GetBuildOutput{}, nil
	}
	build, err := t.reader.GetBuild(ctx, principal, buildID)
	if err != nil {
		return mapBuildToolError(err), GetBuildOutput{}, nil
	}
	return nil, toGetBuildOutput(build), nil
}

func (t buildTools) getBuildFailures(ctx context.Context, _ *sdkmcp.CallToolRequest, input GetBuildFailuresInput) (*sdkmcp.CallToolResult, GetBuildFailuresOutput, error) {
	buildID := strings.TrimSpace(input.BuildID)
	if buildID == "" {
		return toolError("invalid_request"), GetBuildFailuresOutput{}, nil
	}
	principal, ok := currentPrincipal(ctx)
	if !ok {
		return toolError("authentication_required"), GetBuildFailuresOutput{}, nil
	}
	failures, err := t.reader.GetBuildFailures(ctx, principal, buildID)
	if err != nil {
		return mapBuildToolError(err), GetBuildFailuresOutput{}, nil
	}
	return nil, toGetBuildFailuresOutput(failures), nil
}

func (t buildTools) getStepLogs(ctx context.Context, _ *sdkmcp.CallToolRequest, input GetStepLogsInput) (*sdkmcp.CallToolResult, GetStepLogsOutput, error) {
	buildID := strings.TrimSpace(input.BuildID)
	if buildID == "" {
		return toolError("invalid_request"), GetStepLogsOutput{}, nil
	}
	if input.StepIndex == nil {
		return toolError("invalid_request"), GetStepLogsOutput{}, nil
	}
	principal, ok := currentPrincipal(ctx)
	if !ok {
		return toolError("authentication_required"), GetStepLogsOutput{}, nil
	}
	result, err := t.reader.GetStepLogs(ctx, principal, buildID, *input.StepIndex, buildsvc.StepLogOptions{
		AfterSequence: input.AfterSequence,
		Limit:         input.Limit,
		MaxBytes:      input.MaxBytes,
	})
	if err != nil {
		return mapBuildToolError(err), GetStepLogsOutput{}, nil
	}
	return nil, toGetStepLogsOutput(result, *input.StepIndex), nil
}

func mapBuildToolError(err error) *sdkmcp.CallToolResult {
	var chunkTooLargeErr *buildsvc.StepLogChunkTooLargeError
	switch {
	case errors.Is(err, buildsvc.ErrBuildNotFound), errors.Is(err, buildsvc.ErrBuildStepNotFound), errors.Is(err, repository.ErrBuildNotFound):
		return toolError("not_found")
	case errors.Is(err, buildsvc.ErrStepLogIndexInvalid), errors.Is(err, buildsvc.ErrStepLogCursorInvalid), errors.Is(err, buildsvc.ErrStepLogLimitInvalid), errors.Is(err, buildsvc.ErrStepLogMaxBytesInvalid):
		return toolError("invalid_request")
	case errors.As(err, &chunkTooLargeErr):
		return toolError("chunk_exceeds_limit")
	case errors.Is(err, authorization.ErrForbidden), errors.Is(err, authorization.ErrUnauthenticated):
		return toolError("forbidden")
	case errors.Is(err, authorization.ErrProjectIDRequired), errors.Is(err, authorization.ErrProjectAuthorizationNotConfigured), errors.Is(err, authorization.ErrAPITokenIdentityUnavailable):
		log.Printf("MCP get build authorization configuration error")
		return toolError("internal_error")
	default:
		var scopeErr *authorization.MissingAPITokenScopeError
		if errors.As(err, &scopeErr) {
			return toolError("missing_token_scope")
		}
		log.Printf("MCP get build failed")
		return toolError("internal_error")
	}
}

func toolError(code string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: code}},
		IsError: true,
	}
}

func toGetBuildOutput(build domain.Build) GetBuildOutput {
	return GetBuildOutput{
		BuildID:     build.ID,
		BuildNumber: build.BuildNumber,
		ProjectID:   build.ProjectID,
		JobID:       cloneString(build.JobID),
		Status:      build.Status,
		CreatedAt:   build.CreatedAt,
		StartedAt:   cloneTime(build.StartedAt),
		FinishedAt:  cloneTime(build.FinishedAt),
		SourceRef:   sourceRef(build),
		CommitSHA:   cloneString(build.CommitSHA),
	}
}

func toGetBuildFailuresOutput(failures buildsvc.BuildFailures) GetBuildFailuresOutput {
	output := GetBuildFailuresOutput{
		BuildID:     failures.Build.ID,
		Status:      failures.Build.Status,
		CreatedAt:   failures.Build.CreatedAt,
		StartedAt:   cloneTime(failures.Build.StartedAt),
		FinishedAt:  cloneTime(failures.Build.FinishedAt),
		FailedSteps: make([]FailedBuildStepOutput, 0, len(failures.FailedSteps)),
	}
	for _, step := range failures.FailedSteps {
		output.FailedSteps = append(output.FailedSteps, FailedBuildStepOutput{
			StepIndex:  step.StepIndex,
			Name:       step.Name,
			Status:     step.Status,
			ExitCode:   cloneInt(step.ExitCode),
			StartedAt:  cloneTime(step.StartedAt),
			FinishedAt: cloneTime(step.FinishedAt),
			LogsTool:   true,
		})
	}
	return output
}

func toGetStepLogsOutput(result buildsvc.StepLogs, stepIndex int) GetStepLogsOutput {
	output := GetStepLogsOutput{
		BuildID:      result.Build.ID,
		StepIndex:    stepIndex,
		Chunks:       make([]StepLogOutput, 0, len(result.Chunks)),
		NextSequence: result.NextSequence,
		HasMore:      result.HasMore,
	}
	for _, chunk := range result.Chunks {
		output.Chunks = append(output.Chunks, StepLogOutput{
			Sequence:  chunk.SequenceNo,
			Stream:    chunk.Stream,
			Timestamp: chunk.CreatedAt,
			Text:      chunk.ChunkText,
		})
		output.ResponseBytes += len(chunk.ChunkText)
	}
	return output
}

func sourceRef(build domain.Build) *string {
	if value := cloneString(build.SourceRef); value != nil {
		return value
	}
	if value := cloneString(build.Ref); value != nil {
		return value
	}
	if build.Source != nil {
		return cloneString(build.Source.Ref)
	}
	return nil
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
