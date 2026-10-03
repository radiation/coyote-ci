package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const SchemaVersion = 1

var (
	ErrNotFound         = errors.New("release document not found")
	ErrManifestConflict = errors.New("release manifest already exists with different content")
	semVerPattern       = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	commitPattern       = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)
	digestPattern       = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

var requiredComponents = []string{"server", "frontend", "worker", "migrate"}

type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	Release       string            `json:"release"`
	Source        SourceIdentity    `json:"source"`
	Images        map[string]string `json:"images"`
	Deployment    *Deployment       `json:"deployment,omitempty"`
}

type SourceIdentity struct {
	Commit    string `json:"commit"`
	CreatedAt string `json:"created_at"`
}

type Deployment struct {
	ControlChart *Chart `json:"control_chart,omitempty"`
	WorkerChart  *Chart `json:"worker_chart,omitempty"`
}

type Chart struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type ChannelIndex struct {
	SchemaVersion int                     `json:"schema_version"`
	Channels      map[string]ChannelEntry `json:"channels"`
}

type ChannelEntry struct {
	Release        string `json:"release"`
	ManifestDigest string `json:"manifest_digest"`
}

type Document struct {
	Data   []byte
	Digest string
}

type Source interface {
	FetchManifest(context.Context, string) (Document, error)
	FetchChannelIndex(context.Context) (ChannelIndex, error)
}

type Resolved struct {
	Manifest       Manifest
	ManifestDigest string
	Channel        string
}

func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported manifest schema version %d", m.SchemaVersion)
	}
	if !semVerPattern.MatchString(m.Release) {
		return fmt.Errorf("release must be an exact semantic version: %q", m.Release)
	}
	if !commitPattern.MatchString(m.Source.Commit) {
		return fmt.Errorf("source commit must be a full Git commit SHA: %q", m.Source.Commit)
	}
	if _, err := time.Parse(time.RFC3339, m.Source.CreatedAt); err != nil {
		return fmt.Errorf("source created_at must be RFC3339: %w", err)
	}
	if len(m.Images) != len(requiredComponents) {
		return errors.New("manifest images must contain exactly server, frontend, worker, and migrate")
	}
	for _, component := range requiredComponents {
		image, ok := m.Images[component]
		if !ok {
			return fmt.Errorf("manifest is missing %s image", component)
		}
		if err := ValidateImageReference(image); err != nil {
			return fmt.Errorf("%s image: %w", component, err)
		}
	}
	for component := range m.Images {
		if !isRequiredComponent(component) {
			return fmt.Errorf("manifest contains unknown image component %q", component)
		}
	}
	return validateDeployment(m.Deployment)
}

func validateDeployment(deployment *Deployment) error {
	if deployment == nil {
		return nil
	}
	for name, chart := range map[string]*Chart{"control_chart": deployment.ControlChart, "worker_chart": deployment.WorkerChart} {
		if chart == nil {
			continue
		}
		if !semVerPattern.MatchString(chart.Version) || !digestPattern.MatchString(chart.Digest) {
			return fmt.Errorf("%s must contain an exact version and sha256 digest", name)
		}
	}
	return nil
}

func ValidateImageReference(reference string) error {
	trimmed := strings.TrimSpace(reference)
	parts := strings.Split(trimmed, "@")
	if len(parts) != 2 || !strings.Contains(parts[0], "/") || strings.Contains(strings.Split(parts[0], "/")[len(strings.Split(parts[0], "/"))-1], ":") || !digestPattern.MatchString(parts[1]) {
		return fmt.Errorf("must be a digest-pinned registry image reference, got %q", reference)
	}
	return nil
}

func (index ChannelIndex) Validate() error {
	if index.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported channel schema version %d", index.SchemaVersion)
	}
	for channel, entry := range index.Channels {
		if channel != "latest" && channel != "stable" {
			return fmt.Errorf("unsupported release channel %q", channel)
		}
		if !semVerPattern.MatchString(entry.Release) || !digestPattern.MatchString(entry.ManifestDigest) {
			return fmt.Errorf("channel %q must reference an exact release and manifest digest", channel)
		}
	}
	return nil
}

