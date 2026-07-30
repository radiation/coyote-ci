package build

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformgithubapp "github.com/radiation/coyote-ci/backend/internal/platform/githubapp"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service/execution"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

type checkoutRegistrationFake struct {
	value domain.SCMRepositoryRegistration
	err   error
	calls int
}

func (f *checkoutRegistrationFake) GetByID(context.Context, string) (domain.SCMRepositoryRegistration, error) {
	f.calls++
	return f.value, f.err
}

type checkoutConnectionFake struct {
	value domain.SCMConnectionDetail
	err   error
	calls int
}

func (f *checkoutConnectionFake) GetByID(context.Context, string) (domain.SCMConnectionDetail, error) {
	f.calls++
	return f.value, f.err
}

type checkoutSecretFake struct {
	value string
	err   error
	calls int
}

func (f *checkoutSecretFake) Resolve(context.Context, string) (string, error) {
	f.calls++
	return f.value, f.err
}

type checkoutGitHubFake struct {
	repository      platformgithubapp.Repository
	repoErr         error
	tokenErr        error
	freshTokenErr   error
	tokenCalls      int
	freshTokenCalls int
	repoCalls       int
	request         platformgithubapp.InstallationTokenRequest
}

type checkoutAuthenticatedFetcherFake struct {
	calls       int
	credentials []source.HTTPSCredential
	firstErr    error
	localPath   string
	commitSHA   string
}

type checkoutAuthenticatedWorkspaceResolverFake struct {
	cloneCalls         int
	authenticatedCalls int
	credentials        []source.HTTPSCredential
	firstErr           error
}

func (f *checkoutAuthenticatedWorkspaceResolverFake) CloneIntoWorkspace(context.Context, string, string) error {
	f.cloneCalls++
	return nil
}

func (f *checkoutAuthenticatedWorkspaceResolverFake) CloneIntoWorkspaceWithHTTPSCredential(_ context.Context, _ string, _ string, credential source.HTTPSCredential) error {
	f.authenticatedCalls++
	f.credentials = append(f.credentials, credential)
	if f.authenticatedCalls == 1 && f.firstErr != nil {
		return f.firstErr
	}
	return nil
}

func (f *checkoutAuthenticatedWorkspaceResolverFake) CheckoutWorkspaceSource(context.Context, string, source.WorkspaceSourceSpec) (string, error) {
	return "commit", nil
}

func (f *checkoutAuthenticatedFetcherFake) Fetch(context.Context, string, string) (string, string, error) {
	return "", "", errors.New("legacy fetch should not be called")
}

func (f *checkoutAuthenticatedFetcherFake) FetchWithHTTPSCredential(_ context.Context, _ string, _ string, credential source.HTTPSCredential) (string, string, error) {
	f.calls++
	f.credentials = append(f.credentials, credential)
	if f.calls == 1 && f.firstErr != nil {
		return "", "", f.firstErr
	}
	return f.localPath, f.commitSHA, nil
}

func (f *checkoutGitHubFake) GetInstallationToken(_ context.Context, request platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error) {
	f.tokenCalls++
	f.request = request
	if f.tokenErr != nil {
		return platformgithubapp.InstallationToken{}, f.tokenErr
	}
	return platformgithubapp.InstallationToken{Value: "secret-token"}, nil
}

func (f *checkoutGitHubFake) GetFreshInstallationToken(_ context.Context, request platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error) {
	f.freshTokenCalls++
	f.request = request
	if f.freshTokenErr != nil {
		return platformgithubapp.InstallationToken{}, f.freshTokenErr
	}
	return platformgithubapp.InstallationToken{Value: "fresh-secret-token"}, nil
}

func (f *checkoutGitHubFake) GetRepositoryByID(_ context.Context, request platformgithubapp.InstallationTokenRequest, _ string) (platformgithubapp.Repository, error) {
	f.repoCalls++
	f.request = request
	return f.repository, f.repoErr
}

