package execution

import (
	"context"
	"errors"
	"testing"
)

func TestControllerFunc_ReconcileForwardsContextAndError(t *testing.T) {
	type contextKey string
	const key contextKey = "controller"
	wantErr := errors.New("reconcile failed")
	ctx := context.WithValue(context.Background(), key, "value")

	controller := ControllerFunc(func(gotCtx context.Context) error {
		if got := gotCtx.Value(key); got != "value" {
			t.Fatalf("context value = %v, want value", got)
		}
		return wantErr
	})

	if err := controller.Reconcile(ctx); !errors.Is(err, wantErr) {
		t.Fatalf("reconcile error = %v, want %v", err, wantErr)
	}
}
