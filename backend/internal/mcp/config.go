package mcp

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr         = "127.0.0.1:8081"
	defaultRequestTimeout     = 15 * time.Second
	defaultShutdownTimeout    = 10 * time.Second
	defaultMaxRequestBodySize = 1 << 20
)

var ErrInvalidConfig = errors.New("invalid MCP configuration")

type Config struct {
	ListenAddr          string
	AllowedOrigins      []string
	AllowDisabledAuth   bool
	RequestTimeout      time.Duration
	ShutdownTimeout     time.Duration
	MaxRequestBodyBytes int64
}

func LoadConfig() (Config, error) {
	requestTimeout, err := envPositiveDuration("MCP_REQUEST_TIMEOUT", defaultRequestTimeout)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := envPositiveDuration("MCP_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}
	maxRequestBodyBytes, err := envPositiveInt64("MCP_MAX_REQUEST_BODY_BYTES", defaultMaxRequestBodySize)
	if err != nil {
		return Config{}, err
	}

	return Config{
		ListenAddr:          envString("MCP_LISTEN_ADDR", defaultListenAddr),
		AllowedOrigins:      splitCommaSeparated(os.Getenv("MCP_ALLOWED_ORIGINS")),
		AllowDisabledAuth:   envBool("MCP_ALLOW_DISABLED_AUTH", false),
		RequestTimeout:      requestTimeout,
		ShutdownTimeout:     shutdownTimeout,
		MaxRequestBodyBytes: maxRequestBodyBytes,
	}, nil
}

func envString(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envPositiveDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive duration", ErrInvalidConfig, key)
	}
	return parsed, nil
}

func envPositiveInt64(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%w: %s must be a positive integer", ErrInvalidConfig, key)
	}
	return parsed, nil
}

func splitCommaSeparated(value string) []string {
	seen := make(map[string]struct{})
	origins := make([]string, 0)
	for _, part := range strings.Split(value, ",") {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins
}
