package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppPort           string
	DatabaseURLValue  string
	DBHost            string
	DBPort            string
	DBUser            string
	DBPassword        string
	DBName            string
	DBSSLMode         string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	DBConnMaxIdleTime time.Duration
	StepLeaseSeconds  int
	WorkerStatusAddr  string

	ExecutionBackend        string
	ExecutionDefaultImage   string
	ExecutionWorkspaceRoot  string
	WorkerCacheStorageRoot  string
	MountDockerSocket       bool
	ArtifactStorageRoot     string
	ArtifactStorageProvider string
	ArtifactStorageStrict   bool
	ArtifactGCSBucket       string
	ArtifactGCSPrefix       string
	ArtifactGCSProject      string
	PushEventSecret         string
	GitHubWebhookSecret     string
}

func Load() Config {
	return Config{
		AppPort:           getEnv("APP_PORT", "8080"),
		DatabaseURLValue:  getEnv("DATABASE_URL", ""),
		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnv("DB_PORT", "5432"),
		DBUser:            getEnv("DB_USER", "coyote"),
		DBPassword:        getEnv("DB_PASSWORD", "coyote"),
		DBName:            getEnv("DB_NAME", "coyote_ci"),
		DBSSLMode:         getEnv("DB_SSLMODE", "disable"),
		DBMaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 10),
		DBMaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 5),
		DBConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		DBConnMaxIdleTime: getEnvDuration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute),
		StepLeaseSeconds:  getEnvInt("WORKER_STEP_LEASE_SECONDS", 45),
		WorkerStatusAddr:  getEnv("WORKER_STATUS_ADDR", ""),

		ExecutionBackend:        getEnv("WORKER_EXECUTION_BACKEND", "docker"),
		ExecutionDefaultImage:   getEnv("WORKER_EXECUTION_DEFAULT_IMAGE", "alpine:3.20"),
		ExecutionWorkspaceRoot:  getEnv("WORKER_EXECUTION_WORKSPACE_ROOT", filepath.Join(os.TempDir(), "coyote-builds")),
		WorkerCacheStorageRoot:  getEnv("WORKER_CACHE_STORAGE_ROOT", filepath.Join(os.TempDir(), "coyote-cache")),
		MountDockerSocket:       getEnvBool("WORKER_MOUNT_DOCKER_SOCKET", false),
		ArtifactStorageRoot:     getEnv("ARTIFACT_STORAGE_ROOT", filepath.Join(os.TempDir(), "coyote-artifacts")),
		ArtifactStorageProvider: getEnv("ARTIFACT_STORAGE_PROVIDER", "filesystem"),
		ArtifactStorageStrict:   getEnvBool("ARTIFACT_STORAGE_STRICT", false),
		ArtifactGCSBucket:       getEnv("ARTIFACT_GCS_BUCKET", ""),
		ArtifactGCSPrefix:       getEnv("ARTIFACT_GCS_PREFIX", ""),
		ArtifactGCSProject:      getEnv("ARTIFACT_GCS_PROJECT", ""),
		PushEventSecret:         getEnv("PUSH_EVENT_SECRET", ""),
		GitHubWebhookSecret:     getEnv("GITHUB_WEBHOOK_SECRET", getEnv("PUSH_EVENT_SECRET", "")),
	}
}

func (c Config) DatabaseURL() string {
	if databaseURL := strings.TrimSpace(c.DatabaseURLValue); databaseURL != "" {
		return databaseURL
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.DBUser,
		c.DBPassword,
		c.DBHost,
		c.DBPort,
		c.DBName,
		c.DBSSLMode,
	)
}

func (c Config) UsesDatabaseURL() bool {
	return strings.TrimSpace(c.DatabaseURLValue) != ""
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
