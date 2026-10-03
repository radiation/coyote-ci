package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{SchemaVersion: 1, Release: "2.5.1", Source: SourceIdentity{Commit: strings.Repeat("a", 40), CreatedAt: "2026-10-03T14:00:00Z"}, Images: map[string]string{
		"server":   "registry.example/coyote-server@sha256:" + strings.Repeat("a", 64),
		"frontend": "registry.example/coyote-frontend@sha256:" + strings.Repeat("b", 64),
		"worker":   "registry.example/coyote-worker@sha256:" + strings.Repeat("c", 64),
		"migrate":  "registry.example/coyote-migrate@sha256:" + strings.Repeat("d", 64),
	}}
}

func TestManifestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"valid", func(*Manifest) {}},
		{"invalid semver", func(m *Manifest) { m.Release = "latest" }},
		{"missing component", func(m *Manifest) { delete(m.Images, "worker") }},
		{"mutable image", func(m *Manifest) { m.Images["server"] = "registry.example/coyote-server:latest" }},
		{"tagged digest image", func(m *Manifest) {
			m.Images["server"] = "registry.example/coyote-server:2.5.1@sha256:" + strings.Repeat("a", 64)
		}},
		{"leading image whitespace", func(m *Manifest) {
			m.Images["server"] = " registry.example/coyote-server@sha256:" + strings.Repeat("a", 64)
		}},
		{"trailing image whitespace", func(m *Manifest) {
			m.Images["server"] = "registry.example/coyote-server@sha256:" + strings.Repeat("a", 64) + " "
		}},
		{"bad digest", func(m *Manifest) { m.Images["server"] = "registry.example/coyote-server@sha256:bad" }},
		{"bad commit", func(m *Manifest) { m.Source.Commit = "abc123" }},
		{"schema", func(m *Manifest) { m.SchemaVersion = 2 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validManifest()
			tc.mutate(&manifest)
			err := manifest.Validate()
			if tc.name == "valid" && err != nil {
				t.Fatalf("validate: %v", err)
			}
			if tc.name != "valid" && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestResolveAndOverlay(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}
	manifest := validManifest()
	digest, publishErr := publisher.PublishManifest(manifest)
	if publishErr != nil {
		t.Fatalf("publish manifest: %v", publishErr)
	}
	index := ChannelIndex{SchemaVersion: 1, Channels: map[string]ChannelEntry{
		"latest": {Release: "2.5.1", ManifestDigest: digest},
		"stable": {Release: "2.5.1", ManifestDigest: digest},
	}}
	if channelErr := publisher.PublishChannels(index); channelErr != nil {
		t.Fatalf("publish channels: %v", channelErr)
	}
	source, sourceErr := NewFileSource(root)
	if sourceErr != nil {
		t.Fatalf("source: %v", sourceErr)
	}
	for _, selector := range []struct{ release, channel string }{{release: "2.5.1"}, {channel: "latest"}, {channel: "stable"}} {
		resolved, resolveErr := Resolve(context.Background(), source, selector.release, selector.channel)
		if resolveErr != nil {
			t.Fatalf("resolve %+v: %v", selector, resolveErr)
		}
		var output bytes.Buffer
		if overlayErr := WriteValuesOverlay(&output, resolved); overlayErr != nil {
			t.Fatalf("overlay: %v", overlayErr)
		}
		if !strings.Contains(output.String(), "worker: "+manifest.Images["worker"]) || !strings.Contains(output.String(), "manifestDigest: "+digest) {
			t.Fatalf("unexpected overlay: %s", output.String())
		}
	}
}

func TestResolveFailuresAndAppendOnlyPublishing(t *testing.T) {
	root := t.TempDir()
	publisher, _ := NewFilePublisher(root)
	manifest := validManifest()
	digest, publishErr := publisher.PublishManifest(manifest)
	if publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}
	manifest.Images["server"] = "registry.example/coyote-server@sha256:" + strings.Repeat("e", 64)
	if _, conflictErr := publisher.PublishManifest(manifest); !errors.Is(conflictErr, ErrManifestConflict) {
		t.Fatalf("expected conflict, got %v", conflictErr)
	}
	index := ChannelIndex{SchemaVersion: 1, Channels: map[string]ChannelEntry{"stable": {Release: "2.5.1", ManifestDigest: digest}}}
	if indexErr := publisher.PublishChannels(index); indexErr != nil {
		t.Fatalf("index: %v", indexErr)
	}
	source, _ := NewFileSource(root)
	if _, unknownErr := Resolve(context.Background(), source, "", "latest"); unknownErr == nil {
		t.Fatal("expected unknown channel error")
	}
	index.Channels["stable"] = ChannelEntry{Release: "2.5.1", ManifestDigest: "sha256:" + strings.Repeat("0", 64)}
	data, _ := json.Marshal(index)
	if writeErr := os.WriteFile(filepath.Join(root, "channels.json"), data, 0o600); writeErr != nil {
		t.Fatalf("write index: %v", writeErr)
	}
	if _, mismatchErr := Resolve(context.Background(), source, "", "stable"); mismatchErr == nil {
		t.Fatal("expected digest mismatch")
	}
}

