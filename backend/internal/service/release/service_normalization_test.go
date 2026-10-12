package release

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestServiceCreateSnapshot_NormalizesReleaseOutputNames(t *testing.T) {
	version := "1.4.2"
	pipelineYAML := `version: 1
pipeline:
  application_version:
    value: 1.4.2
  release:
    artifacts: [api]
    images: [image]
artifacts:
  - name: " api "
    path: dist/api.tar.gz
steps:
  - name: " image "
    image_build:
      context: .
      dockerfile: Dockerfile
      image: example/api`
	repo := &releaseRepositoryFake{}
	jobID := "job-1"
	_, createErr := NewService(repo).CreateSnapshot(context.Background(), domain.Build{
		ID: "build-1", ProjectID: "project-1", JobID: &jobID, ApplicationVersion: &version, PipelineConfigYAML: &pipelineYAML,
	})
	if createErr != nil {
		t.Fatalf("create snapshot: %v", createErr)
	}
	if len(repo.created.Outputs) != 2 {
		t.Fatalf("outputs=%d, want 2", len(repo.created.Outputs))
	}
	var artifactDefinition map[string]string
	if unmarshalErr := json.Unmarshal([]byte(repo.created.Outputs[0].DefinitionJSON), &artifactDefinition); unmarshalErr != nil {
		t.Fatalf("decode artifact definition: %v", unmarshalErr)
	}
	if artifactDefinition["name"] != "api" || artifactDefinition["path"] != "dist/api.tar.gz" {
		t.Fatalf("artifact definition=%v, want normalized declaration", artifactDefinition)
	}
	var imageDefinition map[string]string
	if unmarshalErr := json.Unmarshal([]byte(repo.created.Outputs[1].DefinitionJSON), &imageDefinition); unmarshalErr != nil {
		t.Fatalf("decode image definition: %v", unmarshalErr)
	}
	if imageDefinition["name"] != "image" || imageDefinition["target_image_reference"] != "example/api" || imageDefinition["node_id"] == "" {
		t.Fatalf("image definition=%v, want normalized declaration", imageDefinition)
	}
}
