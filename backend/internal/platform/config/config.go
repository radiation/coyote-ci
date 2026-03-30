package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	AppPort          string
	DBHost           string
	DBPort           string
	DBUser           string
	DBPassword       string
	DBName           string
	DBSSLMode        string
	StepLeaseSeconds int
	WorkerStatusAddr string

	ExecutionBackend       string
	ExecutionDefaultImage  string
	ExecutionWorkspaceRoot string
	ArtifactStorageRoot    string
}

func Load() Config {
	return Config{
		AppPort:          getEnv("APP_PORT", "8080"),
		DBHost:           getEnv("DB_HOST", "localhost"),
		DBPort:           getEnv("DB_PORT", "5432"),
		DBUser:           getEnv("DB_USER", "coyote"),
		DBPassword:       getEnv("DB_PASSWORD", "coyote"),
		DBName:           getEnv("DB_NAME", "coyote_ci"),
		DBSSLMode:        getEnv("DB_SSLMODE", "disable"),
		StepLeaseSeconds: getEnvInt("WORKER_STEP_LEASE_SECONDS", 45),
		WorkerStatusAddr: getEnv("WORKER_STATUS_ADDR", ""),

		ExecutionBackend:       getEnv("WORKER_EXECUTION_BACKEND", "docker"),
		ExecutionDefaultImage:  getEnv("WORKER_EXECUTION_DEFAULT_IMAGE", "alpine:3.20"),
		ExecutionWorkspaceRoot: getEnv("WORKER_EXECUTION_WORKSPACE_ROOT", filepath.Join(os.TempDir(), "coyote-builds")),
		ArtifactStorageRoot:    getEnv("ARTIFACT_STORAGE_ROOT", filepath.Join(os.TempDir(), "coyote-artifacts")),
	}
}

func (c Config) DatabaseURL() string {
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
