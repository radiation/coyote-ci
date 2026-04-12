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

	kOtherJob, err := ResolveKey(KeyInput{
		Scope:          "job",
		JobIdentity:    "job-2",
		Image:          "golang:1.26",
		Platform:       "linux/amd64",
		Paths:          []string{"/go/pkg/mod", "/root/.cache/go-build"},
		KeyFilesDigest: "abc",
	})
	if err != nil {
		t.Fatalf("resolve key: %v", err)
	}
	if kOtherJob == k1 {
		t.Fatalf("expected different jobs to produce different keys, both=%q", k1)
	}
}

func TestResolveKey_Validation(t *testing.T) {
	_, err := ResolveKey(KeyInput{Scope: "job", Paths: []string{"/go/pkg/mod"}})
	if err == nil {
		t.Fatal("expected error when job scope key is missing job identity")
	}

	_, err = ResolveKey(KeyInput{Scope: "build", Paths: []string{"/go/pkg/mod"}})
	if err == nil {
		t.Fatal("expected error when build scope key is missing build id")
	}
}
