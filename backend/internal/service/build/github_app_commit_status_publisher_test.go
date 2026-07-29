package build

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformgithubapp "github.com/radiation/coyote-ci/backend/internal/platform/githubapp"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type publisherConnectionRepoFake struct {
	detail domain.SCMConnectionDetail
	err    error
	calls  int
}

func (f *publisherConnectionRepoFake) GetByID(_ context.Context, _ string) (domain.SCMConnectionDetail, error) {
	f.calls++
	return f.detail, f.err
}

type publisherRegistrationRepoFake struct {
	registration domain.SCMRepositoryRegistration
	err          error
	calls        int
}

func (f *publisherRegistrationRepoFake) GetByID(_ context.Context, _ string) (domain.SCMRepositoryRegistration, error) {
	f.calls++
	return f.registration, f.err
}

type publisherSecretResolverFake struct {
	value string
	err   error
	calls int
}

func (f *publisherSecretResolverFake) Resolve(_ context.Context, _ string) (string, error) {
	f.calls++
	return f.value, f.err
}

type publisherGitHubAppClientFake struct {
	token    platformgithubapp.InstallationToken
	err      error
	requests []platformgithubapp.InstallationTokenRequest
}

func (f *publisherGitHubAppClientFake) GetInstallationToken(_ context.Context, input platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error) {
	f.requests = append(f.requests, input)
	return f.token, f.err
}

