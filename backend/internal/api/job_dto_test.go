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
