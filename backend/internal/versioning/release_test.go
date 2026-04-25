package versioning

import (
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{name: "manual default", config: Config{Version: "release-2026-04"}},
		{name: "semver patch", config: Config{Strategy: "semver-patch", Version: "1.2"}},
		{name: "template", config: Config{Strategy: "template", Template: "1.2.{build_number}"}},
		{name: "missing manual version", config: Config{}, wantErr: true},
		{name: "bad semver patch", config: Config{Strategy: "semver-patch", Version: "1.2.3"}, wantErr: true},
		{name: "bad template placeholder", config: Config{Strategy: "template", Template: "1.2.{branch}"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	build := domain.Build{BuildNumber: 42, AttemptNumber: 2}
	commitSHA := "0123456789abcdef"
	build.CommitSHA = &commitSHA

	manual, err := ResolveVersion(ResolveInput{Config: Config{Version: "1.2"}, Build: build})
	if err != nil {
		t.Fatalf("unexpected manual error: %v", err)
	}
	if manual != "1.2" {
		t.Fatalf("expected manual version 1.2, got %q", manual)
	}

	semver, err := ResolveVersion(ResolveInput{Config: Config{Strategy: "semver-patch", Version: "1.2"}, ExistingVersions: []string{"1.2.0", "1.2.5", "2.0.0"}})
	if err != nil {
		t.Fatalf("unexpected semver error: %v", err)
	}
	if semver != "1.2.6" {
		t.Fatalf("expected semver version 1.2.6, got %q", semver)
	}

	templated, err := ResolveVersion(ResolveInput{Config: Config{Strategy: "template", Template: "1.2.{build_number}-{short_commit_sha}"}, Build: build})
	if err != nil {
		t.Fatalf("unexpected template error: %v", err)
	}
	if templated != "1.2.42-01234567" {
		t.Fatalf("expected templated version 1.2.42-01234567, got %q", templated)
	}
}
