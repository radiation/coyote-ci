package domain

import (
	"testing"
	"time"
)

func TestWorkspaceRevisionValidation(t *testing.T) {
	now := time.Now().UTC()
	revision := WorkspaceRevision{
		ID:                      "revision-1",
		ProducingExecutionJobID: "job-1",
		BuildID:                 "build-1",
		NodeID:                  "compile",
		AttemptNumber:           1,
		Status:                  WorkspaceRevisionStatusPublishing,
		CreatedAt:               now,
	}
	if err := revision.ValidateForCreate(); err != nil {
		t.Fatalf("validate publishing revision: %v", err)
	}
	if err := (WorkspaceRevisionPublication{ContentDigest: "sha256:abc", StorageKey: "revisions/revision-1"}).Validate(); err != nil {
		t.Fatalf("validate publication: %v", err)
	}

	revision.Status = WorkspaceRevisionStatusPublished
	if err := revision.ValidateForCreate(); err == nil {
		t.Fatal("expected published revision to be rejected at creation")
	}
	if err := (WorkspaceRevisionPublication{ContentDigest: "", StorageKey: "key"}).Validate(); err == nil {
		t.Fatal("expected empty digest to be rejected")
	}
}

func TestWorkspaceRevisionValidationRejectsIncompleteAndPublishedFields(t *testing.T) {
	now := time.Now().UTC()
	valid := WorkspaceRevision{
		ID:                      "revision-1",
		ProducingExecutionJobID: "job-1",
		BuildID:                 "build-1",
		NodeID:                  "compile",
		AttemptNumber:           1,
		Status:                  WorkspaceRevisionStatusPublishing,
		CreatedAt:               now,
	}
	tests := []struct {
		name   string
		adjust func(*WorkspaceRevision)
	}{
		{name: "missing id", adjust: func(revision *WorkspaceRevision) { revision.ID = " " }},
		{name: "missing producing job", adjust: func(revision *WorkspaceRevision) { revision.ProducingExecutionJobID = "" }},
		{name: "missing build", adjust: func(revision *WorkspaceRevision) { revision.BuildID = "" }},
		{name: "missing node", adjust: func(revision *WorkspaceRevision) { revision.NodeID = "" }},
		{name: "invalid attempt", adjust: func(revision *WorkspaceRevision) { revision.AttemptNumber = 0 }},
		{name: "missing created at", adjust: func(revision *WorkspaceRevision) { revision.CreatedAt = time.Time{} }},
		{name: "published content", adjust: func(revision *WorkspaceRevision) { value := "sha256:one"; revision.ContentDigest = &value }},
		{name: "published timestamp", adjust: func(revision *WorkspaceRevision) { revision.PublishedAt = &now }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			revision := valid
			test.adjust(&revision)
			if err := revision.ValidateForCreate(); err == nil {
				t.Fatal("expected invalid revision")
			}
		})
	}

	negativeSize := int64(-1)
	if err := (WorkspaceRevisionPublication{ContentDigest: "sha256:one", StorageKey: "revisions/1", SizeBytes: &negativeSize}).Validate(); err == nil {
		t.Fatal("expected negative size to be rejected")
	}
}

func TestWorkspaceHelperRoleValid(t *testing.T) {
	if !WorkspaceHelperRolePrepare.Valid() || !WorkspaceHelperRolePublish.Valid() {
		t.Fatal("expected supported helper roles to be valid")
	}
	if WorkspaceHelperRole("other").Valid() {
		t.Fatal("unsupported helper role is valid")
	}
}
