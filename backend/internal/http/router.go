package http

import (
	"crypto/subtle"
	"encoding/json"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/http/handler"
)

func NewRouter(buildHandler *handler.BuildHandler, jobHandler *handler.JobHandler, eventHandler *handler.EventHandler, pushEventSecret string) nethttp.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", handler.Health)
	r.Get("/healthz", handler.Health)

	r.Route("/builds", func(r chi.Router) {
		r.Post("/", buildHandler.CreateBuild)
		r.Post("/pipeline", buildHandler.CreatePipelineBuild)
		r.Post("/repo", buildHandler.CreateRepoBuild)
		r.Post("/jobs/{jobID}/retry", buildHandler.RetryJob)
		r.Get("/", buildHandler.ListBuilds)
		r.Get("/{buildID}", buildHandler.GetBuild)
		r.Post("/{buildID}/rerun", buildHandler.RerunBuildFromStep)
		r.Get("/{buildID}/steps", buildHandler.GetBuildSteps)
		r.Get("/{buildID}/steps/{stepIndex}/logs", buildHandler.GetBuildStepLogs)
		r.Get("/{buildID}/steps/{stepIndex}/logs/stream", buildHandler.StreamBuildStepLogs)
		r.Get("/{buildID}/logs", buildHandler.GetBuildLogs)
		r.Get("/{buildID}/artifacts", buildHandler.GetBuildArtifacts)
		r.Get("/{buildID}/artifacts/{artifactID}/download", buildHandler.DownloadBuildArtifact)
		r.Post("/{buildID}/queue", buildHandler.QueueBuild)
		r.Post("/{buildID}/start", buildHandler.StartBuild)
		r.Post("/{buildID}/complete", buildHandler.CompleteBuild)
		r.Post("/{buildID}/fail", buildHandler.FailBuild)
	})

	r.Route("/jobs", func(r chi.Router) {
		r.Post("/", jobHandler.CreateJob)
		r.Get("/", jobHandler.ListJobs)
		r.Get("/{jobID}", jobHandler.GetJob)
		r.Put("/{jobID}", jobHandler.UpdateJob)
		r.Post("/{jobID}/run", jobHandler.RunNow)
	})

	r.Route("/events", func(r chi.Router) {
		if pushEventSecret != "" {
			r.Use(requireSecret(pushEventSecret))
		}
		r.Post("/push", eventHandler.IngestPushEvent)
	})

	return r
}

// requireSecret returns a middleware that validates the X-Coyote-Secret header
// against the configured secret. Requests with a missing or incorrect secret
// are rejected with 401 Unauthorized.
func requireSecret(secret string) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Coyote-Secret")), []byte(secret)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(nethttp.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(api.ErrorResponse{
					Error: api.ErrorBody{Code: "unauthorized", Message: "invalid or missing secret"},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
