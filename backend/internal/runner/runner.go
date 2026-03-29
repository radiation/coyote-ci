package runner

import (
	"context"
	"time"
)

type StepOutputStream string

const (
	StepOutputStreamStdout StepOutputStream = "stdout"
	StepOutputStreamStderr StepOutputStream = "stderr"
	StepOutputStreamSystem StepOutputStream = "system"
)

type StepOutputChunk struct {
	Stream    StepOutputStream
	ChunkText string
	EmittedAt time.Time
}

type StepOutputCallback func(chunk StepOutputChunk) error

// RunStepRequest describes the command a runner should execute for a build step.
type RunStepRequest struct {
	BuildID        string
	StepID         string
	StepIndex      int
	StepName       string
	WorkerID       string
	ClaimToken     string
	Command        string
	Args           []string
	Env            map[string]string
	WorkingDir     string
	TimeoutSeconds int
}

// PrepareBuildRequest describes environment inputs needed before step execution.
type PrepareBuildRequest struct {
	BuildID    string
	RepoURL    string
	Ref        string
	CommitSHA  string
	Image      string
	WorkerID   string
	ClaimToken string
}

// RunStepStatus indicates the outcome of a step execution.
type RunStepStatus string

const (
	RunStepStatusSuccess RunStepStatus = "success"
	RunStepStatusFailed  RunStepStatus = "failed"
)

// RunStepResult is the outcome returned by a runner after executing a step.
type RunStepResult struct {
	Status     RunStepStatus
	ExitCode   int
	Stdout     string
	Stderr     string
	StartedAt  time.Time
	FinishedAt time.Time
}

type Runner interface {
	RunStep(ctx context.Context, request RunStepRequest) (RunStepResult, error)
}

// StreamingRunner emits output incrementally while a step runs.
type StreamingRunner interface {
	RunStepStream(ctx context.Context, request RunStepRequest, onOutput StepOutputCallback) (RunStepResult, error)
}

// BuildScopedRunner can prepare and cleanup per-build execution environments.
type BuildScopedRunner interface {
	StreamingRunner
	PrepareBuild(ctx context.Context, request PrepareBuildRequest) error
	CleanupBuild(ctx context.Context, buildID string) error
}
