package domain

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type SCMProvider string

const (
	SCMProviderGitHub    SCMProvider = "github"
	SCMProviderGitLab    SCMProvider = "gitlab"
	SCMProviderBitbucket SCMProvider = "bitbucket"
)

type SCMDeploymentKind string

const (
	SCMDeploymentKindCloud      SCMDeploymentKind = "cloud"
	SCMDeploymentKindSelfHosted SCMDeploymentKind = "self_hosted"
)

type SCMConnectionHealthStatus string

const (
	SCMConnectionHealthStatusUnknown   SCMConnectionHealthStatus = "unknown"
	SCMConnectionHealthStatusHealthy   SCMConnectionHealthStatus = "healthy"
	SCMConnectionHealthStatusDegraded  SCMConnectionHealthStatus = "degraded"
	SCMConnectionHealthStatusUnhealthy SCMConnectionHealthStatus = "unhealthy"
	SCMConnectionHealthStatusRevoked   SCMConnectionHealthStatus = "revoked"
)

const (
	defaultGitHubAPIBaseURL          = "https://api.github.com"
	defaultGitHubWebBaseURL          = "https://github.com"
	maxSCMConnectionHealthSummaryLen = 512
	maxSCMConnectionDisplayNameLen   = 200
	maxGitHubAppDisplayNameLen       = 200
	maxGitHubAccountLoginLen         = 200
	maxGitHubAccountTypeLen          = 50
)

