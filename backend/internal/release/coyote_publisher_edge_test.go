package release

import (
	"context"
	"strings"
	"testing"
)

func TestPromoteCoyoteLatestIfNewerEdgeCases(t *testing.T) {
	publisher, publisherErr := NewFilePublisher(t.TempDir())
	if publisherErr != nil {
		t.Fatalf("NewFilePublisher(): %v", publisherErr)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	if _, invalidErr := publisher.PromoteCoyoteLatestIfNewer(context.Background(), "not-a-release", digest); invalidErr == nil {
		t.Fatal("expected invalid release rejection")
	}
	if _, invalidErr := publisher.PromoteCoyoteLatestIfNewer(context.Background(), "0.0.1", "not-a-digest"); invalidErr == nil {
		t.Fatal("expected invalid digest rejection")
	}
	advanced, promoteErr := publisher.PromoteCoyoteLatestIfNewer(context.Background(), "0.0.1", digest)
	if promoteErr != nil || !advanced {
		t.Fatalf("initial promotion advanced=%t err=%v", advanced, promoteErr)
	}
	advanced, promoteErr = publisher.PromoteCoyoteLatestIfNewer(context.Background(), "0.0.1", digest)
	if promoteErr != nil || !advanced {
		t.Fatalf("idempotent promotion advanced=%t err=%v", advanced, promoteErr)
	}
	if stableErr := publisher.PromoteChannel(context.Background(), "stable", "0.0.1", digest); stableErr != nil {
		t.Fatalf("PromoteChannel(stable): %v", stableErr)
	}
}