func checkoutDetail(enabled bool) domain.SCMConnectionDetail {
	return domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: "connection-a", Provider: domain.SCMProviderGitHub, APIBaseURL: "https://api.github.com", Enabled: enabled},
		GitHubAppRegistration: &domain.GitHubAppRegistration{ID: "app-registration", AppID: "123", APIBaseURL: "https://api.github.com", PrivateKeySecretRef: "PRIVATE_KEY"},
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: "connection-a", AppRegistrationID: "app-registration", InstallationID: "456"},
	}
}

func TestRepositoryAwareCheckoutResolver_UsesExactSnapshottedIdentityAndCurrentCoordinates(t *testing.T) {
	registrations := &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100", CloneURL: "https://stale.example/old/repo.git"}}
	connections := &checkoutConnectionFake{value: checkoutDetail(true)}
	secrets := &checkoutSecretFake{value: "private-key"}
	github := &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "100", Owner: "renamed", Name: "repository", FullName: "renamed/repository", CloneURL: "https://github.com/renamed/repository.git"}}
	resolver, err := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: connections, Registrations: registrations, Secrets: secrets, GitHub: github})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	checkout, resolveErr := resolver.Resolve(context.Background(), domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"})
	if resolveErr != nil {
		t.Fatalf("resolve: %v", resolveErr)
	}
	if checkout.RepositoryURL != "https://github.com/renamed/repository.git" || checkout.Credential.Username != "x-access-token" || checkout.Credential.Password != "secret-token" {
		t.Fatalf("unexpected checkout: %#v", checkout)
	}
	if len(github.request.RepositoryIDs) != 1 || github.request.RepositoryIDs[0] != "100" {
		t.Fatalf("expected restricted token request, got %#v", github.request.RepositoryIDs)
	}
}

func TestRepositoryAwareCheckoutResolver_RejectsIdentityAndStateBeforeCredentials(t *testing.T) {
	tests := []struct {
		name         string
		registration domain.SCMRepositoryRegistration
		connection   domain.SCMConnectionDetail
		wantErr      error
	}{
		{name: "mismatched connection", registration: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-b", ProviderRepositoryID: "100"}, connection: checkoutDetail(true), wantErr: ErrRepositoryCheckoutIdentityMismatch},
		{name: "disabled repository", registration: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100", Disabled: true}, connection: checkoutDetail(true), wantErr: ErrRepositoryCheckoutRepositoryDisabled},
		{name: "disabled connection", registration: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}, connection: checkoutDetail(false), wantErr: ErrRepositoryCheckoutConnectionDisabled},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			secrets := &checkoutSecretFake{value: "private-key"}
			github := &checkoutGitHubFake{}
			resolver, err := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: &checkoutConnectionFake{value: testCase.connection}, Registrations: &checkoutRegistrationFake{value: testCase.registration}, Secrets: secrets, GitHub: github})
			if err != nil {
				t.Fatalf("new resolver: %v", err)
			}
			_, resolveErr := resolver.Resolve(context.Background(), domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"})
			if !errors.Is(resolveErr, testCase.wantErr) {
				t.Fatalf("expected %v, got %v", testCase.wantErr, resolveErr)
			}
			if secrets.calls != 0 || github.repoCalls != 0 || github.tokenCalls != 0 {
				t.Fatalf("expected no credential use, secret=%d repo=%d token=%d", secrets.calls, github.repoCalls, github.tokenCalls)
			}
		})
	}
}

