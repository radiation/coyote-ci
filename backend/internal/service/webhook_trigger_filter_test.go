package service

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestShouldTriggerBuild(t *testing.T) {
	branchRef := domain.WebhookRef{RawRef: "refs/heads/main", RefType: domain.WebhookRefTypeBranch, RefName: "main"}
	tagRef := domain.WebhookRef{RawRef: "refs/tags/v1.2.3", RefType: domain.WebhookRefTypeTag, RefName: "v1.2.3"}

	t.Run("branch allowed", func(t *testing.T) {
		decision := shouldTriggerBuild(branchRef, WebhookJobTriggerConfig{
			Mode:            domain.JobTriggerModeBranches,
			BranchAllowlist: []string{"main", "develop"},
		})
		if !decision.Matched || decision.Reason != WebhookTriggerDecisionMatched {
			t.Fatalf("expected matched, got %+v", decision)
		}
	})

	t.Run("branch disallowed", func(t *testing.T) {
		decision := shouldTriggerBuild(branchRef, WebhookJobTriggerConfig{
			Mode:            domain.JobTriggerModeBranches,
			BranchAllowlist: []string{"develop"},
		})
		if decision.Matched || decision.Reason != WebhookTriggerDecisionFilteredBranch {
			t.Fatalf("expected filtered branch, got %+v", decision)
		}
	})

	t.Run("tags only mode ignores branch", func(t *testing.T) {
		decision := shouldTriggerBuild(branchRef, WebhookJobTriggerConfig{Mode: domain.JobTriggerModeTags})
		if decision.Matched || decision.Reason != WebhookTriggerDecisionFilteredByMode {
			t.Fatalf("expected filtered by mode, got %+v", decision)
		}
	})

	t.Run("tags enabled matches tag", func(t *testing.T) {
		decision := shouldTriggerBuild(tagRef, WebhookJobTriggerConfig{Mode: domain.JobTriggerModeTags})
		if !decision.Matched || decision.Reason != WebhookTriggerDecisionMatched {
			t.Fatalf("expected matched tag, got %+v", decision)
		}
	})

	t.Run("tag allowlist supports prefix", func(t *testing.T) {
		decision := shouldTriggerBuild(tagRef, WebhookJobTriggerConfig{
			Mode:         domain.JobTriggerModeTags,
			TagAllowlist: []string{"release-*", "v*"},
		})
		if !decision.Matched {
			t.Fatalf("expected prefix allowlist match, got %+v", decision)
		}
	})

	t.Run("deleted refs are ignored", func(t *testing.T) {
		decision := shouldTriggerBuild(domain.WebhookRef{RefType: domain.WebhookRefTypeBranch, RefName: "main", Deleted: true}, WebhookJobTriggerConfig{Mode: domain.JobTriggerModeBranches})
		if decision.Matched || decision.Reason != WebhookTriggerDecisionDeletedRef {
			t.Fatalf("expected deleted_ref decision, got %+v", decision)
		}
	})

	t.Run("unknown refs are ignored", func(t *testing.T) {
		decision := shouldTriggerBuild(domain.WebhookRef{RefType: domain.WebhookRefTypeUnknown, RefName: "custom"}, WebhookJobTriggerConfig{Mode: domain.JobTriggerModeBranchesAndTags})
		if decision.Matched || decision.Reason != WebhookTriggerDecisionUnsupportedRefType {
			t.Fatalf("expected unsupported_ref_type decision, got %+v", decision)
		}
	})
}
