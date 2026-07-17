package service

import (
	"context"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
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
