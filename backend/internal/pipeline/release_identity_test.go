package pipeline

import (
	"strings"
	"testing"
)

func TestValidateReleaseImageIdentityCollision(t *testing.T) {
	image := func(name string) StepDef {
		return StepDef{Name: name, ImageBuild: &ImageBuildDef{Context: ".", Dockerfile: "Dockerfile", Image: "example/image"}}
	}
	file := &PipelineFile{Version: 1, Pipeline: PipelineMeta{
		ApplicationVersion: &ApplicationVersionDef{Value: "1.2.3"},
		Release:            &ReleaseDef{Images: []string{"Foo Bar", "Foo-Bar", "Foo_Bar"}},
	}, Steps: []StepDef{image("Foo Bar"), image("Foo-Bar"), image("Foo_Bar")}}
	if err := Validate(file); err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("Validate() error=%v, want collision", err)
	}
}
