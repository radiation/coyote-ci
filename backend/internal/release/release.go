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
	"golang.org/x/mod/semver"
)

const (
	SchemaVersion           = 1
	ManifestSchemaVersionV2 = 2
	ManifestSchemaVersionV3 = 3
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
	sha256Pattern           = regexp.MustCompile(`^[a-f0-9]{64}$`)
	gooseVersionPattern     = regexp.MustCompile(`^[1-9][0-9]*$`)
	platformPattern         = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)+$`)
	upgradeRangePattern     = regexp.MustCompile(`^>=(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)? <(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
)

var (
	requiredV1Components = []string{"server", "frontend", "worker", "migrate"}
	requiredV2Components = []string{"server", "frontend", "worker", "mcp", "migrate"}
)

type Manifest struct {
	SchemaVersion int                    `json:"schema_version"`
	Release       string                 `json:"release"`
	Source        SourceIdentity         `json:"source,omitempty"`
	Images        map[string]string      `json:"images,omitempty"`
	Components    map[string]Component   `json:"components,omitempty"`
	Deployment    *Deployment            `json:"deployment,omitempty"`
	Assembly      *Assembly              `json:"assembly,omitempty"`
	Helm          *HelmPackage           `json:"helm,omitempty"`
	CLIArtifacts  map[string]CLIArtifact `json:"cli_artifacts,omitempty"`
	Compatibility *Compatibility         `json:"compatibility,omitempty"`
}

type Component struct {
	Version string         `json:"version"`
	Image   string         `json:"image"`
	Build   *BuildIdentity `json:"build,omitempty"`
}

type SourceIdentity struct {
	Commit    string `json:"commit"`
	CreatedAt string `json:"created_at"`
}

type BuildIdentity struct {
	ID     string         `json:"id"`
	Source SourceIdentity `json:"source"`
}

type Assembly struct {
	BuildID string         `json:"build_id"`
	Source  SourceIdentity `json:"source"`
}

type HelmPackage struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Version    string `json:"version"`
	Digest     string `json:"digest"`
}

type CLIArtifact struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	BuildID  string `json:"build_id"`
}

type Compatibility struct {
	SchemaTarget string `json:"schema_target"`
	UpgradeFrom  string `json:"upgrade_from"`
	Maintenance  string `json:"maintenance"`
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

type ChannelPromotionResult struct {
	Previous      *ChannelEntry
	PreviousKnown bool
	Current       ChannelEntry
	Changed       bool
	ChangedKnown  bool
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
	PromoteChannel(context.Context, string, string, string) (ChannelPromotionResult, error)
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
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if decodeErr := json.Unmarshal(data, &header); decodeErr != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", decodeErr)
	}
	var manifest Manifest
	var decodeErr error
	if header.SchemaVersion == SchemaVersion || header.SchemaVersion == ManifestSchemaVersionV2 {
		manifest, decodeErr = decodeLegacyManifest(data)
	} else {
		decodeErr = json.Unmarshal(data, &manifest)
	}
	if decodeErr != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", decodeErr)
	}
	if manifest.SchemaVersion == ManifestSchemaVersionV3 {
		var fields map[string]json.RawMessage
		if decodeErr := json.Unmarshal(data, &fields); decodeErr != nil {
			return Manifest{}, fmt.Errorf("decode schema v3 release manifest fields: %w", decodeErr)
		}
		if fieldErr := validateV3ManifestFields(fields); fieldErr != nil {
			return Manifest{}, fieldErr
		}
	}
	if validateErr := manifest.Validate(); validateErr != nil {
		return Manifest{}, validateErr
	}
	return manifest, nil
}