func TestParsingAndOptionalDeploymentValidation(t *testing.T) {
	manifest := validManifest()
	manifest.Deployment = &Deployment{
		ControlChart: &Chart{Version: "2.5.1", Digest: "sha256:" + strings.Repeat("f", 64)},
		WorkerChart:  &Chart{Version: "2.5.1", Digest: "sha256:" + strings.Repeat("e", 64)},
	}
	data, marshalErr := json.Marshal(manifest)
	if marshalErr != nil {
		t.Fatalf("marshal manifest: %v", marshalErr)
	}
	parsed, parseErr := ParseManifest(data)
	if parseErr != nil || parsed.Release != manifest.Release {
		t.Fatalf("parse manifest: %+v %v", parsed, parseErr)
	}
	if _, invalidJSONErr := ParseManifest([]byte("{")); invalidJSONErr == nil {
		t.Fatal("expected invalid JSON error")
	}
	manifest.Deployment.ControlChart.Digest = "bad"
	if validateErr := manifest.Validate(); validateErr == nil {
		t.Fatal("expected invalid chart error")
	}
}

func TestChannelIndexAndFileSourceFailures(t *testing.T) {
	index := ChannelIndex{SchemaVersion: 1, Channels: map[string]ChannelEntry{"preview": {Release: "2.5.1", ManifestDigest: "sha256:" + strings.Repeat("a", 64)}}}
	if validateErr := index.Validate(); validateErr == nil {
		t.Fatal("expected unsupported channel error")
	}
	index = ChannelIndex{SchemaVersion: 2}
	if validateErr := index.Validate(); validateErr == nil {
		t.Fatal("expected schema error")
	}
	if _, sourceErr := NewFileSource(""); sourceErr == nil {
		t.Fatal("expected empty source error")
	}
	source, sourceErr := NewFileSource("file://" + t.TempDir())
	if sourceErr != nil {
		t.Fatalf("source: %v", sourceErr)
	}
	if _, missingErr := source.FetchManifest(context.Background(), "2.5.1"); !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("expected missing manifest, got %v", missingErr)
	}
	if _, missingIndexErr := source.FetchChannelIndex(context.Background()); !errors.Is(missingIndexErr, ErrNotFound) {
		t.Fatalf("expected missing index, got %v", missingIndexErr)
	}
}

