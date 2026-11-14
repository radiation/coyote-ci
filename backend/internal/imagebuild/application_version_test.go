package imagebuild

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestControllerApplicationVersionBuildArgument(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		applicationVersion *string
		wantVersion        string
		wantVersionPresent bool
	}{
		{name: "persisted version overrides pipeline argument", applicationVersion: stringPtr("1.2.3"), wantVersion: "1.2.3", wantVersionPresent: true},
		{name: "legacy build preserves pipeline arguments", wantVersion: "9.9.9", wantVersionPresent: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			execution := newExecutionFake(t)
			execution.build.ApplicationVersion = testCase.applicationVersion
			execution.build.SourceSHA = stringPtr("0123456789012345678901234567890123456789")
			spec := domain.ExecutionJobSpec{ExecutionKind: domain.ExecutionKindImageBuild, RemoteImageBuild: &domain.RemoteImageBuildSpec{
				ContextPath: "backend", DockerfilePath: "backend/Dockerfile", TargetImageReference: "acme/foo",
				BuildArgs: map[string]string{"COYOTE_APPLICATION_VERSION": "9.9.9", "UNRELATED": "preserved"},
			}}
			encoded, marshalErr := json.Marshal(spec)
			if marshalErr != nil {
				t.Fatalf("Marshal(): %v", marshalErr)
			}
			execution.job.ResolvedSpecJSON = string(encoded)
			builder := &builderFake{result: domain.ImageBuildResult{Status: domain.ImageBuildStatusRunning}}
			controller, newErr := NewController(execution, memoryrepo.NewExternalImageBuildRepository(), builder, &stagerFake{}, &sourceFake{})
			if newErr != nil {
				t.Fatalf("NewController(): %v", newErr)
			}
			if testCase.applicationVersion != nil {
				controller.WithPublicationInspector(&publicationInspectorFake{})
			}
			active, reconcileErr := controller.ReconcileClaimed(context.Background(), execution.step)
			if reconcileErr != nil || !active {
				t.Fatalf("ReconcileClaimed() active=%t err=%v", active, reconcileErr)
			}
			gotVersion, found := builder.request.Spec.BuildArgs["COYOTE_APPLICATION_VERSION"]
			if found != testCase.wantVersionPresent || gotVersion != testCase.wantVersion {
				t.Fatalf("version build arg=%q found=%t, want %q found=%t", gotVersion, found, testCase.wantVersion, testCase.wantVersionPresent)
			}
			if builder.request.Spec.BuildArgs["UNRELATED"] != "preserved" {
				t.Fatalf("unrelated build args=%v", builder.request.Spec.BuildArgs)
			}
			if testCase.applicationVersion != nil && (builder.request.Spec.BuildArgs["COYOTE_COMMIT"] != *execution.build.SourceSHA || builder.request.Spec.BuildArgs["COYOTE_BUILD_DATE"] == "") {
				t.Fatal("missing persisted build provenance")
			}
		})
	}
}

func stringPtr(value string) *string {
	return &value
}
