package config

import (
	"errors"
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

func TestStorePathAndLoadHelpers(t *testing.T) {
	t.Run("path uses user config dir", func(t *testing.T) {
		store := NewStore("")
		store.userConfigDir = func() (string, error) {
			return "/tmp/coyote-config", nil
		}
		path, err := store.Path()
		if err != nil {
			t.Fatalf("path: %v", err)
		}
		if path != "/tmp/coyote-config/coyote/config.json" {
			t.Fatalf("unexpected path: %s", path)
		}
	})

	t.Run("path propagates user config dir error", func(t *testing.T) {
		store := NewStore("")
		store.userConfigDir = func() (string, error) {
			return "", errors.New("boom")
		}
		_, err := store.Path()
		if err == nil || err.Error() != "boom" {
			t.Fatalf("unexpected path error: %v", err)
		}
	})

	t.Run("load backfills names and handles errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "coyote", "config.json")
		store := NewStore(path)
		loaded, err := store.Load()
		if err != nil {
			t.Fatalf("load missing config: %v", err)
		}
		if len(loaded.Contexts) != 0 {
			t.Fatalf("expected empty contexts, got %+v", loaded.Contexts)
		}

		if writeErr := os.MkdirAll(filepath.Dir(path), 0o700); writeErr != nil {
			t.Fatalf("mkdir: %v", writeErr)
		}
		if writeErr := os.WriteFile(path, []byte("{bad json"), 0o600); writeErr != nil {
			t.Fatalf("write invalid config: %v", writeErr)
		}
		_, err = store.Load()
		if err == nil || !errors.Is(err, err) || err.Error() == "" {
			t.Fatalf("expected parse error, got %v", err)
		}

		valid := []byte(`{"current_context":"local","contexts":{"local":{"server_url":"http://localhost:8080"}}}`)
		if writeErr := os.WriteFile(path, valid, 0o600); writeErr != nil {
			t.Fatalf("write valid config: %v", writeErr)
		}
		loaded, err = store.Load()
		if err != nil {
			t.Fatalf("load valid config: %v", err)
		}
		if loaded.Contexts["local"].Name != "local" {
			t.Fatalf("expected context name backfill, got %+v", loaded.Contexts["local"])
		}
	})
}

func TestNormalizeHelpers(t *testing.T) {
	if _, err := NormalizeContextName("   "); err == nil {
		t.Fatal("expected empty context name error")
	}
	name, err := NormalizeContextName(" local ")
	if err != nil || name != "local" {
		t.Fatalf("unexpected context name result: %q %v", name, err)
	}

	for _, tc := range []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "", want: "", ok: true},
		{input: " HUMAN ", want: "human", ok: true},
		{input: "Json", want: "json", ok: true},
		{input: "xml", ok: false},
	} {
		got, outputErr := NormalizeOutput(tc.input)
		if tc.ok {
			if outputErr != nil || got != tc.want {
				t.Fatalf("unexpected normalize output result for %q: %q %v", tc.input, got, outputErr)
			}
			continue
		}
		if outputErr == nil {
			t.Fatalf("expected normalize output error for %q", tc.input)
		}
	}
}