func decodeLegacyManifest(data []byte) (Manifest, error) {
	// V3 field names were ignored extensions in V1/V2, regardless of JSON type.
	type legacyComponent struct {
		Version string `json:"version"`
		Image   string `json:"image"`
	}
	var wire struct {
		SchemaVersion int                        `json:"schema_version"`
		Release       string                     `json:"release"`
		Source        SourceIdentity             `json:"source"`
		Images        map[string]string          `json:"images"`
		Components    map[string]legacyComponent `json:"components"`
		Deployment    *Deployment                `json:"deployment"`
	}
	if decodeErr := json.Unmarshal(data, &wire); decodeErr != nil {
		return Manifest{}, decodeErr
	}
	manifest := Manifest{
		SchemaVersion: wire.SchemaVersion,
		Release:       wire.Release,
		Source:        wire.Source,
		Images:        wire.Images,
		Deployment:    wire.Deployment,
	}
	if wire.Components != nil {
		manifest.Components = make(map[string]Component, len(wire.Components))
		for name, component := range wire.Components {
			manifest.Components[name] = Component{Version: component.Version, Image: component.Image}
		}
	}
	return manifest, nil
}

func (m Manifest) MarshalJSON() ([]byte, error) {
	type manifestJSON struct {
		SchemaVersion int                    `json:"schema_version"`
		Release       string                 `json:"release"`
		Source        *SourceIdentity        `json:"source,omitempty"`
		Images        map[string]string      `json:"images,omitempty"`
		Components    map[string]Component   `json:"components,omitempty"`
		Deployment    *Deployment            `json:"deployment,omitempty"`
		Assembly      *Assembly              `json:"assembly,omitempty"`
		Helm          *HelmPackage           `json:"helm,omitempty"`
		CLIArtifacts  map[string]CLIArtifact `json:"cli_artifacts,omitempty"`
		Compatibility *Compatibility         `json:"compatibility,omitempty"`
	}
	document := manifestJSON{
		SchemaVersion: m.SchemaVersion,
		Release:       m.Release,
		Images:        m.Images,
		Components:    m.Components,
		Deployment:    m.Deployment,
		Assembly:      m.Assembly,
		Helm:          m.Helm,
		CLIArtifacts:  m.CLIArtifacts,
		Compatibility: m.Compatibility,
	}
	if m.SchemaVersion != ManifestSchemaVersionV3 {
		document.Source = &m.Source
	}
	return json.Marshal(document)
}

func validateV3ManifestFields(fields map[string]json.RawMessage) error {
	if fieldErr := validateJSONFields(fields, "schema v3 manifest", "schema_version", "release", "assembly", "components", "helm", "cli_artifacts", "compatibility"); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := validateV3AssemblyFields(fields["assembly"]); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := validateV3ComponentFields(fields["components"]); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := validateV3HelmFields(fields["helm"]); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := validateV3CLIArtifactFields(fields["cli_artifacts"]); fieldErr != nil {
		return fieldErr
	}
	return validateV3CompatibilityFields(fields["compatibility"])
}

func validateV3AssemblyFields(raw json.RawMessage) error {
	fields, fieldsErr := decodeJSONObject(raw, "assembly")
	if fieldsErr != nil {
		return fieldsErr
	}
	if fieldErr := validateJSONFields(fields, "assembly", "build_id", "source"); fieldErr != nil {
		return fieldErr
	}
	return validateV3SourceFields(fields["source"], "assembly source")
}

func validateV3ComponentFields(raw json.RawMessage) error {
	components, componentsErr := decodeJSONObject(raw, "components")
	if componentsErr != nil {
		return componentsErr
	}
	for name, component := range components {
		if !isComponent(name, requiredV2Components) {
			return fmt.Errorf("components contains unsupported field %q", name)
		}
		fields, fieldsErr := decodeJSONObject(component, name+" component")
		if fieldsErr != nil {
			return fieldsErr
		}
		if fieldErr := validateJSONFields(fields, name+" component", "version", "image", "build"); fieldErr != nil {
			return fieldErr
		}
		if buildErr := validateV3BuildFields(fields["build"], name+" component build"); buildErr != nil {
			return buildErr
		}
	}
	return nil
}

func validateV3BuildFields(raw json.RawMessage, field string) error {
	fields, fieldsErr := decodeJSONObject(raw, field)
	if fieldsErr != nil {
		return fieldsErr
	}
	if fieldErr := validateJSONFields(fields, field, "id", "source"); fieldErr != nil {
		return fieldErr
	}
	return validateV3SourceFields(fields["source"], field+" source")
}

