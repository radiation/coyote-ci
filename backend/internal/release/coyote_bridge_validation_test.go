package release

import "testing"

func TestCoyoteManifestRejectsInvalidBridgeInputs(t *testing.T) {
	for _, mutate := range []func(*CoyoteReleaseInput){
		func(input *CoyoteReleaseInput) { input.Status = "pending" },
		func(input *CoyoteReleaseInput) { input.CreatedAt = "bad" },
		func(input *CoyoteReleaseInput) { input.Outputs = input.Outputs[:3] },
		func(input *CoyoteReleaseInput) { input.Outputs[0].TargetImageReference = "wrong" },
		func(input *CoyoteReleaseInput) { input.Outputs[1].Key = CoyoteServerOutputKey },
		func(input *CoyoteReleaseInput) { input.Outputs[0].Kind = "artifact" },
		func(input *CoyoteReleaseInput) { input.Outputs[0].Key = "unknown" },
		func(input *CoyoteReleaseInput) { input.Outputs[0].Status = "failed" },
		func(input *CoyoteReleaseInput) { input.Helm = nil },
		func(input *CoyoteReleaseInput) { input.Helm.Version = "0.0.40" },
		func(input *CoyoteReleaseInput) { input.BuildID = "" },
		func(input *CoyoteReleaseInput) { input.Compatibility.SchemaTarget = "057" },
		func(input *CoyoteReleaseInput) { input.CLIArtifacts["unexpected"] = CLIArtifact{} },
		func(input *CoyoteReleaseInput) { delete(input.CLIArtifacts, "linux-amd64") },
		func(input *CoyoteReleaseInput) {
			artifact := input.CLIArtifacts["linux-amd64"]
			artifact.SHA256 = "invalid"
			input.CLIArtifacts["linux-amd64"] = artifact
		},
	} {
		input := validCoyoteReleaseInput()
		mutate(&input)
		if _, err := CoyoteManifest(input); err == nil {
			t.Fatal("expected bridge validation failure")
		}
	}
	for index := range validCoyoteReleaseInput().Outputs {
		input := validCoyoteReleaseInput()
		input.Outputs = append(input.Outputs[:index], input.Outputs[index+1:]...)
		if _, err := CoyoteManifest(input); err == nil {
			t.Fatalf("missing component %d accepted", index)
		}
	}
}
