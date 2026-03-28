package domain

// IsTerminalBuildStatus reports whether a build status cannot transition further.
func IsTerminalBuildStatus(status BuildStatus) bool {
	return status == BuildStatusSuccess || status == BuildStatusFailed
}

// IsTerminalStepStatus reports whether a step status cannot transition further.
func IsTerminalStepStatus(status BuildStepStatus) bool {
	return status == BuildStepStatusSuccess || status == BuildStepStatusFailed
}

// CanTransitionBuild reports whether a build lifecycle transition is legal.
func CanTransitionBuild(from, to BuildStatus) bool {
	switch from {
	case BuildStatusPending:
		return to == BuildStatusQueued
	case BuildStatusQueued:
		return to == BuildStatusRunning
	case BuildStatusRunning:
		return to == BuildStatusSuccess || to == BuildStatusFailed
	default:
		return false
	}
}

// CanTransitionStep reports whether a step lifecycle transition is legal.
func CanTransitionStep(from, to BuildStepStatus) bool {
	switch from {
	case BuildStepStatusPending:
		return to == BuildStepStatusRunning
	case BuildStepStatusRunning:
		return to == BuildStepStatusSuccess || to == BuildStepStatusFailed
	default:
		return false
	}
}
