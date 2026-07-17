package domain

import (
	"testing"
	"time"
)

func TestSCMConnectionDetailNormalizeAndValidate(t *testing.T) {
	now := time.Now().UTC()
	summary := "  healthy enough  "
	displayName := " GitHub App "
	detail := SCMConnectionDetail{
		Connection: SCMConnection{
			ID:             " connection-1 ",
			Provider:       SCMProvider(" GITHUB "),
			DisplayName:    " Example Connection ",
			DeploymentKind: SCMDeploymentKind(" cloud "),
			Enabled:        true,
			HealthStatus:   SCMConnectionHealthStatus(" healthy "),
			HealthSummary:  &summary,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		GitHubAppRegistration: &GitHubAppRegistration{
			ID:                  " registration-1 ",
			AppID:               " 12345 ",
			DisplayName:         &displayName,
			PrivateKeySecretRef: " secrets/github/private-key ",
			WebhookSecretRef:    " secrets/github/webhook ",
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		GitHubAppInstallation: &GitHubAppInstallation{
			ConnectionID:      " connection-1 ",
			AppRegistrationID: " registration-1 ",
			InstallationID:    " 98765 ",
			AccountLogin:      " octo-org ",
			AccountType:       " Organization ",
			AccountID:         " 42 ",
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}

	normalized := detail.Normalize()
	if normalized.Connection.Provider != SCMProviderGitHub {
		t.Fatalf("expected normalized provider github, got %q", normalized.Connection.Provider)
	}
	if normalized.Connection.APIBaseURL != defaultGitHubAPIBaseURL {
		t.Fatalf("expected default api base url, got %q", normalized.Connection.APIBaseURL)
	}
	if normalized.Connection.WebBaseURL != defaultGitHubWebBaseURL {
		t.Fatalf("expected default web base url, got %q", normalized.Connection.WebBaseURL)
	}
	if normalized.GitHubAppRegistration == nil || normalized.GitHubAppRegistration.APIBaseURL != defaultGitHubAPIBaseURL {
		t.Fatalf("expected github app registration default api base url, got %+v", normalized.GitHubAppRegistration)
	}
	if normalized.GitHubAppInstallation == nil || normalized.GitHubAppInstallation.AccountType != "organization" {
		t.Fatalf("expected normalized account type organization, got %+v", normalized.GitHubAppInstallation)
	}
	if normalized.Connection.HealthSummary == nil || *normalized.Connection.HealthSummary != "healthy enough" {
		t.Fatalf("expected trimmed health summary, got %+v", normalized.Connection.HealthSummary)
	}
	if normalized.GitHubAppRegistration.DisplayName == nil || *normalized.GitHubAppRegistration.DisplayName != "GitHub App" {
		t.Fatalf("expected trimmed github app display name, got %+v", normalized.GitHubAppRegistration.DisplayName)
	}
	if err := normalized.Validate(); err != nil {
		t.Fatalf("expected valid connection detail, got %v", err)
	}
}

func TestSCMConnectionDetailValidateRejectsMismatchedRelationship(t *testing.T) {
	now := time.Now().UTC()
	detail := SCMConnectionDetail{
		Connection:            SCMConnection{ID: "connection-1", Provider: SCMProviderGitHub, DisplayName: "GitHub", DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, Enabled: true, HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &GitHubAppRegistration{ID: "registration-1", AppID: "1", APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, PrivateKeySecretRef: "secret/a", WebhookSecretRef: "secret/b", CreatedAt: now, UpdatedAt: now},
		GitHubAppInstallation: &GitHubAppInstallation{ConnectionID: "connection-2", AppRegistrationID: "registration-1", InstallationID: "9", AccountLogin: "octo", AccountType: "organization", AccountID: "2", CreatedAt: now, UpdatedAt: now},
	}

	if err := detail.Validate(); err == nil {
		t.Fatal("expected mismatched connection id validation error")
	}
}
