package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformgithubapp "github.com/radiation/coyote-ci/backend/internal/platform/githubapp"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestSCMAdminService_CreateConnectionAndRepository(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Now().UTC()
	svc.now = func() time.Time { return now }
	privateKeyPEM, _ := testServiceRSAPrivateKeyPEM(t)
	svc.secrets = &fakeSCMSecretResolver{value: privateKeyPEM}
	registration, registrationErr := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{
		AppID:               "12345",
		PrivateKeySecretRef: "secret/github/private-key",
		WebhookSecretRef:    "secret/github/webhook",
	})
	if registrationErr != nil {
		t.Fatalf("create github app registration failed: %v", registrationErr)
	}
	enabled := true

	connection, createErr := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{
		AppRegistrationID: registration.ID,
		DisplayName:       "octo cloud",
		Enabled:           &enabled,
		InstallationID:    "999",
		AccountLogin:      "octo",
		AccountType:       "organization",
		TargetID:          "42",
	})
	if createErr != nil {
		t.Fatalf("create connection failed: %v", createErr)
	}

	resolver := &fakeSCMGitHubRepositoryResolver{
		byIDRepository: platformgithubapp.Repository{
			ID:            "1001",
			Owner:         "octo-renamed",
			Name:          "widgets-renamed",
			FullName:      "octo-renamed/widgets-renamed",
			CloneURL:      "https://github.com/octo-renamed/widgets-renamed.git",
			WebURL:        "https://github.com/octo-renamed/widgets-renamed",
			DefaultBranch: stringPtr("main"),
			Archived:      false,
			Disabled:      true,
		},
	}
	svc.githubRepos = resolver

	repositoryRegistration, repoErr := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{
		ConnectionID:         connection.Connection.ID,
		ProviderRepositoryID: "1001",
	})
	if repoErr != nil {
		t.Fatalf("create repository failed: %v", repoErr)
	}
	if repositoryRegistration.ConnectionID != connection.Connection.ID {
		t.Fatalf("expected repository to attach to connection, got %+v", repositoryRegistration)
	}
	if repositoryRegistration.FullName != "octo-renamed/widgets-renamed" || !repositoryRegistration.Disabled {
		t.Fatalf("expected authoritative metadata from provider, got %+v", repositoryRegistration)
	}
	if len(resolver.byIDRequests) != 1 || resolver.byIDRequests[0].RepositoryID != "1001" {
		t.Fatalf("expected repository lookup by id, got %+v", resolver.byIDRequests)
	}
}

func TestSCMAdminService_ListAndGetGitHubAppRegistrations(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Now().UTC()
	svc.now = func() time.Time { return now }
	_, firstErr := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{
		AppID:               "12345",
		PrivateKeySecretRef: "secret/github/private-key",
		WebhookSecretRef:    "secret/github/webhook",
	})
	if firstErr != nil {
		t.Fatalf("create first registration failed: %v", firstErr)
	}
	_, secondErr := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{
		AppID:               "54321",
		APIBaseURL:          "https://ghe.example/api/v3",
		WebBaseURL:          "https://ghe.example",
		WebhookSecretRef:    "secret/github/webhook-2",
		PrivateKeySecretRef: "secret/github/private-key-2",
	})
	if secondErr != nil {
		t.Fatalf("create second registration failed: %v", secondErr)
	}
	list, listErr := svc.ListGitHubAppRegistrations(context.Background())
	if listErr != nil {
		t.Fatalf("list registrations failed: %v", listErr)
	}
	if len(list) != 2 {
		t.Fatalf("expected two registrations, got %d", len(list))
	}
	fetched, getErr := svc.GetGitHubAppRegistration(context.Background(), list[0].ID)
	if getErr != nil {
		t.Fatalf("get registration failed: %v", getErr)
	}
	if fetched.ID != list[0].ID {
		t.Fatalf("expected fetched registration id %q, got %q", list[0].ID, fetched.ID)
	}
	_, missingErr := svc.GetGitHubAppRegistration(context.Background(), "missing")
	if missingErr != repository.ErrSCMGitHubAppRegistrationNotFound {
		t.Fatalf("expected missing registration error, got %v", missingErr)
	}
}

