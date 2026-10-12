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
	"github.com/radiation/coyote-ci/backend/internal/repository"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

const GetBuildToolName = "coyote_get_build"

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

type buildTools struct {
	reader *buildsvc.AuthorizedReader
}

func registerBuildTools(server *sdkmcp.Server, reader *buildsvc.AuthorizedReader) {
	tools := buildTools{reader: reader}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        GetBuildToolName,
		Description: "Get safe metadata for a Coyote build by its build_id. Requires build:read for API-token callers.",
	}, tools.getBuild)
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
		return mapBuildToolError(err)
	}
	return nil, toGetBuildOutput(build), nil
}

func mapBuildToolError(err error) (*sdkmcp.CallToolResult, GetBuildOutput, error) {
	switch {
	case errors.Is(err, buildsvc.ErrBuildNotFound), errors.Is(err, repository.ErrBuildNotFound):
		return toolError("not_found"), GetBuildOutput{}, nil
	case errors.Is(err, authorization.ErrForbidden), errors.Is(err, authorization.ErrUnauthenticated):
		return toolError("forbidden"), GetBuildOutput{}, nil
	case errors.Is(err, authorization.ErrProjectIDRequired), errors.Is(err, authorization.ErrProjectAuthorizationNotConfigured), errors.Is(err, authorization.ErrAPITokenIdentityUnavailable):
		log.Printf("MCP get build authorization configuration error")
		return toolError("internal_error"), GetBuildOutput{}, nil
	default:
		var scopeErr *authorization.MissingAPITokenScopeError
		if errors.As(err, &scopeErr) {
			return toolError("missing_token_scope"), GetBuildOutput{}, nil
		}
		log.Printf("MCP get build failed")
		return toolError("internal_error"), GetBuildOutput{}, nil
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
