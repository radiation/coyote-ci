package handler

import (
	"context"
	nethttp "net/http"
)

// Health godoc
// @Summary Health check
// @Description Returns a simple liveness response.
// @Tags health
// @Produce plain
// @Success 200 {string} string "ok"
// @Router /health [get]
func Health(w nethttp.ResponseWriter, _ *nethttp.Request) {
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

type ReadinessCheckFunc func(context.Context) error

func (f ReadinessCheckFunc) Check(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}

type ReadinessChecker interface {
	Check(context.Context) error
}

type ReadinessHandler struct {
	checker ReadinessChecker
}

func NewReadinessHandler(checker ReadinessChecker) *ReadinessHandler {
	return &ReadinessHandler{checker: checker}
}

// Ready godoc
// @Summary Readiness check
// @Description Returns a readiness response after database connectivity and schema checks succeed.
// @Tags health
// @Produce plain
// @Success 200 {string} string "ready"
// @Failure 503 {string} string "not ready"
// @Router /readyz [get]
func (h *ReadinessHandler) Ready(w nethttp.ResponseWriter, r *nethttp.Request) {
	if h != nil && h.checker != nil {
		if err := h.checker.Check(r.Context()); err != nil {
			w.WriteHeader(nethttp.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready"))
			return
		}
	}
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
