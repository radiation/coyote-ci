package release

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestPromoteCoyoteLatestIfNewerConcurrent(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("NewFilePublisher(): %v", publisherErr)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	if channelErr := publisher.PublishChannels(ChannelIndex{SchemaVersion: SchemaVersion, Channels: map[string]ChannelEntry{
		"latest": {Release: "0.0.40", ManifestDigest: digest},
	}}); channelErr != nil {
		t.Fatalf("PublishChannels(): %v", channelErr)
	}
	start := make(chan struct{})
	var group sync.WaitGroup
	for _, version := range []string{"0.0.41", "0.0.42"} {
		group.Add(1)
		go func(version string) {
			defer group.Done()
			<-start
			if _, promoteErr := publisher.PromoteCoyoteLatestIfNewer(context.Background(), version, digest); promoteErr != nil {
				t.Errorf("PromoteCoyoteLatestIfNewer(%s): %v", version, promoteErr)
			}
		}(version)
	}
	close(start)
	group.Wait()
	source, sourceErr := NewFileSource(root)
	if sourceErr != nil {
		t.Fatalf("NewFileSource(): %v", sourceErr)
	}
	index, indexErr := source.FetchChannelIndex(context.Background())
	if indexErr != nil || index.Channels["latest"].Release != "0.0.42" {
		t.Fatalf("latest=%+v err=%v, want 0.0.42", index.Channels["latest"], indexErr)
	}
	advanced, olderErr := publisher.PromoteCoyoteLatestIfNewer(context.Background(), "0.0.41", digest)
	if olderErr != nil || advanced {
		t.Fatalf("older retry advanced=%t err=%v", advanced, olderErr)
	}
}
