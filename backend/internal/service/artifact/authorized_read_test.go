package artifact

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type authorizedReadTagRepo struct{ tags []domain.VersionTag }

type artifactMembershipLookup struct {
	memberships map[string]domain.ProjectMembership
}

func (l artifactMembershipLookup) GetProjectMembership(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	membership, ok := l.memberships[projectID]
	if !ok || membership.UserID != userID {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return membership, nil
}

func (r authorizedReadTagRepo) ListByArtifactID(_ context.Context, id string) ([]domain.VersionTag, error) {
	return r.ListByArtifactIDs(context.Background(), []string{id})
}
func (r authorizedReadTagRepo) ListByArtifactIDs(_ context.Context, ids []string) ([]domain.VersionTag, error) {
	out := []domain.VersionTag{}
	for _, tag := range r.tags {
		for _, id := range ids {
			if tag.ArtifactID != nil && *tag.ArtifactID == id {
				out = append(out, tag)
			}
		}
	}
	return out, nil
}
func (authorizedReadTagRepo) ListByManagedImageVersionID(context.Context, string) ([]domain.VersionTag, error) {
	return nil, nil
}
func (authorizedReadTagRepo) ListByJobID(context.Context, string) ([]domain.VersionTag, error) {
	return nil, nil
}
func (authorizedReadTagRepo) CreateForTargets(context.Context, repository.CreateVersionTagsParams) ([]domain.VersionTag, error) {
	return nil, nil
}
func (authorizedReadTagRepo) ListByJobIDAndVersion(context.Context, string, string) ([]domain.VersionTag, error) {
	return nil, nil
}

func TestAuthorizedReaderAuthorizesCatalogAndArtifactOwnership(t *testing.T) {
	now := time.Now().UTC()
	artifactID := "artifact-1"
	record := domain.ArtifactRecord{Artifact: domain.BuildArtifact{ID: artifactID, BuildID: "build-1", LogicalPath: "dist/app.tgz", CreatedAt: now}, Build: domain.Build{ID: "build-1", ProjectID: "project-1", CreatedAt: now}}
	repo := &fakeCatalogRepo{records: []domain.ArtifactRecord{record, record}, record: record}
	tagRepo := authorizedReadTagRepo{tags: []domain.VersionTag{{ArtifactID: &artifactID, Kind: domain.VersionTagKindVersion, Version: "1.0.0"}, {ArtifactID: &artifactID, Kind: domain.VersionTagKindChannel, Version: "stable"}}}
	reader := NewAuthorizedReader(&Service{catalogRepo: repo}, versiontagsvc.NewService(tagRepo), authorization.NewAuthorizer(false, artifactMembershipLookup{memberships: map[string]domain.ProjectMembership{"project-1": {ProjectID: "project-1", UserID: "viewer-1", Role: domain.ProjectMemberRoleViewer}}}))
	principal := authorization.Principal{User: domain.User{ID: "viewer-1"}, APIToken: &authorization.APITokenIdentity{ID: "token-1", Scopes: []domain.APITokenScope{domain.APITokenScopeArtifactRead}}}

	listed, listErr := reader.ListCatalog(context.Background(), principal, "project-1", CatalogOptions{Limit: 1})
	if listErr != nil || len(listed.Records) != 1 || listed.NextOffset == nil || *listed.NextOffset != 1 || len(listed.TagsByID[artifactID]) != 2 {
		t.Fatalf("unexpected authorized catalog result=%+v err=%v", listed, listErr)
	}
	if _, _, getErr := reader.GetArtifact(context.Background(), principal, artifactID); getErr != nil {
		t.Fatalf("get authorized artifact: %v", getErr)
	}
	outsider := authorization.Principal{User: domain.User{ID: "outsider"}, APIToken: &authorization.APITokenIdentity{Scopes: []domain.APITokenScope{domain.APITokenScopeArtifactRead}}}
	if _, _, getErr := reader.GetArtifact(context.Background(), outsider, artifactID); !errors.Is(getErr, authorization.ErrForbidden) {
		t.Fatalf("expected cross-project denial, got %v", getErr)
	}
	missingScope := principal
	missingScope.APIToken.Scopes = nil
	if _, _, getErr := reader.GetArtifact(context.Background(), missingScope, artifactID); getErr == nil {
		t.Fatal("expected missing artifact scope")
	}
}

func TestAuthorizedReaderCatalogValidation(t *testing.T) {
	reader := NewAuthorizedReader(nil, nil, authorization.NewAuthorizer(true, nil))
	if _, err := reader.ListCatalog(context.Background(), authorization.Principal{}, "project-1", CatalogOptions{}); !errors.Is(err, ErrAuthorizedReaderNotConfigured) {
		t.Fatalf("expected unconfigured reader denial, got %v", err)
	}
}
