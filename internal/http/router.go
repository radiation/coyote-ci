package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/radiation/coyote-ci/internal/http/handler"
)

func NewRouter(buildHandler *handler.BuildHandler) nethttp.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/builds", func(r chi.Router) {
		r.Post("/", buildHandler.CreateBuild)
		r.Get("/{buildID}", buildHandler.GetBuild)
	})

	return r
}