func TestGitHubAppCommitStatusPublisher_PublishesUsingSnapshottedConnectionAndCurrentCoordinates(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestCount++
		if req.URL.Path != "/repos/renamed-owner/renamed-repository/statuses/deadbeef" {
			t.Fatalf("unexpected GitHub status path: %s", req.URL.Path)
		}
		if req.Header.Get("Authorization") != "Bearer installation-token" {
			t.Fatalf("unexpected authorization: %q", req.Header.Get("Authorization"))
		}
		var payload map[string]string
		if decodeErr := json.NewDecoder(req.Body).Decode(&payload); decodeErr != nil {
			t.Fatalf("decode status payload: %v", decodeErr)
		}
		if payload["state"] != "success" || payload["context"] != "coyote/build" {
			t.Fatalf("unexpected status payload: %+v", payload)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	publisher, registrationRepo, connectionRepo, secrets, githubApps := newPublisherHarness(server.URL)
	registrationRepo.registration.Owner = "renamed-owner"
	registrationRepo.registration.Name = "renamed-repository"
	connectionRepo.detail.Connection.APIBaseURL = server.URL
	connectionRepo.detail.GitHubAppRegistration.APIBaseURL = server.URL
	publisher.httpClient = NewGitHubCommitStatusClient("https://default.example", server.Client(), "")

	if err := publisher.PublishCommitStatus(context.Background(), publisherRequest("registration-1", "connection-1", "repository-1")); err != nil {
		t.Fatalf("publish status: %v", err)
	}
	if requestCount != 1 || registrationRepo.calls != 1 || connectionRepo.calls != 1 || secrets.calls != 1 {
		t.Fatalf("unexpected dependency calls: requests=%d registrations=%d connections=%d secrets=%d", requestCount, registrationRepo.calls, connectionRepo.calls, secrets.calls)
	}
	if len(githubApps.requests) != 1 {
		t.Fatalf("expected one installation token request, got %d", len(githubApps.requests))
	}
	request := githubApps.requests[0]
	if request.AppRegistrationID != "app-registration-1" || request.AppID != "app-1" || request.InstallationID != "installation-1" || request.APIBaseURL != server.URL || request.PrivateKeyPEM != "private-key-1" {
		t.Fatalf("token exchange did not use the snapshotted connection credentials: %+v", request)
	}
}

func TestNewGitHubAppCommitStatusPublisherValidationAndDefaults(t *testing.T) {
	if _, err := NewGitHubAppCommitStatusPublisher(GitHubAppCommitStatusPublisherConfig{}); err == nil {
		t.Fatal("expected missing dependencies to be rejected")
	}

	_, registrations, connections, secrets, githubApps := newPublisherHarness("https://api.example.test")
	publisher, err := NewGitHubAppCommitStatusPublisher(GitHubAppCommitStatusPublisherConfig{Connections: connections, Registrations: registrations, Secrets: secrets, GitHubApps: githubApps})
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	if publisher.httpClient == nil || publisher.httpClient.baseURL != defaultGitHubCommitStatusAPIBaseURL {
		t.Fatalf("expected default status client, got %+v", publisher.httpClient)
	}
	if err := scmStatusIdentityError("identity", nil); err.Reason() != "identity" || err.message != "" {
		t.Fatalf("unexpected identity error: %+v", err)
	}
}

func TestGitHubAppCommitStatusPublisher_RejectsInvalidIdentityBeforeProviderCalls(t *testing.T) {
	cases := []struct {
		name          string
		registration  func(*publisherRegistrationRepoFake)
		connection    func(*publisherConnectionRepoFake)
		secrets       func(*publisherSecretResolverFake)
		githubAppsErr error
		request       SCMCommitStatusPublishRequest
		wantReason    string
	}{
		{name: "missing snapshot", request: SCMCommitStatusPublishRequest{}, wantReason: "github_status_delivery_identity_missing"},
		{name: "registration unavailable", registration: func(f *publisherRegistrationRepoFake) { f.err = repository.ErrSCMRepositoryRegistrationNotFound }, request: publisherRequest("registration-1", "connection-1", "repository-1"), wantReason: "github_status_registered_repository_unavailable"},
		{name: "connection mismatch", registration: func(f *publisherRegistrationRepoFake) { f.registration.ConnectionID = "connection-2" }, request: publisherRequest("registration-1", "connection-1", "repository-1"), wantReason: "github_status_repository_identity_mismatch"},
		{name: "provider repository mismatch", registration: func(f *publisherRegistrationRepoFake) { f.registration.ProviderRepositoryID = "repository-2" }, request: publisherRequest("registration-1", "connection-1", "repository-1"), wantReason: "github_status_repository_identity_mismatch"},
		{name: "disabled connection", connection: func(f *publisherConnectionRepoFake) { f.detail.Connection.Enabled = false }, request: publisherRequest("registration-1", "connection-1", "repository-1"), wantReason: "github_status_connection_disabled"},
		{name: "non github connection", connection: func(f *publisherConnectionRepoFake) { f.detail.Connection.Provider = domain.SCMProviderGitLab }, request: publisherRequest("registration-1", "connection-1", "repository-1"), wantReason: "github_status_connection_invalid"},
		{name: "invalid github configuration", connection: func(f *publisherConnectionRepoFake) { f.detail.GitHubAppInstallation = nil }, request: publisherRequest("registration-1", "connection-1", "repository-1"), wantReason: "github_status_connection_invalid"},
		{name: "private key unavailable", secrets: func(f *publisherSecretResolverFake) { f.err = errors.New("secret unavailable") }, request: publisherRequest("registration-1", "connection-1", "repository-1"), wantReason: "github_status_private_key_unavailable"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			publisher, registrationRepo, connectionRepo, secrets, githubApps := newPublisherHarness("https://api.example.test")
			if test.registration != nil {
				test.registration(registrationRepo)
			}
			if test.connection != nil {
				test.connection(connectionRepo)
			}
			if test.secrets != nil {
				test.secrets(secrets)
			}
			githubApps.err = test.githubAppsErr

			err := publisher.PublishCommitStatus(context.Background(), test.request)
			var statusErr *GitHubCommitStatusError
			if !errors.As(err, &statusErr) || statusErr.Reason() != test.wantReason {
				t.Fatalf("expected reason %q, got %v", test.wantReason, err)
			}
			if test.wantReason != "github_status_private_key_unavailable" && test.wantReason != "github_status_connection_invalid" {
				return
			}
			if len(githubApps.requests) != 0 {
				t.Fatalf("invalid configuration must not exchange an installation token: %+v", githubApps.requests)
			}
		})
	}
}

