package repository

import "github.com/radiation/coyote-ci/backend/internal/domain"

// StepCompletionDecision describes whether a completion request may mutate state.
type StepCompletionDecision struct {
	Outcome     StepCompletionOutcome
	AllowUpdate bool
}

// BuildAdvancementDecision describes how build state should move after a step completes.
type BuildAdvancementDecision struct {
	FailBuild               bool
	SucceedBuild            bool
	AdvanceCurrentStepIndex bool
	NextStepIndex           int
}

// DecideStepCompletion evaluates a completion attempt against current step state.
func DecideStepCompletion(currentStatus, targetStatus domain.BuildStepStatus, claimRequired bool, claimMatches bool) StepCompletionDecision {
	if !domain.CanTransitionStep(domain.BuildStepStatusRunning, targetStatus) {
		return StepCompletionDecision{Outcome: StepCompletionInvalidTransition, AllowUpdate: false}
	}

	if currentStatus != domain.BuildStepStatusRunning {
		if domain.IsTerminalStepStatus(currentStatus) {
			return StepCompletionDecision{Outcome: StepCompletionDuplicateTerminal, AllowUpdate: false}
		}
		return StepCompletionDecision{Outcome: StepCompletionInvalidTransition, AllowUpdate: false}
	}

	if claimRequired && !claimMatches {
		return StepCompletionDecision{Outcome: StepCompletionStaleClaim, AllowUpdate: false}
	}

	return StepCompletionDecision{Outcome: StepCompletionCompleted, AllowUpdate: true}
}

// ClassifyCompletionConflict resolves a post-CAS mismatch outcome from persisted status.
func ClassifyCompletionConflict(currentStatus domain.BuildStepStatus, claimRequired bool) StepCompletionOutcome {
	if domain.IsTerminalStepStatus(currentStatus) {
		return StepCompletionDuplicateTerminal
	}
	if claimRequired && currentStatus == domain.BuildStepStatusRunning {
		return StepCompletionStaleClaim
	}
	return StepCompletionInvalidTransition
}

// DecideBuildAdvancement determines how build state should change after step completion.
func DecideBuildAdvancement(completedStepStatus domain.BuildStepStatus, completedStepIndex int, hasNext bool) BuildAdvancementDecision {
	next := completedStepIndex + 1
	if completedStepStatus == domain.BuildStepStatusFailed {
		return BuildAdvancementDecision{FailBuild: true}
	}

	if completedStepStatus == domain.BuildStepStatusSuccess {
		if hasNext {
			return BuildAdvancementDecision{AdvanceCurrentStepIndex: true, NextStepIndex: next}
		}
		return BuildAdvancementDecision{SucceedBuild: true, NextStepIndex: next}
	}

	return BuildAdvancementDecision{}
}
