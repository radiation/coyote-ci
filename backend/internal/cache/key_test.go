package cache

import "testing"

func TestResolveKey_DeterministicAndDistinctByScope(t *testing.T) {
	input := KeyInput{
		Scope:          "job",
		JobIdentity:    "job-1",
		Image:          "golang:1.26",
		Platform:       "linux/amd64",
		Paths:          []string{"/root/.cache/go-build", "/go/pkg/mod"},
		KeyFilesDigest: "abc",
	}

	k1, err := ResolveKey(input)
	if err != nil {
		t.Fatalf("resolve key: %v", err)
	}
	k2, err := ResolveKey(KeyInput{
		Scope:          "job",
		JobIdentity:    "job-1",
		Image:          "golang:1.26",
		Platform:       "linux/amd64",
		Paths:          []string{"/go/pkg/mod", "/root/.cache/go-build"},
		KeyFilesDigest: "abc",
	})
	if err != nil {
		t.Fatalf("resolve key: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("expected deterministic key, got %q vs %q", k1, k2)
	}

	kBuild, err := ResolveKey(KeyInput{
		Scope:          "build",
		BuildID:        "build-1",
		Image:          "golang:1.26",
		Platform:       "linux/amd64",
		Paths:          []string{"/go/pkg/mod", "/root/.cache/go-build"},
		KeyFilesDigest: "abc",
	})
	if err != nil {
		t.Fatalf("resolve key: %v", err)
	}
	if kBuild == k1 {
		t.Fatalf("expected build and job scope keys to differ, both=%q", k1)
	}
}
