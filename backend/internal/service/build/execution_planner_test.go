package build

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/pipeline"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
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

func TestBuildExecutionPlannerFanInTopologyUsesCommonAncestorAfterParallelBranches(t *testing.T) {
	steps := []domain.BuildStep{
		plannerStep("root", "root", nil),
		plannerStep("branch-a", "branch-a", []string{"root"}),
		plannerStep("branch-b", "branch-b", []string{"root"}),
		plannerStep("join", "join", []string{"branch-a", "branch-b"}),
	}
	jobs, planErr := NewBuildExecutionPlanner().Plan(domain.Build{ID: "build-fan-in"}, steps, "alpine:3")
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}
	assertWorkspaceInput(t, jobs[3], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeFanIn, CommonAncestorNodeID: "root"})

	executionJobs := memoryrepo.NewExecutionJobRepository()
	if _, createErr := executionJobs.CreateJobsForBuild(context.Background(), jobs); createErr != nil {
		t.Fatalf("create planned jobs: %v", createErr)
	}
	now := time.Now().UTC()
	claim := func(workerID string, token string) domain.ExecutionJob {
		job, found, claimErr := executionJobs.ClaimNextRunnableJob(context.Background(), repository.StepClaim{WorkerID: workerID, ClaimToken: token, ClaimedAt: now, LeaseExpiresAt: now.Add(time.Minute)})
		if claimErr != nil || !found {
			t.Fatalf("claim %s: found=%t err=%v", workerID, found, claimErr)
		}
		return job
	}
	complete := func(job domain.ExecutionJob, token string) {
		_, outcome, completeErr := executionJobs.CompleteJobSuccess(context.Background(), job.ID, token, now.Add(time.Minute), 0, nil)
		if completeErr != nil || outcome != repository.StepCompletionCompleted {
			t.Fatalf("complete %s: outcome=%q err=%v", job.NodeID, outcome, completeErr)
		}
	}

	root := claim("worker-root", "root-token")
	complete(root, "root-token")
	branchA := claim("worker-a", "branch-a-token")
	branchB := claim("worker-b", "branch-b-token")
	if map[string]bool{branchA.NodeID: true, branchB.NodeID: true}["branch-a"] == false || map[string]bool{branchA.NodeID: true, branchB.NodeID: true}["branch-b"] == false {
		t.Fatalf("expected independent branch claims, got %q and %q", branchA.NodeID, branchB.NodeID)
	}
	complete(branchA, "branch-a-token")
	if _, found, claimErr := executionJobs.ClaimNextRunnableJob(context.Background(), repository.StepClaim{WorkerID: "worker-join", ClaimToken: "join-token", ClaimedAt: now, LeaseExpiresAt: now.Add(time.Minute)}); claimErr != nil || found {
		t.Fatalf("join claimed before both branches completed: found=%t err=%v", found, claimErr)
	}
	complete(branchB, "branch-b-token")
	join := claim("worker-join", "join-token")
	if join.NodeID != "join" {
		t.Fatalf("expected join claim, got %q", join.NodeID)
	}
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

func TestBuildExecutionPlanner_PlanWorkspaceInputs_UsesExplicitPipelineDependencies(t *testing.T) {
	resolved, err := pipeline.LoadAndResolve([]byte(`
version: 1
steps:
  - name: setup
    depends_on: []
    run: true
  - group:
      name: checks
      steps:
        - name: backend
          depends_on: [setup]
          run: true
        - name: frontend
          depends_on: [setup]
          run: true
  - name: package
    depends_on: [backend, frontend]
    run: true
`))
	if err != nil {
		t.Fatalf("load pipeline: %v", err)
	}

	steps := pipelineStepsToDomain("build-1", resolved.Steps)
	jobs, planErr := NewBuildExecutionPlanner().Plan(domain.Build{ID: "build-1"}, steps, "golang:1.24")
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}
	if got := jobs[1].DependsOnNodeIDs; len(got) != 1 || got[0] != jobs[0].NodeID {
		t.Fatalf("backend job dependencies=%#v, want setup node %q", got, jobs[0].NodeID)
	}
	assertWorkspaceInput(t, jobs[1], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModePredecessor, ProducerNodeID: jobs[0].NodeID, IsolatedWritableDescendant: true})
	if got := jobs[3].DependsOnNodeIDs; len(got) != 2 || got[0] != jobs[1].NodeID || got[1] != jobs[2].NodeID {
		t.Fatalf("package job dependencies=%#v, want backend and frontend nodes", got)
	}
	assertWorkspaceInput(t, jobs[3], domain.WorkspaceInputPlan{Mode: domain.WorkspaceInputModeFanIn, CommonAncestorNodeID: jobs[0].NodeID})

	executionJobs := memoryrepo.NewExecutionJobRepository()
	if _, createErr := executionJobs.CreateJobsForBuild(context.Background(), jobs); createErr != nil {
		t.Fatalf("create planned jobs: %v", createErr)
	}
	now := time.Now().UTC()
	claim := func(workerID string, token string) domain.ExecutionJob {
		job, found, claimErr := executionJobs.ClaimNextRunnableJob(context.Background(), repository.StepClaim{WorkerID: workerID, ClaimToken: token, ClaimedAt: now, LeaseExpiresAt: now.Add(time.Minute)})
		if claimErr != nil || !found {
			t.Fatalf("claim %s: found=%t err=%v", workerID, found, claimErr)
		}
		return job
	}
	complete := func(job domain.ExecutionJob, token string) {
		_, outcome, completeErr := executionJobs.CompleteJobSuccess(context.Background(), job.ID, token, now.Add(time.Minute), 0, nil)
		if completeErr != nil || outcome != repository.StepCompletionCompleted {
			t.Fatalf("complete %s: outcome=%q err=%v", job.NodeID, outcome, completeErr)
		}
	}

	setup := claim("worker-setup", "setup-token")
	if setup.NodeID != jobs[0].NodeID {
		t.Fatalf("first claim node=%q, want explicit root %q", setup.NodeID, jobs[0].NodeID)
	}
	complete(setup, "setup-token")
	backend := claim("worker-backend", "backend-token")
	frontend := claim("worker-frontend", "frontend-token")
	complete(backend, "backend-token")
	if _, found, claimErr := executionJobs.ClaimNextRunnableJob(context.Background(), repository.StepClaim{WorkerID: "worker-package", ClaimToken: "package-token", ClaimedAt: now, LeaseExpiresAt: now.Add(time.Minute)}); claimErr != nil || found {
		t.Fatalf("package claimed before both explicit dependencies completed: found=%t err=%v", found, claimErr)
	}
	complete(frontend, "frontend-token")
	packageJob := claim("worker-package", "package-token")
	if packageJob.NodeID != jobs[3].NodeID {
		t.Fatalf("package claim node=%q, want %q", packageJob.NodeID, jobs[3].NodeID)
	}
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
