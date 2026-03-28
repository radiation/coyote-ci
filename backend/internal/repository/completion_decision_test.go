package repository

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestDecideStepCompletion_StaleClaim(t *testing.T) {
	decision := DecideStepCompletion(domain.BuildStepStatusRunning, domain.BuildStepStatusSuccess, true, false)
	if decision.Outcome != StepCompletionStaleClaim {
		t.Fatalf("expected stale claim outcome, got %q", decision.Outcome)
	}
	if decision.AllowUpdate {
		t.Fatal("expected stale claim to reject update")
	}
}

func TestDecideStepCompletion_DuplicateTerminal(t *testing.T) {
	decision := DecideStepCompletion(domain.BuildStepStatusSuccess, domain.BuildStepStatusSuccess, false, true)
	if decision.Outcome != StepCompletionDuplicateTerminal {
		t.Fatalf("expected duplicate terminal outcome, got %q", decision.Outcome)
	}
	if decision.AllowUpdate {
		t.Fatal("expected duplicate terminal to reject update")
	}
}

func TestDecideStepCompletion_InvalidTransition(t *testing.T) {
	decision := DecideStepCompletion(domain.BuildStepStatusPending, domain.BuildStepStatusSuccess, false, true)
	if decision.Outcome != StepCompletionInvalidTransition {
		t.Fatalf("expected invalid transition outcome, got %q", decision.Outcome)
	}
	if decision.AllowUpdate {
		t.Fatal("expected invalid transition to reject update")
	}
}

func TestDecideBuildAdvancement_FailedStepBuildFails(t *testing.T) {
	decision := DecideBuildAdvancement(domain.BuildStepStatusFailed, 2, true)
	if !decision.FailBuild {
		t.Fatal("expected failed step to fail build")
	}
	if decision.SucceedBuild || decision.AdvanceCurrentStepIndex {
		t.Fatal("expected only fail-build action")
	}
}

func TestDecideBuildAdvancement_FinalSuccessBuildSucceeds(t *testing.T) {
	decision := DecideBuildAdvancement(domain.BuildStepStatusSuccess, 1, false)
	if !decision.SucceedBuild {
		t.Fatal("expected final success to succeed build")
	}
	if decision.NextStepIndex != 2 {
		t.Fatalf("expected next step index 2, got %d", decision.NextStepIndex)
	}
	if decision.FailBuild || decision.AdvanceCurrentStepIndex {
		t.Fatal("expected only succeed-build action")
	}
}

func TestDecideBuildAdvancement_IntermediateSuccessAdvances(t *testing.T) {
	decision := DecideBuildAdvancement(domain.BuildStepStatusSuccess, 0, true)
	if !decision.AdvanceCurrentStepIndex {
		t.Fatal("expected intermediate success to advance current step index")
	}
	if decision.NextStepIndex != 1 {
		t.Fatalf("expected next step index 1, got %d", decision.NextStepIndex)
	}
	if decision.FailBuild || decision.SucceedBuild {
		t.Fatal("expected only advance-step action")
	}
}
