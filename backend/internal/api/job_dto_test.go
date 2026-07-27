package api

import (
	"encoding/json"
	"testing"
)

func TestUpdateJobRequestManagedImageOmitted(t *testing.T) {
	var req UpdateJobRequest
	if err := json.Unmarshal([]byte(`{"enabled":true}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.ManagedImagePresent() {
		t.Fatal("expected managed_image to be absent")
	}
	if req.ManagedImage != nil {
		t.Fatalf("expected managed_image nil when absent, got %#v", req.ManagedImage)
	}
}

func TestUpdateJobRequestManagedImageNull(t *testing.T) {
	var req UpdateJobRequest
	if err := json.Unmarshal([]byte(`{"managed_image":null}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !req.ManagedImagePresent() {
		t.Fatal("expected managed_image to be marked present")
	}
	if req.ManagedImage != nil {
		t.Fatalf("expected managed_image nil for explicit null, got %#v", req.ManagedImage)
	}
}

func TestUpdateJobRequestManagedImageObject(t *testing.T) {
	var req UpdateJobRequest
	if err := json.Unmarshal([]byte(`{"managed_image":{"enabled":true,"managed_image_name":"go"}}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !req.ManagedImagePresent() {
		t.Fatal("expected managed_image to be marked present")
	}
	if req.ManagedImage == nil {
		t.Fatal("expected managed_image object")
	}
	if req.ManagedImage.Enabled == nil || !*req.ManagedImage.Enabled {
		t.Fatalf("expected managed_image.enabled=true, got %#v", req.ManagedImage.Enabled)
	}
	if req.ManagedImage.ManagedImageName == nil || *req.ManagedImage.ManagedImageName != "go" {
		t.Fatalf("expected managed_image_name=go, got %#v", req.ManagedImage.ManagedImageName)
	}
}

func TestUpdateJobRequestRepositoryIDOmitted(t *testing.T) {
	var req UpdateJobRequest
	if err := json.Unmarshal([]byte(`{"enabled":true}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if req.RepositoryIDPresent() {
		t.Fatal("expected repository_id to be absent")
	}
	if req.RepositoryID != nil {
		t.Fatalf("expected repository_id nil when absent, got %#v", req.RepositoryID)
	}
}

func TestUpdateJobRequestRepositoryIDNull(t *testing.T) {
	var req UpdateJobRequest
	if err := json.Unmarshal([]byte(`{"repository_id":null}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !req.RepositoryIDPresent() {
		t.Fatal("expected repository_id to be marked present")
	}
	if req.RepositoryID != nil {
		t.Fatalf("expected repository_id nil for explicit null, got %#v", req.RepositoryID)
	}
}

func TestUpdateJobRequestRepositoryIDValueAndEmptyString(t *testing.T) {
	var req UpdateJobRequest
	if err := json.Unmarshal([]byte(`{"repository_id":"repo-1"}`), &req); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !req.RepositoryIDPresent() {
		t.Fatal("expected repository_id to be marked present")
	}
	if req.RepositoryID == nil || *req.RepositoryID != "repo-1" {
		t.Fatalf("expected repository_id=repo-1, got %#v", req.RepositoryID)
	}

	var emptyReq UpdateJobRequest
	if err := json.Unmarshal([]byte(`{"repository_id":""}`), &emptyReq); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !emptyReq.RepositoryIDPresent() {
		t.Fatal("expected empty-string repository_id to be marked present")
	}
	if emptyReq.RepositoryID == nil || *emptyReq.RepositoryID != "" {
		t.Fatalf("expected empty-string repository_id, got %#v", emptyReq.RepositoryID)
	}
}
