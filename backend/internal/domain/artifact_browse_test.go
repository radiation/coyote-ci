package domain

import (
	"testing"
	"time"
)

func TestArtifactIdentityKeyUsesJobIDWhenPresent(t *testing.T) {
	jobID := "job-1"
	key := ArtifactIdentityKey(
		Build{ID: "build-1", JobID: &jobID},
		BuildArtifact{LogicalPath: "dist/app.tar"},
	)
	if key != "job-1::dist/app.tar" {
		t.Fatalf("expected job-scoped key, got %q", key)
	}

	buildScopedKey := ArtifactIdentityKey(
		Build{ID: "build-2"},
		BuildArtifact{LogicalPath: "dist/app.tar"},
	)
	if buildScopedKey != "build-2::dist/app.tar" {
		t.Fatalf("expected build-scoped fallback key, got %q", buildScopedKey)
	}
}

func TestGroupArtifactsGroupsVersionsByIdentity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	jobID := "job-1"
	records := []ArtifactRecord{
		{
			Artifact: BuildArtifact{ID: "artifact-2", BuildID: "build-2", LogicalPath: "dist/app.tar", CreatedAt: now.Add(2 * time.Minute)},
			Build:    Build{ID: "build-2", BuildNumber: 12, JobID: &jobID, ProjectID: "project-1", Status: BuildStatusSuccess, CreatedAt: now.Add(2 * time.Minute)},
		},
		{
			Artifact: BuildArtifact{ID: "artifact-1", BuildID: "build-1", LogicalPath: "dist/app.tar", CreatedAt: now},
			Build:    Build{ID: "build-1", BuildNumber: 11, JobID: &jobID, ProjectID: "project-1", Status: BuildStatusSuccess, CreatedAt: now},
		},
	}

	items := GroupArtifacts(records)
	if len(items) != 1 {
		t.Fatalf("expected 1 grouped artifact, got %d", len(items))
	}
	if items[0].Key != "job-1::dist/app.tar" {
		t.Fatalf("expected stable identity key, got %q", items[0].Key)
	}
	if len(items[0].Versions) != 2 {
		t.Fatalf("expected 2 grouped versions, got %d", len(items[0].Versions))
	}
	if items[0].Versions[0].Artifact.ID != "artifact-2" {
		t.Fatalf("expected newest artifact first, got %q", items[0].Versions[0].Artifact.ID)
	}
}

func TestInferArtifactType(t *testing.T) {
	dockerContentType := "application/vnd.oci.image.layer.v1.tar"
	tests := []struct {
		name        string
		logicalPath string
		contentType *string
		expected    ArtifactType
	}{
		{name: "docker image by content type", logicalPath: "dist/archive.bin", contentType: &dockerContentType, expected: ArtifactTypeDockerImage},
		{name: "docker image by path", logicalPath: "images/backend-image.tar", expected: ArtifactTypeDockerImage},
		{name: "npm package", logicalPath: "packages/pkg-a-1.2.3.tgz", expected: ArtifactTypeNPMPackage},
		{name: "unknown binary", logicalPath: "backend/dist/coyote-server", expected: ArtifactTypeUnknown},
		{name: "generic fallback", logicalPath: "reports/junit.xml", expected: ArtifactTypeGeneric},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := InferArtifactType(tc.logicalPath, tc.contentType); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
