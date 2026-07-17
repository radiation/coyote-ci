package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func TestSCMConnectionRepository_CreateListGetAndSetEnabled(t *testing.T) {
	repo := NewSCMConnectionRepository()
	now := time.Now().UTC()
	detail := testGitHubConnectionDetail(now, "connection-1", "registration-1", "installation-1", "octo")

	created, createErr := repo.CreateGitHubAppInstallationConnection(context.Background(), detail)
	if createErr != nil {
		t.Fatalf("create connection failed: %v", createErr)
	}
	if created.GitHubAppRegistration == nil || created.GitHubAppInstallation == nil {
		t.Fatalf("expected github detail to be persisted, got %+v", created)
	}

	fetched, getErr := repo.GetByID(context.Background(), "connection-1")
	if getErr != nil {
		t.Fatalf("get connection failed: %v", getErr)
	}
	if fetched.Connection.DisplayName != "octo connection" {
		t.Fatalf("expected display name to round-trip, got %q", fetched.Connection.DisplayName)
	}

	list, listErr := repo.List(context.Background())
	if listErr != nil {
		t.Fatalf("list connections failed: %v", listErr)
	}
	if len(list) != 1 {
		t.Fatalf("expected one connection, got %d", len(list))
	}

	updated, updateErr := repo.SetEnabled(context.Background(), "connection-1", false, now.Add(time.Minute))
	if updateErr != nil {
		t.Fatalf("set enabled failed: %v", updateErr)
	}
	if updated.Connection.Enabled {
		t.Fatal("expected connection to be disabled")
	}
}

func TestSCMConnectionRepository_ReusesMatchingGitHubAppRegistrationAndScopesInstallationUniqueness(t *testing.T) {
	repo := NewSCMConnectionRepository()
	now := time.Now().UTC()
	first := testGitHubConnectionDetail(now, "connection-1", "registration-1", "installation-1", "octo")
	second := testGitHubConnectionDetail(now.Add(time.Minute), "connection-2", "registration-2", "installation-2", "octo-two")
	second.GitHubAppRegistration.ID = "registration-2"
	second.GitHubAppRegistration.AppID = first.GitHubAppRegistration.AppID
	second.GitHubAppRegistration.APIBaseURL = first.GitHubAppRegistration.APIBaseURL
	second.GitHubAppRegistration.WebBaseURL = first.GitHubAppRegistration.WebBaseURL
	second.GitHubAppRegistration.PrivateKeySecretRef = first.GitHubAppRegistration.PrivateKeySecretRef
	second.GitHubAppRegistration.WebhookSecretRef = first.GitHubAppRegistration.WebhookSecretRef
	second.GitHubAppInstallation.AppRegistrationID = "registration-2"

	if _, err := repo.CreateGitHubAppInstallationConnection(context.Background(), first); err != nil {
		t.Fatalf("create first failed: %v", err)
	}
	createdSecond, secondErr := repo.CreateGitHubAppInstallationConnection(context.Background(), second)
	if secondErr != nil {
		t.Fatalf("create second failed: %v", secondErr)
	}
	if createdSecond.GitHubAppRegistration == nil || createdSecond.GitHubAppRegistration.ID != "registration-1" {
		t.Fatalf("expected existing registration to be reused, got %+v", createdSecond.GitHubAppRegistration)
	}

	duplicateInstallation := testGitHubConnectionDetail(now.Add(2*time.Minute), "connection-3", "registration-3", "installation-1", "octo-three")
	duplicateInstallation.GitHubAppRegistration.AppID = first.GitHubAppRegistration.AppID
	duplicateInstallation.GitHubAppRegistration.APIBaseURL = first.GitHubAppRegistration.APIBaseURL
	duplicateInstallation.GitHubAppRegistration.WebBaseURL = first.GitHubAppRegistration.WebBaseURL
	duplicateInstallation.GitHubAppRegistration.PrivateKeySecretRef = first.GitHubAppRegistration.PrivateKeySecretRef
	duplicateInstallation.GitHubAppRegistration.WebhookSecretRef = first.GitHubAppRegistration.WebhookSecretRef
	if _, err := repo.CreateGitHubAppInstallationConnection(context.Background(), duplicateInstallation); !errors.Is(err, repository.ErrSCMGitHubAppInstallationConflict) {
		t.Fatalf("expected installation conflict, got %v", err)
	}
}

func testGitHubConnectionDetail(now time.Time, connectionID string, registrationID string, installationID string, accountLogin string) domain.SCMConnectionDetail {
	return domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: connectionID, Provider: domain.SCMProviderGitHub, DisplayName: accountLogin + " connection", DeploymentKind: domain.SCMDeploymentKindCloud, APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", Enabled: true, HealthStatus: domain.SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &domain.GitHubAppRegistration{ID: registrationID, AppID: "12345", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook", CreatedAt: now, UpdatedAt: now},
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: connectionID, AppRegistrationID: registrationID, InstallationID: installationID, AccountLogin: accountLogin, AccountType: "organization", AccountID: installationID + "-account", CreatedAt: now, UpdatedAt: now},
	}
}
