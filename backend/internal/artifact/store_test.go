package artifact

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestResolveStorageKeyUsesOptionalResolver(t *testing.T) {
	if got := resolveStorageKey(&fakeArtifactStore{}, "builds/build-1/out.txt"); got != "builds/build-1/out.txt" {
		t.Fatalf("expected unresolved key passthrough, got %q", got)
	}
	if got := resolveStorageKey(&fakeResolvingArtifactStore{resolved: "prefix/builds/build-1/out.txt"}, "builds/build-1/out.txt"); got != "prefix/builds/build-1/out.txt" {
		t.Fatalf("expected resolved key, got %q", got)
	}
	if got := resolveStorageKey(&fakeResolvingArtifactStore{}, "builds/build-1/out.txt"); got != "builds/build-1/out.txt" {
		t.Fatalf("expected empty resolved key to fall back, got %q", got)
	}
}

func TestExistsValidatesStoreAndFallsBackToOpen(t *testing.T) {
	ctx := context.Background()
	if _, existsErr := Exists(ctx, nil, "builds/build-1/out.txt"); existsErr == nil {
		t.Fatal("expected nil store error")
	}

	found, foundErr := Exists(ctx, &fakeArtifactStore{openReader: io.NopCloser(strings.NewReader("artifact"))}, "builds/build-1/out.txt")
	if foundErr != nil {
		t.Fatalf("exists fallback found: %v", foundErr)
	}
	if !found {
		t.Fatal("expected fallback open to report found")
	}

	missing, missingErr := Exists(ctx, &fakeArtifactStore{openErr: os.ErrNotExist}, "builds/build-1/missing.txt")
	if missingErr != nil {
		t.Fatalf("exists fallback missing: %v", missingErr)
	}
	if missing {
		t.Fatal("expected missing artifact to report false")
	}

	openErr := errors.New("backend unavailable")
	_, existsErr := Exists(ctx, &fakeArtifactStore{openErr: openErr}, "builds/build-1/out.txt")
	if !errors.Is(existsErr, openErr) {
		t.Fatalf("expected open error %v, got %v", openErr, existsErr)
	}
}

func TestValidateKeyRejectsUnsafeKeys(t *testing.T) {
	validKeys := []string{"builds/build-1/out.txt", " artifacts/report.xml "}
	for _, key := range validKeys {
		if validateErr := validateKey(key); validateErr != nil {
			t.Fatalf("expected key %q to be valid, got %v", key, validateErr)
		}
	}

	invalidKeys := []string{"", " ", "/absolute", "..", "../escape", "builds/../escape", `builds\escape`}
	for _, key := range invalidKeys {
		if validateErr := validateKey(key); !errors.Is(validateErr, ErrInvalidStorageKey) {
			t.Fatalf("expected invalid key error for %q, got %v", key, validateErr)
		}
	}
}

type fakeArtifactStore struct {
	openReader io.ReadCloser
	openErr    error
}

func (s *fakeArtifactStore) Save(context.Context, string, io.Reader) (int64, error) {
	return 0, nil
}

func (s *fakeArtifactStore) Open(context.Context, string) (io.ReadCloser, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}
	if s.openReader != nil {
		return s.openReader, nil
	}
	return io.NopCloser(strings.NewReader("")), nil
}

type fakeResolvingArtifactStore struct {
	fakeArtifactStore
	resolved string
}

func (s *fakeResolvingArtifactStore) ResolveStorageKey(string) string {
	return s.resolved
}
