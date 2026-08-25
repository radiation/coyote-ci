package execution

import "context"

// Controller reconciles execution work without requiring the caller to wait
// for a particular workload to finish. Backends may execute synchronously or
// submit and later reconcile external workloads.
type Controller interface {
	Reconcile(ctx context.Context) error
}

// ControllerFunc adapts a function to a Controller.
type ControllerFunc func(context.Context) error

func (fn ControllerFunc) Reconcile(ctx context.Context) error {
	return fn(ctx)
}
