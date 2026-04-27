package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadinessHandlerReady(t *testing.T) {
	handler := NewReadinessHandler(ReadinessCheckFunc(func(context.Context) error {
		return nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	handler.Ready(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if rr.Body.String() != "ready" {
		t.Fatalf("expected body %q, got %q", "ready", rr.Body.String())
	}
}

func TestReadinessHandlerNotReady(t *testing.T) {
	handler := NewReadinessHandler(ReadinessCheckFunc(func(context.Context) error {
		return errors.New("database schema not ready")
	}))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	handler.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
	if rr.Body.String() != "not ready" {
		t.Fatalf("expected body %q, got %q", "not ready", rr.Body.String())
	}
}
