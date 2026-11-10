package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/storage"
)

const (
	SchemaVersion           = 1
	ManifestSchemaVersionV2 = 2
)

var (
	ErrNotFound             = errors.New("release document not found")
	ErrManifestConflict     = errors.New("release manifest already exists with different content")
	ErrChannelPromotionBusy = errors.New("channel promotion is already in progress")
	ErrUnsupportedBackend   = errors.New("release store backend is not supported")
	ErrInvalidLocation      = errors.New("invalid release store location")
	semVerPattern           = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	commitPattern           = regexp.MustCompile(`^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$`)
	digestPattern           = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

var (
	requiredV1Components = []string{"server", "frontend", "worker", "migrate"}
	requiredV2Components = []string{"server", "frontend", "worker", "mcp", "migrate"}
)

type Manifest struct {
	SchemaVersion int                  `json:"schema_version"`
	Release       string               `json:"release"`
	Source        SourceIdentity       `json:"source"`
	Images        map[string]string    `json:"images,omitempty"`
	Components    map[string]Component `json:"components,omitempty"`
	Deployment    *Deployment          `json:"deployment,omitempty"`
}

type Component struct {
	Version string `json:"version"`
	Image   string `json:"image"`
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

type Publisher interface {
	PublishManifest(context.Context, Manifest) (string, error)
	ImportManifest(context.Context, []byte) (Manifest, string, error)
	PromoteChannel(context.Context, string, string, string) error
	PromoteCoyoteLatestIfNewer(context.Context, string, string) (bool, error)
}

// Store combines release document reads with the supported publication actions.
// Provider-specific concurrency controls remain internal to each implementation.
type Store interface {
	Source
	Publisher
}

type Resolved struct {
	Manifest       Manifest
	ManifestDigest string
	Channel        string
}

func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if decodeErr := json.Unmarshal(data, &manifest); decodeErr != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", decodeErr)
	}
	if validateErr := manifest.Validate(); validateErr != nil {
		return Manifest{}, validateErr
	}
	return manifest, nil
}

func ValidateExactSemanticVersion(version string) error {
	if !semVerPattern.MatchString(version) {
		return fmt.Errorf("must be an exact semantic version: %q", version)
	}
	return nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion && m.SchemaVersion != ManifestSchemaVersionV2 {
		return fmt.Errorf("unsupported manifest schema version %d", m.SchemaVersion)
	}
	if versionErr := ValidateExactSemanticVersion(m.Release); versionErr != nil {
		return fmt.Errorf("release %w", versionErr)
	}
	if !commitPattern.MatchString(m.Source.Commit) {
		return fmt.Errorf("source commit must be a full Git commit SHA: %q", m.Source.Commit)
	}
	if _, timestampErr := time.Parse(time.RFC3339, m.Source.CreatedAt); timestampErr != nil {
		return fmt.Errorf("source created_at must be RFC3339: %w", timestampErr)
	}
	switch m.SchemaVersion {
	case SchemaVersion:
		if m.Components != nil {
			return errors.New("schema v1 manifest must not contain components")
		}
		if imageErr := validateImages(m.Images, requiredV1Components); imageErr != nil {
			return imageErr
		}
	case ManifestSchemaVersionV2:
		if m.Images != nil {
			return errors.New("schema v2 manifest must not contain legacy images")
		}
		if componentErr := validateComponents(m.Components); componentErr != nil {
			return componentErr
		}
	}
	return validateDeployment(m.Deployment)
}

func validateImages(images map[string]string, components []string) error {
	if len(images) != len(components) {
		return fmt.Errorf("manifest images must contain exactly %s", strings.Join(components, ", "))
	}
	for _, component := range components {
		image, ok := images[component]
		if !ok {
			return fmt.Errorf("manifest is missing %s image", component)
		}
		if imageErr := ValidateImageReference(image); imageErr != nil {
			return fmt.Errorf("%s image: %w", component, imageErr)
		}
	}
	for component := range images {
		if !isComponent(component, components) {
			return fmt.Errorf("manifest contains unknown image component %q", component)
		}
	}
	return nil
}

func validateComponents(components map[string]Component) error {
	if len(components) != len(requiredV2Components) {
		return fmt.Errorf("manifest components must contain exactly %s", strings.Join(requiredV2Components, ", "))
	}
	for _, name := range requiredV2Components {
		component, ok := components[name]
		if !ok {
			return fmt.Errorf("manifest is missing %s component", name)
		}
		if versionErr := ValidateExactSemanticVersion(component.Version); versionErr != nil {
			return fmt.Errorf("%s component version %w", name, versionErr)
		}
		if imageErr := ValidateImageReference(component.Image); imageErr != nil {
			return fmt.Errorf("%s component image: %w", name, imageErr)
		}
	}
	for name := range components {
		if !isComponent(name, requiredV2Components) {
			return fmt.Errorf("manifest contains unknown component %q", name)
		}
	}
	return nil
}

