package pipeline

import (
	"strings"
	"testing"
)

func TestValidate_ApplicationVersion(t *testing.T) {
	tests := []struct {
		name    string
		version *ApplicationVersionDef
		wantErr string
	}{
		{name: "omitted"},
		{name: "value", version: &ApplicationVersionDef{Value: "2.5.1"}},
		{name: "template", version: &ApplicationVersionDef{Template: "0.4.{build_number}"}},
		{name: "file", version: &ApplicationVersionDef{File: "VERSION"}},
		{name: "multiple", version: &ApplicationVersionDef{Value: "2.5.1", Template: "0.4.{build_number}"}, wantErr: "exactly one"},
		{name: "unknown placeholder", version: &ApplicationVersionDef{Template: "1.0.0-{branch}"}, wantErr: "not supported"},
		{name: "unsafe file", version: &ApplicationVersionDef{File: "../VERSION"}, wantErr: "must not traverse"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			err := Validate(&PipelineFile{
				Version:  1,
				Pipeline: PipelineMeta{ApplicationVersion: testCase.version},
				Steps:    []StepDef{{Name: "build", Run: "true"}},
			})
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("validate pipeline: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("expected error containing %q, got %v", testCase.wantErr, err)
			}
		})
	}
}
