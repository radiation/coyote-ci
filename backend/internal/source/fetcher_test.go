package source

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitFetcher_Fetch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	remoteDir := t.TempDir()
	mustRun(t, remoteDir, "git", "init", "--bare")

	workDir := t.TempDir()
	mustRun(t, workDir, "git", "clone", remoteDir, ".")
	mustRun(t, workDir, "git", "config", "user.email", "test@test.com")
	mustRun(t, workDir, "git", "config", "user.name", "Test")

	pipelineDir := filepath.Join(workDir, ".coyote")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pipelineContent := []byte("version: 1\nsteps:\n  - name: hello\n    run: echo hi\n")
	if err := os.WriteFile(filepath.Join(pipelineDir, "pipeline.yml"), pipelineContent, 0o644); err != nil {
		t.Fatal(err)
	}

	mustRun(t, workDir, "git", "add", ".")
	mustRun(t, workDir, "git", "commit", "-m", "init")
	mustRun(t, workDir, "git", "push", "origin", "HEAD")

	expectedSHA := mustOutput(t, workDir, "git", "rev-parse", "HEAD")

	mustRun(t, workDir, "git", "checkout", "-b", "feature/repo-cloning")
	mustRun(t, workDir, "git", "push", "origin", "feature/repo-cloning")
	featureSHA := mustOutput(t, workDir, "git", "rev-parse", "HEAD")

	fetcher := NewGitFetcher()

	t.Run("fetch by branch", func(t *testing.T) {
		localPath, commitSHA, err := fetcher.Fetch(context.Background(), remoteDir, "master")
		if err != nil {
			localPath, commitSHA, err = fetcher.Fetch(context.Background(), remoteDir, "main")
		}
		if err != nil {
			t.Fatalf("fetch failed: %v", err)
		}
		defer func() { _ = os.RemoveAll(localPath) }()

		if commitSHA != expectedSHA {
			t.Fatalf("expected SHA %q, got %q", expectedSHA, commitSHA)
		}

		pipelinePath := filepath.Join(localPath, ".coyote", "pipeline.yml")
		if _, err := os.Stat(pipelinePath); err != nil {
			t.Fatalf("pipeline file not found at %s: %v", pipelinePath, err)
		}
	})

	t.Run("fetch by commit SHA", func(t *testing.T) {
		localPath, commitSHA, err := fetcher.Fetch(context.Background(), remoteDir, expectedSHA)
		if err != nil {
			t.Fatalf("fetch failed: %v", err)
		}
		defer func() { _ = os.RemoveAll(localPath) }()

		if commitSHA != expectedSHA {
			t.Fatalf("expected SHA %q, got %q", expectedSHA, commitSHA)
		}
	})

	t.Run("fetch with HTTPS credential", func(t *testing.T) {
		localPath, commitSHA, fetchErr := fetcher.FetchWithHTTPSCredential(context.Background(), remoteDir, "main", HTTPSCredential{Username: "x-access-token", Password: "test-token"})
		if fetchErr != nil {
			localPath, commitSHA, fetchErr = fetcher.FetchWithHTTPSCredential(context.Background(), remoteDir, "master", HTTPSCredential{Username: "x-access-token", Password: "test-token"})
		}
		if fetchErr != nil {
			t.Fatalf("authenticated fetch failed: %v", fetchErr)
		}
		defer func() { _ = os.RemoveAll(localPath) }()
		if commitSHA != expectedSHA {
			t.Fatalf("expected SHA %q, got %q", expectedSHA, commitSHA)
		}
	})

	t.Run("fetch by remote feature branch", func(t *testing.T) {
		localPath, commitSHA, err := fetcher.Fetch(context.Background(), remoteDir, "feature/repo-cloning")
		if err != nil {
			t.Fatalf("fetch failed: %v", err)
		}
		defer func() { _ = os.RemoveAll(localPath) }()

		if commitSHA != featureSHA {
			t.Fatalf("expected SHA %q, got %q", featureSHA, commitSHA)
		}
	})

	t.Run("invalid ref", func(t *testing.T) {
		_, _, err := fetcher.Fetch(context.Background(), remoteDir, "nonexistent-branch")
		if err == nil {
			t.Fatal("expected error for invalid ref")
		}
	})

	t.Run("invalid repo URL", func(t *testing.T) {
		_, _, err := fetcher.Fetch(context.Background(), "/nonexistent/repo", "main")
		if err == nil {
			t.Fatal("expected error for invalid repo")
		}
	})
}

