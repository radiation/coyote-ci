package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

func TestServerSourceArchivePreparerCreatesVerifiedArchive(t *testing.T) {
	resolver := &serverSourceResolverFake{}
	preparer, newErr := NewServerSourceArchivePreparer(resolver, nil)
	if newErr != nil {
		t.Fatalf("new preparer: %v", newErr)
	}
	payload, prepareErr := preparer.OpenSourceArchive(context.Background(), domain.Build{}, domain.ExecutionJob{Source: domain.SourceSnapshotRef{RepositoryURL: "https://example.test/repo.git", CommitSHA: "commit-1"}}, domain.ExecutionJobSpec{})
	if prepareErr != nil {
		t.Fatalf("prepare archive: %v", prepareErr)
	}
	defer func() { _ = payload.Archive.Close() }()
	if resolver.repositoryURL != "https://example.test/repo.git" || resolver.spec.CommitSHA != "commit-1" || payload.Publication.Validate() != nil {
		t.Fatalf("resolver=%#v publication=%#v", resolver, payload.Publication)
	}
	if _, readErr := io.ReadAll(payload.Archive); readErr != nil {
		t.Fatalf("read archive: %v", readErr)
	}
}

func TestServerSourceArchivePreparerRejectsMissingResolverOrRepository(t *testing.T) {
	if _, newErr := NewServerSourceArchivePreparer(nil, nil); newErr == nil {
		t.Fatal("expected missing resolver error")
	}
	preparer, newErr := NewServerSourceArchivePreparer(&serverSourceResolverFake{}, nil)
	if newErr != nil {
		t.Fatalf("new preparer: %v", newErr)
	}
	if _, prepareErr := preparer.OpenSourceArchive(context.Background(), domain.Build{}, domain.ExecutionJob{}, domain.ExecutionJobSpec{}); !errors.Is(prepareErr, source.ErrRepositoryURLRequired) {
		t.Fatalf("prepare archive: %v", prepareErr)
	}
}

func TestServerSourceArchivePreparerRejectsSourcePreparationFailures(t *testing.T) {
	cloneFailure := errors.New("clone failed")
	checkoutFailure := errors.New("checkout failed")
	for _, testCase := range []struct {
		name     string
		resolver *serverSourceResolverFake
		want     error
	}{
		{name: "clone", resolver: &serverSourceResolverFake{cloneErr: cloneFailure}, want: cloneFailure},
		{name: "checkout", resolver: &serverSourceResolverFake{checkoutErr: checkoutFailure}, want: checkoutFailure},
		{name: "commit mismatch", resolver: &serverSourceResolverFake{resolvedCommit: "other"}, want: ErrWorkspacePrepareInvalidInput},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			preparer, newErr := NewServerSourceArchivePreparer(testCase.resolver, nil)
			if newErr != nil {
				t.Fatalf("new preparer: %v", newErr)
			}
			_, prepareErr := preparer.OpenSourceArchive(context.Background(), domain.Build{}, domain.ExecutionJob{Source: domain.SourceSnapshotRef{RepositoryURL: "https://example.test/repo.git", CommitSHA: "commit-1"}}, domain.ExecutionJobSpec{})
			if !errors.Is(prepareErr, testCase.want) {
				t.Fatalf("prepare archive: %v", prepareErr)
			}
		})
	}
}

type serverSourceResolverFake struct {
	repositoryURL  string
	spec           source.WorkspaceSourceSpec
	cloneErr       error
	checkoutErr    error
	resolvedCommit string
}

func (f *serverSourceResolverFake) CloneIntoWorkspace(_ context.Context, workspacePath string, repositoryURL string) error {
	f.repositoryURL = repositoryURL
	if f.cloneErr != nil {
		return f.cloneErr
	}
	if mkdirErr := os.MkdirAll(workspacePath, 0o755); mkdirErr != nil {
		return mkdirErr
	}
	return os.WriteFile(filepath.Join(workspacePath, "source.txt"), []byte("source"), 0o644)
}

func (f *serverSourceResolverFake) CheckoutWorkspaceSource(_ context.Context, _ string, spec source.WorkspaceSourceSpec) (string, error) {
	f.spec = spec
	if f.checkoutErr != nil {
		return "", f.checkoutErr
	}
	if f.resolvedCommit != "" {
		return f.resolvedCommit, nil
	}
	return spec.CommitSHA, nil
}

var _ source.WorkspaceSourceResolver = (*serverSourceResolverFake)(nil)
