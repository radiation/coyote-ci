package build

import (
	"encoding/json"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

func TestBuildExecutionPlanner_PlanWorkspaceInputs(t *testing.T) {
	steps := []domain.BuildStep{
		plannerStep("step-a", "node-a", nil),
		plannerStep("step-b", "node-b", []string{"node-a"}),
		plannerStep("step-c1", "node-c1", []string{"node-b"}),
		plannerStep("step-c2", "node-c2", []string{"node-b"}),
		plannerStep("step-c3", "node-c3", []string{"node-b"}),
		plannerStep("step-d", "node-d", []string{"node-c1", "node-c2", "node-c3"}),
		plannerStep("step-e", "node-e", []string{"node-d"}),
	}

	jobs, err := NewBuildExecutionPlanner().Plan(domain.Build{ID: "build-1"}, steps, "alpine:3")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(jobs) != len(steps) {
		t.Fatalf("expected %d jobs, got %d", len(steps), len(jobs))
	}

	assertWorkspaceInput(t, jobs[0], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource})
	assertWorkspaceInput(t, jobs[1], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: "node-a"})
	for _, jobIndex := range []int{2, 3, 4} {
		assertWorkspaceInput(t, jobs[jobIndex], domain.WorkspaceInputPlan{
			Mode:                       domain.WorkspaceInputModePredecessor,
			ProducerNodeID:             "node-b",
			IsolatedWritableDescendant: true,
		})
	}
	assertWorkspaceInput(t, jobs[5], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeFanIn, CommonAncestorNodeID: "node-b"})
	assertWorkspaceInput(t, jobs[6], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: "node-d"})

	if len(jobs[5].DependsOnNodeIDs) != 3 {
		t.Fatalf("expected fan-in dependencies to be unchanged, got %#v", jobs[5].DependsOnNodeIDs)
	}
}

func TestBuildExecutionPlanner_PlanWorkspaceInputs_FanInWithoutCommonAncestorUsesSourceBaseline(t *testing.T) {
	steps := []domain.BuildStep{
		plannerStep("step-a", "node-a", nil),
		plannerStep("step-b", "node-b", nil),
		plannerStep("step-join", "node-join", []string{"node-a", "node-b"}),
	}

	jobs, err := NewBuildExecutionPlanner().Plan(domain.Build{ID: "build-1"}, steps, "alpine:3")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertWorkspaceInput(t, jobs[2], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeFanIn})
}

func TestBuildExecutionPlanner_PlanWorkspaceInputs_UsesLoaderSequentialDependencies(t *testing.T) {
	resolved, err := pipeline.LoadAndResolve([]byte(`
version: 1
steps:
  - name: generate
    run: go generate ./...
  - name: test
    run: go test ./...
  - name: package
    run: go build ./...
`))
	if err != nil {
		t.Fatalf("load pipeline: %v", err)
	}

	steps := pipelineStepsToDomain("build-1", resolved.Steps)
	if len(steps) != 3 {
		t.Fatalf("expected three domain steps, got %d", len(steps))
	}
	if len(steps[0].DependsOnNodes) != 0 || len(steps[1].DependsOnNodes) != 1 || steps[1].DependsOnNodes[0] != steps[0].NodeID || len(steps[2].DependsOnNodes) != 1 || steps[2].DependsOnNodes[0] != steps[1].NodeID {
		t.Fatalf("expected loader-derived sequential dependencies, got %#v", steps)
	}

	jobs, err := NewBuildExecutionPlanner().Plan(domain.Build{ID: "build-1"}, steps, "golang:1.24")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if jobs[0].NodeID != domain.FallbackNodeID(0) || jobs[1].NodeID != domain.FallbackNodeID(1) || jobs[2].NodeID != domain.FallbackNodeID(2) {
		t.Fatalf("expected planned fallback nodes, got %q %q %q", jobs[0].NodeID, jobs[1].NodeID, jobs[2].NodeID)
	}
	assertWorkspaceInput(t, jobs[0], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource})
	assertWorkspaceInput(t, jobs[1], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: steps[0].NodeID})
	assertWorkspaceInput(t, jobs[2], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: steps[1].NodeID})
}

func TestBuildExecutionPlanner_PlanWorkspaceInputs_SequentialWhenNodeIDsMissing(t *testing.T) {
	steps := []domain.BuildStep{
		{ID: "step-0", StepIndex: 0, Name: "step-0", Command: "sh", Args: []string{"-c", "true"}, Env: map[string]string{}, WorkingDir: "."},
		{ID: "step-1", StepIndex: 1, Name: "step-1", Command: "sh", Args: []string{"-c", "true"}, Env: map[string]string{}, WorkingDir: "."},
	}
	jobs, err := NewBuildExecutionPlanner().Plan(domain.Build{ID: "build-1"}, steps, "alpine:3")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertWorkspaceInput(t, jobs[0], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource})
	assertWorkspaceInput(t, jobs[1], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: domain.FallbackNodeID(0)})
}

func TestBuildExecutionPlanner_PlanWorkspaceInputs_UsesFallbackNodesForManualSequentialSteps(t *testing.T) {
	steps := []domain.BuildStep{
		plannerStep("step-setup", "", nil),
		plannerStep("step-test", "", nil),
		plannerStep("step-package", "", nil),
	}
	for index := range steps {
		steps[index].StepIndex = index
	}

	jobs, err := NewBuildExecutionPlanner().Plan(domain.Build{ID: "build-1"}, steps, "golang:1.24")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	assertWorkspaceInput(t, jobs[0], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeSource})
	assertWorkspaceInput(t, jobs[1], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: domain.FallbackNodeID(0)})
	assertWorkspaceInput(t, jobs[2], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: domain.FallbackNodeID(1)})
}

func TestNearestCommonAncestor_UsesGraphDepthInsteadOfStepIndex(t *testing.T) {
	dependenciesByNodeID := map[string][]string{
		"root":      nil,
		"deep":      {"root"},
		"left":      {"deep"},
		"right":     {"deep"},
		"late-root": nil,
		"left-tip":  {"left", "late-root"},
		"right-tip": {"right", "late-root"},
	}

	if got := nearestCommonAncestor([]string{"left-tip", "right-tip"}, dependenciesByNodeID); got != "deep" {
		t.Fatalf("expected nearest common ancestor by graph depth to be deep, got %q", got)
	}
}

func plannerStep(stepID string, nodeID string, dependencies []string) domain.BuildStep {
	return domain.BuildStep{
		ID:             stepID,
		NodeID:         nodeID,
		DependsOnNodes: append([]string(nil), dependencies...),
		Name:           stepID,
		Command:        "sh",
		Args:           []string{"-c", "true"},
		Env:            map[string]string{},
		WorkingDir:     ".",
	}
}

func assertWorkspaceInput(t *testing.T, job domain.ExecutionJob, want domain.WorkspaceInputPlan) {
	t.Helper()
	var spec domain.ExecutionJobSpec
	if err := json.Unmarshal([]byte(job.ResolvedSpecJSON), &spec); err != nil {
		t.Fatalf("unmarshal resolved spec: %v", err)
	}
	if spec.WorkspaceInput != want {
		t.Fatalf("workspace input for node %q: got %#v, want %#v", job.NodeID, spec.WorkspaceInput, want)
	}
}