func TestGitHubAppCommitStatusPublisher_AllowsDisabledOrArchivedRegistrationAndClassifiesTokenErrors(t *testing.T) {
	for _, test := range []struct {
		name          string
		registration  func(*domain.SCMRepositoryRegistration)
		githubAppsErr error
		wantHTTPCalls int
		wantReason    string
		wantRetryable bool
	}{
		{name: "disabled registration", registration: func(r *domain.SCMRepositoryRegistration) { r.Disabled = true }, wantHTTPCalls: 1},
		{name: "archived registration", registration: func(r *domain.SCMRepositoryRegistration) { r.Archived = true }, wantHTTPCalls: 1},
		{name: "authentication", githubAppsErr: platformgithubapp.ErrAuthentication, wantReason: "github_status_app_authentication_failed"},
		{name: "installation unavailable", githubAppsErr: platformgithubapp.ErrInstallationUnavailable, wantReason: "github_status_installation_unavailable"},
		{name: "invalid private key", githubAppsErr: platformgithubapp.ErrPrivateKeyMalformed, wantReason: "github_status_private_key_invalid"},
		{name: "repository inaccessible", githubAppsErr: platformgithubapp.ErrRepositoryInaccessible, wantReason: "github_status_repository_inaccessible"},
		{name: "rate limited", githubAppsErr: platformgithubapp.ErrRateLimited, wantReason: "github_status_rate_limited", wantRetryable: true},
		{name: "provider unavailable", githubAppsErr: platformgithubapp.ErrProviderUnavailable, wantReason: "github_status_provider_unavailable", wantRetryable: true},
		{name: "malformed response", githubAppsErr: platformgithubapp.ErrMalformedResponse, wantReason: "github_status_provider_malformed_response", wantRetryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			httpCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				httpCalls++
				w.WriteHeader(http.StatusCreated)
			}))
			defer server.Close()
			publisher, registrationRepo, connectionRepo, _, githubApps := newPublisherHarness(server.URL)
			connectionRepo.detail.Connection.APIBaseURL = server.URL
			connectionRepo.detail.GitHubAppRegistration.APIBaseURL = server.URL
			if test.registration != nil {
				test.registration(&registrationRepo.registration)
			}
			githubApps.err = test.githubAppsErr

			err := publisher.PublishCommitStatus(context.Background(), publisherRequest("registration-1", "connection-1", "repository-1"))
			if test.githubAppsErr != nil {
				var statusErr *GitHubCommitStatusError
				if !errors.As(err, &statusErr) || statusErr.Reason() != test.wantReason || statusErr.Retryable() != test.wantRetryable {
					t.Fatalf("expected reason=%q retryable=%v, got %v", test.wantReason, test.wantRetryable, err)
				}
				if !errors.Is(err, test.githubAppsErr) {
					t.Fatalf("expected classified error to preserve %v, got %v", test.githubAppsErr, err)
				}
				decision := classifySCMStatusDeliveryFailure(err)
				if decision.reason != test.wantReason || decision.retryable != test.wantRetryable {
					t.Fatalf("expected delivery decision reason=%q retryable=%v, got %+v", test.wantReason, test.wantRetryable, decision)
				}
			} else if err != nil {
				t.Fatalf("publish status: %v", err)
			}
			if httpCalls != test.wantHTTPCalls {
				t.Fatalf("expected %d HTTP calls, got %d", test.wantHTTPCalls, httpCalls)
			}
		})
	}
}

func TestGitHubAppCommitStatusPublisher_SeparatesCredentialsForMatchingMetadataAcrossConnections(t *testing.T) {
	connectionDetails := map[string]domain.SCMConnectionDetail{}
	registrations := map[string]domain.SCMRepositoryRegistration{}
	for _, connectionID := range []string{"connection-1", "connection-2"} {
		_, registrationRepo, connectionRepo, _, _ := newPublisherHarness("https://api.example.test")
		detail := connectionRepo.detail
		detail.Connection.ID = connectionID
		detail.GitHubAppInstallation.ConnectionID = connectionID
		detail.GitHubAppInstallation.InstallationID = "installation-" + connectionID
		connectionDetails[connectionID] = detail
		registration := registrationRepo.registration
		registration.ID = "registration-" + connectionID
		registration.ConnectionID = connectionID
		registrations[registration.ID] = registration
	}
	connections := &publisherConnectionMapFake{details: connectionDetails}
	registrationRepo := &publisherRegistrationMapFake{registrations: registrations}
	secrets := &publisherSecretResolverFake{value: "private-key-1"}
	githubApps := &publisherGitHubAppClientFake{token: platformgithubapp.InstallationToken{Value: "installation-token", ExpiresAt: time.Now().Add(time.Hour)}}
	publisher, err := NewGitHubAppCommitStatusPublisher(GitHubAppCommitStatusPublisherConfig{Connections: connections, Registrations: registrationRepo, Secrets: secrets, GitHubApps: githubApps, HTTPClient: NewGitHubCommitStatusClient("https://api.example.test", &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody, Header: make(http.Header)}, nil
	})}, "")})
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	for _, connectionID := range []string{"connection-1", "connection-2"} {
		registrationID := "registration-" + connectionID
		if publishErr := publisher.PublishCommitStatus(context.Background(), publisherRequest(registrationID, connectionID, "repository-1")); publishErr != nil {
			t.Fatalf("publish for %s: %v", connectionID, publishErr)
		}
	}
	if len(githubApps.requests) != 2 || githubApps.requests[0].InstallationID == githubApps.requests[1].InstallationID {
		t.Fatalf("expected distinct installation credentials for matching repository metadata: %+v", githubApps.requests)
	}
}