func validateV3HelmFields(raw json.RawMessage) error {
	fields, fieldsErr := decodeJSONObject(raw, "helm")
	if fieldsErr != nil {
		return fieldsErr
	}
	return validateJSONFields(fields, "helm", "name", "repository", "version", "digest")
}

func validateV3CLIArtifactFields(raw json.RawMessage) error {
	artifacts, artifactsErr := decodeJSONObject(raw, "cli_artifacts")
	if artifactsErr != nil {
		return artifactsErr
	}
	for platform, artifact := range artifacts {
		fields, fieldsErr := decodeJSONObject(artifact, "CLI artifact "+platform)
		if fieldsErr != nil {
			return fieldsErr
		}
		if fieldErr := validateJSONFields(fields, "CLI artifact "+platform, "url", "filename", "size", "sha256", "build_id"); fieldErr != nil {
			return fieldErr
		}
	}
	return nil
}

func validateV3CompatibilityFields(raw json.RawMessage) error {
	fields, fieldsErr := decodeJSONObject(raw, "compatibility")
	if fieldsErr != nil {
		return fieldsErr
	}
	return validateJSONFields(fields, "compatibility", "schema_target", "upgrade_from", "maintenance")
}

func validateV3SourceFields(raw json.RawMessage, field string) error {
	fields, fieldsErr := decodeJSONObject(raw, field)
	if fieldsErr != nil {
		return fieldsErr
	}
	return validateJSONFields(fields, field, "commit", "created_at")
}

func decodeJSONObject(raw json.RawMessage, field string) (map[string]json.RawMessage, error) {
	if raw == nil {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if decodeErr := json.Unmarshal(raw, &fields); decodeErr != nil {
		return nil, fmt.Errorf("%s must be an object: %w", field, decodeErr)
	}
	return fields, nil
}

func validateJSONFields(fields map[string]json.RawMessage, object string, allowed ...string) error {
	allowedFields := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		allowedFields[field] = struct{}{}
	}
	for field := range fields {
		if _, ok := allowedFields[field]; !ok {
			return fmt.Errorf("%s contains unsupported field %q", object, field)
		}
	}
	return nil
}

func ValidateExactSemanticVersion(version string) error {
	if !semVerPattern.MatchString(version) {
		return fmt.Errorf("must be an exact semantic version: %q", version)
	}
	return nil
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion && m.SchemaVersion != ManifestSchemaVersionV2 && m.SchemaVersion != ManifestSchemaVersionV3 {
		return fmt.Errorf("unsupported manifest schema version %d", m.SchemaVersion)
	}
	if versionErr := ValidateExactSemanticVersion(m.Release); versionErr != nil {
		return fmt.Errorf("release %w", versionErr)
	}
	switch m.SchemaVersion {
	case SchemaVersion:
		if extensionErr := validateLegacyManifestExtensions(m); extensionErr != nil {
			return extensionErr
		}
		if sourceErr := validateSource(m.Source, "source"); sourceErr != nil {
			return sourceErr
		}
		if m.Components != nil {
			return errors.New("schema v1 manifest must not contain components")
		}
		if imageErr := validateImages(m.Images, requiredV1Components); imageErr != nil {
			return imageErr
		}
	case ManifestSchemaVersionV2:
		if extensionErr := validateLegacyManifestExtensions(m); extensionErr != nil {
			return extensionErr
		}
		if sourceErr := validateSource(m.Source, "source"); sourceErr != nil {
			return sourceErr
		}
		if m.Images != nil {
			return errors.New("schema v2 manifest must not contain legacy images")
		}
		if componentErr := validateComponents(m.Components, false); componentErr != nil {
			return componentErr
		}
	case ManifestSchemaVersionV3:
		if v3Err := validateV3Manifest(m); v3Err != nil {
			return v3Err
		}
	}
	return validateDeployment(m.Deployment)
}

func validateLegacyManifestExtensions(m Manifest) error {
	if m.Assembly != nil || m.Helm != nil || m.CLIArtifacts != nil || m.Compatibility != nil {
		return errors.New("schema v1 and v2 manifests must not contain schema v3 fields")
	}
	return nil
}

