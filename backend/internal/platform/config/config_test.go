package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	defaultWorkspaceRoot := filepath.Join(os.TempDir(), "coyote-builds")
	defaultCacheRoot := filepath.Join(os.TempDir(), "coyote-cache")
	defaultArtifactRoot := filepath.Join(os.TempDir(), "coyote-artifacts")

	tests := []struct {
		name      string
		env       map[string]string
		expected  Config
		useNoEnvs bool
	}{
		{
			name: "uses defaults when env is unset",
			env: map[string]string{
				"APP_PORT":                        "",
				"DATABASE_URL":                    "",
				"DB_HOST":                         "",
				"DB_PORT":                         "",
				"DB_USER":                         "",
				"DB_PASSWORD":                     "",
				"DB_NAME":                         "",
				"DB_SSLMODE":                      "",
				"DB_MAX_OPEN_CONNS":               "",
				"DB_MAX_IDLE_CONNS":               "",
				"DB_CONN_MAX_LIFETIME":            "",
				"DB_CONN_MAX_IDLE_TIME":           "",
				"WORKER_STEP_LEASE_SECONDS":       "",
				"WORKER_STATUS_ADDR":              "",
				"WORKER_EXECUTION_BACKEND":        "",
				"WORKER_EXECUTION_DEFAULT_IMAGE":  "",
				"WORKER_EXECUTION_WORKSPACE_ROOT": "",
				"WORKER_KUBERNETES_NAMESPACE":     "",
				"WORKER_KUBERNETES_KUBECONFIG":    "",
				"WORKER_CACHE_STORAGE_ROOT":       "",
				"CACHE_MAX_SIZE_MB":               "",
				"ARTIFACT_STORAGE_ROOT":           "",
				"PUSH_EVENT_SECRET":               "",
				"GITHUB_WEBHOOK_SECRET":           "",
				"AUTH_MODE":                       "",
				"BOOTSTRAP_ADMIN_EMAILS":          "",
				"AUTH_POST_LOGIN_REDIRECT_URL":    "",
				"AUTH_POST_LOGOUT_REDIRECT_URL":   "",
				"EMAIL_NOTIFICATIONS_ENABLED":     "",
				"EMAIL_NOTIFICATION_RECIPIENTS":   "",
				"SMTP_HOST":                       "",
				"SMTP_PORT":                       "",
				"SMTP_USERNAME":                   "",
				"SMTP_PASSWORD":                   "",
				"SMTP_FROM_ADDRESS":               "",
			},
			expected: Config{
				AppPort:                              "8080",
				DatabaseURLValue:                     "",
				DBHost:                               "localhost",
				DBPort:                               "5432",
				DBUser:                               "coyote",
				DBPassword:                           "coyote",
				DBName:                               "coyote_ci",
				DBSSLMode:                            "disable",
				DBMaxOpenConns:                       10,
				DBMaxIdleConns:                       5,
				DBConnMaxLifetime:                    30 * time.Minute,
				DBConnMaxIdleTime:                    5 * time.Minute,
				StepLeaseSeconds:                     45,
				WorkerStatusAddr:                     "",
				ExecutionBackend:                     "docker",
				ExecutionDefaultImage:                "alpine:3.20",
				ExecutionWorkspaceRoot:               defaultWorkspaceRoot,
				WorkerKubernetesNamespace:            "default",
				WorkspaceHelperMaxUploadSizeMB:       1024,
				WorkspaceHelperMaxUncompressedSizeMB: 1024,
				WorkspaceHelperMaxArchiveEntries:     10000,
				WorkerCacheStorageRoot:               defaultCacheRoot,
				WorkerCacheMaxSizeMB:                 10240,
				ArtifactStorageRoot:                  defaultArtifactRoot,
				ArtifactStorageProvider:              "filesystem",
				PushEventSecret:                      "",
				GitHubWebhookSecret:                  "",
				GitHubStatusToken:                    "",
				AuthMode:                             "disabled",
				OIDCScopes:                           "openid email profile",
				SessionCookieName:                    "coyote_session",
				SessionCookieSecure:                  true,
				SessionCookieSameSite:                "lax",
				AuthPostLoginRedirectURL:             "",
				AuthPostLogoutRedirectURL:            "",
				EmailNotificationsEnabled:            true,
				EmailNotificationRecipients:          "dev@localhost",
				NotificationRecoveryInterval:         15 * time.Second,
				NotificationRecoveryBatchSize:        25,
				SCMStatusRecoveryInterval:            15 * time.Second,
				SCMStatusRecoveryBatchSize:           25,
				SMTPHost:                             "mailpit",
				SMTPPort:                             "1025",
				SMTPUsername:                         "",
				SMTPPassword:                         "",
				SMTPFromAddress:                      "coyote-ci@localhost",
			},
		},
		{
			name: "uses env values when set",
			env: map[string]string{
				"APP_PORT":                        "9999",
				"DATABASE_URL":                    "postgres://external/external?sslmode=require",
				"DB_HOST":                         "db.internal",
				"DB_PORT":                         "5433",
				"DB_USER":                         "user1",
				"DB_PASSWORD":                     "pass1",
				"DB_NAME":                         "name1",
				"DB_SSLMODE":                      "require",
				"DB_MAX_OPEN_CONNS":               "25",
				"DB_MAX_IDLE_CONNS":               "12",
				"DB_CONN_MAX_LIFETIME":            "45m",
				"DB_CONN_MAX_IDLE_TIME":           "10m",
				"WORKER_STEP_LEASE_SECONDS":       "60",
				"WORKER_STATUS_ADDR":              "127.0.0.1:9091",
				"WORKER_EXECUTION_BACKEND":        "inprocess",
				"WORKER_EXECUTION_DEFAULT_IMAGE":  "golang:1.23-alpine",
				"WORKER_EXECUTION_WORKSPACE_ROOT": "/var/tmp/coyote-workspaces",
				"WORKER_KUBERNETES_NAMESPACE":     "coyote-system",
				"WORKER_KUBERNETES_KUBECONFIG":    "/tmp/kubeconfig",
				"WORKER_CACHE_STORAGE_ROOT":       "/var/tmp/coyote-cache",
				"CACHE_MAX_SIZE_MB":               "2048",
				"ARTIFACT_STORAGE_ROOT":           "/var/tmp/coyote-artifacts",
				"PUSH_EVENT_SECRET":               "push-secret",
				"GITHUB_WEBHOOK_SECRET":           "github-secret",
				"AUTH_MODE":                       "header",
				"BOOTSTRAP_ADMIN_EMAILS":          "Admin@Example.com,ops@example.com",
				"OIDC_ISSUER_URL":                 "https://issuer.example.com",
				"OIDC_CLIENT_ID":                  "coyote",
				"OIDC_CLIENT_SECRET":              "secret",
				"OIDC_REDIRECT_URL":               "http://localhost:8080/auth/callback",
				"OIDC_SCOPES":                     "openid email",
				"SESSION_SECRET":                  "session-secret",
				"SESSION_COOKIE_NAME":             "custom_session",
				"SESSION_COOKIE_SECURE":           "false",
				"SESSION_COOKIE_SAME_SITE":        "strict",
				"AUTH_POST_LOGIN_REDIRECT_URL":    "http://localhost:3000/",
				"AUTH_POST_LOGOUT_REDIRECT_URL":   "http://localhost:3000/",
				"EMAIL_NOTIFICATIONS_ENABLED":     "false",
				"EMAIL_NOTIFICATION_RECIPIENTS":   "dev1@example.com, dev2@example.com ",
				"SMTP_HOST":                       "smtp.internal",
				"SMTP_PORT":                       "2525",
				"SMTP_USERNAME":                   "mailer",
				"SMTP_PASSWORD":                   "secret",
				"SMTP_FROM_ADDRESS":               "coyote@example.com",
			},
			expected: Config{
				AppPort:                        "9999",
				DatabaseURLValue:               "postgres://external/external?sslmode=require",
				DBHost:                         "db.internal",
				DBPort:                         "5433",
				DBUser:                         "user1",
				DBPassword:                     "pass1",
				DBName:                         "name1",
				DBSSLMode:                      "require",
				DBMaxOpenConns:                 25,
				DBMaxIdleConns:                 12,
				DBConnMaxLifetime:              45 * time.Minute,
				DBConnMaxIdleTime:              10 * time.Minute,
				StepLeaseSeconds:               60,
				WorkerStatusAddr:               "127.0.0.1:9091",
				ExecutionBackend:               "inprocess",
				ExecutionDefaultImage:          "golang:1.23-alpine",
				ExecutionWorkspaceRoot:         "/var/tmp/coyote-workspaces",
				WorkerKubernetesNamespace:      "coyote-system",
				WorkerKubernetesKubeconfig:     "/tmp/kubeconfig",
				WorkspaceHelperMaxUploadSizeMB: 1024,
				WorkerCacheStorageRoot:         "/var/tmp/coyote-cache",
				WorkerCacheMaxSizeMB:           2048,
				ArtifactStorageRoot:            "/var/tmp/coyote-artifacts",
				ArtifactStorageProvider:        "filesystem",
				PushEventSecret:                "push-secret",
				GitHubWebhookSecret:            "github-secret",
				GitHubStatusToken:              "",
				AuthMode:                       "header",
				BootstrapAdminEmails:           "Admin@Example.com,ops@example.com",
				OIDCIssuerURL:                  "https://issuer.example.com",
				OIDCClientID:                   "coyote",
				OIDCClientSecret:               "secret",
				OIDCRedirectURL:                "http://localhost:8080/auth/callback",
				OIDCScopes:                     "openid email",
				SessionSecret:                  "session-secret",
				SessionCookieName:              "custom_session",
				SessionCookieSecure:            false,
				SessionCookieSameSite:          "strict",
				AuthPostLoginRedirectURL:       "http://localhost:3000/",
				AuthPostLogoutRedirectURL:      "http://localhost:3000/",
				EmailNotificationsEnabled:      false,
				EmailNotificationRecipients:    "dev1@example.com, dev2@example.com ",
				NotificationRecoveryInterval:   15 * time.Second,
				NotificationRecoveryBatchSize:  25,
				SCMStatusRecoveryInterval:      15 * time.Second,
				SCMStatusRecoveryBatchSize:     25,
				SMTPHost:                       "smtp.internal",
				SMTPPort:                       "2525",
				SMTPUsername:                   "mailer",
				SMTPPassword:                   "secret",
				SMTPFromAddress:                "coyote@example.com",
			},
		},
		{
			name: "invalid lease seconds falls back to default",
			env: map[string]string{
				"APP_PORT":                        "",
				"DATABASE_URL":                    "",
				"DB_HOST":                         "",
				"DB_PORT":                         "",
				"DB_USER":                         "",
				"DB_PASSWORD":                     "",
				"DB_NAME":                         "",
				"DB_SSLMODE":                      "",
				"DB_MAX_OPEN_CONNS":               "",
				"DB_MAX_IDLE_CONNS":               "",
				"DB_CONN_MAX_LIFETIME":            "",
				"DB_CONN_MAX_IDLE_TIME":           "",
				"WORKER_STEP_LEASE_SECONDS":       "not-an-int",
				"WORKER_STATUS_ADDR":              "",
				"WORKER_EXECUTION_BACKEND":        "",
				"WORKER_EXECUTION_DEFAULT_IMAGE":  "",
				"WORKER_EXECUTION_WORKSPACE_ROOT": "",
				"WORKER_CACHE_STORAGE_ROOT":       "",
				"CACHE_MAX_SIZE_MB":               "",
				"ARTIFACT_STORAGE_ROOT":           "",
				"PUSH_EVENT_SECRET":               "",
				"GITHUB_WEBHOOK_SECRET":           "",
				"AUTH_MODE":                       "",
				"BOOTSTRAP_ADMIN_EMAILS":          "",
				"AUTH_POST_LOGIN_REDIRECT_URL":    "",
				"AUTH_POST_LOGOUT_REDIRECT_URL":   "",
				"EMAIL_NOTIFICATIONS_ENABLED":     "",
				"EMAIL_NOTIFICATION_RECIPIENTS":   "",
				"SMTP_HOST":                       "",
				"SMTP_PORT":                       "",
				"SMTP_USERNAME":                   "",
				"SMTP_PASSWORD":                   "",
				"SMTP_FROM_ADDRESS":               "",
			},
			expected: Config{
				AppPort:                              "8080",
				DatabaseURLValue:                     "",
				DBHost:                               "localhost",
				DBPort:                               "5432",
				DBUser:                               "coyote",
				DBPassword:                           "coyote",
				DBName:                               "coyote_ci",
				DBSSLMode:                            "disable",
				DBMaxOpenConns:                       10,
				DBMaxIdleConns:                       5,
				DBConnMaxLifetime:                    30 * time.Minute,
				DBConnMaxIdleTime:                    5 * time.Minute,
				StepLeaseSeconds:                     45,
				WorkerStatusAddr:                     "",
				ExecutionBackend:                     "docker",
				ExecutionDefaultImage:                "alpine:3.20",
				ExecutionWorkspaceRoot:               defaultWorkspaceRoot,
				WorkerKubernetesNamespace:            "default",
				WorkspaceHelperMaxUploadSizeMB:       1024,
				WorkspaceHelperMaxUncompressedSizeMB: 1024,
				WorkspaceHelperMaxArchiveEntries:     10000,
				WorkerCacheStorageRoot:               defaultCacheRoot,
				WorkerCacheMaxSizeMB:                 10240,
				ArtifactStorageRoot:                  defaultArtifactRoot,
				ArtifactStorageProvider:              "filesystem",
				PushEventSecret:                      "",
				GitHubWebhookSecret:                  "",
				GitHubStatusToken:                    "",
				AuthMode:                             "disabled",
				OIDCScopes:                           "openid email profile",
				SessionCookieName:                    "coyote_session",
				SessionCookieSecure:                  true,
				SessionCookieSameSite:                "lax",
				AuthPostLoginRedirectURL:             "",
				AuthPostLogoutRedirectURL:            "",
				EmailNotificationsEnabled:            true,
				EmailNotificationRecipients:          "dev@localhost",
				NotificationRecoveryInterval:         15 * time.Second,
				NotificationRecoveryBatchSize:        25,
				SCMStatusRecoveryInterval:            15 * time.Second,
				SCMStatusRecoveryBatchSize:           25,
				SMTPHost:                             "mailpit",
				SMTPPort:                             "1025",
				SMTPUsername:                         "",
				SMTPPassword:                         "",
				SMTPFromAddress:                      "coyote-ci@localhost",
			},
		},
		{
			name: "invalid duration falls back to default",
			env: map[string]string{
				"DB_CONN_MAX_LIFETIME":          "invalid",
				"DB_CONN_MAX_IDLE_TIME":         "still-invalid",
				"WORKER_CACHE_STORAGE_ROOT":     "",
				"PUSH_EVENT_SECRET":             "",
				"GITHUB_WEBHOOK_SECRET":         "",
				"AUTH_MODE":                     "",
				"BOOTSTRAP_ADMIN_EMAILS":        "",
				"AUTH_POST_LOGIN_REDIRECT_URL":  "",
				"AUTH_POST_LOGOUT_REDIRECT_URL": "",
				"EMAIL_NOTIFICATIONS_ENABLED":   "not-a-bool",
				"EMAIL_NOTIFICATION_RECIPIENTS": "",
				"SMTP_HOST":                     "",
				"SMTP_PORT":                     "",
				"SMTP_USERNAME":                 "",
				"SMTP_PASSWORD":                 "",
				"SMTP_FROM_ADDRESS":             "",
			},
			expected: Config{
				AppPort:                        "8080",
				DatabaseURLValue:               "",
				DBHost:                         "localhost",
				DBPort:                         "5432",
				DBUser:                         "coyote",
				DBPassword:                     "coyote",
				DBName:                         "coyote_ci",
				DBSSLMode:                      "disable",
				DBMaxOpenConns:                 10,
				DBMaxIdleConns:                 5,
				DBConnMaxLifetime:              30 * time.Minute,
				DBConnMaxIdleTime:              5 * time.Minute,
				StepLeaseSeconds:               45,
				WorkerStatusAddr:               "",
				ExecutionBackend:               "docker",
				ExecutionDefaultImage:          "alpine:3.20",
				ExecutionWorkspaceRoot:         defaultWorkspaceRoot,
				WorkerKubernetesNamespace:      "default",
				WorkspaceHelperMaxUploadSizeMB: 1024,
				WorkerCacheStorageRoot:         defaultCacheRoot,
				WorkerCacheMaxSizeMB:           10240,
				ArtifactStorageRoot:            defaultArtifactRoot,
				ArtifactStorageProvider:        "filesystem",
				PushEventSecret:                "",
				GitHubWebhookSecret:            "",
				GitHubStatusToken:              "",
				AuthMode:                       "disabled",
				OIDCScopes:                     "openid email profile",
				SessionCookieName:              "coyote_session",
				SessionCookieSecure:            true,
				SessionCookieSameSite:          "lax",
				EmailNotificationsEnabled:      true,
				EmailNotificationRecipients:    "dev@localhost",
				NotificationRecoveryInterval:   15 * time.Second,
				NotificationRecoveryBatchSize:  25,
				SCMStatusRecoveryInterval:      15 * time.Second,
				SCMStatusRecoveryBatchSize:     25,
				SMTPHost:                       "mailpit",
				SMTPPort:                       "1025",
				SMTPUsername:                   "",
				SMTPPassword:                   "",
				SMTPFromAddress:                "coyote-ci@localhost",
			},
		},
	}

	managedEnvKeys := []string{
		"WORKER_KUBERNETES_NAMESPACE",
		"WORKER_KUBERNETES_KUBECONFIG",
		"KUBECONFIG",
		"COYOTE_WORKSPACE_HELPER_ENABLED",
		"COYOTE_WORKSPACE_HELPER_KUBECONFIG",
		"COYOTE_WORKSPACE_HELPER_SERVICE_ACCOUNT",
		"COYOTE_WORKSPACE_HELPER_CAPABILITY_SECRET",
		"COYOTE_WORKSPACE_HELPER_MAX_UPLOAD_SIZE_MB",
		"COYOTE_WORKSPACE_HELPER_MAX_UNCOMPRESSED_SIZE_MB",
		"COYOTE_WORKSPACE_HELPER_MAX_ARCHIVE_ENTRIES",
		"OIDC_ISSUER_URL",
		"OIDC_CLIENT_ID",
		"OIDC_CLIENT_SECRET",
		"OIDC_REDIRECT_URL",
		"OIDC_SCOPES",
		"SESSION_SECRET",
		"SESSION_COOKIE_NAME",
		"SESSION_COOKIE_SECURE",
		"SESSION_COOKIE_SAME_SITE",
		"PUSH_EVENT_SECRET",
		"GITHUB_WEBHOOK_SECRET",
		"GITHUB_STATUS_TOKEN",
		"AUTH_POST_LOGIN_REDIRECT_URL",
		"AUTH_POST_LOGOUT_REDIRECT_URL",
		"EMAIL_NOTIFICATIONS_ENABLED",
		"EMAIL_NOTIFICATION_RECIPIENTS",
		"SCM_STATUS_RECOVERY_INTERVAL",
		"SCM_STATUS_RECOVERY_BATCH_SIZE",
		"SMTP_HOST",
		"SMTP_PORT",
		"SMTP_USERNAME",
		"SMTP_PASSWORD",
		"SMTP_FROM_ADDRESS",
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range managedEnvKeys {
				t.Setenv(key, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			got := Load()
			expected := tc.expected
			expected.WorkspaceHelperServiceAccount = "coyote-workspace-helper"
			expected.WorkspaceHelperMaxUncompressedSizeMB = 1024
			expected.WorkspaceHelperMaxArchiveEntries = 10000
			if got != expected {
				t.Fatalf("expected %+v, got %+v", expected, got)
			}
		})
	}
}

