package execution

type stepFailureKind string

const (
	stepFailureKindNone     stepFailureKind = "none"
	stepFailureKindExitCode stepFailureKind = "exit_code"
	stepFailureKindTimeout  stepFailureKind = "timeout"
	stepFailureKindInternal stepFailureKind = "internal"
)
