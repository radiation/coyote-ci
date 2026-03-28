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

func TestCompletionAndAdvancementScenarios(t *testing.T) {
	tests := []struct {
		name          string
		currentStatus domain.BuildStepStatus
		targetStatus  domain.BuildStepStatus
		claimRequired bool
		claimMatches  bool
		hasNext       bool
		wantOutcome   StepCompletionOutcome
		wantAllow     bool
		wantFailBuild bool
		wantSucceed   bool
		wantAdvance   bool
	}{
		{
			name:          "applied success intermediate",
			currentStatus: domain.BuildStepStatusRunning,
			targetStatus:  domain.BuildStepStatusSuccess,
			hasNext:       true,
			wantOutcome:   StepCompletionCompleted,
			wantAllow:     true,
			wantAdvance:   true,
		},
		{
			name:          "applied success final",
			currentStatus: domain.BuildStepStatusRunning,
			targetStatus:  domain.BuildStepStatusSuccess,
			hasNext:       false,
			wantOutcome:   StepCompletionCompleted,
			wantAllow:     true,
			wantSucceed:   true,
		},
		{
			name:          "applied failure",
			currentStatus: domain.BuildStepStatusRunning,
			targetStatus:  domain.BuildStepStatusFailed,
			hasNext:       true,
			wantOutcome:   StepCompletionCompleted,
			wantAllow:     true,
			wantFailBuild: true,
		},
		{
			name:          "stale claim",
			currentStatus: domain.BuildStepStatusRunning,
			targetStatus:  domain.BuildStepStatusSuccess,
			claimRequired: true,
			claimMatches:  false,
			wantOutcome:   StepCompletionStaleClaim,
		},
		{
			name:          "duplicate terminal",
			currentStatus: domain.BuildStepStatusSuccess,
			targetStatus:  domain.BuildStepStatusSuccess,
			wantOutcome:   StepCompletionDuplicateTerminal,
		},
		{
			name:          "invalid transition",
			currentStatus: domain.BuildStepStatusPending,
			targetStatus:  domain.BuildStepStatusSuccess,
			wantOutcome:   StepCompletionInvalidTransition,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			decision := DecideStepCompletion(tc.currentStatus, tc.targetStatus, tc.claimRequired, tc.claimMatches)
			if decision.Outcome != tc.wantOutcome {
				t.Fatalf("expected outcome %q, got %q", tc.wantOutcome, decision.Outcome)
			}
			if decision.AllowUpdate != tc.wantAllow {
				t.Fatalf("expected allow=%v, got %v", tc.wantAllow, decision.AllowUpdate)
			}

			if !tc.wantAllow {
				return
			}

			advance := DecideBuildAdvancement(tc.targetStatus, 0, tc.hasNext)
			if advance.FailBuild != tc.wantFailBuild {
				t.Fatalf("expected fail-build=%v, got %v", tc.wantFailBuild, advance.FailBuild)
			}
			if advance.SucceedBuild != tc.wantSucceed {
				t.Fatalf("expected succeed-build=%v, got %v", tc.wantSucceed, advance.SucceedBuild)
			}
			if advance.AdvanceCurrentStepIndex != tc.wantAdvance {
				t.Fatalf("expected advance-index=%v, got %v", tc.wantAdvance, advance.AdvanceCurrentStepIndex)
			}
		})
	}
}
