package release

import (
	"strings"
	"testing"
)

func TestCoyoteManifestRejectsInvalidBridgeInputs(t *testing.T) {
	valid := CoyoteReleaseInput{Status: "complete", ApplicationVersion: "0.0.41", SourceCommit: strings.Repeat("a", 40), CreatedAt: "2026-10-13T14:00:00Z", Outputs: []CoyoteReleaseOutput{
		{Key: CoyoteServerOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-server", ImmutableReference: "registry/coyote-server@sha256:" + strings.Repeat("a", 64)},
		{Key: CoyoteFrontendOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-frontend", ImmutableReference: "registry/coyote-frontend@sha256:" + strings.Repeat("b", 64)},
		{Key: CoyoteWorkerOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-worker", ImmutableReference: "registry/coyote-worker@sha256:" + strings.Repeat("c", 64)},
		{Key: CoyoteMigrateOutputKey, Kind: "image", Status: "published", TargetImageReference: "coyote-migrate", ImmutableReference: "registry/coyote-migrate@sha256:" + strings.Repeat("d", 64)},
	}}
	for _, mutate := range []func(*CoyoteReleaseInput){
		func(input *CoyoteReleaseInput) { input.Status = "pending" },
		func(input *CoyoteReleaseInput) { input.CreatedAt = "bad" },
		func(input *CoyoteReleaseInput) { input.Outputs = input.Outputs[:3] },
		func(input *CoyoteReleaseInput) { input.Outputs[0].TargetImageReference = "wrong" },
		func(input *CoyoteReleaseInput) { input.Outputs[1].Key = CoyoteServerOutputKey },
	} {
		input := valid
		input.Outputs = append([]CoyoteReleaseOutput(nil), valid.Outputs...)
		mutate(&input)
		if _, err := CoyoteManifest(input); err == nil {
			t.Fatal("expected bridge validation failure")
		}
	}
}