func TestRepositoryAwareCheckoutResolver_RejectsProviderMismatchAndInaccessibleRepository(t *testing.T) {
	registration := &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}}
	connection := &checkoutConnectionFake{value: checkoutDetail(true)}
	secrets := &checkoutSecretFake{value: "private-key"}
	github := &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "other", CloneURL: "https://github.com/other/repository.git"}}
	resolver, _ := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: connection, Registrations: registration, Secrets: secrets, GitHub: github})
	_, err := resolver.Resolve(context.Background(), domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"})
	if !errors.Is(err, ErrRepositoryCheckoutIdentityMismatch) || github.tokenCalls != 0 {
		t.Fatalf("expected identity mismatch before token, err=%v token_calls=%d", err, github.tokenCalls)
	}

	github.repository = platformgithubapp.Repository{}
	github.repoErr = platformgithubapp.ErrRepositoryInaccessible
	_, err = resolver.Resolve(context.Background(), domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"})
	if !errors.Is(err, ErrRepositoryCheckoutRepositoryUnavailable) {
		t.Fatalf("expected inaccessible repository error, got %v", err)
	}

	registration.err = repository.ErrSCMRepositoryRegistrationNotFound
	_, err = resolver.Resolve(context.Background(), domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"})
	if !errors.Is(err, repository.ErrSCMRepositoryRegistrationNotFound) {
		t.Fatalf("expected missing registration, got %v", err)
	}
}

func TestNewRepositoryAwareCheckoutResolver_RequiresAllDependencies(t *testing.T) {
	if _, err := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{}); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestRepositoryAwareCheckoutResolver_ClassifiesConfigurationAndTokenFailures(t *testing.T) {
	snapshot := domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"}
	registration := &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}}
	github := &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "100", CloneURL: "https://github.com/acme/repository.git"}}

	invalidDetail := checkoutDetail(true)
	invalidDetail.GitHubAppRegistration = nil
	resolver, _ := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: &checkoutConnectionFake{value: invalidDetail}, Registrations: registration, Secrets: &checkoutSecretFake{value: "private-key"}, GitHub: github})
	if _, err := resolver.Resolve(context.Background(), snapshot); !errors.Is(err, ErrRepositoryCheckoutConnectionInvalid) {
		t.Fatalf("expected invalid connection, got %v", err)
	}

	resolver, _ = NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: &checkoutConnectionFake{value: checkoutDetail(true)}, Registrations: registration, Secrets: &checkoutSecretFake{}, GitHub: github})
	if _, err := resolver.Resolve(context.Background(), snapshot); !errors.Is(err, ErrRepositoryCheckoutPrivateKeyUnavailable) {
		t.Fatalf("expected unavailable key, got %v", err)
	}

	github.tokenErr = platformgithubapp.ErrInstallationUnavailable
	resolver, _ = NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: &checkoutConnectionFake{value: checkoutDetail(true)}, Registrations: registration, Secrets: &checkoutSecretFake{value: "private-key"}, GitHub: github})
	if _, err := resolver.Resolve(context.Background(), snapshot); !errors.Is(err, ErrRepositoryCheckoutRepositoryUnavailable) {
		t.Fatalf("expected unavailable repository for token failure, got %v", err)
	}
}

func TestClassifyRepositoryCheckoutProviderError(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		input   error
		wantErr error
	}{
		{name: "repository inaccessible", input: platformgithubapp.ErrRepositoryInaccessible, wantErr: ErrRepositoryCheckoutRepositoryUnavailable},
		{name: "installation unavailable", input: platformgithubapp.ErrInstallationUnavailable, wantErr: ErrRepositoryCheckoutRepositoryUnavailable},
		{name: "missing key", input: platformgithubapp.ErrPrivateKeyMissing, wantErr: ErrRepositoryCheckoutPrivateKeyUnavailable},
		{name: "malformed key", input: platformgithubapp.ErrPrivateKeyMalformed, wantErr: ErrRepositoryCheckoutPrivateKeyUnavailable},
		{name: "non rsa key", input: platformgithubapp.ErrPrivateKeyNotRSA, wantErr: ErrRepositoryCheckoutPrivateKeyUnavailable},
		{name: "unclassified", input: platformgithubapp.ErrRateLimited, wantErr: platformgithubapp.ErrRateLimited},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := classifyRepositoryCheckoutProviderError(testCase.input); !errors.Is(got, testCase.wantErr) {
				t.Fatalf("expected %v, got %v", testCase.wantErr, got)
			}
		})
	}
}

