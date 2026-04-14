package webhook

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestShouldTriggerBuild(t *testing.T) {
	branchRef := domain.WebhookRef{RawRef: "refs/heads/main", RefType: domain.WebhookRefTypeBranch, RefName: "main"}
	tagRef := domain.WebhookRef{RawRef: "refs/tags/v1.2.3", RefType: domain.WebhookRefTypeTag, RefName: "v1.2.3"}

	t.Run("branch allowed", func(t *testing.T) {
		decision := webhookFilterShouldTriggerBuild(branchRef, WebhookFilterConfig{
			Mode:            domain.JobTriggerModeBranches,
			BranchAllowlist: []string{"main", "develop"},
		})
		if !decision.Matched || decision.Reason != WebhookFilterDecisionMatched {
			t.Fatalf("expected matched, got %+v", decision)
		}
	})

	t.Run("branch disallowed", func(t *testing.T) {
		decision := webhookFilterShouldTriggerBuild(branchRef, WebhookFilterConfig{
			Mode:            domain.JobTriggerModeBranches,
			BranchAllowlist: []string{"develop"},
		})
		if decision.Matched || decision.Reason != WebhookFilterDecisionFilteredBranch {
			t.Fatalf("expected filtered branch, got %+v", decision)
		}
	})

	t.Run("tags only mode ignores branch", func(t *testing.T) {
		decision := webhookFilterShouldTriggerBuild(branchRef, WebhookFilterConfig{Mode: domain.JobTriggerModeTags})
		if decision.Matched || decision.Reason != WebhookFilterDecisionFilteredByMode {
			t.Fatalf("expected filtered by mode, got %+v", decision)
		}
	})

	t.Run("tags enabled matches tag", func(t *testing.T) {
		decision := webhookFilterShouldTriggerBuild(tagRef, WebhookFilterConfig{Mode: domain.JobTriggerModeTags})
		if !decision.Matched || decision.Reason != WebhookFilterDecisionMatched {
			t.Fatalf("expected matched tag, got %+v", decision)
		}
	})

	t.Run("tag allowlist supports prefix", func(t *testing.T) {
		decision := webhookFilterShouldTriggerBuild(tagRef, WebhookFilterConfig{
			Mode:         domain.JobTriggerModeTags,
			TagAllowlist: []string{"release-*", "v*"},
		})
		if !decision.Matched {
			t.Fatalf("expected prefix allowlist match, got %+v", decision)
		}
	})

	t.Run("deleted refs are ignored", func(t *testing.T) {
		decision := webhookFilterShouldTriggerBuild(domain.WebhookRef{RefType: domain.WebhookRefTypeBranch, RefName: "main", Deleted: true}, WebhookFilterConfig{Mode: domain.JobTriggerModeBranches})
		if decision.Matched || decision.Reason != WebhookFilterDecisionDeletedRef {
			t.Fatalf("expected deleted_ref decision, got %+v", decision)
		}
	})

	t.Run("unknown refs are ignored", func(t *testing.T) {
		decision := webhookFilterShouldTriggerBuild(domain.WebhookRef{RefType: domain.WebhookRefTypeUnknown, RefName: "custom"}, WebhookFilterConfig{Mode: domain.JobTriggerModeBranchesAndTags})
		if decision.Matched || decision.Reason != WebhookFilterDecisionUnsupportedRefType {
			t.Fatalf("expected unsupported_ref_type decision, got %+v", decision)
		}
	})
}