func TestLoadWorkspaceHelperCapabilityConfig(t *testing.T) {
	t.Setenv("COYOTE_WORKSPACE_HELPER_ENABLED", "true")
	t.Setenv("COYOTE_WORKSPACE_HELPER_KUBECONFIG", "/server/kubeconfig")
	t.Setenv("COYOTE_WORKSPACE_HELPER_SERVICE_ACCOUNT", "workspace-helper")
	t.Setenv("COYOTE_WORKSPACE_HELPER_CAPABILITY_SECRET", "workspace-helper-signing-secret")
	t.Setenv("COYOTE_WORKSPACE_HELPER_MAX_UNCOMPRESSED_SIZE_MB", "2048")
	t.Setenv("COYOTE_WORKSPACE_HELPER_MAX_ARCHIVE_ENTRIES", "20000")

	cfg := Load()
	if !cfg.WorkspaceHelperCapabilityEnabled {
		t.Fatal("expected workspace helper capability exchange to be enabled")
	}
	if cfg.WorkspaceHelperKubeconfig != "/server/kubeconfig" {
		t.Fatalf("kubeconfig=%q", cfg.WorkspaceHelperKubeconfig)
	}
	if cfg.WorkspaceHelperServiceAccount != "workspace-helper" {
		t.Fatalf("service account=%q", cfg.WorkspaceHelperServiceAccount)
	}
	if cfg.WorkspaceHelperCapabilitySecret != "workspace-helper-signing-secret" {
		t.Fatalf("capability secret was not loaded")
	}
	if cfg.WorkspaceHelperMaxUncompressedSizeMB != 2048 || cfg.WorkspaceHelperMaxArchiveEntries != 20000 {
		t.Fatalf("workspace limits=%d MiB/%d entries", cfg.WorkspaceHelperMaxUncompressedSizeMB, cfg.WorkspaceHelperMaxArchiveEntries)
	}
}