func validateSource(source SourceIdentity, field string) error {
	if !commitPattern.MatchString(source.Commit) {
		return fmt.Errorf("%s commit must be a full Git commit SHA: %q", field, source.Commit)
	}
	if _, timestampErr := time.Parse(time.RFC3339, source.CreatedAt); timestampErr != nil {
		return fmt.Errorf("%s created_at must be RFC3339: %w", field, timestampErr)
	}
	return nil
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

func validateComponents(components map[string]Component, requireBuildProvenance bool) error {
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
		if requireBuildProvenance {
			if component.Build == nil {
				return fmt.Errorf("%s component build provenance is required", name)
			}
			if buildErr := validateBuildIdentity(*component.Build, name+" component build"); buildErr != nil {
				return buildErr
			}
		} else if component.Build != nil {
			return fmt.Errorf("%s component build provenance is only supported by schema v3", name)
		}
	}
	for name := range components {
		if !isComponent(name, requiredV2Components) {
			return fmt.Errorf("manifest contains unknown component %q", name)
		}
	}
	return nil
}

func validateV3Manifest(m Manifest) error {
	if m.Source != (SourceIdentity{}) || m.Images != nil || m.Deployment != nil {
		return errors.New("schema v3 manifest must use assembly, components, helm, cli_artifacts, and compatibility fields only")
	}
	if m.Assembly == nil {
		return errors.New("schema v3 manifest assembly provenance is required")
	}
	if strings.TrimSpace(m.Assembly.BuildID) == "" {
		return errors.New("assembly build_id is required")
	}
	if sourceErr := validateSource(m.Assembly.Source, "assembly source"); sourceErr != nil {
		return sourceErr
	}
	if componentErr := validateComponents(m.Components, true); componentErr != nil {
		return componentErr
	}
	if helmErr := validateHelmPackage(m.Helm, m.Release); helmErr != nil {
		return helmErr
	}
	if artifactsErr := validateCLIArtifacts(m.CLIArtifacts); artifactsErr != nil {
		return artifactsErr
	}
	return validateCompatibility(m.Compatibility)
}

func validateBuildIdentity(build BuildIdentity, field string) error {
	if strings.TrimSpace(build.ID) == "" {
		return fmt.Errorf("%s id is required", field)
	}
	return validateSource(build.Source, field+" source")
}

func validateHelmPackage(helm *HelmPackage, releaseVersion string) error {
	if helm == nil {
		return errors.New("schema v3 manifest helm package is required")
	}
	if helm.Name != "coyote-ci" {
		return errors.New(`helm package name must be "coyote-ci"`)
	}
	if strings.TrimSpace(helm.Repository) != helm.Repository || !strings.HasPrefix(helm.Repository, "oci://") {
		return fmt.Errorf("helm repository must be an OCI repository: %q", helm.Repository)
	}
	parsed, parseErr := url.Parse(helm.Repository)
	if parseErr != nil || parsed.Host == "" || parsed.Path == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("helm repository must be an OCI repository: %q", helm.Repository)
	}
	if helm.Version != releaseVersion {
		return fmt.Errorf("helm package version %q must equal release %q", helm.Version, releaseVersion)
	}
	if digestPattern.MatchString(helm.Digest) {
		return nil
	}
	return errors.New("helm package digest must be a sha256 digest")
}

