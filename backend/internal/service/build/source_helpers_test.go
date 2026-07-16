package build

import (
	"errors"
	"fmt"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/source"
)

func TestClassifyBuildSourceFailureReason(t *testing.T) {
	sourceSpec := executionResolvedBuildSourceSpec("https://github.com/octo/repo.git", "refs/heads/main", "deadbeef")

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "missing repo from source package", err: source.ErrRepositoryURLRequired, want: "repository URL is required"},
		{name: "missing repo from build package", err: ErrRepoURLRequired, want: "repository URL is required"},
		{name: "clone failed", err: errors.New("repository clone failed: network down"), want: "source checkout failed: repository clone failed: network down"},
		{name: "wrapped clone failed", err: withBuildWrapped(source.ErrCloneFailed, "network down"), want: "repository clone failed: network down"},
		{name: "ref not found", err: source.ErrRefNotFound, want: "ref not found: refs/heads/main"},
		{name: "commit not found", err: source.ErrCommitNotFound, want: "commit not found: deadbeef"},
		{name: "missing checkout target from source package", err: source.ErrCheckoutTargetRequired, want: "ref or commit_sha is required"},
		{name: "missing checkout target from build package", err: ErrSourceTargetRequired, want: "ref or commit_sha is required"},
		{name: "checkout failed", err: withBuildWrapped(source.ErrCheckoutFailed, "detached head conflict"), want: "repository checkout failed: detached head conflict"},
		{name: "resolve commit failed", err: withBuildWrapped(source.ErrResolveCommitFailed, "rev-parse exploded"), want: "unable to resolve final commit SHA: rev-parse exploded"},
		{name: "resolver not configured", err: ErrSourceResolverNotConfigured, want: "source resolver not configured"},
		{name: "workspace root not configured", err: ErrExecutionWorkspaceRootNotConfigured, want: "execution workspace root not configured"},
		{name: "fallback", err: errors.New("permission denied"), want: "source checkout failed: permission denied"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyBuildSourceFailureReason(test.err, sourceSpec); got != test.want {
				t.Fatalf("unexpected reason: got %q want %q", got, test.want)
			}
		})
	}
}

func TestWithBuildSourceFailureDetail(t *testing.T) {
	tests := []struct {
		name string
		base string
		err  error
		want string
	}{
		{name: "empty detail", base: "repository clone failed", err: errors.New("  "), want: "repository clone failed"},
		{name: "same as base", base: "repository clone failed", err: errors.New("repository clone failed"), want: "repository clone failed"},
		{name: "already prefixed", base: "repository clone failed", err: errors.New("repository clone failed: timeout"), want: "repository clone failed: timeout"},
		{name: "new detail", base: "repository clone failed", err: errors.New("timeout"), want: "repository clone failed: timeout"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := withBuildSourceFailureDetail(test.base, test.err); got != test.want {
				t.Fatalf("unexpected detail: got %q want %q", got, test.want)
			}
		})
	}
}

func withBuildWrapped(base error, detail string) error {
	return fmt.Errorf("%w: %s", base, detail)
}
