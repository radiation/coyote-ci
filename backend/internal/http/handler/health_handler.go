package handler

import nethttp "net/http"

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
