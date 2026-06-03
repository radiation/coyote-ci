package versioning

import (
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestResolveArtifactVersionTemplate_BuildNumber(t *testing.T) {
	version, err := ResolveArtifactVersionTemplate("3.1.{build_number}", domain.Build{BuildNumber: 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.1.42" {
		t.Fatalf("expected 3.1.42, got %q", version)
	}
}

func TestResolveArtifactVersionTemplate_GitMetadata(t *testing.T) {
	sha := "abcdef1234567890"
	ref := "refs/heads/main"
	build := domain.Build{SourceSHA: &sha, SourceRef: &ref}

	resolvedSHA, err := ResolveArtifactVersionTemplate("{git_sha}", build)
	if err != nil {
		t.Fatalf("unexpected git sha error: %v", err)
	}
	if resolvedSHA != sha {
		t.Fatalf("expected git sha %q, got %q", sha, resolvedSHA)
	}

	resolvedShort, err := ResolveArtifactVersionTemplate("{git_short_sha}", build)
	if err != nil {
		t.Fatalf("unexpected git short sha error: %v", err)
	}
	if resolvedShort != "abcdef12" {
		t.Fatalf("expected git short sha abcdef12, got %q", resolvedShort)
	}

	resolvedRef, err := ResolveArtifactVersionTemplate("{git_ref}", build)
	if err != nil {
		t.Fatalf("unexpected git ref error: %v", err)
	}
	if resolvedRef != ref {
		t.Fatalf("expected git ref %q, got %q", ref, resolvedRef)
	}
}

func TestResolveArtifactVersionTemplate_MissingMetadataFails(t *testing.T) {
	_, err := ResolveArtifactVersionTemplate("3.1.{git_sha}", domain.Build{BuildNumber: 7})
	if err == nil {
		t.Fatal("expected missing metadata error")
	}
	if !strings.Contains(err.Error(), "git sha metadata") {
		t.Fatalf("expected git sha metadata error, got %v", err)
	}
}

func TestValidateArtifactVersionConfig(t *testing.T) {
	if err := ValidateArtifactVersionConfig("3.1.{build_number}", "latest"); err != nil {
		t.Fatalf("unexpected valid config error: %v", err)
	}
	if err := ValidateArtifactVersionConfig("", "latest"); err == nil || !strings.Contains(err.Error(), "requires a template") {
		t.Fatalf("expected channel requires template error, got %v", err)
	}
	if err := ValidateArtifactVersionConfig("3.1.{branch}", ""); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported placeholder error, got %v", err)
	}
}

func TestValidateArtifactVersionTemplate_MalformedPlaceholders(t *testing.T) {
	if err := ValidateArtifactVersionTemplate("1.0.{{build_number}"); err == nil || !strings.Contains(err.Error(), "malformed placeholders") {
		t.Fatalf("expected malformed placeholder error, got %v", err)
	}
	if err := ValidateArtifactVersionTemplate("1.0.{build_number}}"); err == nil || !strings.Contains(err.Error(), "malformed placeholders") {
		t.Fatalf("expected malformed placeholder error, got %v", err)
	}
}

func TestResolveArtifactVersionTemplate_FallbackMetadataSources(t *testing.T) {
	commitSHA := "1234567"
	ref := " refs/tags/v1.2.3 "
	build := domain.Build{
		CommitSHA: &commitSHA,
		Ref:       &ref,
	}

	resolvedShort, err := ResolveArtifactVersionTemplate("{git_short_sha}", build)
	if err != nil {
		t.Fatalf("unexpected git short sha error: %v", err)
	}
	if resolvedShort != "1234567" {
		t.Fatalf("expected short sha fallback 1234567, got %q", resolvedShort)
	}

	resolvedRef, err := ResolveArtifactVersionTemplate("release-{git_ref}", build)
	if err != nil {
		t.Fatalf("unexpected git ref error: %v", err)
	}
	if resolvedRef != "release-refs/tags/v1.2.3" {
		t.Fatalf("expected trimmed git ref fallback, got %q", resolvedRef)
	}
}

func TestResolveArtifactVersionTemplate_UsesTriggerMetadataFallback(t *testing.T) {
	commitSHA := "abcdef1234567890"
	ref := "refs/heads/main"
	build := domain.Build{
		Trigger: domain.BuildTrigger{CommitSHA: &commitSHA, Ref: &ref},
	}

	resolved, err := ResolveArtifactVersionTemplate("{git_sha}-{git_ref}", build)
	if err != nil {
		t.Fatalf("unexpected trigger metadata error: %v", err)
	}
	if resolved != "abcdef1234567890-refs/heads/main" {
		t.Fatalf("expected trigger metadata fallback, got %q", resolved)
	}
}