func TestSCMAdminService_GitHubRegistrationSharedAcrossInstallationsAndSiblingDisableIndependence(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Now().UTC()
	svc.now = func() time.Time { return now }

	registration, registrationErr := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{
		AppID:               "12345",
		PrivateKeySecretRef: "secret/github/private-key",
		WebhookSecretRef:    "secret/github/webhook",
	})
	if registrationErr != nil {
		t.Fatalf("create github app registration failed: %v", registrationErr)
	}
	enabled := true
	first, firstErr := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{
		AppRegistrationID: registration.ID,
		DisplayName:       "octo cloud",
		Enabled:           &enabled,
		InstallationID:    "999",
		AccountLogin:      "octo",
		AccountType:       "organization",
		TargetID:          "42",
	})
	if firstErr != nil {
		t.Fatalf("create first connection failed: %v", firstErr)
	}
	second, secondErr := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{
		AppRegistrationID: registration.ID,
		DisplayName:       "octo sibling",
		Enabled:           &enabled,
		InstallationID:    "1000",
		AccountLogin:      "octo-two",
		AccountType:       "organization",
		TargetID:          "84",
	})
	if secondErr != nil {
		t.Fatalf("create second connection failed: %v", secondErr)
	}
	if first.GitHubAppRegistration == nil || second.GitHubAppRegistration == nil || first.GitHubAppRegistration.ID != second.GitHubAppRegistration.ID {
		t.Fatalf("expected shared registration, got first=%+v second=%+v", first.GitHubAppRegistration, second.GitHubAppRegistration)
	}

	disable := false
	updated, updateErr := svc.SetConnectionEnabled(context.Background(), first.Connection.ID, &disable)
	if updateErr != nil {
		t.Fatalf("disable first connection failed: %v", updateErr)
	}
	if updated.Connection.Enabled {
		t.Fatal("expected first connection to be disabled")
	}
	fetchedSecond, getErr := svc.GetConnection(context.Background(), second.Connection.ID)
	if getErr != nil {
		t.Fatalf("get second connection failed: %v", getErr)
	}
	if !fetchedSecond.Connection.Enabled {
		t.Fatal("expected sibling connection to remain enabled")
	}
	if fetchedSecond.GitHubAppRegistration == nil || fetchedSecond.GitHubAppRegistration.ID != registration.ID {
		t.Fatalf("expected shared registration to remain attached, got %+v", fetchedSecond.GitHubAppRegistration)
	}
	storedRegistration, storedRegistrationErr := connectionRepo.GetGitHubAppRegistrationByID(context.Background(), registration.ID)
	if storedRegistrationErr != nil {
		t.Fatalf("get registration failed: %v", storedRegistrationErr)
	}
	if storedRegistration.UpdatedAt != registration.UpdatedAt {
		t.Fatalf("expected registration timestamps unchanged, got %s want %s", storedRegistration.UpdatedAt, registration.UpdatedAt)
	}
	_, missingErr := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{
		AppRegistrationID: "missing-registration",
		DisplayName:       "missing",
		Enabled:           &enabled,
		InstallationID:    "1001",
		AccountLogin:      "octo-three",
		AccountType:       "organization",
		TargetID:          "126",
	})
	if missingErr != repository.ErrSCMGitHubAppRegistrationNotFound {
		t.Fatalf("expected missing registration error, got %v", missingErr)
	}
	_, duplicateErr := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{
		AppRegistrationID: registration.ID,
		DisplayName:       "duplicate",
		Enabled:           &enabled,
		InstallationID:    "999",
		AccountLogin:      "octo-duplicate",
		AccountType:       "organization",
		TargetID:          "168",
	})
	if duplicateErr != repository.ErrSCMGitHubAppInstallationConflict {
		t.Fatalf("expected duplicate installation conflict, got %v", duplicateErr)
	}
}

func TestSCMAdminServiceValidationAndReadBranches(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Now().UTC()
	svc.now = func() time.Time { return now }

	if _, err := svc.ListConnections(context.Background()); err != nil {
		t.Fatalf("list connections should succeed on empty repo: %v", err)
	}
	if _, err := svc.ListGitHubAppRegistrations(context.Background()); err != nil {
		t.Fatalf("list registrations should succeed on empty repo: %v", err)
	}
	if _, err := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{}); err != ErrSCMGitHubAppIDRequired {
		t.Fatalf("expected missing app id error, got %v", err)
	}
	if _, err := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{AppID: "12345"}); err != ErrSCMGitHubPrivateKeySecretRefRequired {
		t.Fatalf("expected missing private key ref error, got %v", err)
	}
	if _, err := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{AppID: "12345", PrivateKeySecretRef: "secret/a"}); err != ErrSCMGitHubWebhookSecretRefRequired {
		t.Fatalf("expected missing webhook ref error, got %v", err)
	}

	enabled := true
	if _, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{}); err != ErrSCMGitHubAppRegistrationIDRequired {
		t.Fatalf("expected missing app registration id error, got %v", err)
	}
	if _, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: "reg"}); err != ErrSCMConnectionDisplayNameRequired {
		t.Fatalf("expected missing display name error, got %v", err)
	}
	if _, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: "reg", DisplayName: "name"}); err != ErrSCMConnectionEnabledRequired {
		t.Fatalf("expected missing enabled error, got %v", err)
	}
	if _, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: "reg", DisplayName: "name", Enabled: &enabled}); err != ErrSCMGitHubInstallationIDRequired {
		t.Fatalf("expected missing installation id error, got %v", err)
	}
	if _, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: "reg", DisplayName: "name", Enabled: &enabled, InstallationID: "inst"}); err != ErrSCMGitHubAccountLoginRequired {
		t.Fatalf("expected missing account login error, got %v", err)
	}
	if _, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: "reg", DisplayName: "name", Enabled: &enabled, InstallationID: "inst", AccountLogin: "octo"}); err != ErrSCMGitHubAccountTypeRequired {
		t.Fatalf("expected missing account type error, got %v", err)
	}
	if _, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: "reg", DisplayName: "name", Enabled: &enabled, InstallationID: "inst", AccountLogin: "octo", AccountType: "organization"}); err != ErrSCMGitHubTargetIDRequired {
		t.Fatalf("expected missing target id error, got %v", err)
	}
	if _, err := svc.SetConnectionEnabled(context.Background(), "connection-1", nil); err != ErrSCMConnectionEnabledRequired {
		t.Fatalf("expected missing enabled patch error, got %v", err)
	}
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{}); err != ErrSCMRegisteredRepositoryConnectionIDRequired {
		t.Fatalf("expected missing connection id error, got %v", err)
	}
	for _, input := range []CreateSCMRepositoryRegistrationInput{
		{ConnectionID: "connection-1"},
		{ConnectionID: "connection-1", Owner: "octo"},
		{ConnectionID: "connection-1", Name: "repo"},
		{ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "repo"},
	} {
		if _, err := svc.CreateRegisteredRepository(context.Background(), input); err != ErrSCMRegisteredRepositorySelectorInvalid {
			t.Fatalf("expected selector error for %+v, got %v", input, err)
		}
	}
	if inferGitHubDeploymentKind("https://api.github.com", "https://github.com") != domain.SCMDeploymentKindCloud {
		t.Fatal("expected public GitHub hosts to infer cloud deployment")
	}
	if inferGitHubDeploymentKind("https://ghe.example/api/v3", "https://ghe.example") != domain.SCMDeploymentKindSelfHosted {
		t.Fatal("expected non-public GitHub hosts to infer self-hosted deployment")
	}
}