func TestRepositoryAwareCheckoutResolver_RetriesExactlyOnceWithFreshCredentialAfterAuthenticationFailure(t *testing.T) {
	registrations := &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}}
	connections := &checkoutConnectionFake{value: checkoutDetail(true)}
	secrets := &checkoutSecretFake{value: "private-key"}
	github := &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "100", CloneURL: "https://github.com/acme/repository.git"}}
	resolver, err := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: connections, Registrations: registrations, Secrets: secrets, GitHub: github})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	checkout, err := resolver.Resolve(context.Background(), domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var credentials []source.HTTPSCredential
	retryErr := checkout.RunWithCredentialRetry(context.Background(), func(credential source.HTTPSCredential) error {
		credentials = append(credentials, credential)
		if len(credentials) == 1 {
			return errors.New("fatal: Authentication failed for https://github.com/acme/repository.git")
		}
		return nil
	})
	if retryErr != nil {
		t.Fatalf("retry checkout: %v", retryErr)
	}
	if len(credentials) != 2 || credentials[0].Password != "secret-token" || credentials[1].Password != "fresh-secret-token" {
		t.Fatalf("expected normal then fresh credentials, got %#v", credentials)
	}
	if github.freshTokenCalls != 1 || github.request.AppRegistrationID != "app-registration" || github.request.InstallationID != "456" || len(github.request.RepositoryIDs) != 1 || github.request.RepositoryIDs[0] != "100" {
		t.Fatalf("expected one scoped refresh request, calls=%d request=%+v", github.freshTokenCalls, github.request)
	}
}

func TestRepositoryAwareCheckoutResolver_DoesNotRefreshNonAuthenticationFailure(t *testing.T) {
	checkout := RepositoryAwareCheckout{
		Credential: source.HTTPSCredential{Username: "x-access-token", Password: "secret-token"},
		refreshCredential: func(context.Context) (source.HTTPSCredential, error) {
			t.Fatal("refresh must not be called")
			return source.HTTPSCredential{}, nil
		},
	}
	for _, failure := range []string{"repository not found", "HTTP 404", "permission denied", "network timeout", "rate limit exceeded", "invalid ref", "clone failed"} {
		t.Run(failure, func(t *testing.T) {
			attempts := 0
			err := checkout.RunWithCredentialRetry(context.Background(), func(source.HTTPSCredential) error {
				attempts++
				return errors.New(failure)
			})
			if err == nil || attempts != 1 {
				t.Fatalf("expected one terminal attempt, err=%v attempts=%d", err, attempts)
			}
		})
	}
}

func TestRepositoryAwareCheckoutResolver_SecondAuthenticationFailureStopsAfterTwoAttempts(t *testing.T) {
	refreshes := 0
	checkout := RepositoryAwareCheckout{
		Credential: source.HTTPSCredential{Username: "x-access-token", Password: "secret-token"},
		refreshCredential: func(context.Context) (source.HTTPSCredential, error) {
			refreshes++
			return source.HTTPSCredential{Username: "x-access-token", Password: "fresh-secret-token"}, nil
		},
	}
	attempts := 0
	err := checkout.RunWithCredentialRetry(context.Background(), func(source.HTTPSCredential) error {
		attempts++
		return errors.New("remote: bad credentials")
	})
	if err == nil || attempts != 2 || refreshes != 1 {
		t.Fatalf("expected two attempts and one refresh, err=%v attempts=%d refreshes=%d", err, attempts, refreshes)
	}
}

func TestRepositoryAwareCheckoutResolver_ReturnsRefreshFailureAndRejectsNilOperation(t *testing.T) {
	checkout := RepositoryAwareCheckout{
		Credential: source.HTTPSCredential{Username: "x-access-token", Password: "secret-token"},
		refreshCredential: func(context.Context) (source.HTTPSCredential, error) {
			return source.HTTPSCredential{}, platformgithubapp.ErrInstallationUnavailable
		},
	}
	if err := checkout.RunWithCredentialRetry(context.Background(), nil); err == nil {
		t.Fatal("expected nil operation error")
	}
	err := checkout.RunWithCredentialRetry(context.Background(), func(source.HTTPSCredential) error {
		return errors.New("remote: bad credentials")
	})
	if !errors.Is(err, platformgithubapp.ErrInstallationUnavailable) {
		t.Fatalf("expected refresh failure, got %v", err)
	}
}

