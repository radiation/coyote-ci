package api

import (
	"encoding/json"
	"testing"
)

func TestBuildEnvelope_JSONShape(t *testing.T) {
	errMsg := "failed"
	queuedAt := "2026-03-20T12:01:00Z"
	startedAt := "2026-03-20T12:02:00Z"
	finishedAt := "2026-03-20T12:03:00Z"
	payload := BuildEnvelope{Data: BuildResponse{
		ID:               "build-1",
		ProjectID:        "project-1",
		Status:           "running",
		CreatedAt:        "2026-03-20T12:00:00Z",
		QueuedAt:         &queuedAt,
		StartedAt:        &startedAt,
		FinishedAt:       &finishedAt,
		CurrentStepIndex: 2,
		ErrorMessage:     &errMsg,
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
	for _, field := range []string{"queued_at", "started_at", "finished_at", "current_step_index", "error_message"} {
		if _, ok := data[field]; !ok {
			t.Fatalf("expected %s field in payload: %v", field, data)
		}
	}
}

func TestBuildStepsEnvelope_JSONOptionalAndEmptyCollections(t *testing.T) {
	payload := BuildStepsEnvelope{Data: BuildStepsResponse{
		BuildID: "build-1",
		Steps: []BuildStepResponse{
			{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "checkout", Status: "success", WorkerID: nil, StartedAt: nil, FinishedAt: nil, ExitCode: nil, Stdout: nil, Stderr: nil, ErrorMessage: nil},
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
	if first["finished_at"] != nil {
		t.Fatalf("expected finished_at null, got %v", first["finished_at"])
	}
	for _, field := range []string{"id", "build_id", "step_index", "name", "status", "worker_id", "exit_code", "stdout", "stderr", "error_message"} {
		if _, ok := first[field]; !ok {
			t.Fatalf("expected %s field to be present: %v", field, first)
		}
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
