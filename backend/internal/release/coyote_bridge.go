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
)

type CoyoteReleaseOutput struct {
	Key                  string
	Kind                 string
	Status               string
	TargetImageReference string
	ImmutableReference   string
}

type CoyoteReleaseInput struct {
	Status             string
	ApplicationVersion string
	SourceCommit       string
	CreatedAt          string
	Outputs            []CoyoteReleaseOutput
}

var coyoteOutputComponents = map[string]struct {
	component string
	target    string
}{
	CoyoteServerOutputKey:   {component: "server", target: "coyote-server"},
	CoyoteFrontendOutputKey: {component: "frontend", target: "coyote-frontend"},
	CoyoteWorkerOutputKey:   {component: "worker", target: "coyote-worker"},
	CoyoteMigrateOutputKey:  {component: "migrate", target: "coyote-migrate"},
}

func CoyoteManifest(input CoyoteReleaseInput) (Manifest, error) {
	if strings.TrimSpace(input.Status) != "complete" {
		return Manifest{}, errors.New("generic application release must be complete")
	}
	if len(input.Outputs) != len(coyoteOutputComponents) {
		return Manifest{}, errors.New("generic application release must contain exactly four Coyote image outputs")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(input.CreatedAt)); err != nil {
		return Manifest{}, fmt.Errorf("build created_at must be RFC3339: %w", err)
	}

	images := make(map[string]string, len(coyoteOutputComponents))
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
		if _, duplicate := images[expected.component]; duplicate {
			return Manifest{}, fmt.Errorf("duplicate Coyote release component %q", expected.component)
		}
		if err := ValidateImageReference(output.ImmutableReference); err != nil {
			return Manifest{}, fmt.Errorf("coyote release output %q: %w", output.Key, err)
		}
		images[expected.component] = output.ImmutableReference
	}

	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		Release:       strings.TrimSpace(input.ApplicationVersion),
		Source: SourceIdentity{
			Commit:    strings.TrimSpace(input.SourceCommit),
			CreatedAt: strings.TrimSpace(input.CreatedAt),
		},
		Images: images,
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

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