func TestRepositoryAwareCheckoutResolver_MappedPipelineFetchUsesAuthenticationRefresh(t *testing.T) {
	registrations := &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}}
	connections := &checkoutConnectionFake{value: checkoutDetail(true)}
	github := &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "100", CloneURL: "https://github.com/acme/repository.git"}}
	resolver, err := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: connections, Registrations: registrations, Secrets: &checkoutSecretFake{value: "private-key"}, GitHub: github})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	fetcher := &checkoutAuthenticatedFetcherFake{firstErr: errors.New("remote: bad credentials"), localPath: "/tmp/repository", commitSHA: "abc123"}
	service := NewBuildService(nil, nil, nil)
	service.SetRepoFetcher(fetcher)
	service.SetRepositoryAwareCheckoutResolver(resolver)
	localPath, commitSHA, fetchErr := service.fetchRepositoryForBuildCreation(context.Background(), CreateRepoBuildInput{
		RepositoryIdentity: &domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"},
	}, "main")
	if fetchErr != nil || localPath != "/tmp/repository" || commitSHA != "abc123" {
		t.Fatalf("expected successful refreshed fetch, path=%q sha=%q err=%v", localPath, commitSHA, fetchErr)
	}
	if fetcher.calls != 2 || github.freshTokenCalls != 1 || len(fetcher.credentials) != 2 || fetcher.credentials[0].Password != "secret-token" || fetcher.credentials[1].Password != "fresh-secret-token" {
		t.Fatalf("expected one refreshed API fetch, calls=%d refreshes=%d credentials=%#v", fetcher.calls, github.freshTokenCalls, fetcher.credentials)
	}
}

func TestRepositoryAwareCheckoutResolver_UnmappedPipelineFetchDoesNotResolveOrRefreshCredentials(t *testing.T) {
	service := NewBuildService(nil, nil, nil)
	fetcher := &fakeRepoFetcher{localPath: "/tmp/repository", commitSHA: "abc123"}
	service.SetRepoFetcher(fetcher)
	localPath, commitSHA, fetchErr := service.fetchRepositoryForBuildCreation(context.Background(), CreateRepoBuildInput{RepoURL: "https://example.test/repository.git"}, "main")
	if fetchErr != nil || localPath != "/tmp/repository" || commitSHA != "abc123" || fetcher.calls != 1 {
		t.Fatalf("expected legacy fetch without checkout resolution, path=%q sha=%q calls=%d err=%v", localPath, commitSHA, fetcher.calls, fetchErr)
	}
}

func TestRepositoryAwareCheckoutResolver_MappedPipelineFetchRejectsMissingCheckoutOrAuthenticatedFetcher(t *testing.T) {
	identity := &domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"}
	service := NewBuildService(nil, nil, nil)
	service.SetRepoFetcher(&fakeRepoFetcher{})
	if _, _, err := service.fetchRepositoryForBuildCreation(context.Background(), CreateRepoBuildInput{RepositoryIdentity: identity}, "main"); !errors.Is(err, ErrRepositoryCheckoutConnectionInvalid) {
		t.Fatalf("expected missing checkout resolver error, got %v", err)
	}

	resolver, resolverErr := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: &checkoutConnectionFake{value: checkoutDetail(true)}, Registrations: &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}}, Secrets: &checkoutSecretFake{value: "private-key"}, GitHub: &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "100", CloneURL: "https://github.com/acme/repository.git"}}})
	if resolverErr != nil {
		t.Fatalf("new resolver: %v", resolverErr)
	}
	service.SetRepositoryAwareCheckoutResolver(resolver)
	if _, _, err := service.fetchRepositoryForBuildCreation(context.Background(), CreateRepoBuildInput{RepositoryIdentity: identity}, "main"); !errors.Is(err, ErrRepositoryCheckoutConnectionInvalid) {
		t.Fatalf("expected missing authenticated fetcher error, got %v", err)
	}
}

