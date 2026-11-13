package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
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

func validV3Manifest() Manifest {
	source := SourceIdentity{Commit: strings.Repeat("a", 40), CreatedAt: "2026-11-13T12:00:00Z"}
	component := func(version, imageDigest, buildID string, buildSource SourceIdentity) Component {
		return Component{
			Version: version,
			Image:   "registry.example/coyote-" + buildID + "@sha256:" + imageDigest,
			Build:   &BuildIdentity{ID: buildID, Source: buildSource},
		}
	}
	reusedSource := SourceIdentity{Commit: strings.Repeat("b", 40), CreatedAt: "2026-10-30T12:00:00Z"}
	return Manifest{
		SchemaVersion: ManifestSchemaVersionV3,
		Release:       "3.0.0",
		Assembly:      &Assembly{BuildID: "assembly-build-300", Source: source},
		Components: map[string]Component{
			"server":   component("3.0.0", strings.Repeat("a", 64), "server-build-300", source),
			"frontend": component("3.0.0", strings.Repeat("b", 64), "frontend-build-300", source),
			"worker":   component("3.0.0", strings.Repeat("c", 64), "worker-build-300", source),
			"mcp":      component("2.4.0", strings.Repeat("d", 64), "mcp-build-240", reusedSource),
			"migrate":  component("3.0.0", strings.Repeat("e", 64), "migrate-build-300", source),
		},
		Helm: &HelmPackage{
			Name:       "coyote-ci",
			Repository: "oci://registry.example/charts",
			Version:    "3.0.0",
			Digest:     "sha256:" + strings.Repeat("f", 64),
		},
		CLIArtifacts: map[string]CLIArtifact{
			"linux-amd64": {
				URL:      "https://downloads.example/coyote_3.0.0_linux_amd64.tar.gz",
				Filename: "coyote_3.0.0_linux_amd64.tar.gz",
				Size:     12345,
				SHA256:   strings.Repeat("1", 64),
				BuildID:  "cli-build-300",
			},
		},
		Compatibility: &Compatibility{
			SchemaTarget: "57",
			UpgradeFrom:  ">=2.5.0 <4.0.0",
			Maintenance:  "active",
		},
	}
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

func TestV3ManifestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"valid mixed-origin components", func(*Manifest) {}},
		{"missing component build", func(m *Manifest) {
			component := m.Components["mcp"]
			component.Build = nil
			m.Components["mcp"] = component
		}},
		{"mutable component image", func(m *Manifest) {
			component := m.Components["server"]
			component.Image = "registry.example/coyote-server:latest"
			m.Components["server"] = component
		}},
		{"malformed component digest", func(m *Manifest) {
			component := m.Components["worker"]
			component.Image = "registry.example/coyote-worker@sha256:bad"
			m.Components["worker"] = component
		}},
		{"missing helm package", func(m *Manifest) { m.Helm = nil }},
		{"invalid helm name", func(m *Manifest) { m.Helm.Name = "coyote-worker" }},
		{"helm version conflicts with release", func(m *Manifest) { m.Helm.Version = "3.0.1" }},
		{"missing cli artifacts", func(m *Manifest) { m.CLIArtifacts = nil }},
		{"corrupt cli checksum", func(m *Manifest) {
			artifact := m.CLIArtifacts["linux-amd64"]
			artifact.SHA256 = "bad"
			m.CLIArtifacts["linux-amd64"] = artifact
		}},
		{"invalid cli filename", func(m *Manifest) {
			artifact := m.CLIArtifacts["linux-amd64"]
			artifact.Filename = "../coyote.tar.gz"
			m.CLIArtifacts["linux-amd64"] = artifact
		}},
		{"invalid upgrade range", func(m *Manifest) { m.Compatibility.UpgradeFrom = ">=2.5.0" }},
		{"empty upgrade range", func(m *Manifest) { m.Compatibility.UpgradeFrom = ">=3.0.0 <3.0.0" }},
		{"descending upgrade range", func(m *Manifest) { m.Compatibility.UpgradeFrom = ">=4.0.0 <3.0.0" }},
		{"invalid goose target", func(m *Manifest) { m.Compatibility.SchemaTarget = "00057" }},
		{"invalid maintenance state", func(m *Manifest) { m.Compatibility.Maintenance = "unsupported" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := validV3Manifest()
			tc.mutate(&manifest)
			validateErr := manifest.Validate()
			if tc.name == "valid mixed-origin components" && validateErr != nil {
				t.Fatalf("validate: %v", validateErr)
			}
			if tc.name != "valid mixed-origin components" && validateErr == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestV3ManifestAllowsReusedComponentIdentity(t *testing.T) {
	manifest := validV3Manifest()
	reused := manifest.Components["mcp"]
	manifest.Release = "3.1.0"
	manifest.Assembly.BuildID = "assembly-build-310"
	manifest.Assembly.Source = SourceIdentity{Commit: strings.Repeat("c", 40), CreatedAt: "2026-11-20T12:00:00Z"}
	manifest.Helm.Version = manifest.Release
	if validateErr := manifest.Validate(); validateErr != nil {
		t.Fatalf("validate reused component manifest: %v", validateErr)
	}
	if got := manifest.Components["mcp"]; got != reused {
		t.Fatalf("reused component identity changed: got %+v want %+v", got, reused)
	}
}

func TestV3ManifestRejectsConflictingOrLegacyFields(t *testing.T) {
	manifest := validV3Manifest()
	data, marshalErr := json.Marshal(manifest)
	if marshalErr != nil {
		t.Fatalf("marshal manifest: %v", marshalErr)
	}
	var fields map[string]json.RawMessage
	if decodeErr := json.Unmarshal(data, &fields); decodeErr != nil {
		t.Fatalf("decode manifest fields: %v", decodeErr)
	}
	for name, value := range map[string]json.RawMessage{
		"release_id": json.RawMessage(`"conflicting-identity"`),
		"source":     json.RawMessage(`{"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","created_at":"2026-11-13T12:00:00Z"}`),
	} {
		t.Run(name, func(t *testing.T) {
			withField := make(map[string]json.RawMessage, len(fields)+1)
			for key, fieldValue := range fields {
				withField[key] = fieldValue
			}
			withField[name] = value
			candidate, candidateMarshalErr := json.Marshal(withField)
			if candidateMarshalErr != nil {
				t.Fatalf("marshal candidate: %v", candidateMarshalErr)
			}
			if _, parseErr := ParseManifest(candidate); parseErr == nil {
				t.Fatalf("expected %s to be rejected", name)
			}
		})
	}
}

func TestV3ManifestRejectsUnknownNestedFields(t *testing.T) {
	data, marshalErr := json.Marshal(validV3Manifest())
	if marshalErr != nil {
		t.Fatalf("marshal manifest: %v", marshalErr)
	}
	for _, path := range [][]string{
		{"assembly", "unexpected"},
		{"assembly", "source", "unexpected"},
		{"components", "server", "unexpected"},
		{"components", "server", "build", "unexpected"},
		{"components", "server", "build", "source", "unexpected"},
		{"helm", "unexpected"},
		{"cli_artifacts", "linux-amd64", "unexpected"},
		{"compatibility", "unexpected"},
	} {
		t.Run(strings.Join(path, "."), func(t *testing.T) {
			candidate := addUnknownV3Field(t, data, path)
			if _, parseErr := ParseManifest(candidate); parseErr == nil {
				t.Fatalf("expected unknown nested field %q to be rejected", strings.Join(path, "."))
			}
		})
	}
}

func TestLegacyManifestAllowsUnknownFields(t *testing.T) {
	data, marshalErr := json.Marshal(validManifest())
	if marshalErr != nil {
		t.Fatalf("marshal legacy manifest: %v", marshalErr)
	}
	var fields map[string]json.RawMessage
	if decodeErr := json.Unmarshal(data, &fields); decodeErr != nil {
		t.Fatalf("decode legacy manifest: %v", decodeErr)
	}
	fields["future_extension"] = json.RawMessage(`true`)
	data, marshalErr = json.Marshal(fields)
	if marshalErr != nil {
		t.Fatalf("marshal extended legacy manifest: %v", marshalErr)
	}
	if _, parseErr := ParseManifest(data); parseErr != nil {
		t.Fatalf("parse extended legacy manifest: %v", parseErr)
	}
}

func TestLegacyManifestIgnoresV3Extensions(t *testing.T) {
	for _, legacy := range []Manifest{validManifest(), validV2Manifest()} {
		for _, extension := range []string{`{}`, `"historical"`, `42`, `true`, `[]`, `null`} {
			t.Run(legacy.Release+"/"+extension, func(t *testing.T) {
				data, marshalErr := json.Marshal(legacy)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				var fields map[string]json.RawMessage
				if decodeErr := json.Unmarshal(data, &fields); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				for _, name := range []string{"assembly", "helm", "cli_artifacts", "compatibility"} {
					fields[name] = json.RawMessage(extension)
				}
				if legacy.SchemaVersion == ManifestSchemaVersionV2 {
					var components map[string]map[string]json.RawMessage
					if decodeErr := json.Unmarshal(fields["components"], &components); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					for _, component := range components {
						component["build"] = json.RawMessage(extension)
					}
					componentData, componentErr := json.Marshal(components)
					if componentErr != nil {
						t.Fatal(componentErr)
					}
					fields["components"] = componentData
				}
				data, marshalErr = json.MarshalIndent(fields, "", "  ")
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				data = append(data, '\n')
				parsed, parseErr := ParseManifest(data)
				if parseErr != nil {
					t.Fatalf("parse legacy extensions: %v", parseErr)
				}
				if !reflect.DeepEqual(parsed, legacy) {
					t.Fatalf("extensions changed legacy metadata: %+v", parsed)
				}
				store, storeErr := NewFileStore(t.TempDir())
				if storeErr != nil {
					t.Fatal(storeErr)
				}
				_, digest, importErr := store.ImportManifest(context.Background(), data)
				if importErr != nil {
					t.Fatalf("import legacy extensions: %v", importErr)
				}
				stored, fetchErr := store.FetchManifest(context.Background(), legacy.Release)
				if fetchErr != nil {
					t.Fatal(fetchErr)
				}
				if !bytes.Equal(stored.Data, data) || digest != Digest(data) || stored.Digest != digest {
					t.Fatal("legacy import changed stored bytes or digest")
				}
				resolved, resolveErr := Resolve(context.Background(), store, legacy.Release, "")
				if resolveErr != nil {
					t.Fatalf("resolve legacy extensions: %v", resolveErr)
				}
				if !reflect.DeepEqual(resolved.Manifest, legacy) || resolved.ManifestDigest != digest {
					t.Fatal("resolution changed legacy metadata or digest")
				}
			})
		}
	}
}

func TestLegacyManifestStillRejectsMalformedKnownFields(t *testing.T) {
	for _, legacy := range []Manifest{validManifest(), validV2Manifest()} {
		t.Run(legacy.Release, func(t *testing.T) {
			data, marshalErr := json.Marshal(legacy)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			var fields map[string]json.RawMessage
			if decodeErr := json.Unmarshal(data, &fields); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			fields["source"] = json.RawMessage(`"not an object"`)
			data, marshalErr = json.Marshal(fields)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if _, parseErr := ParseManifest(data); parseErr == nil {
				t.Fatal("expected malformed legacy source to be rejected")
			}
		})
	}
}

func addUnknownV3Field(t *testing.T, data []byte, path []string) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if decodeErr := json.Unmarshal(data, &fields); decodeErr != nil {
		t.Fatalf("decode JSON object: %v", decodeErr)
	}
	if len(path) == 1 {
		fields[path[0]] = json.RawMessage(`true`)
	} else {
		nested, found := fields[path[0]]
		if !found {
			t.Fatalf("missing JSON path %q", path[0])
		}
		fields[path[0]] = addUnknownV3Field(t, nested, path[1:])
	}
	updated, marshalErr := json.Marshal(fields)
	if marshalErr != nil {
		t.Fatalf("marshal JSON object: %v", marshalErr)
	}
	return updated
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
		if !strings.Contains(output.String(), "worker: "+manifest.Images["worker"]) || !strings.Contains(output.String(), "manifestDigest: "+digest) || !strings.Contains(output.String(), "channel: \""+selector.channel+"\"") {
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
	want := "release:\n  version: 0.0.10\n  channel: \"\"\n  manifestDigest: " + digest + "\nimages:\n  server: " + manifest.Components["server"].Image + "\n  frontend: " + manifest.Components["frontend"].Image + "\n  worker: " + manifest.Components["worker"].Image + "\n  migrate: " + manifest.Components["migrate"].Image + "\n  mcp: " + manifest.Components["mcp"].Image + "\n"
	if output.String() != want {
		t.Fatalf("unexpected overlay:\n%s", output.String())
	}
}

func TestV3ManifestResolutionWithoutPredecessorOrGitHistory(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}
	manifest := validV3Manifest()
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
	if got := resolved.Manifest.Components["mcp"].Build.ID; got != "mcp-build-240" {
		t.Fatalf("unexpected reused component build identity: %q", got)
	}
	if !reflect.DeepEqual(resolved.Manifest.Assembly, manifest.Assembly) || !reflect.DeepEqual(resolved.Manifest.Components, manifest.Components) {
		t.Fatal("resolution must preserve independent assembly and component provenance")
	}
	var output bytes.Buffer
	if overlayErr := WriteValuesOverlay(&output, resolved); overlayErr != nil {
		t.Fatalf("overlay: %v", overlayErr)
	}
	if !strings.Contains(output.String(), "mcp: "+manifest.Components["mcp"].Image) || !strings.Contains(output.String(), "manifestDigest: "+digest) {
		t.Fatalf("unexpected v3 overlay:\n%s", output.String())
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
	v3 := validV3Manifest()
	first, marshalFirstErr = json.MarshalIndent(v3, "", "  ")
	if marshalFirstErr != nil {
		t.Fatalf("marshal v3 first: %v", marshalFirstErr)
	}
	second, marshalSecondErr = json.MarshalIndent(v3, "", "  ")
	if marshalSecondErr != nil {
		t.Fatalf("marshal v3 second: %v", marshalSecondErr)
	}
	first = append(first, '\n')
	second = append(second, '\n')
	if string(first) != string(second) || Digest(first) != Digest(second) {
		t.Fatal("v3 manifest serialization or digest is not deterministic")
	}
}

func TestV3ManifestImportPreservesBytesAndRejectsConflicts(t *testing.T) {
	root := t.TempDir()
	publisher, publisherErr := NewFilePublisher(root)
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}
	manifest := validV3Manifest()
	data, marshalErr := json.MarshalIndent(manifest, "", "  ")
	if marshalErr != nil {
		t.Fatalf("marshal manifest: %v", marshalErr)
	}
	data = append(data, '\n')
	_, firstDigest, firstImportErr := publisher.ImportManifest(context.Background(), data)
	if firstImportErr != nil {
		t.Fatalf("first import: %v", firstImportErr)
	}
	_, retryDigest, retryImportErr := publisher.ImportManifest(context.Background(), data)
	if retryImportErr != nil || retryDigest != firstDigest {
		t.Fatalf("identical retry digest=%s err=%v, want %s and nil", retryDigest, retryImportErr, firstDigest)
	}
	stored, fetchErr := publisher.FetchManifest(context.Background(), manifest.Release)
	if fetchErr != nil {
		t.Fatalf("fetch stored manifest: %v", fetchErr)
	}
	if !bytes.Equal(stored.Data, data) || stored.Digest != firstDigest {
		t.Fatalf("stored document was not preserved: digest=%s", stored.Digest)
	}
	manifest.Compatibility.Maintenance = "maintenance"
	conflictingData, marshalConflictErr := json.MarshalIndent(manifest, "", "  ")
	if marshalConflictErr != nil {
		t.Fatalf("marshal conflicting manifest: %v", marshalConflictErr)
	}
	conflictingData = append(conflictingData, '\n')
	if _, _, conflictErr := publisher.ImportManifest(context.Background(), conflictingData); !errors.Is(conflictErr, ErrManifestConflict) {
		t.Fatalf("conflicting import error=%v, want %v", conflictErr, ErrManifestConflict)
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
		store, closer, storeErr := OpenStore(context.Background(), location)
		if storeErr != nil {
			t.Fatalf("OpenStore(%q): %v", location, storeErr)
		}
		if closer != nil {
			t.Fatalf("OpenStore(%q) returned an unexpected closer", location)
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
			_, _, storeErr := OpenStore(context.Background(), tc.location)
			if !errors.Is(storeErr, tc.want) {
				t.Fatalf("OpenStore(%q) error=%v, want %v", tc.location, storeErr, tc.want)
			}
		})
	}
}

