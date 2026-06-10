package source

import (
	"context"
	"fmt"
	"net/mail"
	"os/exec"
	"strings"
)

type CommitMetadata struct {
	AuthorName     string
	AuthorEmail    string
	CommitterName  string
	CommitterEmail string
}

var runGitShowCommitMetadata = defaultRunGitShowCommitMetadata

func ReadWorkspaceCommitMetadata(ctx context.Context, workspacePath string) (CommitMetadata, error) {
	output, err := runGitShowCommitMetadata(ctx, workspacePath)
	if err != nil {
		return CommitMetadata{}, err
	}
	return parseCommitMetadata(output)
}

func defaultRunGitShowCommitMetadata(ctx context.Context, workspacePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "show", "-s", "--format=%an%x00%ae%x00%cn%x00%ce", "HEAD")
	if err := setGitDir(cmd, workspacePath); err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

func parseCommitMetadata(output string) (CommitMetadata, error) {
	trimmed := strings.TrimRight(output, "\r\n")
	parts := strings.Split(trimmed, "\x00")
	if len(parts) < 4 {
		return CommitMetadata{}, fmt.Errorf("unexpected git show metadata format")
	}

	return CommitMetadata{
		AuthorName:     normalizeGitIdentityName(parts[0]),
		AuthorEmail:    normalizeGitIdentityEmail(parts[1]),
		CommitterName:  normalizeGitIdentityName(parts[2]),
		CommitterEmail: normalizeGitIdentityEmail(parts[3]),
	}, nil
}

func normalizeGitIdentityName(value string) string {
	return strings.TrimSpace(value)
}

func normalizeGitIdentityEmail(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Address)
}