func TestRepositoryAwareCheckoutResolver_MappedPipelineFetchReturnsTerminalFailure(t *testing.T) {
	registrations := &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}}
	github := &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "100", CloneURL: "https://github.com/acme/repository.git"}}
	resolver, err := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: &checkoutConnectionFake{value: checkoutDetail(true)}, Registrations: registrations, Secrets: &checkoutSecretFake{value: "private-key"}, GitHub: github})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	fetcher := &checkoutAuthenticatedFetcherFake{firstErr: errors.New("repository not found")}
	service := NewBuildService(nil, nil, nil)
	service.SetRepoFetcher(fetcher)
	service.SetRepositoryAwareCheckoutResolver(resolver)
	_, _, fetchErr := service.fetchRepositoryForBuildCreation(context.Background(), CreateRepoBuildInput{RepositoryIdentity: &domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"}}, "main")
	if fetchErr == nil || fetcher.calls != 1 || github.freshTokenCalls != 0 {
		t.Fatalf("expected one terminal fetch failure without refresh, err=%v calls=%d refreshes=%d", fetchErr, fetcher.calls, github.freshTokenCalls)
	}
}

func TestRepositoryAwareCheckoutResolver_MappedWorkspaceCloneUsesAuthenticatedRetry(t *testing.T) {
	registrations := &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}}
	github := &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "100", CloneURL: "https://github.com/acme/repository.git"}}
	resolver, err := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: &checkoutConnectionFake{value: checkoutDetail(true)}, Registrations: registrations, Secrets: &checkoutSecretFake{value: "private-key"}, GitHub: github})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	workspace := &checkoutAuthenticatedWorkspaceResolverFake{firstErr: errors.New("fatal: Authentication failed")}
	service := NewBuildService(nil, nil, nil)
	service.SetSourceResolver(workspace)
	service.SetRepositoryAwareCheckoutResolver(resolver)
	err = service.cloneBuildSourceIntoWorkspace(context.Background(), "/tmp/build", execution.ResolvedBuildSourceSpec{RepositoryURL: "https://stale.example/repository.git", Ref: "main", HasSource: true, RepositoryIdentity: &domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"}})
	if err != nil {
		t.Fatalf("authenticated clone: %v", err)
	}
	if workspace.cloneCalls != 0 || workspace.authenticatedCalls != 2 || github.freshTokenCalls != 1 || len(workspace.credentials) != 2 || workspace.credentials[1].Password != "fresh-secret-token" {
		t.Fatalf("expected authenticated retry, legacy=%d authenticated=%d refreshes=%d credentials=%#v", workspace.cloneCalls, workspace.authenticatedCalls, github.freshTokenCalls, workspace.credentials)
	}
}

func TestRepositoryAwareCheckoutResolver_WorkspaceCloneRejectsMissingCheckoutOrAuthenticationSupport(t *testing.T) {
	service := NewBuildService(nil, nil, nil)
	legacyResolver := &fakeWorkspaceSourceResolver{}
	service.SetSourceResolver(legacyResolver)
	identity := &domain.RepositoryIdentitySnapshot{RegisteredRepositoryID: "repository-a", SCMConnectionID: "connection-a", ProviderRepositoryID: "100"}
	spec := execution.ResolvedBuildSourceSpec{RepositoryURL: "https://example.test/repository.git", RepositoryIdentity: identity}
	if err := service.cloneBuildSourceIntoWorkspace(context.Background(), "/tmp/build", spec); !errors.Is(err, ErrRepositoryCheckoutConnectionInvalid) {
		t.Fatalf("expected missing checkout resolver error, got %v", err)
	}

	resolver, resolverErr := NewRepositoryAwareCheckoutResolver(RepositoryAwareCheckoutResolverConfig{Connections: &checkoutConnectionFake{value: checkoutDetail(true)}, Registrations: &checkoutRegistrationFake{value: domain.SCMRepositoryRegistration{ID: "repository-a", ConnectionID: "connection-a", ProviderRepositoryID: "100"}}, Secrets: &checkoutSecretFake{value: "private-key"}, GitHub: &checkoutGitHubFake{repository: platformgithubapp.Repository{ID: "100", CloneURL: "https://github.com/acme/repository.git"}}})
	if resolverErr != nil {
		t.Fatalf("new resolver: %v", resolverErr)
	}
	service.SetRepositoryAwareCheckoutResolver(resolver)
	if err := service.cloneBuildSourceIntoWorkspace(context.Background(), "/tmp/build", spec); !errors.Is(err, ErrRepositoryCheckoutConnectionInvalid) {
		t.Fatalf("expected missing authenticated resolver error, got %v", err)
	}
}
