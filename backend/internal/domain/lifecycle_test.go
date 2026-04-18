package domain

import "testing"

func TestCanTransitionBuild_Valid(t *testing.T) {
	tests := []struct {
		name string
		from BuildStatus
		to   BuildStatus
	}{
		{name: "pending to queued", from: BuildStatusPending, to: BuildStatusQueued},
		{name: "queued to preparing", from: BuildStatusQueued, to: BuildStatusPreparing},
		{name: "preparing to running", from: BuildStatusPreparing, to: BuildStatusRunning},
		{name: "preparing to failed", from: BuildStatusPreparing, to: BuildStatusFailed},
		{name: "running to success", from: BuildStatusRunning, to: BuildStatusSuccess},
		{name: "running to failed", from: BuildStatusRunning, to: BuildStatusFailed},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !CanTransitionBuild(tc.from, tc.to) {
				t.Fatalf("expected transition %q -> %q to be valid", tc.from, tc.to)
			}
		})
	}
}

func TestCanTransitionBuild_Invalid(t *testing.T) {
	tests := []struct {
		name string
		from BuildStatus
		to   BuildStatus
	}{
		{name: "pending to running", from: BuildStatusPending, to: BuildStatusRunning},
		{name: "pending to success", from: BuildStatusPending, to: BuildStatusSuccess},
		{name: "queued to running", from: BuildStatusQueued, to: BuildStatusRunning},
		{name: "queued to success", from: BuildStatusQueued, to: BuildStatusSuccess},
		{name: "preparing to success", from: BuildStatusPreparing, to: BuildStatusSuccess},
		{name: "success to failed", from: BuildStatusSuccess, to: BuildStatusFailed},
		{name: "failed to running", from: BuildStatusFailed, to: BuildStatusRunning},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if CanTransitionBuild(tc.from, tc.to) {
				t.Fatalf("expected transition %q -> %q to be invalid", tc.from, tc.to)
			}
		})
	}
}

func TestCanTransitionStep_Valid(t *testing.T) {
	tests := []struct {
		name string
		from BuildStepStatus
		to   BuildStepStatus
	}{
		{name: "pending to running", from: BuildStepStatusPending, to: BuildStepStatusRunning},
		{name: "running to success", from: BuildStepStatusRunning, to: BuildStepStatusSuccess},
		{name: "running to failed", from: BuildStepStatusRunning, to: BuildStepStatusFailed},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !CanTransitionStep(tc.from, tc.to) {
				t.Fatalf("expected transition %q -> %q to be valid", tc.from, tc.to)
			}
		})
	}
}

func TestCanTransitionStep_Invalid(t *testing.T) {
	tests := []struct {
		name string
		from BuildStepStatus
		to   BuildStepStatus
	}{
		{name: "pending to success", from: BuildStepStatusPending, to: BuildStepStatusSuccess},
		{name: "pending to failed", from: BuildStepStatusPending, to: BuildStepStatusFailed},
		{name: "running to pending", from: BuildStepStatusRunning, to: BuildStepStatusPending},
		{name: "success to failed", from: BuildStepStatusSuccess, to: BuildStepStatusFailed},
		{name: "failed to running", from: BuildStepStatusFailed, to: BuildStepStatusRunning},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if CanTransitionStep(tc.from, tc.to) {
				t.Fatalf("expected transition %q -> %q to be invalid", tc.from, tc.to)
			}
		})
	}
}

func TestTerminalBuildStatesRejectFurtherTransitions(t *testing.T) {
	terminalStates := []BuildStatus{BuildStatusSuccess, BuildStatusFailed}
	allTargets := []BuildStatus{BuildStatusPending, BuildStatusQueued, BuildStatusRunning, BuildStatusSuccess, BuildStatusFailed}

	for _, from := range terminalStates {
		if !IsTerminalBuildStatus(from) {
			t.Fatalf("expected %q to be terminal", from)
		}

		for _, to := range allTargets {
			if CanTransitionBuild(from, to) {
				t.Fatalf("expected terminal build state %q to reject transition to %q", from, to)
			}
		}
	}
}

func TestTerminalStepStatesRejectFurtherTransitions(t *testing.T) {
	terminalStates := []BuildStepStatus{BuildStepStatusSuccess, BuildStepStatusFailed}
	allTargets := []BuildStepStatus{BuildStepStatusPending, BuildStepStatusRunning, BuildStepStatusSuccess, BuildStepStatusFailed}

	for _, from := range terminalStates {
		if !IsTerminalStepStatus(from) {
			t.Fatalf("expected %q to be terminal", from)
		}

		for _, to := range allTargets {
			if CanTransitionStep(from, to) {
				t.Fatalf("expected terminal step state %q to reject transition to %q", from, to)
			}
		}
	}
}

func TestCanCancelStepToFailed(t *testing.T) {
	tests := []struct {
		name   string
		status BuildStepStatus
		want   bool
	}{
		{name: "pending is cancel-terminalizable", status: BuildStepStatusPending, want: true},
		{name: "running is cancel-terminalizable", status: BuildStepStatusRunning, want: true},
		{name: "success is already terminal", status: BuildStepStatusSuccess, want: false},
		{name: "failed is already terminal", status: BuildStepStatusFailed, want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CanCancelStepToFailed(tc.status); got != tc.want {
				t.Fatalf("expected CanCancelStepToFailed(%q)=%v, got %v", tc.status, tc.want, got)
			}
		})
	}
}
