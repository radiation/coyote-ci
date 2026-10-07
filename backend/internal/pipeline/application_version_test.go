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
		{name: "empty", version: &ApplicationVersionDef{}, wantErr: "exactly one"},
		{name: "whitespace only", version: &ApplicationVersionDef{Value: " ", Template: "\t", File: "\n"}, wantErr: "exactly one"},
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

func TestParseAndValidate_RejectsEmptyApplicationVersionDeclaration(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "empty object",
			yaml: `
version: 1
pipeline:
  application_version: {}
steps:
  - name: build
    run: true
`,
		},
		{
			name: "whitespace sources",
			yaml: `
version: 1
pipeline:
  application_version:
    value: " "
    template: "\t"
    file: "  "
steps:
  - name: build
    run: true
`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, parseErr := ParseAndValidate([]byte(testCase.yaml))
			if parseErr == nil || !strings.Contains(parseErr.Error(), "pipeline.application_version") || !strings.Contains(parseErr.Error(), "exactly one") {
				t.Fatalf("expected empty application version validation error, got %v", parseErr)
			}
		})
	}
}
