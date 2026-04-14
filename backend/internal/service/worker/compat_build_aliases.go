package worker

import buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"

type StepCompletionReport = buildsvc.StepCompletionReport
type CreateBuildInput = buildsvc.CreateBuildInput
type CreateBuildStepInput = buildsvc.CreateBuildStepInput

var NewBuildService = buildsvc.NewBuildService

var ErrStaleStepClaim = buildsvc.ErrStaleStepClaim
var ErrBuildNotFound = buildsvc.ErrBuildNotFound
var ErrInvalidBuildStatusTransition = buildsvc.ErrInvalidBuildStatusTransition
