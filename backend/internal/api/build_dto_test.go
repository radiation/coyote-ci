package api

import (
	"encoding/json"
	"testing"
)

func TestBuildEnvelope_JSONShape(t *testing.T) {
	payload := BuildEnvelope{Data: BuildResponse{
		ID:        "build-1",
		ProjectID: "project-1",
		Status:    "running",
		CreatedAt: "2026-03-20T12:00:00Z",
	}}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	data, ok := decoded["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %v", decoded)
	}
	if data["id"] != "build-1" || data["project_id"] != "project-1" || data["status"] != "running" {
		t.Fatalf("unexpected build payload: %v", data)
	}
	if _, ok := data["created_at"]; !ok {
		t.Fatalf("expected created_at field in payload: %v", data)
	}
}

func TestBuildStepsEnvelope_JSONOptionalAndEmptyCollections(t *testing.T) {
	payload := BuildStepsEnvelope{Data: BuildStepsResponse{
		BuildID: "build-1",
		Steps: []BuildStepResponse{
			{Name: "checkout", Status: "success", StartedAt: nil, EndedAt: nil},
		},
	}}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	data, ok := decoded["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %v", decoded)
	}

	steps, ok := data["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", data["steps"])
	}
	if len(steps) != 1 {
		t.Fatalf("expected one step, got %d", len(steps))
	}

	first, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first step object, got %T", steps[0])
	}
	if _, ok := first["started_at"]; !ok {
		t.Fatalf("expected started_at field to be present: %v", first)
	}
	if first["started_at"] != nil {
		t.Fatalf("expected started_at null, got %v", first["started_at"])
	}
	if first["ended_at"] != nil {
		t.Fatalf("expected ended_at null, got %v", first["ended_at"])
	}
}

func TestBuildLogsEnvelope_EmptyLogsMarshalAsArray(t *testing.T) {
	payload := BuildLogsEnvelope{Data: BuildLogsResponse{
		BuildID: "build-1",
		Logs:    []BuildLogResponse{},
	}}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	data, ok := decoded["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %v", decoded)
	}
	logs, ok := data["logs"].([]any)
	if !ok {
		t.Fatalf("expected logs array, got %T", data["logs"])
	}
	if len(logs) != 0 {
		t.Fatalf("expected zero logs, got %d", len(logs))
	}
}
