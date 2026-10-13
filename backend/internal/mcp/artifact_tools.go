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
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
)

const (
	ListArtifactVersionsToolName = "coyote_list_artifact_versions"
	GetArtifactVersionToolName   = "coyote_get_artifact_version"
	GetVersionProvenanceToolName = "coyote_get_version_provenance"
)

type ListArtifactVersionsInput struct {
	ProjectID string `json:"project_id"`
	Query     string `json:"query,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

type GetArtifactVersionInput struct {
	ArtifactID string `json:"artifact_id"`
}

type ArtifactVersionOutput struct {
	ArtifactID        string    `json:"artifact_id"`
	LogicalArtifactID string    `json:"logical_artifact_id"`
	Name              string    `json:"name"`
	LogicalPath       string    `json:"logical_path"`
	Type              string    `json:"type"`
	SizeBytes         int64     `json:"size_bytes"`
	ChecksumSHA256    *string   `json:"checksum_sha256,omitempty"`
	VersionLabels     []string  `json:"version_labels"`
	CurrentChannels   []string  `json:"current_channels"`
	ProducingBuildID  string    `json:"producing_build_id"`
	CreatedAt         time.Time `json:"created_at"`
}

type ListArtifactVersionsOutput struct {
	Artifacts  []ArtifactVersionOutput `json:"artifacts"`
	NextOffset *int                    `json:"next_offset,omitempty"`
}

type ArtifactProvenanceOutput struct {
	ArtifactID        string     `json:"artifact_id"`
	ProjectID         string     `json:"project_id"`
	JobID             *string    `json:"job_id,omitempty"`
	BuildID           string     `json:"build_id"`
	BuildNumber       int64      `json:"build_number"`
	StepIndex         *int       `json:"step_index,omitempty"`
	StepName          *string    `json:"step_name,omitempty"`
	SourceRef         *string    `json:"source_ref,omitempty"`
	CommitSHA         *string    `json:"commit_sha,omitempty"`
	BuildCreatedAt    time.Time  `json:"build_created_at"`
	BuildFinishedAt   *time.Time `json:"build_finished_at,omitempty"`
	ArtifactCreatedAt time.Time  `json:"artifact_created_at"`
}

type artifactTools struct{ reader *artifactsvc.AuthorizedReader }

func registerArtifactTools(server *sdkmcp.Server, reader *artifactsvc.AuthorizedReader) {
	tools := artifactTools{reader: reader}
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: ListArtifactVersionsToolName, Description: "List bounded artifact instances in an authorized project. Uses stable offset pagination and requires artifact:read."}, tools.list)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: GetArtifactVersionToolName, Description: "Inspect safe metadata, immutable version labels, and current mutable channels for one authorized artifact instance. Requires artifact:read."}, tools.get)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: GetVersionProvenanceToolName, Description: "Get persisted producing-build and source provenance for one authorized artifact instance. Requires artifact:read."}, tools.provenance)
}

func (t artifactTools) list(ctx context.Context, _ *sdkmcp.CallToolRequest, input ListArtifactVersionsInput) (*sdkmcp.CallToolResult, ListArtifactVersionsOutput, error) {
	principal, ok := currentPrincipal(ctx)
	if !ok || strings.TrimSpace(input.ProjectID) == "" {
		return toolError("invalid_request"), ListArtifactVersionsOutput{}, nil
	}
	result, err := t.reader.ListCatalog(ctx, principal, input.ProjectID, artifactsvc.CatalogOptions{Query: input.Query, Limit: input.Limit, Offset: input.Offset})
	if err != nil {
		return mapArtifactToolError(err), ListArtifactVersionsOutput{}, nil
	}
	output := ListArtifactVersionsOutput{Artifacts: make([]ArtifactVersionOutput, 0, len(result.Records)), NextOffset: result.NextOffset}
	for _, record := range result.Records {
		output.Artifacts = append(output.Artifacts, toArtifactVersionOutput(record, result.TagsByID[record.Artifact.ID]))
	}
	return nil, output, nil
}

func (t artifactTools) get(ctx context.Context, _ *sdkmcp.CallToolRequest, input GetArtifactVersionInput) (*sdkmcp.CallToolResult, ArtifactVersionOutput, error) {
	principal, ok := currentPrincipal(ctx)
	if !ok || strings.TrimSpace(input.ArtifactID) == "" {
		return toolError("invalid_request"), ArtifactVersionOutput{}, nil
	}
	record, tags, err := t.reader.GetArtifact(ctx, principal, input.ArtifactID)
	if err != nil {
		return mapArtifactToolError(err), ArtifactVersionOutput{}, nil
	}
	return nil, toArtifactVersionOutput(record, tags), nil
}

func (t artifactTools) provenance(ctx context.Context, _ *sdkmcp.CallToolRequest, input GetArtifactVersionInput) (*sdkmcp.CallToolResult, ArtifactProvenanceOutput, error) {
	principal, ok := currentPrincipal(ctx)
	if !ok || strings.TrimSpace(input.ArtifactID) == "" {
		return toolError("invalid_request"), ArtifactProvenanceOutput{}, nil
	}
	record, _, err := t.reader.GetArtifact(ctx, principal, input.ArtifactID)
	if err != nil {
		return mapArtifactToolError(err), ArtifactProvenanceOutput{}, nil
	}
	return nil, toArtifactProvenanceOutput(record), nil
}

func mapArtifactToolError(err error) *sdkmcp.CallToolResult {
	switch {
	case errors.Is(err, repository.ErrArtifactNotFound):
		return toolError("not_found")
	case errors.Is(err, artifactsvc.ErrProjectIDRequired), errors.Is(err, artifactsvc.ErrCatalogLimitInvalid), errors.Is(err, artifactsvc.ErrCatalogOffsetInvalid):
		return toolError("invalid_request")
	case errors.Is(err, authorization.ErrForbidden), errors.Is(err, authorization.ErrUnauthenticated):
		return toolError("not_found")
	default:
		var scopeErr *authorization.MissingAPITokenScopeError
		if errors.As(err, &scopeErr) {
			return toolError("missing_token_scope")
		}
		log.Printf("MCP artifact tool failed")
		return toolError("internal_error")
	}
}

func toArtifactVersionOutput(record domain.ArtifactRecord, tags []domain.VersionTag) ArtifactVersionOutput {
	output := ArtifactVersionOutput{ArtifactID: record.Artifact.ID, LogicalArtifactID: domain.ArtifactIdentityKeyFromRecord(record), Name: record.Artifact.Name, LogicalPath: record.Artifact.LogicalPath, Type: string(domain.ResolveArtifactType(record.Artifact)), SizeBytes: record.Artifact.SizeBytes, ChecksumSHA256: cloneString(record.Artifact.ChecksumSHA256), VersionLabels: []string{}, CurrentChannels: []string{}, ProducingBuildID: record.Build.ID, CreatedAt: record.Artifact.CreatedAt}
	for _, tag := range tags {
		switch tag.Kind {
		case domain.VersionTagKindVersion:
			output.VersionLabels = append(output.VersionLabels, tag.Version)
		case domain.VersionTagKindChannel:
			output.CurrentChannels = append(output.CurrentChannels, tag.Version)
		}
	}
	return output
}

func toArtifactProvenanceOutput(record domain.ArtifactRecord) ArtifactProvenanceOutput {
	output := ArtifactProvenanceOutput{ArtifactID: record.Artifact.ID, ProjectID: record.Build.ProjectID, JobID: cloneString(record.Build.JobID), BuildID: record.Build.ID, BuildNumber: record.Build.BuildNumber, SourceRef: sourceRef(record.Build), CommitSHA: cloneString(record.Build.CommitSHA), BuildCreatedAt: record.Build.CreatedAt, BuildFinishedAt: cloneTime(record.Build.FinishedAt), ArtifactCreatedAt: record.Artifact.CreatedAt}
	if record.Step != nil {
		stepIndex := record.Step.StepIndex
		stepName := record.Step.Name
		output.StepIndex = &stepIndex
		output.StepName = &stepName
	}
	return output
}
