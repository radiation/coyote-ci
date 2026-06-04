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

type routerConfig struct {
	authMiddleware           func(nethttp.Handler) nethttp.Handler
	authHandler              *handler.AuthHandler
	notificationHandler      *handler.NotificationHandler
	userHandler              *handler.UserHandler
	apiTokenHandler          *handler.APITokenHandler
	projectMembershipHandler *handler.ProjectMembershipHandler
	workerHandler            *handler.WorkerHandler
}

type RouterOption func(*routerConfig)

func WithAuthMiddleware(middleware func(nethttp.Handler) nethttp.Handler) RouterOption {
	return func(cfg *routerConfig) {
		cfg.authMiddleware = middleware
	}
}

func WithUserHandler(userHandler *handler.UserHandler) RouterOption {
	return func(cfg *routerConfig) {
		cfg.userHandler = userHandler
	}
}

func WithNotificationHandler(notificationHandler *handler.NotificationHandler) RouterOption {
	return func(cfg *routerConfig) {
		cfg.notificationHandler = notificationHandler
	}
}

func WithAPITokenHandler(apiTokenHandler *handler.APITokenHandler) RouterOption {
	return func(cfg *routerConfig) {
		cfg.apiTokenHandler = apiTokenHandler
	}
}

func WithAuthHandler(authHandler *handler.AuthHandler) RouterOption {
	return func(cfg *routerConfig) {
		cfg.authHandler = authHandler
	}
}

func WithProjectMembershipHandler(projectMembershipHandler *handler.ProjectMembershipHandler) RouterOption {
	return func(cfg *routerConfig) {
		cfg.projectMembershipHandler = projectMembershipHandler
	}
}

func WithWorkerHandler(workerHandler *handler.WorkerHandler) RouterOption {
	return func(cfg *routerConfig) {
		cfg.workerHandler = workerHandler
	}
}

func NewRouter(buildHandler *handler.BuildHandler, artifactHandler *handler.ArtifactHandler, jobHandler *handler.JobHandler, projectHandler *handler.ProjectHandler, versionTagHandler *handler.VersionTagHandler, credentialHandler *handler.SourceCredentialHandler, eventHandler *handler.EventHandler, pushEventSecret string, options ...RouterOption) nethttp.Handler {
	cfg := routerConfig{}
	for _, option := range options {
		option(&cfg)
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(limitRequestBody(maxRequestBodySize))

	// Keep bare health endpoints for simple infra probes.
	r.Get("/health", handler.Health)
	r.Get("/healthz", handler.Health)
	if cfg.authHandler != nil {
		r.Get("/auth/login", cfg.authHandler.Login)
		r.Get("/auth/callback", cfg.authHandler.Callback)
		r.Post("/auth/logout", cfg.authHandler.Logout)
	}

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", handler.Health)
		r.Get("/healthz", handler.Health)
		if cfg.userHandler != nil {
			r.Get("/auth/config", cfg.userHandler.GetAuthConfig)
		}
		if cfg.notificationHandler != nil {
			r.Post("/dev/notifications/sample-build", cfg.notificationHandler.SendSampleBuildFailure)
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

		r.Group(func(r chi.Router) {
			if cfg.authMiddleware != nil {
				r.Use(cfg.authMiddleware)
			}

			r.Get("/queue", buildHandler.ListQueue)
			if cfg.workerHandler != nil {
				r.Get("/workers", cfg.workerHandler.ListWorkers)
			}

			if cfg.userHandler != nil {
				r.Get("/me", cfg.userHandler.GetMe)
				if cfg.apiTokenHandler != nil {
					r.Get("/me/tokens", cfg.apiTokenHandler.ListMyTokens)
					r.Post("/me/tokens", cfg.apiTokenHandler.CreateMyToken)
					r.Delete("/me/tokens/{token_id}", cfg.apiTokenHandler.RevokeMyToken)
				}
				r.Route("/users", func(r chi.Router) {
					r.Get("/", cfg.userHandler.ListUsers)
					r.Post("/", cfg.userHandler.CreateUser)
					r.Get("/{id}", cfg.userHandler.GetUser)
					r.Patch("/{id}", cfg.userHandler.UpdateUser)
					r.Delete("/{id}", cfg.userHandler.DeleteUser)
				})
			}

			r.Route("/builds", func(r chi.Router) {
				r.Post("/", buildHandler.CreateBuild)
				r.Post("/pipeline", buildHandler.CreatePipelineBuild)
				r.Post("/repo", buildHandler.CreateRepoBuild)
				r.Post("/jobs/{jobID}/retry", buildHandler.RetryJob)
				r.Get("/", buildHandler.ListBuilds)
				r.Get("/{buildID}", buildHandler.GetBuild)
				r.Post("/{buildID}/rerun", buildHandler.RerunBuild)
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
				if versionTagHandler != nil {
					r.Post("/{jobID}/version-tags", versionTagHandler.CreateJobVersionTags)
					r.Get("/{jobID}/version-tags", versionTagHandler.ListJobVersionTags)
				}
			})

			if projectHandler != nil {
				r.Route("/projects", func(r chi.Router) {
					r.Get("/", projectHandler.ListProjects)
					r.Post("/", projectHandler.CreateProject)
					r.Get("/{id}", projectHandler.GetProject)
					r.Patch("/{id}", projectHandler.UpdateProject)
					r.Delete("/{id}", projectHandler.DeleteProject)
					r.Get("/{id}/jobs", projectHandler.ListProjectJobs)
					if cfg.projectMembershipHandler != nil {
						r.Get("/{id}/members", cfg.projectMembershipHandler.ListProjectMembers)
						r.Put("/{id}/members/{user_id}", cfg.projectMembershipHandler.UpsertProjectMember)
						r.Patch("/{id}/members/{user_id}", cfg.projectMembershipHandler.UpdateProjectMember)
						r.Delete("/{id}/members/{user_id}", cfg.projectMembershipHandler.DeleteProjectMember)
					}
				})
			}

			if artifactHandler != nil {
				r.Get("/artifacts/catalog", artifactHandler.ListArtifactCatalog)
				r.Get("/artifacts", artifactHandler.ListArtifacts)
			}

			if versionTagHandler != nil {
				r.Get("/artifacts/{artifactID}/version-tags", versionTagHandler.ListArtifactVersionTags)
				r.Get("/managed-image-versions/{managedImageVersionID}/version-tags", versionTagHandler.ListManagedImageVersionTags)
			}

			if artifactHandler != nil {
				r.Get("/artifacts/{artifactID}", artifactHandler.GetArtifact)
			}

			if credentialHandler != nil {
				r.Route("/source-credentials", func(r chi.Router) {
					r.Post("/", credentialHandler.CreateSourceCredential)
					r.Get("/", credentialHandler.ListSourceCredentials)
					r.Get("/{credentialID}", credentialHandler.GetSourceCredential)
					r.Put("/{credentialID}", credentialHandler.UpdateSourceCredential)
					r.Delete("/{credentialID}", credentialHandler.DeleteSourceCredential)
				})
			}
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