func TestSCMAdminService_GitHubInstallationTokenValidationAndSuccess(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	privateKeyPEM, _ := testServiceRSAPrivateKeyPEM(t)
	resolver := &fakeSCMSecretResolver{value: privateKeyPEM}
	githubApps := &fakeSCMGitHubAppClient{token: platformgithubapp.InstallationToken{Value: "ghs_token", ExpiresAt: now.Add(10 * time.Minute)}}
	svc.secrets = resolver
	svc.githubApps = githubApps

	registration, err := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{AppID: "12345", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook"})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	enabled := true
	connection, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: registration.ID, DisplayName: "octo", Enabled: &enabled, InstallationID: "999", AccountLogin: "octo", AccountType: "organization", TargetID: "42"})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	token, err := svc.GetGitHubAppInstallationToken(context.Background(), connection.Connection.ID)
	if err != nil {
		t.Fatalf("get installation token: %v", err)
	}
	if token != "ghs_token" {
		t.Fatalf("expected token value, got %q", token)
	}
	if len(githubApps.tokenRequests) != 1 {
		t.Fatalf("expected one token request, got %d", len(githubApps.tokenRequests))
	}
	if strings.Contains(githubApps.tokenRequests[0].PrivateKeyPEM, "ghs_") {
		t.Fatal("unexpected token leaked into private key request")
	}

	disable := false
	_, setEnabledErr := svc.SetConnectionEnabled(context.Background(), connection.Connection.ID, &disable)
	if setEnabledErr != nil {
		t.Fatalf("disable connection: %v", setEnabledErr)
	}
	_, err = svc.GetGitHubAppInstallationToken(context.Background(), connection.Connection.ID)
	if err != ErrSCMConnectionDisabled {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestSCMAdminService_TestConnectionPersistsHealthyAndUnhealthyStates(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Date(2026, 7, 17, 19, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	privateKeyPEM, _ := testServiceRSAPrivateKeyPEM(t)
	resolver := &fakeSCMSecretResolver{value: privateKeyPEM}
	githubApps := &fakeSCMGitHubAppClient{probe: platformgithubapp.InstallationProbeResult{InstallationID: "999", AccountLogin: "octo"}}
	svc.secrets = resolver
	svc.githubApps = githubApps
	registration, err := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{AppID: "12345", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook"})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	enabled := true
	connection, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: registration.ID, DisplayName: "octo", Enabled: &enabled, InstallationID: "999", AccountLogin: "octo", AccountType: "organization", TargetID: "42"})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	updated, err := svc.TestConnection(context.Background(), connection.Connection.ID)
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if updated.Connection.HealthStatus != domain.SCMConnectionHealthStatusHealthy {
		t.Fatalf("expected healthy status, got %q", updated.Connection.HealthStatus)
	}
	if updated.Connection.HealthSummary == nil || *updated.Connection.HealthSummary == "" {
		t.Fatal("expected health summary")
	}
	if updated.Connection.LastHealthCheckedAt == nil || !updated.Connection.LastHealthCheckedAt.Equal(now) {
		t.Fatalf("expected checked at %s, got %+v", now, updated.Connection.LastHealthCheckedAt)
	}

	for _, tc := range []struct {
		name       string
		probeErr   error
		wantErr    error
		wantStatus domain.SCMConnectionHealthStatus
	}{
		{name: "auth", probeErr: platformgithubapp.ErrAuthentication, wantErr: ErrSCMGitHubAuthenticationFailed, wantStatus: domain.SCMConnectionHealthStatusUnhealthy},
		{name: "installation unavailable", probeErr: platformgithubapp.ErrInstallationUnavailable, wantErr: ErrSCMGitHubInstallationUnavailable, wantStatus: domain.SCMConnectionHealthStatusRevoked},
		{name: "rate limited", probeErr: platformgithubapp.ErrRateLimited, wantErr: ErrSCMGitHubRateLimited, wantStatus: domain.SCMConnectionHealthStatusDegraded},
		{name: "provider unavailable", probeErr: platformgithubapp.ErrProviderUnavailable, wantErr: ErrSCMGitHubProviderUnavailable, wantStatus: domain.SCMConnectionHealthStatusDegraded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			githubApps.probeErr = tc.probeErr
			failed, testErr := svc.TestConnection(context.Background(), connection.Connection.ID)
			if testErr != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, testErr)
			}
			if failed.Connection.HealthStatus != tc.wantStatus {
				t.Fatalf("expected status %q, got %q", tc.wantStatus, failed.Connection.HealthStatus)
			}
			if failed.Connection.HealthSummary == nil || *failed.Connection.HealthSummary != tc.wantErr.Error() {
				t.Fatalf("expected summary %q, got %+v", tc.wantErr.Error(), failed.Connection.HealthSummary)
			}
		})
	}
}