func validateCLIArtifacts(artifacts map[string]CLIArtifact) error {
	if len(artifacts) == 0 {
		return errors.New("schema v3 manifest must contain at least one CLI artifact")
	}
	for platform, artifact := range artifacts {
		if !platformPattern.MatchString(platform) {
			return fmt.Errorf("CLI artifact platform %q must identify an OS and architecture", platform)
		}
		parsed, parseErr := url.Parse(artifact.URL)
		if parseErr != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("CLI artifact %q url must be an absolute HTTP(S) download URL", platform)
		}
		if strings.TrimSpace(artifact.Filename) == "" || strings.ContainsAny(artifact.Filename, `/\`) {
			return fmt.Errorf("CLI artifact %q filename must be a file name", platform)
		}
		if artifact.Size <= 0 {
			return fmt.Errorf("CLI artifact %q size must be greater than zero", platform)
		}
		if !sha256Pattern.MatchString(artifact.SHA256) {
			return fmt.Errorf("CLI artifact %q sha256 must be a lowercase SHA-256 checksum", platform)
		}
		if strings.TrimSpace(artifact.BuildID) == "" {
			return fmt.Errorf("CLI artifact %q build_id is required", platform)
		}
	}
	return nil
}

func validateCompatibility(compatibility *Compatibility) error {
	if compatibility == nil {
		return errors.New("schema v3 manifest compatibility is required")
	}
	if !gooseVersionPattern.MatchString(compatibility.SchemaTarget) {
		return errors.New("compatibility schema_target must be a nonzero decimal Goose migration version")
	}
	if !upgradeRangePattern.MatchString(compatibility.UpgradeFrom) {
		return errors.New("compatibility upgrade_from must use the form \">=MIN_VERSION <MAX_VERSION\"")
	}
	rangeParts := strings.Split(strings.TrimPrefix(compatibility.UpgradeFrom, ">="), " <")
	if semver.Compare("v"+rangeParts[0], "v"+rangeParts[1]) >= 0 {
		return errors.New("compatibility upgrade_from must be a nonempty ascending version interval")
	}
	switch compatibility.Maintenance {
	case "active", "maintenance", "end-of-life":
		return nil
	default:
		return fmt.Errorf("unsupported compatibility maintenance state %q", compatibility.Maintenance)
	}
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

func (p *FilePublisher) PromoteChannel(ctx context.Context, channel, releaseVersion, manifestDigest string) (ChannelPromotionResult, error) {
	if channel != "latest" && channel != "stable" {
		return ChannelPromotionResult{}, fmt.Errorf("unsupported release channel %q", channel)
	}

	if !semVerPattern.MatchString(releaseVersion) || !digestPattern.MatchString(manifestDigest) {
		return ChannelPromotionResult{}, errors.New("channel promotion requires an exact release and manifest digest")
	}
	releaseLock, lockErr := p.acquireLock(ctx, filepath.Join(p.root, ".channels.lock"))
	if lockErr != nil {
		return ChannelPromotionResult{}, lockErr
	}
	defer func() {
		_ = os.Remove(releaseLock)
	}()

	index, indexErr := p.FetchChannelIndex(ctx)
	if errors.Is(indexErr, ErrNotFound) {
		index = ChannelIndex{SchemaVersion: SchemaVersion, Channels: map[string]ChannelEntry{}}
	} else if indexErr != nil {
		return ChannelPromotionResult{}, indexErr
	}
	if validateErr := index.Validate(); validateErr != nil {
		return ChannelPromotionResult{}, validateErr
	}
	current := ChannelEntry{Release: releaseVersion, ManifestDigest: manifestDigest}
	result := ChannelPromotionResult{Current: current, PreviousKnown: true, ChangedKnown: true}
	if previous, found := index.Channels[channel]; found {
		result.Previous = &previous
		if previous == current {
			return result, nil
		}
	}
	index.Channels[channel] = current
	if publishErr := p.PublishChannels(index); publishErr != nil {
		return ChannelPromotionResult{}, publishErr
	}
	result.Changed = true
	return result, nil
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
	if resolved.Manifest.SchemaVersion == ManifestSchemaVersionV2 || resolved.Manifest.SchemaVersion == ManifestSchemaVersionV3 {
		images = make(map[string]string, len(resolved.Manifest.Components))
		for name, component := range resolved.Manifest.Components {
			images[name] = component.Image
		}
	}
	format := "release:\n  version: %s\n  channel: %q\n  manifestDigest: %s\nimages:\n  server: %s\n  frontend: %s\n  worker: %s\n  migrate: %s\n"
	arguments := []any{resolved.Manifest.Release, resolved.Channel, resolved.ManifestDigest, images["server"], images["frontend"], images["worker"], images["migrate"]}
	if resolved.Manifest.SchemaVersion == ManifestSchemaVersionV2 || resolved.Manifest.SchemaVersion == ManifestSchemaVersionV3 {
		format = "release:\n  version: %s\n  channel: %q\n  manifestDigest: %s\nimages:\n  server: %s\n  frontend: %s\n  worker: %s\n  migrate: %s\n  mcp: %s\n"
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
