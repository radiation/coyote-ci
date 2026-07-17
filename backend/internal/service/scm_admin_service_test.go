package service

import (
	"context"
	"testing"
	"time"

	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestSCMAdminService_CreateConnectionAndRepository(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	svc := NewSCMAdminService(connectionRepo, repositoryRepo)
	now := time.Now().UTC()
	svc.now = func() time.Time { return now }

	connection, createErr := svc.CreateGitHubAppInstallationConnection(context.Background(), CreateGitHubAppInstallationConnectionInput{
		DisplayName:         "octo cloud",
		DeploymentKind:      "cloud",
		AppID:               "12345",
		PrivateKeySecretRef: "secret/github/private-key",
		WebhookSecretRef:    "secret/github/webhook",
		InstallationID:      "999",
		AccountLogin:        "octo",
		AccountType:         "organization",
		AccountID:           "42",
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
