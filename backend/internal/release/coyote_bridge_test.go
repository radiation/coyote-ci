package release

import (
	"context"
	"strings"
	"testing"
)

func TestCoyoteManifest(t *testing.T) {
	input := CoyoteReleaseInput{
		Status: "complete", ApplicationVersion: "0.0.41", SourceCommit: strings.Repeat("a", 40), CreatedAt: "2026-10-13T14:00:00Z",
		Outputs: []CoyoteReleaseOutput{
			{Key: CoyoteServerOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-server", ImmutableReference: "registry.example/coyote-server@sha256:" + strings.Repeat("a", 64)},
			{Key: CoyoteFrontendOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-frontend", ImmutableReference: "registry.example/coyote-frontend@sha256:" + strings.Repeat("b", 64)},
			{Key: CoyoteWorkerOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-worker", ImmutableReference: "registry.example/coyote-worker@sha256:" + strings.Repeat("c", 64)},
			{Key: CoyoteMigrateOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-migrate", ImmutableReference: "registry.example/coyote-migrate@sha256:" + strings.Repeat("d", 64)},
		},
	}

	manifest, err := CoyoteManifest(input)
	if err != nil || manifest.Release != "0.0.41" || manifest.Images["worker"] != input.Outputs[2].ImmutableReference {
		t.Fatalf("CoyoteManifest() manifest=%+v err=%v", manifest, err)
	}
	input.Outputs[0].ImmutableReference = "registry.example/coyote-server:latest"
	if _, invalidErr := CoyoteManifest(input); invalidErr == nil {
		t.Fatal("expected mutable reference rejection")
	}
}

func TestShouldAdvanceCoyoteLatest(t *testing.T) {
	root := t.TempDir()
	publisher, err := NewFilePublisher(root)
	if err != nil {
		t.Fatalf("NewFilePublisher(): %v", err)
	}
	if channelErr := publisher.PublishChannels(ChannelIndex{SchemaVersion: SchemaVersion, Channels: map[string]ChannelEntry{
		"latest": {Release: "0.0.41", ManifestDigest: "sha256:" + strings.Repeat("a", 64)},
	}}); channelErr != nil {
		t.Fatalf("PublishChannels(): %v", channelErr)
	}
	source, err := NewFileSource(root)
	if err != nil {
		t.Fatalf("NewFileSource(): %v", err)
	}
	for _, testCase := range []struct {
		version string
		want    bool
	}{
		{version: "0.0.40", want: false},
		{version: "0.0.41", want: true},
		{version: "0.0.42", want: true},
	} {
		advance, advanceErr := ShouldAdvanceCoyoteLatest(context.Background(), source, testCase.version)
		if advanceErr != nil || advance != testCase.want {
			t.Fatalf("ShouldAdvanceCoyoteLatest(%q) = %t, %v; want %t", testCase.version, advance, advanceErr, testCase.want)
		}
	}
}