func TestIsAuthenticationFailure_OnlyAcceptsExplicitCredentialRejection(t *testing.T) {
	for _, testCase := range []struct {
		message string
		want    bool
	}{
		{message: "fatal: Authentication failed", want: true},
		{message: "remote: bad credentials", want: true},
		{message: "HTTP 401 unauthorized", want: true},
		{message: "repository not found", want: false},
		{message: "HTTP 404", want: false},
		{message: "permission denied", want: false},
		{message: "network timeout", want: false},
		{message: "rate limit exceeded", want: false},
		{message: "invalid ref", want: false},
		{message: "generic clone failure", want: false},
	} {
		t.Run(testCase.message, func(t *testing.T) {
			if got := IsAuthenticationFailure(errors.New(testCase.message)); got != testCase.want {
				t.Fatalf("expected %t, got %t", testCase.want, got)
			}
		})
	}
}

func TestGitFetcher_AuthenticatedInputValidation(t *testing.T) {
	fetcher := NewGitFetcher()
	for _, testCase := range []struct {
		name       string
		repository string
		ref        string
		credential HTTPSCredential
	}{
		{name: "missing username", repository: "https://github.com/acme/repository.git", ref: "main", credential: HTTPSCredential{Password: "token"}},
		{name: "missing password", repository: "https://github.com/acme/repository.git", ref: "main", credential: HTTPSCredential{Username: "x-access-token"}},
		{name: "empty URL", repository: " ", ref: "main", credential: HTTPSCredential{Username: "x-access-token", Password: "token"}},
		{name: "option URL", repository: "-bad", ref: "main", credential: HTTPSCredential{Username: "x-access-token", Password: "token"}},
		{name: "empty ref", repository: "https://github.com/acme/repository.git", ref: " ", credential: HTTPSCredential{Username: "x-access-token", Password: "token"}},
		{name: "invalid ref", repository: "https://github.com/acme/repository.git", ref: "bad\\ref", credential: HTTPSCredential{Username: "x-access-token", Password: "token"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := fetcher.FetchWithHTTPSCredential(context.Background(), testCase.repository, testCase.ref, testCase.credential)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestGitCloneWithHTTPSCredential_RejectsMissingCredentials(t *testing.T) {
	if err := gitCloneWithHTTPSCredential(context.Background(), "https://github.com/acme/repository.git", t.TempDir(), HTTPSCredential{}); err == nil {
		t.Fatal("expected missing credential error")
	}
}

func TestCreateGitTokenAskPassScript_IsExecutableAndRoutesCredentialPrompts(t *testing.T) {
	path, err := createGitTokenAskPassScript()
	if err != nil {
		t.Fatalf("create askpass script: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	info, statErr := os.Stat(path)
	if statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("expected executable 0700 askpass script, info=%v err=%v", info, statErr)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read askpass script: %v", readErr)
	}
	if !strings.Contains(string(content), "COYOTE_GIT_ASKPASS_USERNAME") || !strings.Contains(string(content), "COYOTE_GIT_ASKPASS_TOKEN") {
		t.Fatalf("expected askpass script to route username and token prompts, got %q", content)
	}
}

func TestGitCloneWithHTTPSCredential_CleansUniqueAskpassAndRedactsToken(t *testing.T) {
	binDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "askpass-paths")
	gitPath := filepath.Join(binDir, "git")
	script := "#!/bin/sh\nprintf '%s\\n' \"$GIT_ASKPASS\" >> \"$ASKPASS_MARKER\"\nprintf 'fatal: Authentication failed for %s\\n' \"$COYOTE_GIT_ASKPASS_TOKEN\" >&2\nexit 1\n"
	if writeErr := os.WriteFile(gitPath, []byte(script), 0o700); writeErr != nil {
		t.Fatalf("write fake git: %v", writeErr)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ASKPASS_MARKER", markerPath)

	for _, token := range []string{"old-secret-token", "new-secret-token"} {
		err := gitCloneWithHTTPSCredential(context.Background(), "https://github.com/acme/repository.git", t.TempDir(), HTTPSCredential{Username: "x-access-token", Password: token})
		if err == nil || strings.Contains(err.Error(), token) || !IsAuthenticationFailure(err) {
			t.Fatalf("expected sanitized authentication failure for %q, err=%v", token, err)
		}
	}
	pathsData, readErr := os.ReadFile(markerPath)
	if readErr != nil {
		t.Fatalf("read captured askpass paths: %v", readErr)
	}
	paths := strings.Fields(string(pathsData))
	if len(paths) != 2 || paths[0] == paths[1] {
		t.Fatalf("expected unique askpass paths per attempt, got %q", paths)
	}
	for _, path := range paths {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected askpass path %q to be removed, err=%v", path, statErr)
		}
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func mustOutput(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v", name, args, err)
	}
	result := string(out)
	if len(result) > 0 && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}
	return result
}
