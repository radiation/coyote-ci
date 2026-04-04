package domain

import "testing"

func TestNormalizeWebhookRef(t *testing.T) {
	t.Run("branch", func(t *testing.T) {
		ref := NormalizeWebhookRef("refs/heads/main", false)
		if ref.RawRef != "refs/heads/main" || ref.RefType != WebhookRefTypeBranch || ref.RefName != "main" {
			t.Fatalf("unexpected branch ref normalization: %+v", ref)
		}
	})

	t.Run("tag", func(t *testing.T) {
		ref := NormalizeWebhookRef("refs/tags/v1.2.3", false)
		if ref.RefType != WebhookRefTypeTag || ref.RefName != "v1.2.3" {
			t.Fatalf("unexpected tag ref normalization: %+v", ref)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		ref := NormalizeWebhookRef("custom/ref", false)
		if ref.RefType != WebhookRefTypeUnknown || ref.RefName != "custom/ref" {
			t.Fatalf("unexpected unknown ref normalization: %+v", ref)
		}
	})

	t.Run("deleted", func(t *testing.T) {
		ref := NormalizeWebhookRef("refs/heads/main", true)
		if !ref.Deleted {
			t.Fatal("expected deleted=true")
		}
	})
}
