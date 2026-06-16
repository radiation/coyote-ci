package source

import (
	"context"
	"errors"
	"testing"
)

func TestReadWorkspaceCommitMetadata_ParsesValidOutput(t *testing.T) {
	original := runGitShowCommitMetadata
	t.Cleanup(func() {
		runGitShowCommitMetadata = original
	})
	runGitShowCommitMetadata = func(context.Context, string) (string, error) {
		return " Ada Lovelace \x00 ada@example.com \x00 Grace Hopper \x00 not-an-email \n", nil
	}

	metadata, err := ReadWorkspaceCommitMetadata(context.Background(), "/tmp/workspace")
	if err != nil {
		t.Fatalf("read commit metadata failed: %v", err)
	}
	if metadata.AuthorName != "Ada Lovelace" {
		t.Fatalf("expected trimmed author name, got %q", metadata.AuthorName)
	}
	if metadata.AuthorEmail != "ada@example.com" {
		t.Fatalf("expected valid author email, got %q", metadata.AuthorEmail)
	}
	if metadata.CommitterEmail != "" {
		t.Fatalf("expected invalid committer email to be dropped, got %q", metadata.CommitterEmail)
	}
}

func TestReadWorkspaceCommitMetadata_PropagatesRunnerError(t *testing.T) {
	original := runGitShowCommitMetadata
	t.Cleanup(func() {
		runGitShowCommitMetadata = original
	})
	runGitShowCommitMetadata = func(context.Context, string) (string, error) {
		return "", errors.New("git unavailable")
	}

	if _, err := ReadWorkspaceCommitMetadata(context.Background(), "/tmp/workspace"); err == nil || err.Error() != "git unavailable" {
		t.Fatalf("expected git runner error, got %v", err)
	}
}
