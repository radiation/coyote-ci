package release

import (
	"context"
	"strings"
	"testing"
)

func TestCoyoteManifest(t *testing.T) {
	input := validCoyoteReleaseInput()
	manifest, err := CoyoteManifest(input)
	if err != nil || manifest.SchemaVersion != ManifestSchemaVersionV3 || manifest.Release != "0.0.41" || manifest.Components["worker"].Image != input.Outputs[2].ImmutableReference {
		t.Fatalf("CoyoteManifest() manifest=%+v err=%v", manifest, err)
	}
	for _, component := range manifest.Components {
		if component.Version != input.ApplicationVersion || component.Build.ID != input.BuildID || component.Build.Source.Commit != input.SourceCommit {
			t.Fatalf("lost original build provenance: %+v", component)
		}
	}
	input.Outputs[0].ImmutableReference = "registry.example/coyote-server:latest"
	if _, invalidErr := CoyoteManifest(input); invalidErr == nil {
		t.Fatal("expected mutable reference rejection")
	}
}

func validCoyoteReleaseInput() CoyoteReleaseInput {
	input := CoyoteReleaseInput{
		BuildID: "build-41",
		Status:  "complete", ApplicationVersion: "0.0.41", SourceCommit: strings.Repeat("a", 40), CreatedAt: "2026-10-13T14:00:00Z",
		Outputs: []CoyoteReleaseOutput{
			{Key: CoyoteServerOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-server", ImmutableReference: "registry.example/coyote-server@sha256:" + strings.Repeat("a", 64)},
			{Key: CoyoteFrontendOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-frontend", ImmutableReference: "registry.example/coyote-frontend@sha256:" + strings.Repeat("b", 64)},
			{Key: CoyoteWorkerOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-worker", ImmutableReference: "registry.example/coyote-worker@sha256:" + strings.Repeat("c", 64)},
			{Key: CoyoteMigrateOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-migrate", ImmutableReference: "registry.example/coyote-migrate@sha256:" + strings.Repeat("d", 64)},
			{Key: CoyoteMCPOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-mcp", ImmutableReference: "registry.example/coyote-mcp@sha256:" + strings.Repeat("e", 64)},
		},
		Helm:          &HelmPackage{Name: "coyote-ci", Repository: "oci://registry.example/charts/coyote-ci", Version: "0.0.41", Digest: "sha256:" + strings.Repeat("f", 64)},
		Compatibility: &Compatibility{SchemaTarget: "57", UpgradeFrom: ">=0.0.11 <0.0.41", Maintenance: "active"},
		CLIArtifacts:  make(map[string]CLIArtifact),
	}
	for _, platform := range CoyoteCLIPlatforms {
		input.CLIArtifacts[platform] = CLIArtifact{URL: "https://ci.example/" + platform, Filename: platform + ".tar.gz", Size: 123, SHA256: strings.Repeat("a", 64), BuildID: input.BuildID}
	}
	return input
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
