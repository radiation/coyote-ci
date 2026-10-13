package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
)

type mcpArtifactRepo struct{ record domain.ArtifactRecord }

func (r mcpArtifactRepo) Browse(context.Context, repository.BrowseArtifactsParams) ([]domain.ArtifactRecord, error) {
	return []domain.ArtifactRecord{r.record}, nil
}

func TestMapArtifactToolErrorNormalizesUnauthorizedLookup(t *testing.T) {
	for _, err := range []error{repository.ErrArtifactNotFound, authorization.ErrForbidden, authorization.ErrUnauthenticated} {
		result := mapArtifactToolError(err)
		if !result.IsError || toolResultText(result) != "not_found" {
			t.Fatalf("expected safe not_found for %v, got %+v", err, result)
		}
	}
	scopeErr := &authorization.MissingAPITokenScopeError{}
	if result := mapArtifactToolError(scopeErr); !result.IsError || toolResultText(result) != "missing_token_scope" {
		t.Fatalf("expected missing scope result, got %+v", result)
	}
	if result := mapArtifactToolError(errors.New("database unavailable")); !result.IsError || toolResultText(result) != "internal_error" {
		t.Fatalf("expected safe internal error, got %+v", result)
	}
}
func (r mcpArtifactRepo) ListCatalog(context.Context, repository.ArtifactCatalogParams) ([]domain.ArtifactRecord, error) {
	return []domain.ArtifactRecord{r.record}, nil
}
func (r mcpArtifactRepo) GetCatalogByID(_ context.Context, id string) (domain.ArtifactRecord, error) {
	if id != r.record.Artifact.ID {
		return domain.ArtifactRecord{}, repository.ErrArtifactNotFound
	}
	return r.record, nil
}

type mcpVersionTagRepo struct{ tags []domain.VersionTag }

func (r mcpVersionTagRepo) ListByArtifactID(_ context.Context, id string) ([]domain.VersionTag, error) {
	return r.ListByArtifactIDs(context.Background(), []string{id})
}
func (r mcpVersionTagRepo) ListByArtifactIDs(_ context.Context, ids []string) ([]domain.VersionTag, error) {
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
func (mcpVersionTagRepo) ListByManagedImageVersionID(context.Context, string) ([]domain.VersionTag, error) {
	return nil, nil
}
func (mcpVersionTagRepo) ListByJobID(context.Context, string) ([]domain.VersionTag, error) {
	return nil, nil
}
func (mcpVersionTagRepo) CreateForTargets(context.Context, repository.CreateVersionTagsParams) ([]domain.VersionTag, error) {
	return nil, nil
}
func (mcpVersionTagRepo) ListByJobIDAndVersion(context.Context, string, string) ([]domain.VersionTag, error) {
	return nil, nil
}

func TestArtifactToolsReturnSafeAuthorizedMetadata(t *testing.T) {
	now := time.Now().UTC()
	id := "artifact-1"
	step := &domain.BuildStep{StepIndex: 2, Name: "package"}
	record := domain.ArtifactRecord{Artifact: domain.BuildArtifact{ID: id, BuildID: "build-1", Name: "app", LogicalPath: "dist/app.tgz", StorageKey: "private/key", SizeBytes: 12, CreatedAt: now}, Build: domain.Build{ID: "build-1", ProjectID: "project-1", CommitSHA: stringPointer("abc"), CreatedAt: now}, Step: step}
	reader := artifactsvc.NewAuthorizedReader(artifactsvc.NewService(mcpArtifactRepo{record: record}), versiontagsvc.NewService(mcpVersionTagRepo{tags: []domain.VersionTag{{ArtifactID: &id, Kind: domain.VersionTagKindVersion, Version: "1.0.0"}, {ArtifactID: &id, Kind: domain.VersionTagKindChannel, Version: "stable"}}}), authorization.NewAuthorizer(true, nil))
	tools := artifactTools{reader: reader}
	ctx := context.WithValue(context.Background(), principalContextKey{}, authorization.Principal{})

	_, list, listErr := tools.list(ctx, nil, ListArtifactVersionsInput{ProjectID: "project-1"})
	if listErr != nil || len(list.Artifacts) != 1 || list.Artifacts[0].LogicalPath != "dist/app.tgz" || len(list.Artifacts[0].VersionLabels) != 1 || len(list.Artifacts[0].CurrentChannels) != 1 {
		t.Fatalf("unexpected list result=%+v err=%v", list, listErr)
	}
	_, get, getErr := tools.get(ctx, nil, GetArtifactVersionInput{ArtifactID: id})
	if getErr != nil || get.ArtifactID != id || get.LogicalPath != "dist/app.tgz" {
		t.Fatalf("unexpected get result=%+v err=%v", get, getErr)
	}
	_, provenance, provenanceErr := tools.provenance(ctx, nil, GetArtifactVersionInput{ArtifactID: id})
	if provenanceErr != nil || provenance.StepIndex == nil || *provenance.StepIndex != 2 || provenance.CommitSHA == nil || *provenance.CommitSHA != "abc" {
		t.Fatalf("unexpected provenance=%+v err=%v", provenance, provenanceErr)
	}
	if get.ProducingBuildID == "" || get.LogicalPath == "private/key" {
		t.Fatalf("unsafe artifact output: %+v", get)
	}
}
