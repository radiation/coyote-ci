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
	registration := testGitHubAppRegistration(now, "registration-1")
	if _, err := repo.CreateGitHubAppRegistration(context.Background(), registration); err != nil {
		t.Fatalf("create registration failed: %v", err)
	}
	detail := testGitHubConnectionDetail(now, "connection-1", registration, "installation-1", "octo")

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
	healthy, healthErr := repo.UpdateHealth(context.Background(), "connection-1", domain.SCMConnectionHealthStatusHealthy, stringPointer("ok"), now.Add(2*time.Minute), now.Add(2*time.Minute))
	if healthErr != nil {
		t.Fatalf("update health failed: %v", healthErr)
	}
	if healthy.Connection.HealthStatus != domain.SCMConnectionHealthStatusHealthy || healthy.Connection.HealthSummary == nil || *healthy.Connection.HealthSummary != "ok" {
		t.Fatalf("expected healthy metadata, got %+v", healthy.Connection)
	}
}

func TestSCMConnectionRepository_ListAndGetGitHubAppRegistrations(t *testing.T) {
	repo := NewSCMConnectionRepository()
	emptyList, emptyListErr := repo.ListGitHubAppRegistrations(context.Background())
	if emptyListErr != nil {
		t.Fatalf("list empty registrations failed: %v", emptyListErr)
	}
	if len(emptyList) != 0 {
		t.Fatalf("expected zero registrations, got %d", len(emptyList))
	}

	now := time.Now().UTC()
	older := testGitHubAppRegistration(now.Add(-time.Minute), "registration-1")
	newer := testGitHubAppRegistration(now, "registration-2")
	newer.AppID = "54321"
	newer.APIBaseURL = "https://ghe.example/api/v3"
	newer.WebBaseURL = "https://ghe.example"
	if _, err := repo.CreateGitHubAppRegistration(context.Background(), older); err != nil {
		t.Fatalf("create older registration failed: %v", err)
	}
	if _, err := repo.CreateGitHubAppRegistration(context.Background(), newer); err != nil {
		t.Fatalf("create newer registration failed: %v", err)
	}

	list, listErr := repo.ListGitHubAppRegistrations(context.Background())
	if listErr != nil {
		t.Fatalf("list registrations failed: %v", listErr)
	}
	if len(list) != 2 {
		t.Fatalf("expected two registrations, got %d", len(list))
	}
	if list[0].ID != "registration-2" || list[1].ID != "registration-1" {
		t.Fatalf("expected registrations sorted newest first, got %+v", list)
	}
	fetched, getErr := repo.GetGitHubAppRegistrationByID(context.Background(), "registration-1")
	if getErr != nil {
		t.Fatalf("get registration failed: %v", getErr)
	}
	if fetched.ID != "registration-1" {
		t.Fatalf("expected registration-1, got %q", fetched.ID)
	}
	_, missingErr := repo.GetGitHubAppRegistrationByID(context.Background(), "missing")
	if !errors.Is(missingErr, repository.ErrSCMGitHubAppRegistrationNotFound) {
		t.Fatalf("expected not found error, got %v", missingErr)
	}
}

func TestSCMConnectionRepository_ReusesMatchingGitHubAppRegistrationAndScopesInstallationUniqueness(t *testing.T) {
	repo := NewSCMConnectionRepository()
	now := time.Now().UTC()
	registration := testGitHubAppRegistration(now, "registration-1")
	if _, err := repo.CreateGitHubAppRegistration(context.Background(), registration); err != nil {
		t.Fatalf("create registration failed: %v", err)
	}
	first := testGitHubConnectionDetail(now, "connection-1", registration, "installation-1", "octo")
	second := testGitHubConnectionDetail(now.Add(time.Minute), "connection-2", registration, "installation-2", "octo-two")

	if _, err := repo.CreateGitHubAppInstallationConnection(context.Background(), first); err != nil {
		t.Fatalf("create first failed: %v", err)
	}
	createdSecond, secondErr := repo.CreateGitHubAppInstallationConnection(context.Background(), second)
	if secondErr != nil {
		t.Fatalf("create second failed: %v", secondErr)
	}
	if createdSecond.GitHubAppRegistration == nil || createdSecond.GitHubAppRegistration.ID != registration.ID {
		t.Fatalf("expected existing registration to be reused, got %+v", createdSecond.GitHubAppRegistration)
	}
	updatedFirst, updateErr := repo.SetEnabled(context.Background(), first.Connection.ID, false, now.Add(2*time.Minute))
	if updateErr != nil {
		t.Fatalf("disable first failed: %v", updateErr)
	}
	if updatedFirst.Connection.Enabled {
		t.Fatal("expected first connection to be disabled")
	}
	storedRegistration, registrationErr := repo.GetGitHubAppRegistrationByID(context.Background(), registration.ID)
	if registrationErr != nil {
		t.Fatalf("get registration failed: %v", registrationErr)
	}
	if storedRegistration.UpdatedAt != registration.UpdatedAt {
		t.Fatalf("expected registration to remain unchanged, got %s want %s", storedRegistration.UpdatedAt, registration.UpdatedAt)
	}
	fetchedSecond, getErr := repo.GetByID(context.Background(), second.Connection.ID)
	if getErr != nil {
		t.Fatalf("get second failed: %v", getErr)
	}
	if !fetchedSecond.Connection.Enabled {
		t.Fatal("expected sibling connection to remain enabled")
	}

	duplicateInstallation := testGitHubConnectionDetail(now.Add(3*time.Minute), "connection-3", registration, "installation-1", "octo-three")
	if _, err := repo.CreateGitHubAppInstallationConnection(context.Background(), duplicateInstallation); !errors.Is(err, repository.ErrSCMGitHubAppInstallationConflict) {
		t.Fatalf("expected installation conflict, got %v", err)
	}
	missingRegistration := testGitHubConnectionDetail(now.Add(4*time.Minute), "connection-4", testGitHubAppRegistration(now, "missing-registration"), "installation-4", "octo-four")
	if _, err := repo.CreateGitHubAppInstallationConnection(context.Background(), missingRegistration); !errors.Is(err, repository.ErrSCMGitHubAppRegistrationNotFound) {
		t.Fatalf("expected missing registration error, got %v", err)
	}
}

func testGitHubAppRegistration(now time.Time, registrationID string) domain.GitHubAppRegistration {
	return domain.GitHubAppRegistration{ID: registrationID, AppID: "12345", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", PrivateKeySecretRef: "secret/github/private-key", WebhookSecretRef: "secret/github/webhook", CreatedAt: now, UpdatedAt: now}
}

func testGitHubConnectionDetail(now time.Time, connectionID string, registration domain.GitHubAppRegistration, installationID string, accountLogin string) domain.SCMConnectionDetail {
	return domain.SCMConnectionDetail{
		Connection:            domain.SCMConnection{ID: connectionID, Provider: domain.SCMProviderGitHub, DisplayName: accountLogin + " connection", DeploymentKind: domain.SCMDeploymentKindCloud, APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", Enabled: true, HealthStatus: domain.SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &registration,
		GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: connectionID, AppRegistrationID: registration.ID, InstallationID: installationID, AccountLogin: accountLogin, AccountType: "organization", AccountID: installationID + "-account", CreatedAt: now, UpdatedAt: now},
	}
}

func stringPointer(value string) *string {
	return &value
}
