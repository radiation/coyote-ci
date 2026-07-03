package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_SaveLoadAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coyote", "config.json")
	store := NewStore(path)

	if err := store.Save(File{
		CurrentContext: "local",
		Contexts: map[string]Context{
			"local": {Name: "local", ServerURL: "http://localhost:8080", CredentialRef: "context:local", DefaultOutput: "json"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.CurrentContext != "local" || loaded.Contexts["local"].ServerURL != "http://localhost:8080" {
		t.Fatalf("unexpected config: %+v", loaded)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected file mode 0600, got %o", got)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected dir mode 0700, got %o", got)
	}
}

func TestStore_SaveReplacesExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coyote", "config.json")
	store := NewStore(path)

	if err := store.Save(File{CurrentContext: "one", Contexts: map[string]Context{"one": {ServerURL: "http://localhost:8080"}}}); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	if err := store.Save(File{CurrentContext: "two", Contexts: map[string]Context{"two": {ServerURL: "https://example.com/coyote"}}}); err != nil {
		t.Fatalf("replace save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load replaced config: %v", err)
	}
	if loaded.CurrentContext != "two" || len(loaded.Contexts) != 1 || loaded.Contexts["two"].ServerURL != "https://example.com/coyote" {
		t.Fatalf("unexpected replaced config: %+v", loaded)
	}
}

func TestStore_SaveFailureLeavesExistingConfigIntactAndCleansTemp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coyote", "config.json")
	store := NewStore(path)
	if err := store.Save(File{CurrentContext: "one", Contexts: map[string]Context{"one": {ServerURL: "http://localhost:8080"}}}); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	store.replaceFile = func(source string, destination string) error {
		return os.ErrPermission
	}
	err := store.Save(File{CurrentContext: "two", Contexts: map[string]Context{"two": {ServerURL: "https://example.com/coyote"}}})
	if err == nil {
		t.Fatal("expected save failure")
	}

	loaded, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("load after failed save: %v", loadErr)
	}
	if loaded.CurrentContext != "one" || loaded.Contexts["one"].ServerURL != "http://localhost:8080" {
		t.Fatalf("existing config was not preserved: %+v", loaded)
	}

	matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(path), "config-*.tmp"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no orphaned temp files, found %v", matches)
	}
}

func TestNormalizeServerURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "https", input: "https://example.com/", want: "https://example.com"},
		{name: "http localhost", input: "http://localhost:8080/api/", want: "http://localhost:8080/api"},
		{name: "one level prefix", input: "https://example.com/coyote/", want: "https://example.com/coyote"},
		{name: "multi level prefix", input: "https://example.com/platform/coyote/", want: "https://example.com/platform/coyote"},
		{name: "missing host", input: "https:///", wantErr: true},
		{name: "unsupported scheme", input: "ftp://example.com", wantErr: true},
		{name: "query disallowed", input: "https://example.com/?x=1", wantErr: true},
		{name: "fragment disallowed", input: "https://example.com/coyote#frag", wantErr: true},
		{name: "embedded credentials disallowed", input: "https://user:pass@example.com/coyote", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeServerURL(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize server url: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
