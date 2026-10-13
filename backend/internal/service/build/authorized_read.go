package build

import (
	"context"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
)

const (
	DefaultStepLogLimit    = 100
	MaxStepLogLimit        = 500
	DefaultStepLogMaxBytes = 64 * 1024
	MaxStepLogMaxBytes     = 256 * 1024
)

var ErrStepLogIndexInvalid = errors.New("step index must be non-negative")
var ErrStepLogCursorInvalid = errors.New("after sequence must be non-negative")
var ErrStepLogLimitInvalid = errors.New("step log limit must be between 1 and 500")
var ErrStepLogMaxBytesInvalid = errors.New("step log max bytes must be between 1 and 262144")

type StepLogChunkTooLargeError struct {
	MinimumMaxBytes int
}

func (e *StepLogChunkTooLargeError) Error() string {
	return "step log chunk exceeds the maximum byte budget"
}

type AuthorizedReader struct {
	builds     *BuildService
	authorizer authorization.Authorizer
}

type BuildFailures struct {
	Build       domain.Build
	FailedSteps []domain.BuildStep
}

type StepLogOptions struct {
	AfterSequence int64
	Limit         int
	MaxBytes      int
}

type StepLogs struct {
	Build        domain.Build
	Chunks       []logs.StepLogChunk
	NextSequence int64
	HasMore      bool
}

func NewAuthorizedReader(builds *BuildService, authorizer authorization.Authorizer) *AuthorizedReader {
	return &AuthorizedReader{builds: builds, authorizer: authorizer}
}

func (r *AuthorizedReader) GetBuild(ctx context.Context, principal authorization.Principal, buildID string) (domain.Build, error) {
	return r.authorizeBuild(ctx, principal, buildID, domain.APITokenScopeBuildRead)
}

func (r *AuthorizedReader) GetBuildFailures(ctx context.Context, principal authorization.Principal, buildID string) (BuildFailures, error) {
	build, err := r.authorizeBuild(ctx, principal, buildID, domain.APITokenScopeBuildRead)
	if err != nil {
		return BuildFailures{}, err
	}
	steps, err := r.builds.GetBuildSteps(ctx, build.ID)
	if err != nil {
		return BuildFailures{}, err
	}

	failedSteps := make([]domain.BuildStep, 0)
	for _, step := range steps {
		if step.Status == domain.BuildStepStatusFailed {
			failedSteps = append(failedSteps, step)
		}
	}
	return BuildFailures{Build: build, FailedSteps: failedSteps}, nil
}

func (r *AuthorizedReader) GetStepLogs(ctx context.Context, principal authorization.Principal, buildID string, stepIndex int, options StepLogOptions) (StepLogs, error) {
	if stepIndex < 0 {
		return StepLogs{}, ErrStepLogIndexInvalid
	}
	normalized, err := normalizeStepLogOptions(options)
	if err != nil {
		return StepLogs{}, err
	}
	build, err := r.authorizeBuild(ctx, principal, buildID, domain.APITokenScopeBuildRead, domain.APITokenScopeBuildLogs)
	if err != nil {
		return StepLogs{}, err
	}
	steps, err := r.builds.GetBuildSteps(ctx, build.ID)
	if err != nil {
		return StepLogs{}, err
	}
	if !containsStepIndex(steps, stepIndex) {
		return StepLogs{}, ErrBuildStepNotFound
	}

	chunks, err := r.builds.GetStepLogChunks(ctx, build.ID, stepIndex, normalized.AfterSequence, normalized.Limit+1)
	if err != nil {
		return StepLogs{}, err
	}

	result := StepLogs{
		Build:        build,
		Chunks:       make([]logs.StepLogChunk, 0, min(len(chunks), normalized.Limit)),
		NextSequence: normalized.AfterSequence,
	}
	responseBytes := 0
	for _, chunk := range chunks {
		if len(result.Chunks) >= normalized.Limit {
			result.HasMore = true
			break
		}
		if len(chunk.ChunkText)+responseBytes > normalized.MaxBytes {
			if len(result.Chunks) == 0 {
				return StepLogs{}, &StepLogChunkTooLargeError{MinimumMaxBytes: len(chunk.ChunkText)}
			}
			result.HasMore = true
			break
		}
		result.Chunks = append(result.Chunks, chunk)
		responseBytes += len(chunk.ChunkText)
		result.NextSequence = chunk.SequenceNo
	}
	if len(chunks) > len(result.Chunks) {
		result.HasMore = true
	}
	return result, nil
}

func (r *AuthorizedReader) authorizeBuild(ctx context.Context, principal authorization.Principal, buildID string, scopes ...domain.APITokenScope) (domain.Build, error) {
	build, err := r.builds.GetBuild(ctx, strings.TrimSpace(buildID))
	if err != nil {
		return domain.Build{}, err
	}
	if err := r.authorizer.RequireProjectRole(ctx, principal, build.ProjectID, domain.ProjectMemberRoleViewer); err != nil {
		return domain.Build{}, err
	}
	for _, scope := range scopes {
		if err := r.authorizer.RequireAPITokenScope(principal, scope); err != nil {
			return domain.Build{}, err
		}
	}
	return build, nil
}

func normalizeStepLogOptions(options StepLogOptions) (StepLogOptions, error) {
	if options.AfterSequence < 0 {
		return StepLogOptions{}, ErrStepLogCursorInvalid
	}
	if options.Limit == 0 {
		options.Limit = DefaultStepLogLimit
	}
	if options.Limit < 1 || options.Limit > MaxStepLogLimit {
		return StepLogOptions{}, ErrStepLogLimitInvalid
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = DefaultStepLogMaxBytes
	}
	if options.MaxBytes < 1 || options.MaxBytes > MaxStepLogMaxBytes {
		return StepLogOptions{}, ErrStepLogMaxBytesInvalid
	}
	return options, nil
}

func containsStepIndex(steps []domain.BuildStep, stepIndex int) bool {
	for _, step := range steps {
		if step.StepIndex == stepIndex {
			return true
		}
	}
	return false
}

func min(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
