package webhook

import (
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type WebhookFilterDecisionReason string

const (
	WebhookFilterDecisionMatched            WebhookFilterDecisionReason = "matched"
	WebhookFilterDecisionDeletedRef         WebhookFilterDecisionReason = "deleted_ref"
	WebhookFilterDecisionUnsupportedRefType WebhookFilterDecisionReason = "unsupported_ref_type"
	WebhookFilterDecisionFilteredBranch     WebhookFilterDecisionReason = "filtered_branch"
	WebhookFilterDecisionFilteredTag        WebhookFilterDecisionReason = "filtered_tag"
	WebhookFilterDecisionFilteredByMode     WebhookFilterDecisionReason = "filtered_trigger_mode"
)

type WebhookFilterConfig struct {
	Mode            domain.JobTriggerMode
	BranchAllowlist []string
	TagAllowlist    []string
}

type WebhookFilterDecision struct {
	Matched bool
	Reason  WebhookFilterDecisionReason
}

func webhookFilterShouldTriggerBuild(ref domain.WebhookRef, config WebhookFilterConfig) WebhookFilterDecision {
	if ref.Deleted {
		return WebhookFilterDecision{Matched: false, Reason: WebhookFilterDecisionDeletedRef}
	}

	mode := normalizeWebhookFilterMode(config.Mode)
	if ref.RefType == domain.WebhookRefTypeUnknown {
		return WebhookFilterDecision{Matched: false, Reason: WebhookFilterDecisionUnsupportedRefType}
	}

	switch ref.RefType {
	case domain.WebhookRefTypeBranch:
		if mode == domain.JobTriggerModeTags {
			return WebhookFilterDecision{Matched: false, Reason: WebhookFilterDecisionFilteredByMode}
		}
		if !matchesWebhookBranchAllowlist(ref.RefName, config.BranchAllowlist) {
			return WebhookFilterDecision{Matched: false, Reason: WebhookFilterDecisionFilteredBranch}
		}
		return WebhookFilterDecision{Matched: true, Reason: WebhookFilterDecisionMatched}
	case domain.WebhookRefTypeTag:
		if mode == domain.JobTriggerModeBranches {
			return WebhookFilterDecision{Matched: false, Reason: WebhookFilterDecisionFilteredByMode}
		}
		if !matchesWebhookTagAllowlist(ref.RefName, config.TagAllowlist) {
			return WebhookFilterDecision{Matched: false, Reason: WebhookFilterDecisionFilteredTag}
		}
		return WebhookFilterDecision{Matched: true, Reason: WebhookFilterDecisionMatched}
	default:
		return WebhookFilterDecision{Matched: false, Reason: WebhookFilterDecisionUnsupportedRefType}
	}
}

func WebhookFilterShouldTriggerBuild(ref domain.WebhookRef, config WebhookFilterConfig) WebhookFilterDecision {
	return webhookFilterShouldTriggerBuild(ref, config)
}

func normalizeWebhookFilterMode(mode domain.JobTriggerMode) domain.JobTriggerMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(domain.JobTriggerModeTags):
		return domain.JobTriggerModeTags
	case string(domain.JobTriggerModeBranchesAndTags):
		return domain.JobTriggerModeBranchesAndTags
	default:
		return domain.JobTriggerModeBranches
	}
}

func NormalizeWebhookFilterMode(mode domain.JobTriggerMode) domain.JobTriggerMode {
	return normalizeWebhookFilterMode(mode)
}

func matchesWebhookBranchAllowlist(refName string, allowlist []string) bool {
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

func matchesWebhookTagAllowlist(refName string, allowlist []string) bool {
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
