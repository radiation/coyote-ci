package versioning

import (
	"strings"
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

func TestConfigEmptyAndNormalizeStrategy(t *testing.T) {
	if !(Config{}).Empty() {
		t.Fatal("expected zero config to be empty")
	}
	if (Config{Strategy: " manual "}).Empty() {
		t.Fatal("expected strategy config to be non-empty")
	}

	tests := []struct {
		input string
		want  Strategy
	}{
		{input: "", want: ReleaseStrategyManual},
		{input: " MANUAL ", want: ReleaseStrategyManual},
		{input: " semver-patch ", want: ReleaseStrategySemverPatch},
		{input: " TEMPLATE ", want: ReleaseStrategyTemplate},
		{input: " custom ", want: Strategy("custom")},
	}
	for _, tc := range tests {
		if got := NormalizeStrategy(tc.input); got != tc.want {
			t.Fatalf("NormalizeStrategy(%q): expected %q, got %q", tc.input, tc.want, got)
		}
	}
}

func TestValidateConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		message string
	}{
		{name: "unknown strategy", config: Config{Strategy: "calendar"}, message: "release.strategy"},
		{name: "empty semver series", config: Config{Strategy: "semver-patch"}, message: "required"},
		{name: "leading zero semver", config: Config{Strategy: "semver-patch", Version: "01.2"}, message: "leading zeroes"},
		{name: "nonnumeric semver", config: Config{Strategy: "semver-patch", Version: "1.x"}, message: "numeric"},
		{name: "empty template", config: Config{Strategy: "template"}, message: "release.template"},
		{name: "malformed template open", config: Config{Strategy: "template", Template: "v{build_number}{"}, message: "malformed"},
		{name: "malformed template close", config: Config{Strategy: "template", Template: "v}{build_number}"}, message: "malformed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("expected error containing %q, got %v", tt.message, err)
			}
		})
	}
}

func TestResolveVersion_TemplateFallbacksAndSemverFiltering(t *testing.T) {
	sourceCommit := "abcdef1234567890"
	build := domain.Build{
		BuildNumber:   0,
		AttemptNumber: 0,
		Source:        &domain.SourceSpec{CommitSHA: &sourceCommit},
	}

	version, err := ResolveVersion(ResolveInput{
		Config: Config{Strategy: "template", Template: "release-{build_number}-{attempt_number}-{commit_sha}-{short_commit_sha}"},
		Build:  build,
	})
	if err != nil {
		t.Fatalf("resolve template version failed: %v", err)
	}
	if version != "release--1-abcdef1234567890-abcdef12" {
		t.Fatalf("unexpected fallback template version: %q", version)
	}

	semver, err := ResolveVersion(ResolveInput{
		Config:           Config{Strategy: "semver-patch", Version: "2.5"},
		ExistingVersions: []string{"2.5.0", " 2.5.3 ", "2.5.x", "2.4.9", "bad", "2.5.02"},
	})
	if err != nil {
		t.Fatalf("resolve semver version failed: %v", err)
	}
	if semver != "2.5.4" {
		t.Fatalf("expected next semver patch 2.5.4, got %q", semver)
	}
}

func TestReleaseHelperFunctions(t *testing.T) {
	if isSupportedPlaceholder("branch") {
		t.Fatal("branch should not be a supported placeholder")
	}
	if got := releasePlaceholderValue("unknown", domain.Build{}); got != "" {
		t.Fatalf("expected unknown placeholder to resolve empty, got %q", got)
	}
	if _, err := parseReleasePart(""); err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatalf("expected empty release segment error, got %v", err)
	}
	if _, err := parseReleasePart("-1"); err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatalf("expected negative release segment error, got %v", err)
	}
}
