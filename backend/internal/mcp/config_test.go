package mcp

import (
	"errors"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("MCP_LISTEN_ADDR", "")
	t.Setenv("MCP_ALLOWED_ORIGINS", "")
	t.Setenv("MCP_ALLOW_DISABLED_AUTH", "")
	t.Setenv("MCP_REQUEST_TIMEOUT", "")
	t.Setenv("MCP_SHUTDOWN_TIMEOUT", "")
	t.Setenv("MCP_MAX_REQUEST_BODY_BYTES", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:8081" {
		t.Fatalf("listen address=%q", cfg.ListenAddr)
	}
	if cfg.AllowDisabledAuth {
		t.Fatal("disabled auth should not be allowed by default")
	}
	if cfg.RequestTimeout != 15*time.Second {
		t.Fatalf("request timeout=%s", cfg.RequestTimeout)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("shutdown timeout=%s", cfg.ShutdownTimeout)
	}
	if cfg.MaxRequestBodyBytes != 1<<20 {
		t.Fatalf("max request body bytes=%d", cfg.MaxRequestBodyBytes)
	}
}

func TestLoadConfigUsesExplicitValues(t *testing.T) {
	t.Setenv("MCP_LISTEN_ADDR", "0.0.0.0:9080")
	t.Setenv("MCP_ALLOWED_ORIGINS", "https://one.example, https://two.example,https://one.example")
	t.Setenv("MCP_ALLOW_DISABLED_AUTH", "true")
	t.Setenv("MCP_REQUEST_TIMEOUT", "20s")
	t.Setenv("MCP_SHUTDOWN_TIMEOUT", "11s")
	t.Setenv("MCP_MAX_REQUEST_BODY_BYTES", "2048")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.ListenAddr != "0.0.0.0:9080" {
		t.Fatalf("listen address=%q", cfg.ListenAddr)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://one.example" || cfg.AllowedOrigins[1] != "https://two.example" {
		t.Fatalf("allowed origins=%v", cfg.AllowedOrigins)
	}
	if !cfg.AllowDisabledAuth {
		t.Fatal("expected disabled auth opt-in")
	}
	if cfg.RequestTimeout != 20*time.Second || cfg.ShutdownTimeout != 11*time.Second || cfg.MaxRequestBodyBytes != 2048 {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestLoadConfigRejectsInvalidLimits(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "request timeout", key: "MCP_REQUEST_TIMEOUT", value: "0s"},
		{name: "shutdown timeout", key: "MCP_SHUTDOWN_TIMEOUT", value: "-1s"},
		{name: "request size", key: "MCP_MAX_REQUEST_BODY_BYTES", value: "0"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(testCase.key, testCase.value)
			_, err := LoadConfig()
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("load config error=%v", err)
			}
		})
	}
}