func TestOpenStoreSelectsGCS(t *testing.T) {
	store, closer, storeErr := openStore(context.Background(), "gs://release-bucket/releases", func(ctx context.Context) (*storage.Client, error) {
		return storage.NewClient(ctx, option.WithoutAuthentication())
	})
	if storeErr != nil {
		t.Fatalf("openStore: %v", storeErr)
	}
	if _, ok := store.(*GCSStore); !ok {
		t.Fatalf("openStore returned %T, want *GCSStore", store)
	}
	if closer == nil {
		t.Fatal("openStore did not return a GCS client closer")
	}
	if closeErr := closer.Close(); closeErr != nil {
		t.Fatalf("close GCS client: %v", closeErr)
	}
}

func TestOpenStoreFilesystemDoesNotCreateGCSClient(t *testing.T) {
	called := false
	store, closer, storeErr := openStore(context.Background(), t.TempDir(), func(context.Context) (*storage.Client, error) {
		called = true
		return nil, errors.New("GCS client must not be created")
	})
	if storeErr != nil {
		t.Fatalf("openStore: %v", storeErr)
	}
	if _, ok := store.(*FilePublisher); !ok || closer != nil || called {
		t.Fatalf("store=%T closer=%v GCS client called=%t", store, closer, called)
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
	_, err := publisher.PromoteChannel(ctx, "latest", "2.5.1", "sha256:"+strings.Repeat("a", 64))
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
		_, promoteErr := publisher.PromoteChannel(context.Background(), "latest", "2.5.2", latestDigest)
		promotionErrors <- promoteErr
	}()
	go func() {
		_, promoteErr := publisher.PromoteChannel(context.Background(), "stable", "2.5.1", stableDigest)
		promotionErrors <- promoteErr
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

func TestFilePublisherPromotionResult(t *testing.T) {
	publisher, publisherErr := NewFilePublisher(t.TempDir())
	if publisherErr != nil {
		t.Fatalf("publisher: %v", publisherErr)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	result, promoteErr := publisher.PromoteChannel(context.Background(), "latest", "2.5.1", digest)
	if promoteErr != nil {
		t.Fatalf("initial promotion: %v", promoteErr)
	}
	if !result.PreviousKnown || result.Previous != nil || !result.ChangedKnown || !result.Changed || result.Current != (ChannelEntry{Release: "2.5.1", ManifestDigest: digest}) {
		t.Fatalf("unexpected initial result: %+v", result)
	}

	result, promoteErr = publisher.PromoteChannel(context.Background(), "latest", "2.5.1", digest)
	if promoteErr != nil {
		t.Fatalf("repeated promotion: %v", promoteErr)
	}
	if result.Previous == nil || *result.Previous != result.Current || !result.PreviousKnown || !result.ChangedKnown || result.Changed {
		t.Fatalf("unexpected repeated result: %+v", result)
	}

	if _, invalidChannelErr := publisher.PromoteChannel(context.Background(), "preview", "2.5.1", digest); invalidChannelErr == nil {
		t.Fatal("expected unsupported channel error")
	}
	if _, invalidVersionErr := publisher.PromoteChannel(context.Background(), "latest", "2.5", digest); invalidVersionErr == nil {
		t.Fatal("expected invalid release error")
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
