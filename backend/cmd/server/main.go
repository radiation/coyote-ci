package main

import (
	"context"
	"errors"
	"expvar"
	"log"
	nethttp "net/http"
	"strings"
	"time"

	docs "github.com/radiation/coyote-ci/backend/docs"
	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	apphttp "github.com/radiation/coyote-ci/backend/internal/http"
	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	"github.com/radiation/coyote-ci/backend/internal/platform/dbopen"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/service"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	managedimagesvc "github.com/radiation/coyote-ci/backend/internal/service/managedimage"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
	"github.com/radiation/coyote-ci/backend/internal/source"
	httpSwagger "github.com/swaggo/http-swagger"
)

// @title Coyote CI API
// @version 0.1
// @description HTTP API for Coyote CI control-plane workflows.
// @BasePath /api
// @schemes http

func main() {
	cfg := config.Load()
	emailSender, emailSenderErr := platformemail.NewSender(platformemail.Config{
		Enabled:     cfg.EmailNotificationsEnabled,
		Host:        cfg.SMTPHost,
		Port:        cfg.SMTPPort,
		Username:    cfg.SMTPUsername,
		Password:    cfg.SMTPPassword,
		FromAddress: cfg.SMTPFromAddress,
	})
	if emailSenderErr != nil {
		log.Fatalf("failed to configure email sender: %v", emailSenderErr)
	}
	docs.SwaggerInfo.BasePath = "/api"
	log.Printf("database config: %s", dbopen.ConfigMode(cfg))
	if cfg.EmailNotificationsEnabled {
		log.Printf("email notifications enabled via smtp %s:%s", cfg.SMTPHost, cfg.SMTPPort)
		if strings.TrimSpace(cfg.EmailNotificationRecipients) == "" {
			log.Printf("email notifications enabled but no recipients configured")
		}
	} else {
		log.Printf("email notifications disabled")
	}
	if strings.TrimSpace(cfg.PublicURL) == "" {
		log.Printf("public url is not configured; slack project/job/build links are disabled")
	} else {
		log.Printf("public url configured for notification links: %s", cfg.PublicURL)
	}

	dbURL, dbPoolCfg := dbopen.FromConfig(cfg)
	db, err := platformdb.Open(dbURL, dbPoolCfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Printf("error closing database: %v", closeErr)
		}
	}()

	buildRepo := repositorypostgres.NewBuildRepository(db)
	executionJobRepo := repositorypostgres.NewExecutionJobRepository(db)
	executionJobOutputRepo := repositorypostgres.NewExecutionJobOutputRepository(db)
	jobRepo := repositorypostgres.NewJobRepository(db)
	projectRepo := repositorypostgres.NewProjectRepository(db)
	userRepo := repositorypostgres.NewUserRepository(db)
	apiTokenRepo := repositorypostgres.NewAPITokenRepository(db)
	projectMembershipRepo := repositorypostgres.NewProjectMembershipRepository(db)
	jobManagedImageConfigRepo := repositorypostgres.NewJobManagedImageConfigRepository(db)
	sourceCredentialRepo := repositorypostgres.NewSourceCredentialRepository(db)
	managedImageCatalogRepo := repositorypostgres.NewManagedImageCatalogRepository(db)
	versionTagRepo := repositorypostgres.NewVersionTagRepository(db)
	artifactLabelRepo := repositorypostgres.NewArtifactLabelRepository(db)
	webhookDeliveryRepo := repositorypostgres.NewWebhookDeliveryRepository(db)
	notificationDeliveryRepo := repositorypostgres.NewNotificationDeliveryRepository(db)
	notificationSubscriptionRepo := repositorypostgres.NewNotificationSubscriptionRepository(db)
	notificationPreferenceRepo := repositorypostgres.NewUserNotificationPreferenceRepository(db)
	notificationInstanceSettingsRepo := repositorypostgres.NewNotificationInstanceSettingsRepository(db)
	artifactRepo := repositorypostgres.NewArtifactRepository(db)
	workerRepo := repositorypostgres.NewWorkerRepository(db)
	buildNotificationService, buildNotificationErr := buildsvc.NewBuildNotificationService(buildsvc.BuildNotificationConfig{
		Enabled:          cfg.EmailNotificationsEnabled,
		Recipients:       cfg.EmailNotificationRecipients,
		Sender:           emailSender,
		SlackSender:      buildsvc.NewSlackWebhookSender(nil),
		JobRepo:          jobRepo,
		ProjectRepo:      projectRepo,
		DeliveryRepo:     notificationDeliveryRepo,
		SubscriptionRepo: notificationSubscriptionRepo,
		UserRepo:         userRepo,
		PreferenceRepo:   notificationPreferenceRepo,
		PublicBaseURL:    cfg.PublicURL,
	})
	if buildNotificationErr != nil {
		log.Fatalf("failed to configure build notifications: %v", buildNotificationErr)
	}
	managedImageRefresher := managedimagesvc.NewService(
		source.NewGitFetcher(),
		jobManagedImageConfigRepo,
		sourceCredentialRepo,
		managedImageCatalogRepo,
		managedimagesvc.NewDeterministicPublisher(),
		source.NewGitWriteBackClient(),
		source.NewGitHubPullRequestClient("", nil),
	)
	artifactResolver, err := artifact.ResolveStores(artifact.StoreConfig{
		Provider:    cfg.ArtifactStorageProvider,
		StorageRoot: cfg.ArtifactStorageRoot,
		GCSBucket:   cfg.ArtifactGCSBucket,
		GCSPrefix:   cfg.ArtifactGCSPrefix,
		GCSProject:  cfg.ArtifactGCSProject,
		Strict:      cfg.ArtifactStorageStrict,
	})
	if err != nil {
		log.Fatalf("failed to resolve artifact stores: %v", err)
	}
	logSink := logs.NewPostgresSink(db)
	versionTagService := versiontagsvc.NewService(versionTagRepo).WithArtifactLabels(artifactLabelRepo)
	artifactService := artifactsvc.NewService(artifactRepo)
	buildService := buildsvc.NewBuildServiceFromConfig(buildRepo, nil, logSink, buildsvc.BuildServiceConfig{
		ExecutionJobRepo:      executionJobRepo,
		ExecutionOutputRepo:   executionJobOutputRepo,
		BuildNotifier:         buildNotificationService,
		RepoFetcher:           source.NewGitFetcher(),
		ManagedImageRefresher: managedImageRefresher,
		VersionTagger:         versionTagService,
		ArtifactRepo:          artifactRepo,
		ArtifactResolver:      artifactResolver,
		ArtifactWorkspace:     cfg.ExecutionWorkspaceRoot,
		ExecutionWorkspace:    cfg.ExecutionWorkspaceRoot,
		DefaultImage:          cfg.ExecutionDefaultImage,
	})
	projectService := service.NewProjectService(projectRepo)
	userService := service.NewUserService(userRepo)
	apiTokenService := service.NewAPITokenService(apiTokenRepo, userRepo)
	projectMembershipService := service.NewProjectMembershipService(projectRepo, projectMembershipRepo)
	jobService := service.NewJobService(jobRepo, buildService).WithProjectRepository(projectRepo).WithManagedImageConfigRepository(jobManagedImageConfigRepo, sourceCredentialRepo)
	sourceCredentialService := service.NewSourceCredentialService(sourceCredentialRepo)
	notificationService := service.NewNotificationService(notificationSubscriptionRepo).WithPreferenceRepository(notificationPreferenceRepo).WithInstanceSettingsRepository(notificationInstanceSettingsRepo)
	webhookService := webhooksvc.NewDeliveryIngressService(webhookDeliveryRepo, jobService)
	webhookMetrics := observability.NewExpvarWebhookIngressMetrics()
	webhookService.SetMetrics(webhookMetrics)
	buildHandler := handler.NewBuildHandler(buildService)
	workerVisibilityService := workersvc.NewVisibilityService(workerRepo, buildService)
	workerVisibilityService.SetProjectRepository(projectRepo)
	workerVisibilityService.SetJobRepository(jobRepo)
	workerHandler := handler.NewWorkerHandler(workerVisibilityService)
	buildHandler.SetVersionTagService(versionTagService)
	buildHandler.SetProjectService(projectService)
	artifactHandler := handler.NewArtifactHandler(artifactService)
	artifactHandler.SetVersionTagService(versionTagService)
	artifactHandler.SetProjectService(projectService)
	artifactHandler.SetJobService(jobService)
	jobHandler := handler.NewJobHandler(jobService)
	projectHandler := handler.NewProjectHandler(projectService, jobService)
	authMode := auth.ParseMode(cfg.AuthMode)
	bootstrapAdmins := auth.ParseBootstrapAdminEmails(cfg.BootstrapAdminEmails)
	buildHandler.SetAuthorization(authMode, projectMembershipService)
	artifactHandler.SetAuthorization(authMode, projectMembershipService)
	jobHandler.SetAuthorization(authMode, projectMembershipService)
	projectHandler.SetAuthorization(authMode, projectMembershipService)
	userHandler := handler.NewUserHandler(userService, authMode)
	apiTokenHandler := handler.NewAPITokenHandler(apiTokenService)
	projectMembershipHandler := handler.NewProjectMembershipHandler(projectMembershipService, authMode)
	versionTagHandler := handler.NewVersionTagHandler(versionTagService)
	credentialHandler := handler.NewSourceCredentialHandler(sourceCredentialService)
	credentialHandler.SetAuthorization(authMode)
	notificationHandler := handler.NewNotificationHandler(nil)
	notificationHandler.SetAdminService(notificationService)
	notificationHandler.SetAuthorization(authMode)
	if authMode == auth.ModeDisabled {
		notificationHandler = handler.NewNotificationHandler(buildNotificationService)
		notificationHandler.SetAdminService(notificationService)
		notificationHandler.SetAuthorization(authMode)
	}
	eventHandler := handler.NewEventHandler(jobService, webhookService, webhookMetrics, cfg.GitHubWebhookSecret)
	readyHandler := handler.NewReadinessHandler(handler.ReadinessCheckFunc(func(ctx context.Context) error {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := db.PingContext(checkCtx); err != nil {
			return err
		}

		const readinessQuery = `
			SELECT
				to_regclass('public.goose_db_version') IS NOT NULL
				AND to_regclass('public.builds') IS NOT NULL
				AND to_regclass('public.build_jobs') IS NOT NULL
		`
		var ready bool
		if err := db.QueryRowContext(checkCtx, readinessQuery).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			return errors.New("database schema not ready")
		}
		return nil
	}))

	var sessionManager *auth.CookieSessionManager
	var authHandler *handler.AuthHandler
	if authMode == auth.ModeOIDC {
		sameSite, sameSiteErr := auth.ParseSameSite(cfg.SessionCookieSameSite)
		if sameSiteErr != nil {
			log.Fatalf("invalid session cookie same-site setting: %v", sameSiteErr)
		}
		createdSessionManager, sessionErr := auth.NewCookieSessionManager(auth.CookieSessionConfig{
			Secret:     cfg.SessionSecret,
			CookieName: cfg.SessionCookieName,
			Secure:     cfg.SessionCookieSecure,
			SameSite:   sameSite,
		})
		if sessionErr != nil {
			log.Fatalf("failed to configure sessions: %v", sessionErr)
		}
		sessionManager = createdSessionManager

		oidcAuthenticator, oidcErr := auth.NewOIDCAuthenticator(context.Background(), auth.OIDCConfig{
			IssuerURL:    cfg.OIDCIssuerURL,
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Scopes:       auth.ParseOIDCScopes(cfg.OIDCScopes),
		})
		if oidcErr != nil {
			log.Fatalf("failed to configure OIDC: %v", oidcErr)
		}
		authHandler = handler.NewAuthHandler(oidcAuthenticator, sessionManager, userService, handler.AuthHandlerConfig{
			BootstrapAdminEmails:  bootstrapAdmins,
			PostLoginRedirectURL:  cfg.AuthPostLoginRedirectURL,
			PostLogoutRedirectURL: cfg.AuthPostLogoutRedirectURL,
		})
	}

	authMiddleware := auth.Middleware(auth.MiddlewareConfig{
		Mode:                 authMode,
		BootstrapAdminEmails: bootstrapAdmins,
		Sessions:             sessionManager,
		APITokens:            apiTokenService,
	}, userService)
	router := apphttp.NewRouter(
		buildHandler,
		artifactHandler,
		jobHandler,
		projectHandler,
		versionTagHandler,
		credentialHandler,
		eventHandler,
		cfg.PushEventSecret,
		apphttp.WithAuthHandler(authHandler),
		apphttp.WithAuthMiddleware(authMiddleware),
		apphttp.WithNotificationHandler(notificationHandler),
		apphttp.WithUserHandler(userHandler),
		apphttp.WithAPITokenHandler(apiTokenHandler),
		apphttp.WithProjectMembershipHandler(projectMembershipHandler),
		apphttp.WithWorkerHandler(workerHandler),
	)
	mux := nethttp.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	mux.Handle("/swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	mux.Handle("/readyz", nethttp.HandlerFunc(readyHandler.Ready))
	mux.Handle("/api/readyz", nethttp.HandlerFunc(readyHandler.Ready))
	mux.Handle("/", router)

	addr := ":" + cfg.AppPort
	log.Printf("starting server on %s", addr)

	if err := nethttp.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
