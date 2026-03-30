package workspace

import (
	"path/filepath"
	"testing"
)

func TestWorkspace_ContainerWorkingDir(t *testing.T) {
	ws := New("build-1", "/tmp/build-1")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty defaults to workspace", input: "", expected: "/workspace"},
		{name: "dot defaults to workspace", input: ".", expected: "/workspace"},
		{name: "safe relative", input: "backend", expected: "/workspace/backend"},
		{name: "cleaned relative", input: "a/../backend", expected: "/workspace/backend"},
		{name: "escape blocked", input: "../../etc", expected: "/workspace"},
		{name: "absolute under workspace allowed", input: "/workspace/sub", expected: "/workspace/sub"},
		{name: "absolute outside blocked", input: "/etc", expected: "/workspace"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := ws.ContainerWorkingDir(tc.input); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestWorkspace_ValidateArtifactPath(t *testing.T) {
	ws := New("build-1", "/tmp/build-1")

	valid := []string{"dist/app.tar.gz", "reports/*.xml", "dist/**", "coverage.out"}
	for _, item := range valid {
		if err := ws.ValidateArtifactPath(item); err != nil {
			t.Fatalf("expected valid artifact path %q, got error %v", item, err)
		}
	}

	invalid := []string{"", "/tmp/file", "../secret.txt", "dist/../../secret.txt"}
	for _, item := range invalid {
		if err := ws.ValidateArtifactPath(item); err == nil {
			t.Fatalf("expected invalid artifact path %q to fail validation", item)
		}
	}
}

func TestWorkspace_ResolveRelativePath(t *testing.T) {
	hostRoot := t.TempDir()
	ws := New("build-1", hostRoot)

	resolved, err := ws.ResolveRelativePath("dist/app.tar.gz")
	if err != nil {
		t.Fatalf("expected valid relative path, got %v", err)
	}
	want := filepath.Join(hostRoot, "dist", "app.tar.gz")
	if resolved != want {
		t.Fatalf("expected %q, got %q", want, resolved)
	}

	if _, err := ws.ResolveRelativePath("../secret.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}

	if _, err := ws.ResolveRelativePath("/tmp/secret.txt"); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
}
