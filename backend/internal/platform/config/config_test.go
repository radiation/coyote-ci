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
				"CACHE_MAX_SIZE_MB":               "",
				"ARTIFACT_STORAGE_ROOT":           "",
			},
			expected: Config{
				AppPort:                 "8080",
				DatabaseURLValue:        "",
				DBHost:                  "localhost",
				DBPort:                  "5432",
				DBUser:                  "coyote",
				DBPassword:              "coyote",
				DBName:                  "coyote_ci",
				DBSSLMode:               "disable",
				DBMaxOpenConns:          10,
				DBMaxIdleConns:          5,
				DBConnMaxLifetime:       30 * time.Minute,
				DBConnMaxIdleTime:       5 * time.Minute,
				StepLeaseSeconds:        45,
				WorkerStatusAddr:        "",
				ExecutionBackend:        "docker",
				ExecutionDefaultImage:   "alpine:3.20",
				ExecutionWorkspaceRoot:  defaultWorkspaceRoot,
				WorkerCacheStorageRoot:  defaultCacheRoot,
				WorkerCacheMaxSizeMB:    10240,
				ArtifactStorageRoot:     defaultArtifactRoot,
				ArtifactStorageProvider: "filesystem",
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
				"CACHE_MAX_SIZE_MB":               "2048",
				"ARTIFACT_STORAGE_ROOT":           "/var/tmp/coyote-artifacts",
			},
			expected: Config{
				AppPort:                 "9999",
				DatabaseURLValue:        "postgres://external/external?sslmode=require",
				DBHost:                  "db.internal",
				DBPort:                  "5433",
				DBUser:                  "user1",
				DBPassword:              "pass1",
				DBName:                  "name1",
				DBSSLMode:               "require",
				DBMaxOpenConns:          25,
				DBMaxIdleConns:          12,
				DBConnMaxLifetime:       45 * time.Minute,
				DBConnMaxIdleTime:       10 * time.Minute,
				StepLeaseSeconds:        60,
				WorkerStatusAddr:        "127.0.0.1:9091",
				ExecutionBackend:        "inprocess",
				ExecutionDefaultImage:   "golang:1.23-alpine",
				ExecutionWorkspaceRoot:  "/var/tmp/coyote-workspaces",
				WorkerCacheStorageRoot:  defaultCacheRoot,
				WorkerCacheMaxSizeMB:    2048,
				ArtifactStorageRoot:     "/var/tmp/coyote-artifacts",
				ArtifactStorageProvider: "filesystem",
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
				"CACHE_MAX_SIZE_MB":               "",
				"ARTIFACT_STORAGE_ROOT":           "",
			},
			expected: Config{
				AppPort:                 "8080",
				DatabaseURLValue:        "",
				DBHost:                  "localhost",
				DBPort:                  "5432",
				DBUser:                  "coyote",
				DBPassword:              "coyote",
				DBName:                  "coyote_ci",
				DBSSLMode:               "disable",
				DBMaxOpenConns:          10,
				DBMaxIdleConns:          5,
				DBConnMaxLifetime:       30 * time.Minute,
				DBConnMaxIdleTime:       5 * time.Minute,
				StepLeaseSeconds:        45,
				WorkerStatusAddr:        "",
				ExecutionBackend:        "docker",
				ExecutionDefaultImage:   "alpine:3.20",
				ExecutionWorkspaceRoot:  defaultWorkspaceRoot,
				WorkerCacheStorageRoot:  defaultCacheRoot,
				WorkerCacheMaxSizeMB:    10240,
				ArtifactStorageRoot:     defaultArtifactRoot,
				ArtifactStorageProvider: "filesystem",
			},
		},
		{
			name: "invalid duration falls back to default",
			env: map[string]string{
				"DB_CONN_MAX_LIFETIME":  "invalid",
				"DB_CONN_MAX_IDLE_TIME": "still-invalid",
			},
			expected: Config{
				AppPort:                 "8080",
				DatabaseURLValue:        "",
				DBHost:                  "localhost",
				DBPort:                  "5432",
				DBUser:                  "coyote",
				DBPassword:              "coyote",
				DBName:                  "coyote_ci",
				DBSSLMode:               "disable",
				DBMaxOpenConns:          10,
				DBMaxIdleConns:          5,
				DBConnMaxLifetime:       30 * time.Minute,
				DBConnMaxIdleTime:       5 * time.Minute,
				StepLeaseSeconds:        45,
				WorkerStatusAddr:        "",
				ExecutionBackend:        "docker",
				ExecutionDefaultImage:   "alpine:3.20",
				ExecutionWorkspaceRoot:  defaultWorkspaceRoot,
				WorkerCacheStorageRoot:  defaultCacheRoot,
				WorkerCacheMaxSizeMB:    10240,
				ArtifactStorageRoot:     defaultArtifactRoot,
				ArtifactStorageProvider: "filesystem",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			got := Load()
			if got != tc.expected {
				t.Fatalf("expected %+v, got %+v", tc.expected, got)
			}
		})
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