func TestSCMAdminService_TestConnectionDoesNotPersistCanceledContexts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		probeErr error
	}{
		{name: "canceled", probeErr: context.Canceled},
		{name: "deadline", probeErr: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connectionRepo := repositorymemory.NewSCMConnectionRepository()
			repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
			svc := NewSCMAdminService(connectionRepo, repositoryRepo)
			now := time.Date(2026, 7, 17, 21, 0, 0, 0, time.UTC)
			svc.now = func() time.Time { return now }
			privateKeyPEM, _ := testServiceRSAPrivateKeyPEM(t)
			svc.secrets = &fakeSCMSecretResolver{value: privateKeyPEM}
			githubApps := &fakeSCMGitHubAppClient{probeErr: tc.probeErr}
			svc.githubApps = githubApps
			registration, createRegistrationErr := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{AppID: "12345", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook"})
			if createRegistrationErr != nil {
				t.Fatalf("create registration: %v", createRegistrationErr)
			}
			enabled := true
			connection, createConnectionErr := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: registration.ID, DisplayName: "octo", Enabled: &enabled, InstallationID: "999", AccountLogin: "octo", AccountType: "organization", TargetID: "42"})
			if createConnectionErr != nil {
				t.Fatalf("create connection: %v", createConnectionErr)
			}
			_, testErr := svc.TestConnection(context.Background(), connection.Connection.ID)
			if testErr != tc.probeErr {
				t.Fatalf("expected %v, got %v", tc.probeErr, testErr)
			}
			stored, getErr := connectionRepo.GetByID(context.Background(), connection.Connection.ID)
			if getErr != nil {
				t.Fatalf("get connection: %v", getErr)
			}
			if stored.Connection.HealthStatus != domain.SCMConnectionHealthStatusUnknown {
				t.Fatalf("expected unknown health status, got %q", stored.Connection.HealthStatus)
			}
			if stored.Connection.HealthSummary != nil || stored.Connection.LastHealthCheckedAt != nil {
				t.Fatalf("expected no persisted health metadata, got summary=%+v checkedAt=%+v", stored.Connection.HealthSummary, stored.Connection.LastHealthCheckedAt)
			}
		})
	}
}

func TestSCMAdminService_TestConnectionRejectsMismatchesAndSanitizesFailures(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	privateKeyPEM, _ := testServiceRSAPrivateKeyPEM(t)
	svc.secrets = &fakeSCMSecretResolver{err: errors.New("missing BEGIN PRIVATE KEY ghs_secret")}
	svc.githubApps = &fakeSCMGitHubAppClient{}
	registration, err := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{AppID: "12345", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook"})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	enabled := true
	connection, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: registration.ID, DisplayName: "octo", Enabled: &enabled, InstallationID: "999", AccountLogin: "octo", AccountType: "organization", TargetID: "42"})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	failed, err := svc.TestConnection(context.Background(), connection.Connection.ID)
	if err != ErrSCMGitHubPrivateKeyResolveFailed {
		t.Fatalf("expected secret resolution failure, got %v", err)
	}
	if strings.Contains(err.Error(), "BEGIN PRIVATE KEY") || strings.Contains(err.Error(), "ghs_secret") {
		t.Fatalf("expected sanitized error, got %v", err)
	}
	if failed.Connection.HealthStatus != domain.SCMConnectionHealthStatusUnhealthy {
		t.Fatalf("expected unhealthy status, got %q", failed.Connection.HealthStatus)
	}

	svc.secrets = &fakeSCMSecretResolver{value: privateKeyPEM}
	badDetail := domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: "connection-bad", Provider: domain.SCMProviderGitHub, DisplayName: "bad", DeploymentKind: domain.SCMDeploymentKindSelfHosted, APIBaseURL: "https://other.example/api/v3", WebBaseURL: "https://other.example", Enabled: true, HealthStatus: domain.SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &registration,
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: "connection-bad", AppRegistrationID: registration.ID, InstallationID: "1000", AccountLogin: "octo-two", AccountType: "organization", AccountID: "84", CreatedAt: now, UpdatedAt: now},
	}
	svc.connections = &fakeSCMConnectionRepositoryForMismatch{detail: badDetail}
	_, err = svc.GetGitHubAppInstallationToken(context.Background(), "connection-bad")
	if err != ErrSCMGitHubConnectionConfigurationInvalid {
		t.Fatalf("expected config invalid error, got %v", err)
	}
}

func TestSCMAdminService_CreateRegisteredRepositoryByOwnerAndNamePersistsAuthoritativeMetadata(t *testing.T) {
	svc, connectionRepo, repositoryRepo, resolver := newSCMRepositoryRegistrationHarness(t)
	connection := createSCMInstallationConnection(t, svc, "connection-1", "999")
	resolver.byOwnerAndNameRepository = platformgithubapp.Repository{
		ID:            "2002",
		Owner:         "Acme-Org",
		Name:          "Platform",
		FullName:      "Acme-Org/Platform",
		CloneURL:      "https://github.com/Acme-Org/Platform.git",
		WebURL:        "https://github.com/Acme-Org/Platform",
		DefaultBranch: stringPtr("trunk"),
		Archived:      true,
		Disabled:      false,
	}

	created, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{
		ConnectionID: connection.Connection.ID,
		Owner:        "acme-org",
		Name:         "platform",
	})
	if err != nil {
		t.Fatalf("create registered repository: %v", err)
	}
	if created.ProviderRepositoryID != "2002" || created.FullName != "Acme-Org/Platform" || created.Archived != true {
		t.Fatalf("expected provider metadata, got %+v", created)
	}
	stored, getErr := repositoryRepo.GetByID(context.Background(), created.ID)
	if getErr != nil {
		t.Fatalf("get stored registration: %v", getErr)
	}
	if stored.Owner != "Acme-Org" || stored.Name != "Platform" {
		t.Fatalf("expected canonical coordinates from provider response, got %+v", stored)
	}
	assertResolverUsedConnectionPath(t, resolver.byOwnerAndNameRequests[0].Request, connectionRepo, connection.Connection.ID)
}