func validateDeployment(deployment *Deployment) error {
	if deployment == nil {
		return nil
	}
	for name, chart := range map[string]*Chart{"control_chart": deployment.ControlChart, "worker_chart": deployment.WorkerChart} {
		if chart == nil {
			continue
		}
		if ValidateExactSemanticVersion(chart.Version) != nil || !digestPattern.MatchString(chart.Digest) {
			return fmt.Errorf("%s must contain an exact version and sha256 digest", name)
		}
	}
	return nil
}

func ValidateImageReference(reference string) error {
	if reference != strings.TrimSpace(reference) {
		return fmt.Errorf("must not contain leading or trailing whitespace: %q", reference)
	}
	parts := strings.Split(reference, "@")
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
		if ValidateExactSemanticVersion(entry.Release) != nil || !digestPattern.MatchString(entry.ManifestDigest) {
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
		if validateErr := index.Validate(); validateErr != nil {
			return Resolved{}, validateErr
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

// OpenStore resolves a release source location to its backing store. The
// returned closer is non-nil only when the selected provider owns a client.
func OpenStore(ctx context.Context, location string) (Store, io.Closer, error) {
	return openStore(ctx, location, func(clientContext context.Context) (*storage.Client, error) {
		return storage.NewClient(clientContext)
	})
}

func openStore(ctx context.Context, location string, newGCSClient func(context.Context) (*storage.Client, error)) (Store, io.Closer, error) {
	trimmed := strings.TrimSpace(location)
	if strings.HasPrefix(trimmed, "file:") {
		if !strings.HasPrefix(trimmed, "file://") {
			return nil, nil, fmt.Errorf("%w: %q", ErrInvalidLocation, location)
		}
		store, storeErr := NewFileStore(trimmed)
		return store, nil, storeErr
	}
	if strings.HasPrefix(trimmed, "gs:") {
		if _, _, parseErr := parseGCSStoreLocation(trimmed); parseErr != nil {
			return nil, nil, parseErr
		}
		client, clientErr := newGCSClient(releaseContext(ctx))
		if clientErr != nil {
			return nil, nil, clientErr
		}
		store, storeErr := NewGCSStore(client, trimmed)
		if storeErr != nil {
			_ = client.Close()
			return nil, nil, storeErr
		}
		return store, client, nil
	}
	if strings.Contains(trimmed, "://") {
		parsed, parseErr := url.Parse(trimmed)
		if parseErr != nil || parsed.Scheme == "" {
			return nil, nil, fmt.Errorf("%w: %q", ErrInvalidLocation, location)
		}
		return nil, nil, fmt.Errorf("%w: %s", ErrUnsupportedBackend, parsed.Scheme)
	}
	store, storeErr := NewFileStore(trimmed)
	return store, nil, storeErr
}

func parseGCSStoreLocation(location string) (string, string, error) {
	parsed, parseErr := url.Parse(location)
	if parseErr != nil || parsed.Scheme != "gs" || parsed.Hostname() == "" || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !validGCSPrefix(parsed.EscapedPath()) {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidLocation, location)
	}
	return parsed.Hostname(), strings.Trim(parsed.Path, "/"), nil
}

func validGCSPrefix(escapedPath string) bool {
	prefix := strings.TrimPrefix(escapedPath, "/")
	if prefix == "" {
		return true
	}
	for _, escapedComponent := range strings.Split(prefix, "/") {
		component, unescapeErr := url.PathUnescape(escapedComponent)
		if unescapeErr != nil || component == "" || component == "." || component == ".." || strings.ContainsAny(component, "/\\") {
			return false
		}
	}
	return true
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

type FilePublisher struct {
	root   string
	source *FileSource
}

// NewFileStore returns the filesystem release store for a local path or file URL.
func NewFileStore(location string) (*FilePublisher, error) {
	source, err := NewFileSource(location)
	if err != nil {
		return nil, err
	}
	return &FilePublisher{root: source.root, source: source}, nil
}

func NewFilePublisher(location string) (*FilePublisher, error) {
	return NewFileStore(location)
}

func (p *FilePublisher) FetchManifest(ctx context.Context, version string) (Document, error) {
	return p.source.FetchManifest(ctx, version)
}

func (p *FilePublisher) FetchChannelIndex(ctx context.Context) (ChannelIndex, error) {
	return p.source.FetchChannelIndex(ctx)
}

func (p *FilePublisher) PublishManifest(ctx context.Context, manifest Manifest) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if validateErr := manifest.Validate(); validateErr != nil {
		return "", validateErr
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	return p.publishManifestData(ctx, manifest.Release, data)
}

func (p *FilePublisher) ImportManifest(ctx context.Context, data []byte) (Manifest, string, error) {
	manifest, parseErr := ParseManifest(data)
	if parseErr != nil {
		return Manifest{}, "", parseErr
	}
	digest, publishErr := p.publishManifestData(ctx, manifest.Release, data)
	if publishErr != nil {
		return Manifest{}, "", publishErr
	}
	return manifest, digest, nil
}

func (p *FilePublisher) publishManifestData(ctx context.Context, releaseVersion string, data []byte) (string, error) {
	ctx = releaseContext(ctx)
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	path := filepath.Join(p.root, "releases", releaseVersion+".json")
	releaseLock, lockErr := p.acquireLock(ctx, path+".lock")
	if lockErr != nil {
		return "", lockErr
	}
	defer func() {
		_ = os.Remove(releaseLock)
	}()
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if string(existing) == string(data) {
			return Digest(existing), nil
		}
		return "", ErrManifestConflict
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", readErr
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if writeErr := writeAtomic(path, data); writeErr != nil {
		return "", writeErr
	}
	return Digest(data), nil
}

func (p *FilePublisher) PublishChannels(index ChannelIndex) error {
	if validateErr := index.Validate(); validateErr != nil {
		return validateErr
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(p.root, "channels.json"), append(data, '\n'))
}

func (p *FilePublisher) PromoteChannel(ctx context.Context, channel, releaseVersion, manifestDigest string) error {
	if channel != "latest" && channel != "stable" {
		return fmt.Errorf("unsupported release channel %q", channel)
	}

	if !semVerPattern.MatchString(releaseVersion) || !digestPattern.MatchString(manifestDigest) {
		return errors.New("channel promotion requires an exact release and manifest digest")
	}
	releaseLock, lockErr := p.acquireLock(ctx, filepath.Join(p.root, ".channels.lock"))
	if lockErr != nil {
		return lockErr
	}
	defer func() {
		_ = os.Remove(releaseLock)
	}()

	index, indexErr := p.FetchChannelIndex(ctx)
	if errors.Is(indexErr, ErrNotFound) {
		index = ChannelIndex{SchemaVersion: SchemaVersion, Channels: map[string]ChannelEntry{}}
	} else if indexErr != nil {
		return indexErr
	}
	if validateErr := index.Validate(); validateErr != nil {
		return validateErr
	}
	index.Channels[channel] = ChannelEntry{Release: releaseVersion, ManifestDigest: manifestDigest}
	return p.PublishChannels(index)
}

func (p *FilePublisher) acquireLock(ctx context.Context, lockPath string) (string, error) {
	if mkdirErr := os.MkdirAll(filepath.Dir(lockPath), 0o755); mkdirErr != nil {
		return "", mkdirErr
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	for {
		if mkdirErr := os.Mkdir(lockPath, 0o700); mkdirErr == nil {
			return lockPath, nil
		} else if !errors.Is(mkdirErr, os.ErrExist) {
			return "", mkdirErr
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", ErrChannelPromotionBusy
		case <-retry.C:
		}
	}
}

func writeAtomic(path string, data []byte) error {
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		return mkdirErr
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".coyote-release-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() {
		_ = os.Remove(name)
	}()
	if _, writeErr := file.Write(data); writeErr != nil {
		_ = file.Close()
		return writeErr
	}
	if closeErr := file.Close(); closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

func WriteValuesOverlay(w io.Writer, resolved Resolved) error {
	if validateErr := resolved.Manifest.Validate(); validateErr != nil {
		return validateErr
	}
	images := resolved.Manifest.Images
	if resolved.Manifest.SchemaVersion == ManifestSchemaVersionV2 {
		images = make(map[string]string, len(resolved.Manifest.Components))
		for name, component := range resolved.Manifest.Components {
			images[name] = component.Image
		}
	}
	format := "release:\n  version: %s\n  channel: %s\n  manifestDigest: %s\nimages:\n  server: %s\n  frontend: %s\n  worker: %s\n  migrate: %s\n"
	arguments := []any{resolved.Manifest.Release, resolved.Channel, resolved.ManifestDigest, images["server"], images["frontend"], images["worker"], images["migrate"]}
	if resolved.Manifest.SchemaVersion == ManifestSchemaVersionV2 {
		format = "release:\n  version: %s\n  channel: %s\n  manifestDigest: %s\nimages:\n  server: %s\n  frontend: %s\n  worker: %s\n  migrate: %s\n  mcp: %s\n"
		arguments = append(arguments, images["mcp"])
	}
	_, err := fmt.Fprintf(w, format, arguments...)
	return err
}

func isComponent(component string, components []string) bool {
	for _, required := range components {
		if component == required {
			return true
		}
	}
	return false
}
