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

// maxRequestBodySize is the default limit applied to POST/PUT/PATCH request
// bodies. Requests exceeding this size receive 413 Request Entity Too Large.
const maxRequestBodySize = 1 << 20 // 1 MiB

func NewRouter(buildHandler *handler.BuildHandler, jobHandler *handler.JobHandler, settingsHandler *handler.ManagedImageSettingsHandler, eventHandler *handler.EventHandler, pushEventSecret string) nethttp.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(limitRequestBody(maxRequestBodySize))

	// Keep bare health endpoints for simple infra probes.
	r.Get("/health", handler.Health)
	r.Get("/healthz", handler.Health)

	r.Route("/api", func(r chi.Router) {
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
			r.Post("/{buildID}/cancel", buildHandler.CancelBuild)
		})

		r.Route("/jobs", func(r chi.Router) {
			r.Post("/", jobHandler.CreateJob)
			r.Get("/", jobHandler.ListJobs)
			r.Get("/{jobID}", jobHandler.GetJob)
			r.Put("/{jobID}", jobHandler.UpdateJob)
			r.Get("/{jobID}/builds", jobHandler.ListJobBuilds)
			r.Post("/{jobID}/run", jobHandler.RunNow)
		})

		if settingsHandler != nil {
			r.Route("/source-credentials", func(r chi.Router) {
				r.Post("/", settingsHandler.CreateSourceCredential)
				r.Get("/", settingsHandler.ListSourceCredentials)
				r.Get("/{credentialID}", settingsHandler.GetSourceCredential)
				r.Put("/{credentialID}", settingsHandler.UpdateSourceCredential)
				r.Delete("/{credentialID}", settingsHandler.DeleteSourceCredential)
			})

			r.Route("/repo-writeback-configs", func(r chi.Router) {
				r.Post("/", settingsHandler.CreateRepoWritebackConfig)
				r.Get("/", settingsHandler.ListRepoWritebackConfigs)
				r.Get("/{configID}", settingsHandler.GetRepoWritebackConfig)
				r.Put("/{configID}", settingsHandler.UpdateRepoWritebackConfig)
				r.Delete("/{configID}", settingsHandler.DeleteRepoWritebackConfig)
			})
		}

		r.Route("/events", func(r chi.Router) {
			if pushEventSecret != "" {
				r.Use(requireSecret(pushEventSecret))
			}
			r.Post("/push", eventHandler.IngestPushEvent)
		})

		r.Route("/webhooks", func(r chi.Router) {
			r.Post("/github", eventHandler.IngestGitHubWebhook)
		})
	})

	return r
}

// limitRequestBody returns a middleware that caps the request body size for
// mutating HTTP methods (POST, PUT, PATCH). GET, HEAD, DELETE, and OPTIONS
// requests are passed through unchanged.
func limitRequestBody(maxBytes int64) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			switch r.Method {
			case nethttp.MethodPost, nethttp.MethodPut, nethttp.MethodPatch:
				r.Body = nethttp.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
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