func TestSCMAdminService_CreateRegisteredRepositoryRejectsProviderIDMismatch(t *testing.T) {
	svc, _, _, resolver := newSCMRepositoryRegistrationHarness(t)
	connection := createSCMInstallationConnection(t, svc, "connection-1", "999")
	resolver.byIDRepository = platformgithubapp.Repository{ID: "9999", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets"}

	_, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: connection.Connection.ID, ProviderRepositoryID: "1001"})
	if err != ErrSCMGitHubProviderMalformedResponse {
		t.Fatalf("expected provider mismatch to be malformed response, got %v", err)
	}
}

func TestSCMAdminService_CreateRegisteredRepositoryConnectionValidationAndConflictBranches(t *testing.T) {
	svc, connectionRepo, repositoryRepo, resolver := newSCMRepositoryRegistrationHarness(t)
	first := createSCMInstallationConnection(t, svc, "connection-1", "999")
	second := createSCMInstallationConnection(t, svc, "connection-2", "1000")
	resolver.byIDRepository = platformgithubapp.Repository{ID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", DefaultBranch: stringPtr("main")}

	created, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: first.Connection.ID, ProviderRepositoryID: "1001"})
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}
	if _, duplicateErr := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: first.Connection.ID, ProviderRepositoryID: "1001"}); duplicateErr != repository.ErrSCMRepositoryRegistrationDuplicate {
		t.Fatalf("expected duplicate conflict, got %v", duplicateErr)
	}
	other, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: second.Connection.ID, ProviderRepositoryID: "1001"})
	if err != nil {
		t.Fatalf("second connection registration failed: %v", err)
	}
	if created.ProviderRepositoryID != other.ProviderRepositoryID || created.ConnectionID == other.ConnectionID {
		t.Fatalf("expected same provider id under different connections, got first=%+v second=%+v", created, other)
	}
	list, listErr := repositoryRepo.List(context.Background())
	if listErr != nil || len(list) != 2 {
		t.Fatalf("expected two stored registrations, got len=%d err=%v", len(list), listErr)
	}

	disable := false
	_, setErr := svc.SetConnectionEnabled(context.Background(), first.Connection.ID, &disable)
	if setErr != nil {
		t.Fatalf("disable connection: %v", setErr)
	}
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: first.Connection.ID, ProviderRepositoryID: "1002"}); err != ErrSCMConnectionDisabled {
		t.Fatalf("expected disabled connection error, got %v", err)
	}

	privateKeyPEM, _ := testServiceRSAPrivateKeyPEM(t)
	registration := domain.GitHubAppRegistration{ID: "registration-3", AppID: "12345", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook", CreatedAt: svc.now(), UpdatedAt: svc.now()}
	badDetail := domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: "connection-bad", Provider: domain.SCMProviderGitHub, DisplayName: "bad", DeploymentKind: domain.SCMDeploymentKindSelfHosted, APIBaseURL: "https://other.example/api/v3", WebBaseURL: "https://other.example", Enabled: true, HealthStatus: domain.SCMConnectionHealthStatusUnknown, CreatedAt: svc.now(), UpdatedAt: svc.now()},
		GitHubAppRegistration: &registration,
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: "connection-bad", AppRegistrationID: registration.ID, InstallationID: "1001", AccountLogin: "octo-bad", AccountType: "organization", AccountID: "84", CreatedAt: svc.now(), UpdatedAt: svc.now()},
	}
	svc.connections = &fakeSCMConnectionRepositoryForMismatch{detail: badDetail}
	svc.secrets = &fakeSCMSecretResolver{value: privateKeyPEM}
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: "connection-bad", ProviderRepositoryID: "2002"}); err != ErrSCMGitHubConnectionConfigurationInvalid {
		t.Fatalf("expected incompatible connection error, got %v", err)
	}

	_ = connectionRepo
}

func TestSCMAdminService_CreateRegisteredRepositoryFailurePersistsNothing(t *testing.T) {
	svc, _, repositoryRepo, resolver := newSCMRepositoryRegistrationHarness(t)
	connection := createSCMInstallationConnection(t, svc, "connection-1", "999")
	resolver.byIDErr = platformgithubapp.ErrRepositoryInaccessible

	_, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: connection.Connection.ID, ProviderRepositoryID: "1001"})
	if err != ErrSCMGitHubRepositoryNotAccessible {
		t.Fatalf("expected repository inaccessible error, got %v", err)
	}
	list, listErr := repositoryRepo.List(context.Background())
	if listErr != nil {
		t.Fatalf("list registrations: %v", listErr)
	}
	if len(list) != 0 {
		t.Fatalf("expected no repository persistence on failure, got %+v", list)
	}
}