func TestPublisherAndResolverInputFailures(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}

	manifest := validManifest()
	digest, publishErr := publisher.PublishManifest(manifest)
	if publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}
	repeatedDigest, repeatErr := publisher.PublishManifest(manifest)
	if repeatErr != nil || repeatedDigest != digest {
		t.Fatalf("repeated publish: %q %v", repeatedDigest, repeatErr)
	}
	if channelErr := publisher.PublishChannels(ChannelIndex{SchemaVersion: 1, Channels: map[string]ChannelEntry{"preview": {Release: "2.5.1", ManifestDigest: digest}}}); channelErr == nil {
		t.Fatal("expected invalid channels error")
	}
	if _, resolveErr := Resolve(context.Background(), nil, "2.5.1", ""); resolveErr == nil {
		t.Fatal("expected missing source error")
	}
	source, sourceErr := NewFileSource(root)
	if sourceErr != nil {
		t.Fatalf("source: %v", sourceErr)
	}
	if _, selectorErr := Resolve(context.Background(), source, "", ""); selectorErr == nil {
		t.Fatal("expected selector error")
	}
	var output bytes.Buffer
	invalid := Resolved{Manifest: Manifest{SchemaVersion: 1}}
	if overlayErr := WriteValuesOverlay(&output, invalid); overlayErr == nil {
		t.Fatal("expected invalid overlay error")
	}
}

func TestPromoteChannelPreservesConcurrentUpdates(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}

	latestDigest := "sha256:" + strings.Repeat("a", 64)
	stableDigest := "sha256:" + strings.Repeat("b", 64)
	promotionErrors := make(chan error, 2)
	go func() {
		promotionErrors <- publisher.PromoteChannel(context.Background(), "latest", "2.5.2", latestDigest)
	}()
	go func() {
		promotionErrors <- publisher.PromoteChannel(context.Background(), "stable", "2.5.1", stableDigest)
	}()
	for range 2 {
		if promotionErr := <-promotionErrors; promotionErr != nil {
			t.Fatalf("promote channel: %v", promotionErr)
		}
	}
	source, sourceErr := NewFileSource(root)
	if sourceErr != nil {
		t.Fatalf("source: %v", sourceErr)
	}
	index, indexErr := source.FetchChannelIndex(context.Background())
	if indexErr != nil {
		t.Fatalf("fetch channel index: %v", indexErr)
	}
	if index.Channels["latest"].ManifestDigest != latestDigest || index.Channels["stable"].ManifestDigest != stableDigest {
		t.Fatalf("concurrent channel updates were not preserved: %+v", index.Channels)
	}
}

func TestPublishManifestPreservesConcurrentImmutability(t *testing.T) {
	root := t.TempDir()
	firstPublisher, firstPublisherErr := NewFilePublisher(root)
	if firstPublisherErr != nil {
		t.Fatalf("first publisher: %v", firstPublisherErr)
	}
	secondPublisher, secondPublisherErr := NewFilePublisher(root)
	if secondPublisherErr != nil {
		t.Fatalf("second publisher: %v", secondPublisherErr)
	}
	firstManifest := validManifest()
	secondManifest := validManifest()
	secondManifest.Images["server"] = "registry.example/coyote-server@sha256:" + strings.Repeat("e", 64)
	results := make(chan error, 2)
	go func() {
		_, publishErr := firstPublisher.PublishManifest(firstManifest)
		results <- publishErr
	}()
	go func() {
		_, publishErr := secondPublisher.PublishManifest(secondManifest)
		results <- publishErr
	}()
	var successes, conflicts int
	for range 2 {
		publishErr := <-results
		if publishErr == nil {
			successes++
			continue
		}
		if errors.Is(publishErr, ErrManifestConflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected publish error: %v", publishErr)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one winner and one conflict, got %d successes and %d conflicts", successes, conflicts)
	}
	data, readErr := os.ReadFile(filepath.Join(root, "releases", "2.5.1.json"))
	if readErr != nil {
		t.Fatalf("read winning manifest: %v", readErr)
	}
	winningManifest, parseErr := ParseManifest(data)
	if parseErr != nil {
		t.Fatalf("parse winning manifest: %v", parseErr)
	}
	winningImage := winningManifest.Images["server"]
	if winningImage != firstManifest.Images["server"] && winningImage != secondManifest.Images["server"] {
		t.Fatalf("unexpected winning manifest image: %s", winningImage)
	}
}
