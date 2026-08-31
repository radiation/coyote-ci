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
