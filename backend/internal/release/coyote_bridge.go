package release

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	CoyoteServerOutputKey   = "image:coyote-server-image"
	CoyoteFrontendOutputKey = "image:coyote-frontend-image"
	CoyoteWorkerOutputKey   = "image:coyote-worker-image"
	CoyoteMigrateOutputKey  = "image:coyote-migrate-image"
	CoyoteMCPOutputKey      = "image:coyote-mcp-image"
)

type CoyoteReleaseOutput struct {
	Key                  string
	Kind                 string
	Status               string
	TargetImageReference string
	ImmutableReference   string
}

type CoyoteReleaseInput struct {
	BuildID            string
	Status             string
	ApplicationVersion string
	SourceCommit       string
	CreatedAt          string
	Outputs            []CoyoteReleaseOutput
	Helm               *HelmPackage
	CLIArtifacts       map[string]CLIArtifact
	Compatibility      *Compatibility
}

var coyoteOutputComponents = map[string]struct {
	component string
	target    string
}{
	CoyoteServerOutputKey:   {component: "server", target: "coyote-server"},
	CoyoteFrontendOutputKey: {component: "frontend", target: "coyote-frontend"},
	CoyoteWorkerOutputKey:   {component: "worker", target: "coyote-worker"},
	CoyoteMigrateOutputKey:  {component: "migrate", target: "coyote-migrate"},
	CoyoteMCPOutputKey:      {component: "mcp", target: "coyote-mcp"},
}

func CoyoteManifest(input CoyoteReleaseInput) (Manifest, error) {
	if strings.TrimSpace(input.Status) != "complete" {
		return Manifest{}, errors.New("generic application release must be complete")
	}
	if len(input.Outputs) != len(coyoteOutputComponents) {
		return Manifest{}, errors.New("generic application release must contain exactly five Coyote image outputs")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(input.CreatedAt)); err != nil {
		return Manifest{}, fmt.Errorf("build created_at must be RFC3339: %w", err)
	}

	components := make(map[string]Component, len(coyoteOutputComponents))
	source := SourceIdentity{Commit: strings.TrimSpace(input.SourceCommit), CreatedAt: strings.TrimSpace(input.CreatedAt)}
	for _, output := range input.Outputs {
		expected, found := coyoteOutputComponents[strings.TrimSpace(output.Key)]
		if !found {
			return Manifest{}, fmt.Errorf("unexpected Coyote release output %q", output.Key)
		}
		if strings.TrimSpace(output.Kind) != "image" || strings.TrimSpace(output.Status) != "published" {
			return Manifest{}, fmt.Errorf("coyote release output %q must be a published image", output.Key)
		}
		if strings.TrimSpace(output.TargetImageReference) != expected.target {
			return Manifest{}, fmt.Errorf("coyote release output %q must target %q", output.Key, expected.target)
		}
		if _, duplicate := components[expected.component]; duplicate {
			return Manifest{}, fmt.Errorf("duplicate Coyote release component %q", expected.component)
		}
		if err := ValidateImageReference(output.ImmutableReference); err != nil {
			return Manifest{}, fmt.Errorf("coyote release output %q: %w", output.Key, err)
		}
		components[expected.component] = Component{Version: input.ApplicationVersion, Image: output.ImmutableReference, Build: &BuildIdentity{ID: input.BuildID, Source: source}}
	}

	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersionV3,
		Release:       strings.TrimSpace(input.ApplicationVersion),
		Assembly:      &Assembly{BuildID: input.BuildID, Source: source},
		Components:    components,
		Helm:          input.Helm,
		CLIArtifacts:  input.CLIArtifacts,
		Compatibility: input.Compatibility,
	}
	for _, platform := range CoyoteCLIPlatforms {
		artifact, found := manifest.CLIArtifacts[platform]
		if !found || artifact.BuildID != input.BuildID {
			return Manifest{}, fmt.Errorf("CLI artifact %s must belong to release build %s", platform, input.BuildID)
		}
	}
	if len(manifest.CLIArtifacts) != len(CoyoteCLIPlatforms) {
		return Manifest{}, errors.New("unexpected CLI artifact platform")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

var CoyoteCLIPlatforms = []string{"darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64"}

func ShouldAdvanceCoyoteLatest(ctx context.Context, source Source, candidate string) (bool, error) {
	index, err := source.FetchChannelIndex(ctx)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err := index.Validate(); err != nil {
		return false, err
	}
	current, found := index.Channels["latest"]
	if !found {
		return true, nil
	}
	currentNumber, currentOK := coyoteBuildNumber(current.Release)
	candidateNumber, candidateOK := coyoteBuildNumber(candidate)
	if !currentOK || !candidateOK {
		return false, errors.New("automatic Coyote latest requires 0.0.<build_number> releases")
	}
	return candidateNumber >= currentNumber, nil
}

func coyoteBuildNumber(version string) (int64, bool) {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) != 3 || parts[0] != "0" || parts[1] != "0" {
		return 0, false
	}
	number, err := strconv.ParseInt(parts[2], 10, 64)
	return number, err == nil && number >= 0
}
