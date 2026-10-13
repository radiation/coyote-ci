package artifact

import (
	"context"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

const (
	DefaultCatalogLimit = 50
	MaxCatalogLimit     = 200
)

var ErrProjectIDRequired = errors.New("project id is required")
var ErrCatalogLimitInvalid = errors.New("catalog limit must be between 1 and 200")
var ErrCatalogOffsetInvalid = errors.New("catalog offset must be non-negative")
var ErrAuthorizedReaderNotConfigured = errors.New("artifact authorized reader is not configured")

type AuthorizedReader struct {
	artifacts   *Service
	versionTags *versiontagsvc.Service
	authorizer  authorization.Authorizer
}

type CatalogOptions struct {
	Query  string
	Limit  int
	Offset int
}

type CatalogResult struct {
	Records    []domain.ArtifactRecord
	TagsByID   map[string][]domain.VersionTag
	NextOffset *int
}

func NewAuthorizedReader(artifacts *Service, versionTags *versiontagsvc.Service, authorizer authorization.Authorizer) *AuthorizedReader {
	return &AuthorizedReader{artifacts: artifacts, versionTags: versionTags, authorizer: authorizer}
}

func (r *AuthorizedReader) ListCatalog(ctx context.Context, principal authorization.Principal, projectID string, options CatalogOptions) (CatalogResult, error) {
	if r == nil || r.artifacts == nil || r.versionTags == nil {
		return CatalogResult{}, ErrAuthorizedReaderNotConfigured
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return CatalogResult{}, ErrProjectIDRequired
	}
	normalized, err := normalizeCatalogOptions(options)
	if err != nil {
		return CatalogResult{}, err
	}
	authErr := r.authorizeProject(ctx, principal, projectID)
	if authErr != nil {
		return CatalogResult{}, authErr
	}
	records, err := r.artifacts.ListCatalog(ctx, ListCatalogInput{
		Query:     normalized.Query,
		ProjectID: projectID,
		Limit:     normalized.Limit + 1,
		Offset:    normalized.Offset,
	})
	if err != nil {
		return CatalogResult{}, err
	}
	result := CatalogResult{Records: records, TagsByID: map[string][]domain.VersionTag{}}
	if len(result.Records) > normalized.Limit {
		next := normalized.Offset + normalized.Limit
		result.NextOffset = &next
		result.Records = result.Records[:normalized.Limit]
	}
	return r.withTags(ctx, result)
}

func (r *AuthorizedReader) GetArtifact(ctx context.Context, principal authorization.Principal, artifactID string) (domain.ArtifactRecord, []domain.VersionTag, error) {
	if r == nil || r.artifacts == nil || r.versionTags == nil {
		return domain.ArtifactRecord{}, nil, ErrAuthorizedReaderNotConfigured
	}
	record, err := r.artifacts.GetArtifact(ctx, strings.TrimSpace(artifactID))
	if err != nil {
		return domain.ArtifactRecord{}, nil, err
	}
	authErr := r.authorizeProject(ctx, principal, record.Build.ProjectID)
	if authErr != nil {
		return domain.ArtifactRecord{}, nil, authErr
	}
	tags, err := r.versionTags.ListArtifactTags(ctx, record.Artifact.ID)
	if err != nil {
		return domain.ArtifactRecord{}, nil, err
	}
	return record, tags, nil
}

func (r *AuthorizedReader) withTags(ctx context.Context, result CatalogResult) (CatalogResult, error) {
	ids := make([]string, 0, len(result.Records))
	for _, record := range result.Records {
		ids = append(ids, record.Artifact.ID)
	}
	tags, err := r.versionTags.ListArtifactTagsByIDs(ctx, ids)
	if err != nil {
		return CatalogResult{}, err
	}
	result.TagsByID = tags
	return result, nil
}

func (r *AuthorizedReader) authorizeProject(ctx context.Context, principal authorization.Principal, projectID string) error {
	if err := r.authorizer.RequireProjectRole(ctx, principal, projectID, domain.ProjectMemberRoleViewer); err != nil {
		return err
	}
	return r.authorizer.RequireAPITokenScope(principal, domain.APITokenScopeArtifactRead)
}

func normalizeCatalogOptions(options CatalogOptions) (CatalogOptions, error) {
	if options.Limit == 0 {
		options.Limit = DefaultCatalogLimit
	}
	if options.Limit < 1 || options.Limit > MaxCatalogLimit {
		return CatalogOptions{}, ErrCatalogLimitInvalid
	}
	if options.Offset < 0 {
		return CatalogOptions{}, ErrCatalogOffsetInvalid
	}
	options.Query = strings.TrimSpace(options.Query)
	return options, nil
}
