package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/radiation/coyote-ci/backend/internal/http/handler"
)

func NewRouter(buildHandler *handler.BuildHandler) nethttp.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	healthHandler := func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}

	r.Get("/health", healthHandler)
	r.Get("/healthz", healthHandler)

	r.Route("/builds", func(r chi.Router) {
		r.Post("/", buildHandler.CreateBuild)
		r.Get("/", buildHandler.ListBuilds)
		r.Get("/{buildID}", buildHandler.GetBuild)
		r.Get("/{buildID}/steps", buildHandler.GetBuildSteps)
		r.Get("/{buildID}/logs", buildHandler.GetBuildLogs)
		r.Post("/{buildID}/queue", buildHandler.QueueBuild)
		r.Post("/{buildID}/start", buildHandler.StartBuild)
		r.Post("/{buildID}/complete", buildHandler.CompleteBuild)
		r.Post("/{buildID}/fail", buildHandler.FailBuild)
	})

	return r
}
