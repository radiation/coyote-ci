package main

import (
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/platform/config"
)

func TestDatabaseConfigMode_UsesDatabaseURL(t *testing.T) {
	cfg := config.Config{DatabaseURLValue: "postgres://example/db?sslmode=require"}

	got := databaseConfigMode(cfg)
	if got != "using DATABASE_URL" {
		t.Fatalf("expected using DATABASE_URL, got %q", got)
	}
}

func TestDatabaseConfigMode_UsesDiscreteSettings(t *testing.T) {
	cfg := config.Config{}

	got := databaseConfigMode(cfg)
	if got != "using discrete DB_* settings" {
		t.Fatalf("expected using discrete DB_* settings, got %q", got)
	}
}

func TestDatabaseOpenConfig_UsesDatabaseURLAndPoolSettings(t *testing.T) {
	cfg := config.Config{
		DatabaseURLValue:  "postgres://external-user:external-pass@external-host:5432/external-db?sslmode=require",
		DBHost:            "ignored-host",
		DBPort:            "5432",
		DBUser:            "ignored-user",
		DBPassword:        "ignored-pass",
		DBName:            "ignored-db",
		DBSSLMode:         "disable",
		DBMaxOpenConns:    31,
		DBMaxIdleConns:    9,
		DBConnMaxLifetime: 44 * time.Minute,
		DBConnMaxIdleTime: 11 * time.Minute,
	}

	gotURL, gotPool := databaseOpenConfig(cfg)
	if gotURL != cfg.DatabaseURLValue {
		t.Fatalf("expected DATABASE_URL precedence, got %q", gotURL)
	}
	if gotPool.MaxOpenConns != 31 {
		t.Fatalf("expected MaxOpenConns=31, got %d", gotPool.MaxOpenConns)
	}
	if gotPool.MaxIdleConns != 9 {
		t.Fatalf("expected MaxIdleConns=9, got %d", gotPool.MaxIdleConns)
	}
	if gotPool.ConnMaxLifetime != 44*time.Minute {
		t.Fatalf("expected ConnMaxLifetime=44m, got %s", gotPool.ConnMaxLifetime)
	}
	if gotPool.ConnMaxIdleTime != 11*time.Minute {
		t.Fatalf("expected ConnMaxIdleTime=11m, got %s", gotPool.ConnMaxIdleTime)
	}
}
