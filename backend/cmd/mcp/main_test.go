package main

import (
	"context"
	"database/sql"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	coyotemcp "github.com/radiation/coyote-ci/backend/internal/mcp"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
)

func TestValidateAuthMode(t *testing.T) {
	tests := []struct {
		name    string
		mode    auth.Mode
		config  coyotemcp.Config
		wantErr bool
	}{
		{name: "disabled mode requires opt in", mode: auth.ModeDisabled, config: testMCPConfig(), wantErr: true},
		{name: "disabled mode explicitly allowed on loopback", mode: auth.ModeDisabled, config: coyotemcp.Config{ListenAddr: "127.0.0.1:8081", AllowDisabledAuth: true}},
		{name: "disabled mode rejects external listener", mode: auth.ModeDisabled, config: coyotemcp.Config{ListenAddr: "0.0.0.0:8081", AllowDisabledAuth: true}, wantErr: true},
		{name: "header mode", mode: auth.ModeHeader, config: testMCPConfig()},
		{name: "oidc mode", mode: auth.ModeOIDC, config: testMCPConfig()},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateAuthMode(testCase.mode, testCase.config)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validate auth mode error=%v", err)
			}
		})
	}
}

func TestRunRejectsDisabledAuthBeforeOpeningDatabase(t *testing.T) {
	openCalled := false
	runErr := run(context.Background(), config.Config{AuthMode: string(auth.ModeDisabled)}, testMCPConfig(), func(string, platformdb.PoolConfig) (*sql.DB, error) {
		openCalled = true
		return nil, nil
	}, func(*nethttp.Server) error {
		return nil
	})
	if runErr == nil || !strings.Contains(runErr.Error(), "MCP_ALLOW_DISABLED_AUTH") {
		t.Fatalf("run error=%v", runErr)
	}
	if openCalled {
		t.Fatal("database opener should not run when auth is disabled")
	}
}

func TestRunReportsDatabaseConfigurationFailure(t *testing.T) {
	runErr := run(context.Background(), config.Config{
		AuthMode:        string(auth.ModeOIDC),
		DatabaseURLFile: "/does/not/exist",
	}, testMCPConfig(), func(string, platformdb.PoolConfig) (*sql.DB, error) {
		t.Fatal("database opener should not be called")
		return nil, nil
	}, func(*nethttp.Server) error {
		return nil
	})
	if runErr == nil || !strings.Contains(runErr.Error(), "failed to resolve database configuration") {
		t.Fatalf("run error=%v", runErr)
	}
}

func TestRunWiresHealthServerAndShutsDown(t *testing.T) {
	db, mock, mockErr := sqlmock.New()
	if mockErr != nil {
		t.Fatalf("new mock database: %v", mockErr)
	}
	mock.ExpectClose()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	appConfig := config.Config{
		AuthMode:          string(auth.ModeOIDC),
		DatabaseURLValue:  "postgres://coyote:secret@db/coyote_ci?sslmode=disable",
		DBMaxOpenConns:    2,
		DBMaxIdleConns:    1,
		DBConnMaxLifetime: time.Minute,
		DBConnMaxIdleTime: time.Minute,
	}
	runErr := run(ctx, appConfig, testMCPConfig(), func(databaseURL string, pool platformdb.PoolConfig) (*sql.DB, error) {
		if databaseURL != appConfig.DatabaseURLValue {
			t.Fatalf("database URL=%q", databaseURL)
		}
		if pool.MaxOpenConns != 2 || pool.MaxIdleConns != 1 {
			t.Fatalf("database pool=%+v", pool)
		}
		return db, nil
	}, func(server *nethttp.Server) error {
		request := httptest.NewRequest(nethttp.MethodGet, coyotemcp.HealthPath, nil)
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, request)
		if response.Code != nethttp.StatusOK || response.Body.String() != "ok" {
			t.Fatalf("health status=%d body=%q", response.Code, response.Body.String())
		}
		return nethttp.ErrServerClosed
	})
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("database expectations: %v", expectationsErr)
	}
}

func TestRunReturnsDatabaseOpenFailure(t *testing.T) {
	wantErr := errors.New("database unavailable")
	runErr := run(context.Background(), config.Config{
		AuthMode:         string(auth.ModeOIDC),
		DatabaseURLValue: "postgres://coyote:secret@db/coyote_ci?sslmode=disable",
	}, testMCPConfig(), func(string, platformdb.PoolConfig) (*sql.DB, error) {
		return nil, wantErr
	}, func(*nethttp.Server) error {
		return nil
	})
	if !errors.Is(runErr, wantErr) {
		t.Fatalf("run error=%v", runErr)
	}
}

func testMCPConfig() coyotemcp.Config {
	return coyotemcp.Config{
		ListenAddr:          "127.0.0.1:8081",
		RequestTimeout:      time.Second,
		ShutdownTimeout:     time.Second,
		MaxRequestBodyBytes: 1024,
	}
}
