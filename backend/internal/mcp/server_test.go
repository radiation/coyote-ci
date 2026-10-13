package mcp

import (
	"context"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewHandlerHealthAndReadiness(t *testing.T) {
	handler, err := NewHandler(testConfig(), ReadinessCheckFunc(func(context.Context) error {
		return nil
	}), testImplementation())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	for _, testCase := range []struct {
		path string
		want int
		body string
	}{
		{path: HealthPath, want: http.StatusOK, body: "ok"},
		{path: ReadyPath, want: http.StatusOK, body: "ready"},
	} {
		request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != testCase.want || response.Body.String() != testCase.body {
			t.Fatalf("%s status=%d body=%q", testCase.path, response.Code, response.Body.String())
		}
	}
}

func TestNewHandlerReportsUnavailableReadiness(t *testing.T) {
	handler, err := NewHandler(testConfig(), ReadinessCheckFunc(func(context.Context) error {
		return errors.New("database unavailable")
	}), testImplementation())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, ReadyPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "not ready" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestMCPHandlerFailsClosedUntilAuthenticationIsConfigured(t *testing.T) {
	handler, err := NewHandler(testConfig(), nil, testImplementation())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestMCPHandlerRejectsUnsupportedMethods(t *testing.T) {
	handler, err := NewHandler(testConfig(), nil, testImplementation())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, Path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("allow=%q", response.Header().Get("Allow"))
	}
}

func TestMCPHandlerRejectsCrossOriginRequests(t *testing.T) {
	handler, err := NewHandler(testConfig(), nil, testImplementation())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "http://localhost"+Path, nil)
	request.Header.Set("Origin", "https://untrusted.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestMCPHandlerAllowsConfiguredOriginBeforeAuthentication(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedOrigins = []string{"https://client.example"}
	handler, err := NewHandler(cfg, nil, testImplementation())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "http://localhost"+Path, nil)
	request.Header.Set("Origin", "https://client.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestNewHandlerRejectsInvalidConfiguredOrigin(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedOrigins = []string{"not an origin"}
	_, err := NewHandler(cfg, nil, testImplementation())
	if err == nil {
		t.Fatal("expected invalid origin error")
	}
}

func TestMCPHandlerRejectsOversizedRequests(t *testing.T) {
	cfg := testConfig()
	cfg.MaxRequestBodyBytes = 2
	handler, err := NewHandler(cfg, nil, testImplementation())
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader("too large"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestNewProtocolHandlerUsesStatelessTransport(t *testing.T) {
	handler, err := newProtocolHandler(testConfig(), testImplementation())
	if err != nil {
		t.Fatalf("new protocol handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, Path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestNewProtocolHandlerRequiresImplementation(t *testing.T) {
	_, err := newProtocolHandler(testConfig(), nil)
	if err == nil {
		t.Fatal("expected missing implementation error")
	}
}

func TestRequestTimeoutCancelsSlowHandler(t *testing.T) {
	started := make(chan struct{})
	handler := requestTimeout(10*time.Millisecond, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	request := httptest.NewRequest(http.MethodPost, Path, nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	<-started
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "request timed out" {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestNewDatabaseReadiness(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("new mock database: %v", err)
	}
	defer func() {
		mock.ExpectClose()
		_ = db.Close()
	}()

	mock.ExpectPing()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"ready"}).AddRow(true))
	if readinessErr := NewDatabaseReadiness(db).Check(context.Background()); readinessErr != nil {
		t.Fatalf("readiness check: %v", readinessErr)
	}
	if expectationsErr := mock.ExpectationsWereMet(); expectationsErr != nil {
		t.Fatalf("database expectations: %v", expectationsErr)
	}
}

func TestNewDatabaseReadinessRejectsUnavailableDatabase(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("new mock database: %v", err)
	}
	defer func() {
		mock.ExpectClose()
		_ = db.Close()
	}()

	mock.ExpectPing().WillReturnError(driver.ErrBadConn)
	if readinessErr := NewDatabaseReadiness(db).Check(context.Background()); readinessErr == nil {
		t.Fatal("expected readiness failure")
	}
}

func TestNewDatabaseReadinessRejectsMissingSchema(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("new mock database: %v", err)
	}
	defer func() {
		mock.ExpectClose()
		_ = db.Close()
	}()

	mock.ExpectPing()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"ready"}).AddRow(false))
	if readinessErr := NewDatabaseReadiness(db).Check(context.Background()); readinessErr == nil {
		t.Fatal("expected schema readiness failure")
	}
}

func testConfig() Config {
	return Config{
		ListenAddr:          "127.0.0.1:8081",
		RequestTimeout:      time.Second,
		ShutdownTimeout:     time.Second,
		MaxRequestBodyBytes: 1024,
	}
}

func testImplementation() *sdkmcp.Implementation {
	return &sdkmcp.Implementation{Name: "coyote-mcp", Version: "test"}
}