func TestNormalizePublicURL(t *testing.T) {
	if got := normalizePublicURL(" https://ci.example.com/ "); got != "https://ci.example.com" {
		t.Fatalf("expected trimmed public url, got %q", got)
	}
	if got := normalizePublicURL(""); got != "" {
		t.Fatalf("expected empty public url, got %q", got)
	}
}

func TestConfig_DatabaseURL(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		expected string
	}{
		{
			name: "returns explicit database url when provided",
			cfg: Config{
				DatabaseURLValue: "postgres://example/overridden?sslmode=require",
				DBUser:           "ignored",
				DBPassword:       "ignored",
				DBHost:           "ignored",
				DBPort:           "5432",
				DBName:           "ignored",
				DBSSLMode:        "disable",
			},
			expected: "postgres://example/overridden?sslmode=require",
		},
		{
			name: "builds url from config fields",
			cfg: Config{
				DBUser:     "user",
				DBPassword: "pass",
				DBHost:     "localhost",
				DBPort:     "5432",
				DBName:     "db",
				DBSSLMode:  "disable",
			},
			expected: "postgres://user:pass@localhost:5432/db?sslmode=disable",
		},
		{
			name: "ignores whitespace-only explicit database url",
			cfg: Config{
				DatabaseURLValue: "   \t\n  ",
				DBUser:           "user",
				DBPassword:       "pass",
				DBHost:           "localhost",
				DBPort:           "5432",
				DBName:           "db",
				DBSSLMode:        "disable",
			},
			expected: "postgres://user:pass@localhost:5432/db?sslmode=disable",
		},
		{
			name: "keeps provided ssl mode",
			cfg: Config{
				DBUser:     "u",
				DBPassword: "p",
				DBHost:     "h",
				DBPort:     "1",
				DBName:     "n",
				DBSSLMode:  "require",
			},
			expected: "postgres://u:p@h:1/n?sslmode=require",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.cfg.DatabaseURL()
			if got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestConfig_UsesDatabaseURL(t *testing.T) {
	cfg := Config{
		DatabaseURLValue: "postgres://example/db?sslmode=require",
	}
	if !cfg.UsesDatabaseURL() {
		t.Fatalf("expected UsesDatabaseURL to return true")
	}

	cfg.DatabaseURLValue = "   "
	if cfg.UsesDatabaseURL() {
		t.Fatalf("expected UsesDatabaseURL to return false for whitespace-only value")
	}
}

func TestLoad_DatabaseURLPrecedenceOverSplitFields(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://external-user:external-pass@external-host:5432/external-db?sslmode=require")
	t.Setenv("DB_HOST", "local-host")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "local-user")
	t.Setenv("DB_PASSWORD", "local-pass")
	t.Setenv("DB_NAME", "local-db")
	t.Setenv("DB_SSLMODE", "disable")

	cfg := Load()
	got := cfg.DatabaseURL()
	expected := "postgres://external-user:external-pass@external-host:5432/external-db?sslmode=require"
	if got != expected {
		t.Fatalf("expected DATABASE_URL to take precedence, got %q", got)
	}
	if !cfg.UsesDatabaseURL() {
		t.Fatalf("expected UsesDatabaseURL to return true when DATABASE_URL is set")
	}
}
