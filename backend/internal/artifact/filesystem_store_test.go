package artifact

import (
	"context"
	"errors"
	"io"
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