func Resolve(ctx context.Context, source Source, releaseVersion, channel string) (Resolved, error) {
	if source == nil {
		return Resolved{}, errors.New("release source is required")
	}
	releaseVersion = strings.TrimSpace(releaseVersion)
	channel = strings.TrimSpace(channel)
	if (releaseVersion == "") == (channel == "") {
		return Resolved{}, errors.New("exactly one of release or channel is required")
	}
	expectedDigest := ""
	if channel != "" {
		index, err := source.FetchChannelIndex(ctx)
		if err != nil {
			return Resolved{}, err
		}
		if err := index.Validate(); err != nil {
			return Resolved{}, err
		}
		entry, ok := index.Channels[channel]
		if !ok {
			return Resolved{}, fmt.Errorf("release channel %q was not found", channel)
		}
		releaseVersion, expectedDigest = entry.Release, entry.ManifestDigest
	}
	document, err := source.FetchManifest(ctx, releaseVersion)
	if err != nil {
		return Resolved{}, err
	}
	manifest, err := ParseManifest(document.Data)
	if err != nil {
		return Resolved{}, err
	}
	if manifest.Release != releaseVersion {
		return Resolved{}, fmt.Errorf("release source returned manifest %q for requested release %q", manifest.Release, releaseVersion)
	}
	if expectedDigest != "" && document.Digest != expectedDigest {
		return Resolved{}, fmt.Errorf("channel %q manifest digest does not match channel index", channel)
	}
	return Resolved{Manifest: manifest, ManifestDigest: document.Digest, Channel: channel}, nil
}

type FileSource struct{ root string }

func NewFileSource(location string) (*FileSource, error) {
	location = strings.TrimSpace(strings.TrimPrefix(location, "file://"))
	if location == "" {
		return nil, errors.New("release source path is required")
	}
	return &FileSource{root: filepath.Clean(location)}, nil
}

func (s *FileSource) FetchManifest(_ context.Context, version string) (Document, error) {
	return readDocument(filepath.Join(s.root, "releases", version+".json"))
}

func (s *FileSource) FetchChannelIndex(_ context.Context) (ChannelIndex, error) {
	document, err := readDocument(filepath.Join(s.root, "channels.json"))
	if err != nil {
		return ChannelIndex{}, err
	}
	var index ChannelIndex
	if decodeErr := json.Unmarshal(document.Data, &index); decodeErr != nil {
		return ChannelIndex{}, fmt.Errorf("decode channel index: %w", decodeErr)
	}
	return index, nil
}

func readDocument(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Document{}, fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if err != nil {
		return Document{}, err
	}
	return Document{Data: data, Digest: Digest(data)}, nil
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type FilePublisher struct{ root string }

func NewFilePublisher(location string) (*FilePublisher, error) {
	source, err := NewFileSource(location)
	if err != nil {
		return nil, err
	}
	return &FilePublisher{root: source.root}, nil
}

func (p *FilePublisher) PublishManifest(manifest Manifest) (string, error) {
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	path := filepath.Join(p.root, "releases", manifest.Release+".json")
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if string(existing) == string(data) {
			return Digest(existing), nil
		}
		return "", ErrManifestConflict
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", readErr
	}
	if err := writeAtomic(path, data); err != nil {
		return "", err
	}
	return Digest(data), nil
}

func (p *FilePublisher) PublishChannels(index ChannelIndex) error {
	if err := index.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(p.root, "channels.json"), append(data, '\n'))
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".coyote-release-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() {
		_ = os.Remove(name)
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func WriteValuesOverlay(w io.Writer, resolved Resolved) error {
	if err := resolved.Manifest.Validate(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "release:\n  version: %s\n  channel: %s\n  manifestDigest: %s\nimages:\n  server: %s\n  frontend: %s\n  worker: %s\n  migrate: %s\n",
		resolved.Manifest.Release, resolved.Channel, resolved.ManifestDigest,
		resolved.Manifest.Images["server"], resolved.Manifest.Images["frontend"],
		resolved.Manifest.Images["worker"], resolved.Manifest.Images["migrate"])
	return err
}

func isRequiredComponent(component string) bool {
	for _, required := range requiredComponents {
		if component == required {
			return true
		}
	}
	return false
}