type SCMConnection struct {
	ID                  string
	Provider            SCMProvider
	DisplayName         string
	DeploymentKind      SCMDeploymentKind
	APIBaseURL          string
	WebBaseURL          string
	Enabled             bool
	HealthStatus        SCMConnectionHealthStatus
	HealthSummary       *string
	LastHealthCheckedAt *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type GitHubAppRegistration struct {
	ID                  string
	AppID               string
	DisplayName         *string
	APIBaseURL          string
	WebBaseURL          string
	PrivateKeySecretRef string
	WebhookSecretRef    string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type GitHubAppInstallation struct {
	ConnectionID      string
	AppRegistrationID string
	InstallationID    string
	AccountLogin      string
	AccountType       string
	AccountID         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type SCMConnectionDetail struct {
	Connection            SCMConnection
	GitHubAppRegistration *GitHubAppRegistration
	GitHubAppInstallation *GitHubAppInstallation
}

func (p SCMProvider) IsValid() bool {
	switch p {
	case SCMProviderGitHub, SCMProviderGitLab, SCMProviderBitbucket:
		return true
	default:
		return false
	}
}

func (k SCMDeploymentKind) IsValid() bool {
	switch k {
	case SCMDeploymentKindCloud, SCMDeploymentKindSelfHosted:
		return true
	default:
		return false
	}
}

func (s SCMConnectionHealthStatus) IsValid() bool {
	switch s {
	case SCMConnectionHealthStatusUnknown,
		SCMConnectionHealthStatusHealthy,
		SCMConnectionHealthStatusDegraded,
		SCMConnectionHealthStatusUnhealthy,
		SCMConnectionHealthStatusRevoked:
		return true
	default:
		return false
	}
}

func (c SCMConnection) Normalize() SCMConnection {
	c.ID = strings.TrimSpace(c.ID)
	c.Provider = SCMProvider(strings.ToLower(strings.TrimSpace(string(c.Provider))))
	c.DisplayName = truncateDomainText(strings.TrimSpace(c.DisplayName), maxSCMConnectionDisplayNameLen)
	c.DeploymentKind = SCMDeploymentKind(strings.ToLower(strings.TrimSpace(string(c.DeploymentKind))))
	c.APIBaseURL = normalizeSCMBaseURL(c.Provider, c.DeploymentKind, c.APIBaseURL, true)
	c.WebBaseURL = normalizeSCMBaseURL(c.Provider, c.DeploymentKind, c.WebBaseURL, false)
	c.HealthStatus = SCMConnectionHealthStatus(strings.ToLower(strings.TrimSpace(string(c.HealthStatus))))
	c.HealthSummary = trimAndTruncateOptionalString(c.HealthSummary, maxSCMConnectionHealthSummaryLen)
	c.LastHealthCheckedAt = normalizeSCMTimePointer(c.LastHealthCheckedAt)
	c.CreatedAt = c.CreatedAt.UTC()
	c.UpdatedAt = c.UpdatedAt.UTC()
	if c.HealthStatus == "" {
		c.HealthStatus = SCMConnectionHealthStatusUnknown
	}
	return c
}

func (c SCMConnection) Validate() error {
	c = c.Normalize()
	if c.ID == "" {
		return fmt.Errorf("scm connection id is required")
	}
	if !c.Provider.IsValid() {
		return fmt.Errorf("unsupported scm provider %q", c.Provider)
	}
	if c.DisplayName == "" {
		return fmt.Errorf("scm connection display name is required")
	}
	if !c.DeploymentKind.IsValid() {
		return fmt.Errorf("unsupported scm deployment kind %q", c.DeploymentKind)
	}
	if c.APIBaseURL == "" || !isAbsoluteURL(c.APIBaseURL) {
		return fmt.Errorf("scm connection api base url is required")
	}
	if c.WebBaseURL == "" || !isAbsoluteURL(c.WebBaseURL) {
		return fmt.Errorf("scm connection web base url is required")
	}
	if !c.HealthStatus.IsValid() {
		return fmt.Errorf("unsupported scm connection health status %q", c.HealthStatus)
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return fmt.Errorf("scm connection timestamps are required")
	}
	return nil
}

func (r GitHubAppRegistration) Normalize() GitHubAppRegistration {
	r.ID = strings.TrimSpace(r.ID)
	r.AppID = strings.TrimSpace(r.AppID)
	r.DisplayName = trimAndTruncateOptionalString(r.DisplayName, maxGitHubAppDisplayNameLen)
	r.APIBaseURL = normalizeSCMBaseURL(SCMProviderGitHub, "", r.APIBaseURL, true)
	r.WebBaseURL = normalizeSCMBaseURL(SCMProviderGitHub, "", r.WebBaseURL, false)
	r.PrivateKeySecretRef = strings.TrimSpace(r.PrivateKeySecretRef)
	r.WebhookSecretRef = strings.TrimSpace(r.WebhookSecretRef)
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return r
}

func (r GitHubAppRegistration) Validate() error {
	r = r.Normalize()
	if r.ID == "" {
		return fmt.Errorf("github app registration id is required")
	}
	if r.AppID == "" {
		return fmt.Errorf("github app registration app id is required")
	}
	if r.APIBaseURL == "" || !isAbsoluteURL(r.APIBaseURL) {
		return fmt.Errorf("github app registration api base url is required")
	}
	if r.WebBaseURL == "" || !isAbsoluteURL(r.WebBaseURL) {
		return fmt.Errorf("github app registration web base url is required")
	}
	if r.PrivateKeySecretRef == "" {
		return fmt.Errorf("github app registration private key secret ref is required")
	}
	if r.WebhookSecretRef == "" {
		return fmt.Errorf("github app registration webhook secret ref is required")
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return fmt.Errorf("github app registration timestamps are required")
	}
	return nil
}

func (i GitHubAppInstallation) Normalize() GitHubAppInstallation {
	i.ConnectionID = strings.TrimSpace(i.ConnectionID)
	i.AppRegistrationID = strings.TrimSpace(i.AppRegistrationID)
	i.InstallationID = strings.TrimSpace(i.InstallationID)
	i.AccountLogin = truncateDomainText(strings.TrimSpace(i.AccountLogin), maxGitHubAccountLoginLen)
	i.AccountType = truncateDomainText(strings.ToLower(strings.TrimSpace(i.AccountType)), maxGitHubAccountTypeLen)
	i.AccountID = strings.TrimSpace(i.AccountID)
	i.CreatedAt = i.CreatedAt.UTC()
	i.UpdatedAt = i.UpdatedAt.UTC()
	return i
}

func (i GitHubAppInstallation) Validate() error {
	i = i.Normalize()
	if i.ConnectionID == "" {
		return fmt.Errorf("github app installation connection id is required")
	}
	if i.AppRegistrationID == "" {
		return fmt.Errorf("github app installation app registration id is required")
	}
	if i.InstallationID == "" {
		return fmt.Errorf("github app installation installation id is required")
	}
	if i.AccountLogin == "" {
		return fmt.Errorf("github app installation account login is required")
	}
	if i.AccountType == "" {
		return fmt.Errorf("github app installation account type is required")
	}
	if i.AccountID == "" {
		return fmt.Errorf("github app installation account id is required")
	}
	if i.CreatedAt.IsZero() || i.UpdatedAt.IsZero() {
		return fmt.Errorf("github app installation timestamps are required")
	}
	return nil
}

func (d SCMConnectionDetail) Normalize() SCMConnectionDetail {
	d.Connection = d.Connection.Normalize()
	if d.GitHubAppRegistration != nil {
		registration := d.GitHubAppRegistration.Normalize()
		d.GitHubAppRegistration = &registration
	}
	if d.GitHubAppInstallation != nil {
		installation := d.GitHubAppInstallation.Normalize()
		d.GitHubAppInstallation = &installation
	}
	return d
}

func (d SCMConnectionDetail) Validate() error {
	d = d.Normalize()
	if err := d.Connection.Validate(); err != nil {
		return err
	}
	if d.Connection.Provider == SCMProviderGitHub {
		if d.GitHubAppRegistration == nil {
			return fmt.Errorf("github app registration is required for github connections")
		}
		if d.GitHubAppInstallation == nil {
			return fmt.Errorf("github app installation is required for github connections")
		}
		if err := d.GitHubAppRegistration.Validate(); err != nil {
			return err
		}
		if err := d.GitHubAppInstallation.Validate(); err != nil {
			return err
		}
		if d.GitHubAppInstallation.ConnectionID != d.Connection.ID {
			return fmt.Errorf("github app installation connection id must match scm connection id")
		}
		if d.GitHubAppInstallation.AppRegistrationID != d.GitHubAppRegistration.ID {
			return fmt.Errorf("github app installation app registration id must match github app registration id")
		}
		if d.Connection.APIBaseURL != d.GitHubAppRegistration.APIBaseURL {
			return fmt.Errorf("github connection api base url must match github app registration api base url")
		}
		if d.Connection.WebBaseURL != d.GitHubAppRegistration.WebBaseURL {
			return fmt.Errorf("github connection web base url must match github app registration web base url")
		}
	}
	return nil
}

func normalizeSCMBaseURL(provider SCMProvider, deploymentKind SCMDeploymentKind, raw string, api bool) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" && provider == SCMProviderGitHub {
		if api {
			trimmed = defaultGitHubAPIBaseURL
		} else {
			trimmed = defaultGitHubWebBaseURL
		}
	}
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func trimAndTruncateOptionalString(value *string, maxLen int) *string {
	if value == nil {
		return nil
	}
	trimmed := truncateDomainText(strings.TrimSpace(*value), maxLen)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func truncateDomainText(value string, maxLen int) string {
	if maxLen <= 0 || len([]rune(value)) <= maxLen {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxLen])
}

func normalizeSCMTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := value.UTC()
	return &converted
}

func isAbsoluteURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed == nil {
		return false
	}
	return parsed.Scheme != "" && parsed.Host != ""
}