func TestSCMAdminService_RefreshRegisteredRepositoryUsesStoredConnectionAndProviderIDOnly(t *testing.T) {
	svc, connectionRepo, repositoryRepo, resolver := newSCMRepositoryRegistrationHarness(t)
	first := createSCMInstallationConnection(t, svc, "connection-1", "999")
	_ = createSCMInstallationConnection(t, svc, "connection-2", "1000")
	resolver.byIDRepository = platformgithubapp.Repository{ID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", DefaultBranch: stringPtr("main")}
	created, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: first.Connection.ID, ProviderRepositoryID: "1001"})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	resolver.byIDRequests = nil
	resolver.byOwnerAndNameRequests = nil
	resolver.byIDRepository = platformgithubapp.Repository{ID: "1001", Owner: "acme", Name: "platform", FullName: "acme/platform", CloneURL: "https://github.com/acme/platform.git", WebURL: "https://github.com/acme/platform", DefaultBranch: stringPtr("trunk"), Archived: true, Disabled: true}

	refreshed, err := svc.RefreshRegisteredRepository(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("refresh registration: %v", err)
	}
	if len(resolver.byOwnerAndNameRequests) != 0 {
		t.Fatalf("expected refresh to avoid owner/name lookup, got %+v", resolver.byOwnerAndNameRequests)
	}
	if len(resolver.byIDRequests) != 1 || resolver.byIDRequests[0].RepositoryID != "1001" {
		t.Fatalf("expected refresh by stored provider id, got %+v", resolver.byIDRequests)
	}
	assertResolverUsedConnectionPath(t, resolver.byIDRequests[0].Request, connectionRepo, first.Connection.ID)
	if refreshed.ID != created.ID || refreshed.ConnectionID != created.ConnectionID || refreshed.ProviderRepositoryID != created.ProviderRepositoryID || !refreshed.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("expected immutable identity fields, got before=%+v after=%+v", created, refreshed)
	}
	if refreshed.FullName != "acme/platform" || refreshed.DefaultBranch == nil || *refreshed.DefaultBranch != "trunk" || !refreshed.Archived || !refreshed.Disabled {
		t.Fatalf("expected refreshed mutable metadata, got %+v", refreshed)
	}
	stored, getErr := repositoryRepo.GetByID(context.Background(), created.ID)
	if getErr != nil {
		t.Fatalf("get refreshed registration: %v", getErr)
	}
	if stored.FullName != "acme/platform" || stored.ProviderRepositoryID != "1001" {
		t.Fatalf("expected stored refreshed metadata with stable identity, got %+v", stored)
	}
}

func TestSCMAdminService_RefreshRegisteredRepositoryFailureLeavesStoredStateUntouched(t *testing.T) {
	svc, _, repositoryRepo, resolver := newSCMRepositoryRegistrationHarness(t)
	connection := createSCMInstallationConnection(t, svc, "connection-1", "999")
	resolver.byIDRepository = platformgithubapp.Repository{ID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", DefaultBranch: stringPtr("main")}
	created, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: connection.Connection.ID, ProviderRepositoryID: "1001"})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	before, getErr := repositoryRepo.GetByID(context.Background(), created.ID)
	if getErr != nil {
		t.Fatalf("get created registration: %v", getErr)
	}
	resolver.byIDErr = platformgithubapp.ErrRepositoryInaccessible

	_, err = svc.RefreshRegisteredRepository(context.Background(), created.ID)
	if err != ErrSCMGitHubRepositoryNotAccessible {
		t.Fatalf("expected repository inaccessible on refresh, got %v", err)
	}
	after, getErr := repositoryRepo.GetByID(context.Background(), created.ID)
	if getErr != nil {
		t.Fatalf("get registration after failed refresh: %v", getErr)
	}
	if after != before {
		t.Fatalf("expected failed refresh to leave stored metadata unchanged, before=%+v after=%+v", before, after)
	}
}

func TestSCMAdminService_ListAndGetRegisteredRepositories(t *testing.T) {
	svc, _, _, resolver := newSCMRepositoryRegistrationHarness(t)
	connection := createSCMInstallationConnection(t, svc, "connection-1", "999")
	resolver.byIDRepository = platformgithubapp.Repository{ID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", DefaultBranch: stringPtr("main")}

	created, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: connection.Connection.ID, ProviderRepositoryID: "1001"})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	listed, err := svc.ListRegisteredRepositories(context.Background())
	if err != nil {
		t.Fatalf("list registered repositories: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("expected listed repository %q, got %+v", created.ID, listed)
	}
	fetched, err := svc.GetRegisteredRepository(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get registered repository: %v", err)
	}
	if fetched.ID != created.ID || fetched.ProviderRepositoryID != "1001" {
		t.Fatalf("unexpected fetched repository: %+v", fetched)
	}
	_, err = svc.GetRegisteredRepository(context.Background(), "missing")
	if err != repository.ErrSCMRepositoryRegistrationNotFound {
		t.Fatalf("expected missing registration error, got %v", err)
	}
}

func TestSCMAdminService_RefreshRegisteredRepositoryRejectsProviderIDMismatch(t *testing.T) {
	svc, _, repositoryRepo, resolver := newSCMRepositoryRegistrationHarness(t)
	connection := createSCMInstallationConnection(t, svc, "connection-1", "999")
	resolver.byIDRepository = platformgithubapp.Repository{ID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", DefaultBranch: stringPtr("main")}
	created, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: connection.Connection.ID, ProviderRepositoryID: "1001"})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	before, err := repositoryRepo.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get created registration: %v", err)
	}
	resolver.byIDRepository = platformgithubapp.Repository{ID: "9999", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets"}

	_, err = svc.RefreshRegisteredRepository(context.Background(), created.ID)
	if err != ErrSCMGitHubProviderMalformedResponse {
		t.Fatalf("expected provider mismatch error, got %v", err)
	}
	after, err := repositoryRepo.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get repository after failed refresh: %v", err)
	}
	if after != before {
		t.Fatalf("expected mismatched refresh to leave stored metadata unchanged, before=%+v after=%+v", before, after)
	}
}

