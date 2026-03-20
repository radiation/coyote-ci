package runner

import (
	"context"

	"github.com/radiation/coyote-ci/backend/pkg/contracts"
)

type Runner interface {
	RunStep(ctx context.Context, request contracts.RunStepRequest) (contracts.RunStepResult, error)
}
