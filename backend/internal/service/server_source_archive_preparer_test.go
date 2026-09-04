package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformgithubapp "github.com/radiation/coyote-ci/backend/internal/platform/githubapp"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
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

func TestServerSourceArchivePreparerSourceHelpersPreferExplicitValues(t *testing.T) {
	explicitRef := " explicit-ref "
	fallbackRef := " fallback-ref "
	if got := optionalSourceRef(domain.SourceSnapshotRef{RefName: &explicitRef}, domain.SourceSnapshotRef{RefName: &fallbackRef}); got != "explicit-ref" {
		t.Fatalf("explicit ref=%q", got)
	}
	if got := optionalSourceRef(domain.SourceSnapshotRef{}, domain.SourceSnapshotRef{RefName: &fallbackRef}); got != "fallback-ref" {
		t.Fatalf("fallback ref=%q", got)
	}
	if got := optionalSourceRef(domain.SourceSnapshotRef{}, domain.SourceSnapshotRef{}); got != "" {
		t.Fatalf("empty ref=%q", got)
	}
	if got := optionalSourceCommit(domain.SourceSnapshotRef{CommitSHA: " explicit-commit "}, domain.SourceSnapshotRef{CommitSHA: "fallback-commit"}); got != "explicit-commit" {
		t.Fatalf("explicit commit=%q", got)
	}
	if got := optionalSourceCommit(domain.SourceSnapshotRef{}, domain.SourceSnapshotRef{CommitSHA: " fallback-commit "}); got != "fallback-commit" {
		t.Fatalf("fallback commit=%q", got)
	}
}

func TestServerSourceArchivePreparerRejectsAuthenticatedSourceWithoutCheckout(t *testing.T) {
	registeredRepositoryID := "registered"
	connectionID := "connection"
	providerRepositoryID := "provider"
	preparer, newErr := NewServerSourceArchivePreparer(&serverSourceResolverFake{}, nil)
	if newErr != nil {
		t.Fatalf("new preparer: %v", newErr)
	}
	_, prepareErr := preparer.OpenSourceArchive(context.Background(), domain.Build{RegisteredRepositoryID: &registeredRepositoryID, SCMConnectionID: &connectionID, ProviderRepositoryID: &providerRepositoryID}, domain.ExecutionJob{Source: domain.SourceSnapshotRef{RepositoryURL: "https://example.test/repo.git"}}, domain.ExecutionJobSpec{})
	if !errors.Is(prepareErr, buildsvc.ErrRepositoryCheckoutConnectionInvalid) {
		t.Fatalf("prepare archive: %v", prepareErr)
	}
}

func TestServerSourceArchivePreparerUsesAuthenticatedCheckout(t *testing.T) {
	resolver := &authenticatedServerSourceResolverFake{}
	checkoutResolver, resolverErr := buildsvc.NewRepositoryAwareCheckoutResolver(buildsvc.RepositoryAwareCheckoutResolverConfig{
		Connections:   &serverSourceConnectionFake{detail: serverSourceCheckoutDetail()},
		Registrations: &serverSourceRegistrationFake{registration: domain.SCMRepositoryRegistration{ID: "registered", ConnectionID: "connection", ProviderRepositoryID: "provider"}},
		Secrets:       &serverSourceSecretFake{value: "private-key"},
		GitHub:        &serverSourceGitHubFake{repository: platformgithubapp.Repository{ID: "provider", CloneURL: "https://github.com/acme/repository.git"}},
	})
	if resolverErr != nil {
		t.Fatalf("new checkout resolver: %v", resolverErr)
	}
	preparer, newErr := NewServerSourceArchivePreparer(resolver, checkoutResolver)
	if newErr != nil {
		t.Fatalf("new preparer: %v", newErr)
	}
	registeredRepositoryID := "registered"
	connectionID := "connection"
	providerRepositoryID := "provider"
	payload, prepareErr := preparer.OpenSourceArchive(context.Background(), domain.Build{RegisteredRepositoryID: &registeredRepositoryID, SCMConnectionID: &connectionID, ProviderRepositoryID: &providerRepositoryID}, domain.ExecutionJob{Source: domain.SourceSnapshotRef{RepositoryURL: "https://stale.example/repository.git", CommitSHA: "commit-1"}}, domain.ExecutionJobSpec{})
	if prepareErr != nil {
		t.Fatalf("prepare authenticated archive: %v", prepareErr)
	}
	defer func() { _ = payload.Archive.Close() }()
	if resolver.repositoryURL != "https://github.com/acme/repository.git" || resolver.credential.Password != "installation-token" || payload.Publication.Validate() != nil {
		t.Fatalf("resolver=%#v publication=%#v", resolver, payload.Publication)
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

type authenticatedServerSourceResolverFake struct {
	serverSourceResolverFake
	credential source.HTTPSCredential
}

func (f *authenticatedServerSourceResolverFake) CloneIntoWorkspaceWithHTTPSCredential(ctx context.Context, workspacePath string, repositoryURL string, credential source.HTTPSCredential) error {
	f.credential = credential
	return f.CloneIntoWorkspace(ctx, workspacePath, repositoryURL)
}

var _ source.AuthenticatedWorkspaceSourceResolver = (*authenticatedServerSourceResolverFake)(nil)

type serverSourceConnectionFake struct{ detail domain.SCMConnectionDetail }

func (f *serverSourceConnectionFake) GetByID(context.Context, string) (domain.SCMConnectionDetail, error) {
	return f.detail, nil
}

type serverSourceRegistrationFake struct {
	registration domain.SCMRepositoryRegistration
}

func (f *serverSourceRegistrationFake) GetByID(context.Context, string) (domain.SCMRepositoryRegistration, error) {
	return f.registration, nil
}

type serverSourceSecretFake struct{ value string }

func (f *serverSourceSecretFake) Resolve(context.Context, string) (string, error) {
	return f.value, nil
}

type serverSourceGitHubFake struct{ repository platformgithubapp.Repository }

func (f *serverSourceGitHubFake) GetInstallationToken(context.Context, platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error) {
	return platformgithubapp.InstallationToken{Value: "installation-token"}, nil
}

func (f *serverSourceGitHubFake) GetFreshInstallationToken(context.Context, platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error) {
	return platformgithubapp.InstallationToken{Value: "fresh-installation-token"}, nil
}

func (f *serverSourceGitHubFake) GetRepositoryByID(context.Context, platformgithubapp.InstallationTokenRequest, string) (platformgithubapp.Repository, error) {
	return f.repository, nil
}

func serverSourceCheckoutDetail() domain.SCMConnectionDetail {
	return domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: "connection", Provider: domain.SCMProviderGitHub, APIBaseURL: "https://api.github.com", Enabled: true},
		GitHubAppRegistration: &domain.GitHubAppRegistration{ID: "app", AppID: "123", APIBaseURL: "https://api.github.com", PrivateKeySecretRef: "PRIVATE_KEY"},
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: "connection", AppRegistrationID: "app", InstallationID: "456"},
	}
}