func TestSCMAdminService_HelperMappingsAndResolutionBranches(t *testing.T) {
	if status, summary := scmConnectionHealthFailure(ErrSCMGitHubProviderMalformedResponse); status != domain.SCMConnectionHealthStatusDegraded || summary != ErrSCMGitHubProviderMalformedResponse.Error() {
		t.Fatalf("expected malformed response to degrade health, got status=%q summary=%q", status, summary)
	}
	if status, summary := scmConnectionHealthFailure(errors.New("unexpected")); status != domain.SCMConnectionHealthStatusUnhealthy || summary != ErrSCMGitHubProviderUnavailable.Error() {
		t.Fatalf("expected default provider unavailable summary, got status=%q summary=%q", status, summary)
	}
	if mapped := mapSCMGitHubProviderError(platformgithubapp.ErrMalformedResponse); mapped != ErrSCMGitHubProviderMalformedResponse {
		t.Fatalf("expected malformed provider mapping, got %v", mapped)
	}
	if mapped := mapSCMGitHubProviderError(platformgithubapp.ErrPrivateKeyNotRSA); mapped != ErrSCMGitHubPrivateKeyResolveFailed {
		t.Fatalf("expected private key mapping, got %v", mapped)
	}
	if mapped := mapSCMGitHubProviderError(platformgithubapp.ErrRepositoryInaccessible); mapped != ErrSCMGitHubRepositoryNotAccessible {
		t.Fatalf("expected repository inaccessible mapping, got %v", mapped)
	}
	if mapped := mapSCMGitHubProviderError(context.DeadlineExceeded); mapped != context.DeadlineExceeded {
		t.Fatalf("expected deadline to pass through, got %v", mapped)
	}
	if got := stringPtr("   "); got != nil {
		t.Fatalf("expected blank string pointer to be nil, got %v", *got)
	}
	if got := stringPtr("  ok  "); got == nil || *got != "ok" {
		t.Fatalf("expected trimmed pointer, got %+v", got)
	}
	if !isContextCancellation(context.Canceled) || !isContextCancellation(context.DeadlineExceeded) || isContextCancellation(errors.New("boom")) {
		t.Fatal("unexpected context cancellation classification")
	}

	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	validRegistration := domain.GitHubAppRegistration{ID: "registration-1", AppID: "12345", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook", CreatedAt: now, UpdatedAt: now}
	validInstallation := &domain.GitHubAppInstallation{ConnectionID: "connection-1", AppRegistrationID: "registration-1", InstallationID: "999", AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now}
	for _, tc := range []struct {
		name     string
		detail   domain.SCMConnectionDetail
		resolver scmSecretResolver
		wantErr  error
	}{
		{name: "non github provider", detail: domain.SCMConnectionDetail{Connection: domain.SCMConnection{ID: "connection-1", Provider: "", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", Enabled: true}, GitHubAppRegistration: &validRegistration, GitHubAppInstallation: validInstallation}, resolver: &fakeSCMSecretResolver{value: "pem"}, wantErr: ErrSCMGitHubConnectionConfigurationInvalid},
		{name: "blank app id", detail: domain.SCMConnectionDetail{Connection: domain.SCMConnection{ID: "connection-1", Provider: domain.SCMProviderGitHub, APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", Enabled: true}, GitHubAppRegistration: &domain.GitHubAppRegistration{ID: "registration-1", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", PrivateKeySecretRef: "secret/github/private-key"}, GitHubAppInstallation: validInstallation}, resolver: &fakeSCMSecretResolver{value: "pem"}, wantErr: ErrSCMGitHubConnectionConfigurationInvalid},
		{name: "blank resolved secret", detail: domain.SCMConnectionDetail{Connection: domain.SCMConnection{ID: "connection-1", Provider: domain.SCMProviderGitHub, APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", Enabled: true}, GitHubAppRegistration: &validRegistration, GitHubAppInstallation: validInstallation}, resolver: &fakeSCMSecretResolver{value: "   "}, wantErr: ErrSCMGitHubPrivateKeyResolveFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewSCMAdminService(repositorymemory.NewSCMConnectionRepository(), repositorymemory.NewSCMRepositoryRegistrationRepository())
			svc.connections = &fakeSCMConnectionRepositoryForMismatch{detail: tc.detail}
			svc.secrets = tc.resolver
			_, _, err := svc.resolveGitHubInstallationTokenRequest(context.Background(), "connection-1")
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func newSCMRepositoryRegistrationHarness(t *testing.T) (*SCMAdminService, repository.SCMConnectionRepository, repository.SCMRepositoryRegistrationRepository, *fakeSCMGitHubRepositoryResolver) {
	t.Helper()
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	privateKeyPEM, _ := testServiceRSAPrivateKeyPEM(t)
	svc.secrets = &fakeSCMSecretResolver{value: privateKeyPEM}
	resolver := &fakeSCMGitHubRepositoryResolver{}
	svc.githubRepos = resolver
	return svc, connectionRepo, repositoryRepo, resolver
}

func createSCMInstallationConnection(t *testing.T, svc *SCMAdminService, connectionID string, installationID string) domain.SCMConnectionDetail {
	t.Helper()
	registration, err := svc.CreateGitHubAppRegistration(context.Background(), CreateGitHubAppRegistrationInput{AppID: "app-" + installationID, PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook"})
	if err != nil {
		t.Fatalf("create app registration: %v", err)
	}
	enabled := true
	connection, err := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{AppRegistrationID: registration.ID, DisplayName: connectionID, Enabled: &enabled, InstallationID: installationID, AccountLogin: "octo-" + installationID, AccountType: "organization", TargetID: installationID})
	if err != nil {
		t.Fatalf("create installation connection: %v", err)
	}
	return connection
}

func assertResolverUsedConnectionPath(t *testing.T, request platformgithubapp.InstallationTokenRequest, connectionRepo repository.SCMConnectionRepository, connectionID string) {
	t.Helper()
	detail, err := connectionRepo.GetByID(context.Background(), connectionID)
	if err != nil {
		t.Fatalf("get connection detail: %v", err)
	}
	if detail.GitHubAppRegistration == nil || detail.GitHubAppInstallation == nil {
		t.Fatalf("expected github installation detail, got %+v", detail)
	}
	if request.AppRegistrationID != detail.GitHubAppRegistration.ID || request.InstallationID != detail.GitHubAppInstallation.InstallationID || request.APIBaseURL != detail.Connection.APIBaseURL {
		t.Fatalf("expected resolver request to use stored connection path, got %+v detail=%+v", request, detail)
	}
}

type fakeSCMSecretResolver struct {
	value string
	err   error
}

func (f *fakeSCMSecretResolver) Resolve(_ context.Context, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.value, nil
}

func testServiceRSAPrivateKeyPEM(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})), privateKey
}

type fakeSCMConnectionRepositoryForMismatch struct {
	detail domain.SCMConnectionDetail
}

func (f *fakeSCMConnectionRepositoryForMismatch) CreateGitHubAppRegistration(context.Context, domain.GitHubAppRegistration) (domain.GitHubAppRegistration, error) {
	return domain.GitHubAppRegistration{}, nil
}

func (f *fakeSCMConnectionRepositoryForMismatch) ListGitHubAppRegistrations(context.Context) ([]domain.GitHubAppRegistration, error) {
	return nil, nil
}

func (f *fakeSCMConnectionRepositoryForMismatch) GetGitHubAppRegistrationByID(context.Context, string) (domain.GitHubAppRegistration, error) {
	return domain.GitHubAppRegistration{}, nil
}

func (f *fakeSCMConnectionRepositoryForMismatch) CreateGitHubAppInstallationConnection(context.Context, domain.SCMConnectionDetail) (domain.SCMConnectionDetail, error) {
	return domain.SCMConnectionDetail{}, nil
}

func (f *fakeSCMConnectionRepositoryForMismatch) GetGitHubAppInstallationConnection(context.Context, string, string) (domain.SCMConnectionDetail, error) {
	return domain.SCMConnectionDetail{}, repository.ErrSCMConnectionNotFound
}

func (f *fakeSCMConnectionRepositoryForMismatch) List(context.Context) ([]domain.SCMConnectionDetail, error) {
	return nil, nil
}

func (f *fakeSCMConnectionRepositoryForMismatch) GetByID(context.Context, string) (domain.SCMConnectionDetail, error) {
	return f.detail, nil
}

func (f *fakeSCMConnectionRepositoryForMismatch) SetEnabled(context.Context, string, bool, time.Time) (domain.SCMConnectionDetail, error) {
	return f.detail, nil
}

func (f *fakeSCMConnectionRepositoryForMismatch) UpdateHealth(context.Context, string, domain.SCMConnectionHealthStatus, *string, time.Time, time.Time) (domain.SCMConnectionDetail, error) {
	return f.detail, nil
}

type fakeSCMGitHubAppClient struct {
	token         platformgithubapp.InstallationToken
	probe         platformgithubapp.InstallationProbeResult
	tokenErr      error
	probeErr      error
	tokenRequests []platformgithubapp.InstallationTokenRequest
	probeRequests []platformgithubapp.InstallationTokenRequest
}

func (f *fakeSCMGitHubAppClient) GetInstallationToken(_ context.Context, input platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationToken, error) {
	f.tokenRequests = append(f.tokenRequests, input)
	if f.tokenErr != nil {
		return platformgithubapp.InstallationToken{}, f.tokenErr
	}
	return f.token, nil
}

func (f *fakeSCMGitHubAppClient) ProbeInstallation(_ context.Context, input platformgithubapp.InstallationTokenRequest) (platformgithubapp.InstallationProbeResult, error) {
	f.probeRequests = append(f.probeRequests, input)
	if f.probeErr != nil {
		return platformgithubapp.InstallationProbeResult{}, f.probeErr
	}
	return f.probe, nil
}

type fakeSCMGitHubRepositoryResolver struct {
	byIDRepository           platformgithubapp.Repository
	byIDErr                  error
	byOwnerAndNameRepository platformgithubapp.Repository
	byOwnerAndNameErr        error
	byIDRequests             []fakeRepositoryByIDRequest
	byOwnerAndNameRequests   []fakeRepositoryByOwnerAndNameRequest
}

type fakeRepositoryByIDRequest struct {
	Request      platformgithubapp.InstallationTokenRequest
	RepositoryID string
}

type fakeRepositoryByOwnerAndNameRequest struct {
	Request platformgithubapp.InstallationTokenRequest
	Owner   string
	Name    string
}

func (f *fakeSCMGitHubRepositoryResolver) GetRepositoryByID(_ context.Context, input platformgithubapp.InstallationTokenRequest, repositoryID string) (platformgithubapp.Repository, error) {
	f.byIDRequests = append(f.byIDRequests, fakeRepositoryByIDRequest{Request: input, RepositoryID: repositoryID})
	if f.byIDErr != nil {
		return platformgithubapp.Repository{}, f.byIDErr
	}
	return f.byIDRepository, nil
}

func (f *fakeSCMGitHubRepositoryResolver) GetRepositoryByOwnerAndName(_ context.Context, input platformgithubapp.InstallationTokenRequest, owner string, name string) (platformgithubapp.Repository, error) {
	f.byOwnerAndNameRequests = append(f.byOwnerAndNameRequests, fakeRepositoryByOwnerAndNameRequest{Request: input, Owner: owner, Name: name})
	if f.byOwnerAndNameErr != nil {
		return platformgithubapp.Repository{}, f.byOwnerAndNameErr
	}
	return f.byOwnerAndNameRepository, nil
}
