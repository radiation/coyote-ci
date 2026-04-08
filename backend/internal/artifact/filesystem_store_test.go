package artifact

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemStore_SaveAndOpen(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())

	wrote, err := store.Save(context.Background(), "build-1/reports/output.txt", strings.NewReader("hello artifact"))
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if wrote != int64(len("hello artifact")) {
		t.Fatalf("expected wrote bytes %d, got %d", len("hello artifact"), wrote)
	}

	reader, err := store.Open(context.Background(), "build-1/reports/output.txt")
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(body) != "hello artifact" {
		t.Fatalf("unexpected stored body: %q", string(body))
	}
}

func TestFilesystemStore_RejectsInvalidKey(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	_, err := store.Save(context.Background(), "../escape.txt", strings.NewReader("x"))
	if !errors.Is(err, ErrInvalidStorageKey) {
		t.Fatalf("expected ErrInvalidStorageKey, got %v", err)
	}
}

func TestFilesystemStore_Save_WritesUnderConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	store := NewFilesystemStore(root)

	if _, err := store.Save(context.Background(), "build-1/dist/hello.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	target := filepath.Join(root, "build-1", "dist", "hello.txt")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected file under configured root, read failed: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("expected stored body hello, got %q", string(body))
	}
}

func TestFilesystemStore_Exists(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	if _, err := store.Save(ctx, "build-1/dist/file.txt", strings.NewReader("body")); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	exists, err := store.Exists(ctx, "build-1/dist/file.txt")
	if err != nil {
		t.Fatalf("exists failed: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for saved artifact")
	}

	exists, err = store.Exists(ctx, "build-1/dist/missing.txt")
	if err != nil {
		t.Fatalf("exists missing failed: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false for missing artifact")
	}
}

func TestExists_FallbackToOpen(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	ctx := context.Background()

	if _, err := store.Save(ctx, "build-2/reports/out.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	exists, err := Exists(ctx, store, "build-2/reports/out.txt")
	if err != nil {
		t.Fatalf("Exists helper failed: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true")
	}
}
