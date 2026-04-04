package domain

import "strings"

type WebhookRefType string

const (
	WebhookRefTypeBranch  WebhookRefType = "branch"
	WebhookRefTypeTag     WebhookRefType = "tag"
	WebhookRefTypeUnknown WebhookRefType = "unknown"
)

type WebhookRef struct {
	RawRef  string
	RefType WebhookRefType
	RefName string
	Deleted bool
}

func NormalizeWebhookRef(rawRef string, deleted bool) WebhookRef {
	trimmed := strings.TrimSpace(rawRef)
	ref := WebhookRef{RawRef: trimmed, RefType: WebhookRefTypeUnknown, Deleted: deleted}
	if trimmed == "" {
		return ref
	}

	switch {
	case strings.HasPrefix(trimmed, "refs/heads/"):
		ref.RefType = WebhookRefTypeBranch
		ref.RefName = strings.TrimPrefix(trimmed, "refs/heads/")
	case strings.HasPrefix(trimmed, "refs/tags/"):
		ref.RefType = WebhookRefTypeTag
		ref.RefName = strings.TrimPrefix(trimmed, "refs/tags/")
	default:
		ref.RefName = trimmed
	}

	ref.RefName = strings.TrimSpace(ref.RefName)
	return ref
}
