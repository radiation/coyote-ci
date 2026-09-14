package domain

import "time"

// ExecutionTiming captures provider-neutral timing facts for one execution job.
// A nil timestamp means the provider did not expose that phase boundary.
type ExecutionTiming struct {
	Phases []ExecutionPhaseTiming `json:"phases"`
}

type ExecutionPhaseTiming struct {
	Name       string     `json:"name"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}
