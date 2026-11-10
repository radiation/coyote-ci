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

func validV2Manifest() Manifest {
	return Manifest{SchemaVersion: ManifestSchemaVersionV2, Release: "0.0.10", Source: SourceIdentity{Commit: strings.Repeat("a", 40), CreatedAt: "2026-11-03T14:00:00Z"}, Components: map[string]Component{
		"server":   {Version: "1.2.3", Image: "registry.example/coyote-server@sha256:" + strings.Repeat("a", 64)},
		"frontend": {Version: "2.3.4", Image: "registry.example/coyote-frontend@sha256:" + strings.Repeat("b", 64)},
		"worker":   {Version: "1.2.3", Image: "registry.example/coyote-worker@sha256:" + strings.Repeat("c", 64)},
		"mcp":      {Version: "0.4.0", Image: "registry.example/coyote-mcp@sha256:" + strings.Repeat("d", 64)},
		"migrate":  {Version: "3.0.0", Image: "registry.example/coyote-migrate@sha256:" + strings.Repeat("e", 64)},
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
		{"components", func(m *Manifest) { m.Components = validV2Manifest().Components }},
		{"empty components", func(m *Manifest) { m.Components = map[string]Component{} }},
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

func TestV2ManifestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"valid", func(*Manifest) {}},
		{"missing component", func(m *Manifest) { delete(m.Components, "mcp") }},
		{"missing version", func(m *Manifest) { m.Components["mcp"] = Component{Image: m.Components["mcp"].Image} }},
		{"mutable image", func(m *Manifest) {
			m.Components["mcp"] = Component{Version: "0.4.0", Image: "registry.example/coyote-mcp:latest"}
		}},
		{"legacy images", func(m *Manifest) { m.Images = validManifest().Images }},
		{"empty legacy images", func(m *Manifest) { m.Images = map[string]string{} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validV2Manifest()
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

func TestManifestFormatExclusivityAfterJSONDecoding(t *testing.T) {
	tests := []struct {
		name      string
		manifest  Manifest
		forbidden string
	}{
		{"v1 empty components", validManifest(), "components"},
		{"v2 empty images", validV2Manifest(), "images"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, marshalErr := json.Marshal(tc.manifest)
			if marshalErr != nil {
				t.Fatalf("marshal manifest: %v", marshalErr)
			}
			var document map[string]any
			if decodeErr := json.Unmarshal(data, &document); decodeErr != nil {
				t.Fatalf("decode manifest document: %v", decodeErr)
			}
			document[tc.forbidden] = map[string]any{}
			data, marshalErr = json.Marshal(document)
			if marshalErr != nil {
				t.Fatalf("marshal document with forbidden field: %v", marshalErr)
			}
			if _, parseErr := ParseManifest(data); parseErr == nil {
				t.Fatal("expected forbidden empty object to fail validation")
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
	digest, publishErr := publisher.PublishManifest(context.Background(), manifest)
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

func TestV2ManifestParsingResolutionAndOverlay(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}
	manifest := validV2Manifest()
	digest, publishErr := publisher.PublishManifest(context.Background(), manifest)
	if publishErr != nil {
		t.Fatalf("publish manifest: %v", publishErr)
	}
	source, sourceErr := NewFileSource(root)
	if sourceErr != nil {
		t.Fatalf("source: %v", sourceErr)
	}
	resolved, resolveErr := Resolve(context.Background(), source, manifest.Release, "")
	if resolveErr != nil {
		t.Fatalf("resolve: %v", resolveErr)
	}
	if resolved.Manifest.Components["mcp"].Version != "0.4.0" {
		t.Fatalf("unexpected MCP component: %+v", resolved.Manifest.Components["mcp"])
	}
	var output bytes.Buffer
	if overlayErr := WriteValuesOverlay(&output, resolved); overlayErr != nil {
		t.Fatalf("overlay: %v", overlayErr)
	}
	want := "release:\n  version: 0.0.10\n  channel: \n  manifestDigest: " + digest + "\nimages:\n  server: " + manifest.Components["server"].Image + "\n  frontend: " + manifest.Components["frontend"].Image + "\n  worker: " + manifest.Components["worker"].Image + "\n  migrate: " + manifest.Components["migrate"].Image + "\n  mcp: " + manifest.Components["mcp"].Image + "\n"
	if output.String() != want {
		t.Fatalf("unexpected overlay:\n%s", output.String())
	}
}

func TestManifestSerializationAndDigestCompatibility(t *testing.T) {
	v1 := validManifest()
	v1Data, marshalV1Err := json.MarshalIndent(v1, "", "  ")
	if marshalV1Err != nil {
		t.Fatalf("marshal v1: %v", marshalV1Err)
	}
	v1Data = append(v1Data, '\n')
	if got, want := Digest(v1Data), "sha256:575e21c962745837da2f9708898bb440f37eb3a8eb8a34b6505c3eadf4a646dd"; got != want {
		t.Fatalf("v1 digest changed: got %s want %s", got, want)
	}
	v2 := validV2Manifest()
	first, marshalFirstErr := json.MarshalIndent(v2, "", "  ")
	if marshalFirstErr != nil {
		t.Fatalf("marshal v2 first: %v", marshalFirstErr)
	}
	second, marshalSecondErr := json.MarshalIndent(v2, "", "  ")
	if marshalSecondErr != nil {
		t.Fatalf("marshal v2 second: %v", marshalSecondErr)
	}
	first = append(first, '\n')
	second = append(second, '\n')
	if string(first) != string(second) || Digest(first) != Digest(second) {
		t.Fatal("v2 manifest serialization or digest is not deterministic")
	}
}

func TestResolveFailuresAndAppendOnlyPublishing(t *testing.T) {
	root := t.TempDir()
	publisher, _ := NewFilePublisher(root)
	manifest := validManifest()
	digest, publishErr := publisher.PublishManifest(context.Background(), manifest)
	if publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}
	manifest.Images["server"] = "registry.example/coyote-server@sha256:" + strings.Repeat("e", 64)
	if _, conflictErr := publisher.PublishManifest(context.Background(), manifest); !errors.Is(conflictErr, ErrManifestConflict) {
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

func TestOpenStoreLocations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "release store #1")
	for _, location := range []string{root, "file://" + root} {
		store, storeErr := OpenStore(location)
		if storeErr != nil {
			t.Fatalf("OpenStore(%q): %v", location, storeErr)
		}
		if _, ok := store.(*FilePublisher); !ok {
			t.Fatalf("OpenStore(%q) returned %T, want *FilePublisher", location, store)
		}
		manifest := validManifest()
		if _, publishErr := store.PublishManifest(context.Background(), manifest); publishErr != nil {
			t.Fatalf("PublishManifest(%q): %v", location, publishErr)
		}
		if _, fetchErr := store.FetchManifest(context.Background(), manifest.Release); fetchErr != nil {
			t.Fatalf("FetchManifest(%q): %v", location, fetchErr)
		}
	}
}

func TestOpenStoreRejectsUnsupportedAndMalformedLocations(t *testing.T) {
	tests := []struct {
		location string
		want     error
	}{
		{location: "gs://release-bucket/releases", want: ErrUnsupportedBackend},
		{location: "s3://release-bucket/releases", want: ErrUnsupportedBackend},
		{location: "gs://", want: ErrInvalidLocation},
		{location: "gs://release-bucket:443/releases", want: ErrInvalidLocation},
		{location: "gs://user@release-bucket/releases", want: ErrInvalidLocation},
		{location: "gs://release-bucket?unexpected=query", want: ErrInvalidLocation},
		{location: "gs://release-bucket/releases#fragment", want: ErrInvalidLocation},
		{location: "gs://release-bucket/../escape", want: ErrInvalidLocation},
		{location: "gs://release-bucket/%2e%2e/escape", want: ErrInvalidLocation},
		{location: "gs://release-bucket/releases//next", want: ErrInvalidLocation},
		{location: "file:/tmp/releases", want: ErrInvalidLocation},
		{location: "://release-store", want: ErrInvalidLocation},
	}
	for _, tc := range tests {
		t.Run(tc.location, func(t *testing.T) {
			_, storeErr := OpenStore(tc.location)
			if !errors.Is(storeErr, tc.want) {
				t.Fatalf("OpenStore(%q) error=%v, want %v", tc.location, storeErr, tc.want)
			}
		})
	}
}

func TestFilePublisherPromotionHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}
	if mkdirErr := os.Mkdir(filepath.Join(root, ".channels.lock"), 0o700); mkdirErr != nil {
		t.Fatalf("create lock: %v", mkdirErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := publisher.PromoteChannel(ctx, "latest", "2.5.1", "sha256:"+strings.Repeat("a", 64))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PromoteChannel() error=%v, want context cancellation", err)
	}
}

func TestFilePublisherPublicationHonorsCanceledContext(t *testing.T) {
	publisher, publisherErr := NewFilePublisher(t.TempDir())
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, publishErr := publisher.PublishManifest(ctx, validManifest()); !errors.Is(publishErr, context.Canceled) {
		t.Fatalf("PublishManifest() error=%v, want context cancellation", publishErr)
	}
}

func TestPublisherAndResolverInputFailures(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}

	manifest := validManifest()
	digest, publishErr := publisher.PublishManifest(context.Background(), manifest)
	if publishErr != nil {
		t.Fatalf("publish: %v", publishErr)
	}
	repeatedDigest, repeatErr := publisher.PublishManifest(context.Background(), manifest)
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
		_, publishErr := firstPublisher.PublishManifest(context.Background(), firstManifest)
		results <- publishErr
	}()
	go func() {
		_, publishErr := secondPublisher.PublishManifest(context.Background(), secondManifest)
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
