package webhook

import (
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type WebhookTriggerDecisionReason string

const (
	WebhookTriggerDecisionMatched            WebhookTriggerDecisionReason = "matched"
	WebhookTriggerDecisionDeletedRef         WebhookTriggerDecisionReason = "deleted_ref"
	WebhookTriggerDecisionUnsupportedRefType WebhookTriggerDecisionReason = "unsupported_ref_type"
	WebhookTriggerDecisionFilteredBranch     WebhookTriggerDecisionReason = "filtered_branch"
	WebhookTriggerDecisionFilteredTag        WebhookTriggerDecisionReason = "filtered_tag"
	WebhookTriggerDecisionFilteredByMode     WebhookTriggerDecisionReason = "filtered_trigger_mode"
)

type WebhookJobTriggerConfig struct {
	Mode            domain.JobTriggerMode
	BranchAllowlist []string
	TagAllowlist    []string
}

type WebhookTriggerDecision struct {
	Matched bool
	Reason  WebhookTriggerDecisionReason
}

func shouldTriggerBuild(ref domain.WebhookRef, config WebhookJobTriggerConfig) WebhookTriggerDecision {
	if ref.Deleted {
		return WebhookTriggerDecision{Matched: false, Reason: WebhookTriggerDecisionDeletedRef}
	}

	mode := normalizeJobTriggerMode(config.Mode)
	if ref.RefType == domain.WebhookRefTypeUnknown {
		return WebhookTriggerDecision{Matched: false, Reason: WebhookTriggerDecisionUnsupportedRefType}
	}

	switch ref.RefType {
	case domain.WebhookRefTypeBranch:
		if mode == domain.JobTriggerModeTags {
			return WebhookTriggerDecision{Matched: false, Reason: WebhookTriggerDecisionFilteredByMode}
		}
		if !matchesBranchAllowlist(ref.RefName, config.BranchAllowlist) {
			return WebhookTriggerDecision{Matched: false, Reason: WebhookTriggerDecisionFilteredBranch}
		}
		return WebhookTriggerDecision{Matched: true, Reason: WebhookTriggerDecisionMatched}
	case domain.WebhookRefTypeTag:
		if mode == domain.JobTriggerModeBranches {
			return WebhookTriggerDecision{Matched: false, Reason: WebhookTriggerDecisionFilteredByMode}
		}
		if !matchesTagAllowlist(ref.RefName, config.TagAllowlist) {
			return WebhookTriggerDecision{Matched: false, Reason: WebhookTriggerDecisionFilteredTag}
		}
		return WebhookTriggerDecision{Matched: true, Reason: WebhookTriggerDecisionMatched}
	default:
		return WebhookTriggerDecision{Matched: false, Reason: WebhookTriggerDecisionUnsupportedRefType}
	}
}

func ShouldTriggerBuild(ref domain.WebhookRef, config WebhookJobTriggerConfig) WebhookTriggerDecision {
	return shouldTriggerBuild(ref, config)
}

func normalizeJobTriggerMode(mode domain.JobTriggerMode) domain.JobTriggerMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(domain.JobTriggerModeTags):
		return domain.JobTriggerModeTags
	case string(domain.JobTriggerModeBranchesAndTags):
		return domain.JobTriggerModeBranchesAndTags
	default:
		return domain.JobTriggerModeBranches
	}
}

func NormalizeJobTriggerMode(mode domain.JobTriggerMode) domain.JobTriggerMode {
	return normalizeJobTriggerMode(mode)
}

func matchesBranchAllowlist(refName string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	for _, item := range allowlist {
		if strings.TrimSpace(item) == refName {
			return true
		}
	}
	return false
}

func matchesTagAllowlist(refName string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	for _, item := range allowlist {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(trimmed, "*") {
			prefix := strings.TrimSuffix(trimmed, "*")
			if strings.HasPrefix(refName, prefix) {
				return true
			}
			continue
		}
		if trimmed == refName {
			return true
		}
	}
	return false
}
