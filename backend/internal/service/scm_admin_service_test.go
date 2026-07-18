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

	branch := "main"
	repository, repoErr := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{
		ConnectionID:         connection.Connection.ID,
		ProviderRepositoryID: "1001",
		Owner:                "octo",
		Name:                 "widgets",
		FullName:             "octo/widgets",
		CloneURL:             "https://github.com/octo/widgets.git",
		WebURL:               "https://github.com/octo/widgets",
		DefaultBranch:        &branch,
	})
	if repoErr != nil {
		t.Fatalf("create repository failed: %v", repoErr)
	}
	if repository.ConnectionID != connection.Connection.ID {
		t.Fatalf("expected repository to attach to connection, got %+v", repository)
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
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: "connection-1"}); err != ErrSCMRegisteredRepositoryProviderRepositoryIDRequired {
		t.Fatalf("expected missing provider repository id error, got %v", err)
	}
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: "connection-1", ProviderRepositoryID: "1001"}); err != ErrSCMRegisteredRepositoryOwnerRequired {
		t.Fatalf("expected missing owner error, got %v", err)
	}
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo"}); err != ErrSCMRegisteredRepositoryNameRequired {
		t.Fatalf("expected missing name error, got %v", err)
	}
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "repo"}); err != ErrSCMRegisteredRepositoryFullNameRequired {
		t.Fatalf("expected missing full name error, got %v", err)
	}
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "repo", FullName: "octo/repo"}); err != ErrSCMRegisteredRepositoryCloneURLRequired {
		t.Fatalf("expected missing clone url error, got %v", err)
	}
	if _, err := svc.CreateRegisteredRepository(context.Background(), CreateSCMRepositoryRegistrationInput{ConnectionID: "connection-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "repo", FullName: "octo/repo", CloneURL: "https://github.com/octo/repo.git"}); err != ErrSCMRegisteredRepositoryWebURLRequired {
		t.Fatalf("expected missing web url error, got %v", err)
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
