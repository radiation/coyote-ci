package domain

import (
	"strings"
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

func TestSCMConnectionValidationHelpersAndErrors(t *testing.T) {
	if !SCMProviderGitHub.IsValid() || SCMProvider("svn").IsValid() {
		t.Fatal("expected provider validity checks to discriminate known and unknown providers")
	}
	if !SCMDeploymentKindCloud.IsValid() || SCMDeploymentKind("other").IsValid() {
		t.Fatal("expected deployment kind validity checks to discriminate known and unknown kinds")
	}
	if !SCMConnectionHealthStatusHealthy.IsValid() || SCMConnectionHealthStatus("bad").IsValid() {
		t.Fatal("expected health status validity checks to discriminate known and unknown statuses")
	}
	if normalizeSCMBaseURL(SCMProviderGitHub, "", "", true) != defaultGitHubAPIBaseURL {
		t.Fatalf("expected github api default")
	}
	if normalizeSCMBaseURL(SCMProvider("custom"), "", "", false) != "" {
		t.Fatalf("expected empty default for non-github provider")
	}
	if got := normalizeSCMBaseURL("", "", "https://example.com/path/?q=1#frag", false); got != "https://example.com/path" {
		t.Fatalf("expected normalized url, got %q", got)
	}
	if got := normalizeSCMBaseURL("", "", "not-a-url", false); got != "" {
		t.Fatalf("expected invalid url to normalize to empty, got %q", got)
	}
	blank := "   "
	if trimAndTruncateOptionalString(&blank, 10) != nil {
		t.Fatal("expected blank optional string to normalize to nil")
	}
	longValue := strings.Repeat("x", maxSCMConnectionDisplayNameLen+5)
	if got := truncateDomainText(longValue, maxSCMConnectionDisplayNameLen); len([]rune(got)) != maxSCMConnectionDisplayNameLen {
		t.Fatalf("expected truncated text length %d, got %d", maxSCMConnectionDisplayNameLen, len([]rune(got)))
	}
	now := time.Now()
	converted := normalizeSCMTimePointer(&now)
	if converted == nil || converted.Location() != time.UTC {
		t.Fatal("expected normalized time pointer to be converted to UTC")
	}
	if normalizeSCMTimePointer(nil) != nil {
		t.Fatal("expected nil time pointer to stay nil")
	}
	if !isAbsoluteURL("https://example.com") || isAbsoluteURL("/relative") {
		t.Fatal("expected absolute url helper to discriminate absolute and relative urls")
	}

	now = time.Now().UTC()
	connectionCases := []struct {
		name  string
		value SCMConnection
	}{
		{name: "missing id", value: SCMConnection{Provider: SCMProviderGitHub, DisplayName: "name", DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now}},
		{name: "bad provider", value: SCMConnection{ID: "id", Provider: SCMProvider("other"), DisplayName: "name", DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now}},
		{name: "missing display", value: SCMConnection{ID: "id", Provider: SCMProviderGitHub, DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now}},
		{name: "bad deployment", value: SCMConnection{ID: "id", Provider: SCMProviderGitHub, DisplayName: "name", DeploymentKind: SCMDeploymentKind("other"), APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now}},
		{name: "bad api url", value: SCMConnection{ID: "id", Provider: SCMProviderGitHub, DisplayName: "name", DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: "bad", WebBaseURL: defaultGitHubWebBaseURL, HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now}},
		{name: "bad web url", value: SCMConnection{ID: "id", Provider: SCMProviderGitHub, DisplayName: "name", DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: "bad", HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now}},
		{name: "bad health", value: SCMConnection{ID: "id", Provider: SCMProviderGitHub, DisplayName: "name", DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, HealthStatus: SCMConnectionHealthStatus("other"), CreatedAt: now, UpdatedAt: now}},
		{name: "missing timestamps", value: SCMConnection{ID: "id", Provider: SCMProviderGitHub, DisplayName: "name", DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, HealthStatus: SCMConnectionHealthStatusUnknown}},
	}
	for _, test := range connectionCases {
		if err := test.value.Validate(); err == nil {
			t.Fatalf("expected validation error for %s", test.name)
		}
	}

	registrationCases := []struct {
		name  string
		value GitHubAppRegistration
	}{
		{name: "missing id", value: GitHubAppRegistration{AppID: "1", APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, PrivateKeySecretRef: "secret/a", WebhookSecretRef: "secret/b", CreatedAt: now, UpdatedAt: now}},
		{name: "missing app id", value: GitHubAppRegistration{ID: "id", APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, PrivateKeySecretRef: "secret/a", WebhookSecretRef: "secret/b", CreatedAt: now, UpdatedAt: now}},
		{name: "bad api url", value: GitHubAppRegistration{ID: "id", AppID: "1", APIBaseURL: "bad", WebBaseURL: defaultGitHubWebBaseURL, PrivateKeySecretRef: "secret/a", WebhookSecretRef: "secret/b", CreatedAt: now, UpdatedAt: now}},
		{name: "bad web url", value: GitHubAppRegistration{ID: "id", AppID: "1", APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: "bad", PrivateKeySecretRef: "secret/a", WebhookSecretRef: "secret/b", CreatedAt: now, UpdatedAt: now}},
		{name: "missing private key", value: GitHubAppRegistration{ID: "id", AppID: "1", APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, WebhookSecretRef: "secret/b", CreatedAt: now, UpdatedAt: now}},
		{name: "missing webhook", value: GitHubAppRegistration{ID: "id", AppID: "1", APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, PrivateKeySecretRef: "secret/a", CreatedAt: now, UpdatedAt: now}},
		{name: "missing timestamps", value: GitHubAppRegistration{ID: "id", AppID: "1", APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, PrivateKeySecretRef: "secret/a", WebhookSecretRef: "secret/b"}},
	}
	for _, test := range registrationCases {
		if err := test.value.Validate(); err == nil {
			t.Fatalf("expected github app registration validation error for %s", test.name)
		}
	}

	installationCases := []struct {
		name  string
		value GitHubAppInstallation
	}{
		{name: "missing connection id", value: GitHubAppInstallation{AppRegistrationID: "reg", InstallationID: "inst", AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now}},
		{name: "missing registration id", value: GitHubAppInstallation{ConnectionID: "conn", InstallationID: "inst", AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now}},
		{name: "missing installation id", value: GitHubAppInstallation{ConnectionID: "conn", AppRegistrationID: "reg", AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now}},
		{name: "missing login", value: GitHubAppInstallation{ConnectionID: "conn", AppRegistrationID: "reg", InstallationID: "inst", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now}},
		{name: "missing type", value: GitHubAppInstallation{ConnectionID: "conn", AppRegistrationID: "reg", InstallationID: "inst", AccountLogin: "octo", AccountID: "42", CreatedAt: now, UpdatedAt: now}},
		{name: "missing account id", value: GitHubAppInstallation{ConnectionID: "conn", AppRegistrationID: "reg", InstallationID: "inst", AccountLogin: "octo", AccountType: "organization", CreatedAt: now, UpdatedAt: now}},
		{name: "missing timestamps", value: GitHubAppInstallation{ConnectionID: "conn", AppRegistrationID: "reg", InstallationID: "inst", AccountLogin: "octo", AccountType: "organization", AccountID: "42"}},
	}
	for _, test := range installationCases {
		if err := test.value.Validate(); err == nil {
			t.Fatalf("expected github app installation validation error for %s", test.name)
		}
	}

	nonGitHubDetail := SCMConnectionDetail{Connection: SCMConnection{ID: "conn", Provider: SCMProviderGitLab, DisplayName: "GitLab", DeploymentKind: SCMDeploymentKindSelfHosted, APIBaseURL: "https://gitlab.example/api/v4", WebBaseURL: "https://gitlab.example", HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now}}
	if err := nonGitHubDetail.Validate(); err != nil {
		t.Fatalf("expected non-github detail without github substructures to validate, got %v", err)
	}

	baseDetail := SCMConnectionDetail{
		Connection:            SCMConnection{ID: "conn", Provider: SCMProviderGitHub, DisplayName: "GitHub", DeploymentKind: SCMDeploymentKindCloud, APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, HealthStatus: SCMConnectionHealthStatusUnknown, CreatedAt: now, UpdatedAt: now},
		GitHubAppRegistration: &GitHubAppRegistration{ID: "reg", AppID: "1", APIBaseURL: defaultGitHubAPIBaseURL, WebBaseURL: defaultGitHubWebBaseURL, PrivateKeySecretRef: "secret/a", WebhookSecretRef: "secret/b", CreatedAt: now, UpdatedAt: now},
		GitHubAppInstallation: &GitHubAppInstallation{ConnectionID: "conn", AppRegistrationID: "reg", InstallationID: "inst", AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now},
	}
	missingRegistration := baseDetail
	missingRegistration.GitHubAppRegistration = nil
	if err := missingRegistration.Validate(); err == nil {
		t.Fatal("expected missing github app registration to fail validation")
	}
	missingInstallation := baseDetail
	missingInstallation.GitHubAppInstallation = nil
	if err := missingInstallation.Validate(); err == nil {
		t.Fatal("expected missing github app installation to fail validation")
	}
	mismatchedRegistrationID := baseDetail
	mismatchedRegistrationID.GitHubAppInstallation = &GitHubAppInstallation{ConnectionID: "conn", AppRegistrationID: "other", InstallationID: "inst", AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now}
	if err := mismatchedRegistrationID.Validate(); err == nil {
		t.Fatal("expected mismatched registration id to fail validation")
	}
	mismatchedAPIBaseURL := baseDetail
	mismatchedAPIBaseURL.Connection.APIBaseURL = "https://ghe.example/api/v3"
	if err := mismatchedAPIBaseURL.Validate(); err == nil {
		t.Fatal("expected mismatched api base url to fail validation")
	}
	mismatchedWebBaseURL := baseDetail
	mismatchedWebBaseURL.Connection.WebBaseURL = "https://ghe.example"
	if err := mismatchedWebBaseURL.Validate(); err == nil {
		t.Fatal("expected mismatched web base url to fail validation")
	}
}
