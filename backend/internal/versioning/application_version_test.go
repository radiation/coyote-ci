package versioning

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestResolveApplicationVersion(t *testing.T) {
	sha := "abcdef1234567890"
	ref := "refs/tags/v1.2.3"
	build := domain.Build{BuildNumber: 137, SourceSHA: &sha, SourceRef: &ref}

	tests := []struct {
		name    string
		config  ApplicationVersionConfig
		want    string
		wantErr string
	}{
		{name: "explicit", config: ApplicationVersionConfig{Value: "2.5.1"}, want: "2.5.1"},
		{name: "prerelease", config: ApplicationVersionConfig{Value: "1.4.0-rc.1"}, want: "1.4.0-rc.1"},
		{name: "numeric prerelease zero", config: ApplicationVersionConfig{Value: "1.2.3-0"}, want: "1.2.3-0"},
		{name: "numeric prerelease leading zero", config: ApplicationVersionConfig{Value: "1.2.3-01"}, wantErr: "SemVer"},
		{name: "alphanumeric prerelease", config: ApplicationVersionConfig{Value: "1.2.3-alpha.1"}, want: "1.2.3-alpha.1"},
		{name: "alphanumeric prerelease numeric leading zero", config: ApplicationVersionConfig{Value: "1.2.3-alpha.01"}, wantErr: "SemVer"},
		{name: "build number", config: ApplicationVersionConfig{Template: "0.4.{build_number}"}, want: "0.4.137"},
		{name: "git sha", config: ApplicationVersionConfig{Template: "1.0.0-{git_sha}"}, want: "1.0.0-abcdef1234567890"},
		{name: "short git sha", config: ApplicationVersionConfig{Template: "1.0.0-{git_short_sha}"}, want: "1.0.0-abcdef12"},
		{name: "git ref", config: ApplicationVersionConfig{Template: "1.0.0-{git_ref}"}, wantErr: "SemVer"},
		{name: "build metadata", config: ApplicationVersionConfig{Value: "1.2.3+build.4"}, wantErr: "SemVer"},
		{name: "unknown placeholder", config: ApplicationVersionConfig{Template: "1.0.0-{branch}"}, wantErr: "not supported"},
		{name: "multiple sources", config: ApplicationVersionConfig{Value: "1.2.3", Template: "1.2.{build_number}"}, wantErr: "exactly one"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ResolveApplicationVersion(testCase.config, build)
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("expected error containing %q, got %v", testCase.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve application version: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("version=%q, want %q", got, testCase.want)
			}
		})
	}
}

func TestResolveApplicationVersion_MissingTemplateMetadataFails(t *testing.T) {
	_, err := ResolveApplicationVersion(ApplicationVersionConfig{Template: "1.0.0-{git_sha}"}, domain.Build{BuildNumber: 1})
	if err == nil || !strings.Contains(err.Error(), "git sha metadata") {
		t.Fatalf("expected missing git SHA error, got %v", err)
	}
}

func TestReadApplicationVersionFile(t *testing.T) {
	root := t.TempDir()
	writeFile := func(name string, data []byte) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return name
	}

	tests := []struct {
		name    string
		path    string
		data    []byte
		want    string
		wantErr string
	}{
		{name: "trimmed", path: "VERSION", data: []byte(" 1.2.3 \n"), want: "1.2.3"},
		{name: "empty", path: "empty", data: []byte(" \n"), wantErr: "empty"},
		{name: "multiline", path: "multi", data: []byte("1.2.3\n1.2.4"), wantErr: "one line"},
		{name: "invalid utf8", path: "invalid", data: []byte{0xff}, wantErr: "UTF-8"},
		{name: "invalid semver", path: "bad", data: []byte("latest"), wantErr: "SemVer"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			writeFile(testCase.path, testCase.data)
			got, err := ReadApplicationVersionFile(root, testCase.path)
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("expected error containing %q, got %v", testCase.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("read application version: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("version=%q, want %q", got, testCase.want)
			}
		})
	}

	if _, err := ReadApplicationVersionFile(root, "/VERSION"); err == nil || !strings.Contains(err.Error(), "workspace-relative") {
		t.Fatalf("expected absolute path rejection, got %v", err)
	}
	if _, err := ReadApplicationVersionFile(root, "../VERSION"); err == nil || !strings.Contains(err.Error(), "must not traverse") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
	if _, err := ReadApplicationVersionFile(root, "missing"); err == nil || !strings.Contains(err.Error(), "read application version file") {
		t.Fatalf("expected missing file error, got %v", err)
	}
	if _, err := ReadApplicationVersionFile(root, writeFile("oversized", []byte(strings.Repeat("1", maxApplicationVersionFileSize+1)))); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized file error, got %v", err)
	}

	outsidePath := filepath.Join(t.TempDir(), "VERSION")
	if writeErr := os.WriteFile(outsidePath, []byte("1.2.3\n"), 0o600); writeErr != nil {
		t.Fatalf("write external version file: %v", writeErr)
	}
	if symlinkErr := os.Symlink(outsidePath, filepath.Join(root, "outside-version")); symlinkErr != nil {
		t.Fatalf("create external version-file symlink: %v", symlinkErr)
	}
	if _, err := ReadApplicationVersionFile(root, "outside-version"); err == nil || !strings.Contains(err.Error(), "must remain within") {
		t.Fatalf("expected external symlink rejection, got %v", err)
	}
}