func TestClassifyGitHubAppTokenErrorPreservesContextAndTimeoutErrors(t *testing.T) {
	for _, tokenErr := range []error{context.Canceled, context.DeadlineExceeded, scmTimeoutNetError{}} {
		classified := classifyGitHubAppTokenError(tokenErr)
		if classified != tokenErr {
			t.Fatalf("expected %v to remain unwrapped, got %v", tokenErr, classified)
		}
		decision := classifySCMStatusDeliveryFailure(classified)
		if !decision.retryable || (decision.reason != "context_canceled" && decision.reason != "network_timeout") {
			t.Fatalf("expected existing retry classifier decision, got %+v", decision)
		}
	}
}

func newPublisherHarness(apiBaseURL string) (*GitHubAppCommitStatusPublisher, *publisherRegistrationRepoFake, *publisherConnectionRepoFake, *publisherSecretResolverFake, *publisherGitHubAppClientFake) {
	now := time.Now().UTC()
	registrationRepo := &publisherRegistrationRepoFake{registration: domain.SCMRepositoryRegistration{ID: "registration-1", ConnectionID: "connection-1", ProviderRepositoryID: "repository-1", Owner: "octo", Name: "repo"}}
	connectionRepo := &publisherConnectionRepoFake{detail: domain.SCMConnectionDetail{Connection: domain.SCMConnection{ID: "connection-1", Provider: domain.SCMProviderGitHub, APIBaseURL: apiBaseURL, Enabled: true}, GitHubAppRegistration: &domain.GitHubAppRegistration{ID: "app-registration-1", AppID: "app-1", APIBaseURL: apiBaseURL, PrivateKeySecretRef: "secret/private-key", CreatedAt: now, UpdatedAt: now}, GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: "connection-1", AppRegistrationID: "app-registration-1", InstallationID: "installation-1", CreatedAt: now, UpdatedAt: now}}}
	secrets := &publisherSecretResolverFake{value: "private-key-1"}
	githubApps := &publisherGitHubAppClientFake{token: platformgithubapp.InstallationToken{Value: "installation-token", ExpiresAt: now.Add(time.Hour)}}
	publisher, _ := NewGitHubAppCommitStatusPublisher(GitHubAppCommitStatusPublisherConfig{Connections: connectionRepo, Registrations: registrationRepo, Secrets: secrets, GitHubApps: githubApps, HTTPClient: NewGitHubCommitStatusClient(apiBaseURL, nil, "")})
	return publisher, registrationRepo, connectionRepo, secrets, githubApps
}

func publisherRequest(registrationID string, connectionID string, providerRepositoryID string) SCMCommitStatusPublishRequest {
	return SCMCommitStatusPublishRequest{RegisteredRepositoryID: &registrationID, SCMConnectionID: &connectionID, ProviderRepositoryID: &providerRepositoryID, CommitSHA: "deadbeef", Context: "coyote/build", State: domain.SCMCommitStatusStateSuccess, Description: "Coyote build succeeded"}
}

type publisherConnectionMapFake struct {
	details map[string]domain.SCMConnectionDetail
}

func (f *publisherConnectionMapFake) GetByID(_ context.Context, id string) (domain.SCMConnectionDetail, error) {
	detail, ok := f.details[id]
	if !ok {
		return domain.SCMConnectionDetail{}, errors.New("connection not found")
	}
	return detail, nil
}

type publisherRegistrationMapFake struct {
	registrations map[string]domain.SCMRepositoryRegistration
}

func (f *publisherRegistrationMapFake) GetByID(_ context.Context, id string) (domain.SCMRepositoryRegistration, error) {
	registration, ok := f.registrations[id]
	if !ok {
		return domain.SCMRepositoryRegistration{}, errors.New("registration not found")
	}
	return registration, nil
}
