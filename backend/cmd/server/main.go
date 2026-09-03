package main

import (
	"context"
	"errors"
	"expvar"
	"fmt"
	"log"
	nethttp "net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	docs "github.com/radiation/coyote-ci/backend/docs"
	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	apphttp "github.com/radiation/coyote-ci/backend/internal/http"
	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	kubernetesexec "github.com/radiation/coyote-ci/backend/internal/kubernetes"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	"github.com/radiation/coyote-ci/backend/internal/platform/dbopen"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformgithubapp "github.com/radiation/coyote-ci/backend/internal/platform/githubapp"
	platformsecret "github.com/radiation/coyote-ci/backend/internal/platform/secret"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/service"
	artifactsvc "github.com/radiation/coyote-ci/backend/internal/service/artifact"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	managedimagesvc "github.com/radiation/coyote-ci/backend/internal/service/managedimage"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
	"github.com/radiation/coyote-ci/backend/internal/source"
	"github.com/radiation/coyote-ci/backend/internal/versioninfo"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
	httpSwagger "github.com/swaggo/http-swagger"
)

var osServerHostname = os.Hostname

type scmStatusRuntime struct {
	reporter      buildsvc.BuildSCMStatusReporter
	reporterImpl  *buildsvc.SCMStatusReporter
	recoveryDrain *buildsvc.SCMStatusRecoveryDrain
}

type checkoutResolverConnectionRepository interface {
	GetByID(context.Context, string) (domain.SCMConnectionDetail, error)
}

type checkoutResolverRegistrationRepository interface {
	GetByID(context.Context, string) (domain.SCMRepositoryRegistration, error)
}

func newRepositoryAwareCheckoutResolver(connections checkoutResolverConnectionRepository, registrations checkoutResolverRegistrationRepository) (*buildsvc.RepositoryAwareCheckoutResolver, error) {
	return buildsvc.NewRepositoryAwareCheckoutResolver(buildsvc.RepositoryAwareCheckoutResolverConfig{
		Connections: connections, Registrations: registrations, Secrets: platformsecret.NewEnvResolver(), GitHub: platformgithubapp.NewClient(nil),
	})
}

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
	docs.SwaggerInfo.Version = versioninfo.Current().Version
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
	workspaceRevisionRepo := repositorypostgres.NewWorkspaceRevisionRepository(db)
	executionJobOutputRepo := repositorypostgres.NewExecutionJobOutputRepository(db)
	jobRepo := repositorypostgres.NewJobRepository(db)
	projectRepo := repositorypostgres.NewProjectRepository(db)
	userRepo := repositorypostgres.NewUserRepository(db)
	apiTokenRepo := repositorypostgres.NewAPITokenRepository(db)
	projectMembershipRepo := repositorypostgres.NewProjectMembershipRepository(db)
	jobManagedImageConfigRepo := repositorypostgres.NewJobManagedImageConfigRepository(db)
	sourceCredentialRepo := repositorypostgres.NewSourceCredentialRepository(db)
	scmConnectionRepo := repositorypostgres.NewSCMConnectionRepository(db)
	scmRepositoryRegistrationRepo := repositorypostgres.NewSCMRepositoryRegistrationRepository(db)
	managedImageCatalogRepo := repositorypostgres.NewManagedImageCatalogRepository(db)
	versionTagRepo := repositorypostgres.NewVersionTagRepository(db)
	artifactLabelRepo := repositorypostgres.NewArtifactLabelRepository(db)
	webhookDeliveryRepo := repositorypostgres.NewWebhookDeliveryRepository(db)
	artifactTriggerDeliveryRepo := repositorypostgres.NewArtifactTriggerDeliveryRepository(db)
	notificationDeliveryRepo := repositorypostgres.NewNotificationDeliveryRepository(db)
	scmStatusDeliveryRepo := repositorypostgres.NewSCMStatusDeliveryRepository(db)
	notificationSubscriptionRepo := repositorypostgres.NewNotificationSubscriptionRepository(db)
	notificationPreferenceRepo := repositorypostgres.NewUserNotificationPreferenceRepository(db)
	notificationInstanceSettingsRepo := repositorypostgres.NewNotificationInstanceSettingsRepository(db)
	slackWorkspaceIntegrationRepo := repositorypostgres.NewSlackWorkspaceIntegrationRepository(db)
	userSlackIdentityRepo := repositorypostgres.NewUserSlackIdentityRepository(db)
	artifactRepo := repositorypostgres.NewArtifactRepository(db)
	workerRepo := repositorypostgres.NewWorkerRepository(db)
	notificationMetrics := observability.NewExpvarNotificationDeliveryMetrics()
	buildNotificationService, buildNotificationErr := buildsvc.NewBuildNotificationService(buildsvc.BuildNotificationConfig{
		Enabled:          cfg.EmailNotificationsEnabled,
		Recipients:       cfg.EmailNotificationRecipients,
		Sender:           emailSender,
		SlackSender:      buildsvc.NewSlackWebhookSender(nil),
		SlackClient:      platformslack.NewClient(nil),
		BuildRepo:        buildRepo,
		ArtifactRepo:     artifactRepo,
		JobRepo:          jobRepo,
		ProjectRepo:      projectRepo,
		DeliveryRepo:     notificationDeliveryRepo,
		SubscriptionRepo: notificationSubscriptionRepo,
		UserRepo:         userRepo,
		PreferenceRepo:   notificationPreferenceRepo,
		IdentityRepo:     userSlackIdentityRepo,
		WorkspaceRepo:    slackWorkspaceIntegrationRepo,
		PublicBaseURL:    cfg.PublicURL,
		ClaimOwner:       defaultServerNotificationClaimOwner(),
		DeliveryMetrics:  notificationMetrics,
	})
	if buildNotificationErr != nil {
		log.Fatalf("failed to configure build notifications: %v", buildNotificationErr)
	}
	notificationRecoveryDrain, notificationRecoveryErr := buildsvc.NewNotificationRecoveryDrain(buildsvc.NotificationRecoveryDrainConfig{
		Notifier:  buildNotificationService,
		Interval:  cfg.NotificationRecoveryInterval,
		BatchSize: cfg.NotificationRecoveryBatchSize,
	})
	if notificationRecoveryErr != nil {
		log.Fatalf("failed to configure notification recovery drain: %v", notificationRecoveryErr)
	}
	var scmStatusReporter buildsvc.BuildSCMStatusReporter
	var scmStatusRecoveryDrain *buildsvc.SCMStatusRecoveryDrain
	scmStatusDeps, err := configureSCMStatusRuntime(cfg, buildRepo, projectRepo, scmStatusDeliveryRepo, scmConnectionRepo, scmRepositoryRegistrationRepo)
	if err != nil {
		log.Fatalf("failed to configure scm status reporting: %v", err)
	}
	if scmStatusDeps.reporterImpl != nil {
		scmStatusRecoveryDrain = scmStatusDeps.recoveryDrain
		scmStatusReporter = scmStatusDeps.reporter
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
	checkoutResolver, checkoutResolverErr := newRepositoryAwareCheckoutResolver(scmConnectionRepo, scmRepositoryRegistrationRepo)
	if checkoutResolverErr != nil {
		log.Fatalf("failed to configure repository-aware checkout: %v", checkoutResolverErr)
	}
	artifactService := artifactsvc.NewService(artifactRepo)
	buildService := buildsvc.NewBuildServiceFromConfig(buildRepo, nil, logSink, buildsvc.BuildServiceConfig{
		ExecutionJobRepo:      executionJobRepo,
		ExecutionOutputRepo:   executionJobOutputRepo,
		BuildNotifier:         buildNotificationService,
		SCMStatusReporter:     scmStatusReporter,
		SCMStatusDeliveryRepo: scmStatusDeliveryRepo,
		RepoFetcher:           source.NewGitFetcher(),
		RepositoryCheckout:    checkoutResolver,
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
	jobService := service.NewJobService(jobRepo, buildService).
		WithProjectRepository(projectRepo).
		WithSCMRepositoryRegistrationRepository(scmRepositoryRegistrationRepo).
		WithManagedImageConfigRepository(jobManagedImageConfigRepo, sourceCredentialRepo).
		WithArtifactTriggerDeliveryRepository(artifactTriggerDeliveryRepo)
	buildService.SetArtifactTriggerDispatcher(jobService)
	sourceCredentialService := service.NewSourceCredentialService(sourceCredentialRepo)
	scmAdminService := service.NewSCMAdminService(scmConnectionRepo, scmRepositoryRegistrationRepo)
	notificationService := service.NewNotificationService(notificationSubscriptionRepo).
		WithPreferenceRepository(notificationPreferenceRepo).
		WithInstanceSettingsRepository(notificationInstanceSettingsRepo).
		WithUserSlackIdentityRepository(userSlackIdentityRepo).
		WithSlackWorkspaceIntegrationRepository(slackWorkspaceIntegrationRepo)
	slackWorkspaceIntegrationService := service.NewSlackWorkspaceIntegrationService(slackWorkspaceIntegrationRepo, platformslack.NewClient(nil))
	personalSlackIdentityService := service.NewUserSlackIdentityService(userSlackIdentityRepo, slackWorkspaceIntegrationRepo, platformslack.NewClient(nil))
	webhookService := webhooksvc.NewDeliveryIngressService(webhookDeliveryRepo, jobService)
	webhookMetrics := observability.NewExpvarWebhookIngressMetrics()
	webhookService.SetMetrics(webhookMetrics)
	buildHandler := handler.NewBuildHandler(buildService)
	workerVisibilityService := workersvc.NewVisibilityService(workerRepo, buildService)
	workerVisibilityService.SetProjectRepository(projectRepo)
	workerVisibilityService.SetJobRepository(jobRepo)
	workerHandler := handler.NewWorkerHandler(workerVisibilityService)
	workspaceHelperHandler, workspaceHelperErr := newWorkspaceHelperHandler(cfg, executionJobRepo)
	if workspaceHelperErr != nil {
		log.Fatalf("failed to configure workspace helper capability exchange: %v", workspaceHelperErr)
	}
	if workspaceHelperHandler != nil {
		workspaceRevisionStore := workspaceRevisionStoreFromConfig(cfg)
		archiveReader, archiveReaderOK := workspaceRevisionStore.(workspacepkg.WorkspaceRevisionArchiveReader)
		if workspaceRevisionStore == nil || !archiveReaderOK {
			log.Fatal("workspace helper prepare requires workspace revision storage")
		}
		sourceArchives, sourceArchiveErr := service.NewServerSourceArchivePreparer(source.NewGitWorkspaceSourceResolver(), checkoutResolver)
		if sourceArchiveErr != nil {
			log.Fatalf("failed to configure workspace source archiver: %v", sourceArchiveErr)
		}
		prepareService, prepareErr := service.NewWorkspacePrepareService(service.WorkspacePrepareServiceConfig{
			CapabilityAuthorizer: workspaceHelperHandler.PrepareCapabilityAuthorizer(),
			ExecutionJobs:        executionJobRepo,
			Builds:               buildRepo,
			WorkspaceRevisions:   workspaceRevisionRepo,
			RevisionArchives:     archiveReader,
			SourceArchives:       sourceArchives,
		})
		if prepareErr != nil {
			log.Fatalf("failed to configure workspace prepare service: %v", prepareErr)
		}
		workspaceHelperHandler.SetPrepareService(prepareService)
	}
	buildHandler.SetVersionTagService(versionTagService)
	buildHandler.SetProjectService(projectService)
	buildHandler.SetJobService(jobService)
	artifactHandler := handler.NewArtifactHandler(artifactService)
	artifactHandler.SetVersionTagService(versionTagService)
	artifactHandler.SetProjectService(projectService)
	artifactHandler.SetJobService(jobService)
	jobHandler := handler.NewJobHandler(jobService)
	projectHandler := handler.NewProjectHandler(projectService, jobService)
	publicHandler := handler.NewPublicHandler(projectService, buildService, jobService)
	authMode := auth.ParseMode(cfg.AuthMode)
	bootstrapAdmins := auth.ParseBootstrapAdminEmails(cfg.BootstrapAdminEmails)
	if bootstrapErr := bootstrapAdminsAtStartup(context.Background(), userService, bootstrapAdmins); bootstrapErr != nil {
		log.Fatalf("failed to provision bootstrap admins: %v", bootstrapErr)
	}
	buildHandler.SetAuthorization(authMode, projectMembershipService)
	artifactHandler.SetAuthorization(authMode, projectMembershipService)
	jobHandler.SetAuthorization(authMode, projectMembershipService)
	projectHandler.SetAuthorization(authMode, projectMembershipService)
	userHandler := handler.NewUserHandler(userService, authMode)
	serverInfoHandler := handler.NewServerInfoHandler()
	apiTokenHandler := handler.NewAPITokenHandler(apiTokenService)
	projectMembershipHandler := handler.NewProjectMembershipHandler(projectMembershipService, authMode)
	versionTagHandler := handler.NewVersionTagHandler(versionTagService)
	credentialHandler := handler.NewSourceCredentialHandler(sourceCredentialService)
	credentialHandler.SetAuthorization(authMode)
	scmHandler := handler.NewSCMHandler(scmAdminService)
	scmHandler.SetAuthorization(authMode)
	notificationHandler := handler.NewNotificationHandler(nil)
	notificationHandler.SetAdminService(notificationService)
	notificationHandler.SetSlackWorkspaceIntegrationService(slackWorkspaceIntegrationService)
	notificationHandler.SetPersonalSlackIdentityService(personalSlackIdentityService)
	notificationHandler.SetAuthorization(authMode)
	if authMode == auth.ModeDisabled {
		notificationHandler = handler.NewNotificationHandler(buildNotificationService)
		notificationHandler.SetAdminService(notificationService)
		notificationHandler.SetSlackWorkspaceIntegrationService(slackWorkspaceIntegrationService)
		notificationHandler.SetPersonalSlackIdentityService(personalSlackIdentityService)
		notificationHandler.SetAuthorization(authMode)
	}
	githubWebhookResolver := webhooksvc.NewGitHubConnectionResolver(scmConnectionRepo, platformsecret.NewEnvResolver())
	eventHandler := handler.NewEventHandler(jobService, webhookService, webhookMetrics, githubWebhookResolver)
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
			PostLoginRedirectURL:  cfg.AuthPostLoginRedirectURL,
			PostLogoutRedirectURL: cfg.AuthPostLogoutRedirectURL,
		})
	}

	authMiddleware := auth.Middleware(auth.MiddlewareConfig{
		Mode:      authMode,
		Sessions:  sessionManager,
		APITokens: apiTokenService,
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
		apphttp.WithSCMHandler(scmHandler),
		apphttp.WithNotificationHandler(notificationHandler),
		apphttp.WithUserHandler(userHandler),
		apphttp.WithServerInfoHandler(serverInfoHandler),
		apphttp.WithAPITokenHandler(apiTokenHandler),
		apphttp.WithProjectMembershipHandler(projectMembershipHandler),
		apphttp.WithWorkerHandler(workerHandler),
		apphttp.WithWorkspaceHelperHandler(workspaceHelperHandler),
		apphttp.WithPublicHandler(publicHandler),
	)
	mux := nethttp.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	mux.Handle("/swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	mux.Handle("/readyz", nethttp.HandlerFunc(readyHandler.Ready))
	mux.Handle("/api/readyz", nethttp.HandlerFunc(readyHandler.Ready))
	mux.Handle("/", router)

	addr := ":" + cfg.AppPort
	log.Printf("starting server on %s", addr)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := &nethttp.Server{Addr: addr, Handler: mux}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := notificationRecoveryDrain.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("notification recovery drain stopped with error: %v", err)
		}
	}()
	if scmStatusRecoveryDrain != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := scmStatusRecoveryDrain.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("scm status recovery drain stopped with error: %v", err)
			}
		}()
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("server shutdown error: %v", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
	wg.Wait()
}

func newWorkspaceHelperHandler(cfg config.Config, executionJobs repository.ExecutionJobRepository) (*handler.WorkspaceHelperHandler, error) {
	return newWorkspaceHelperHandlerWithVerifier(cfg, executionJobs, func(kubeconfig string, serviceAccount string) (service.WorkloadIdentityVerifier, error) {
		return kubernetesexec.NewWorkloadIdentityVerifier(kubeconfig, serviceAccount)
	})
}

func workspaceRevisionStoreFromConfig(cfg config.Config) workspacepkg.WorkspaceRevisionStore {
	if strings.TrimSpace(cfg.WorkspaceRevisionStorageRoot) == "" {
		return nil
	}
	return workspacepkg.NewFilesystemWorkspaceRevisionStore(cfg.WorkspaceRevisionStorageRoot)
}

func newWorkspaceHelperHandlerWithVerifier(cfg config.Config, executionJobs repository.ExecutionJobRepository, newVerifier func(string, string) (service.WorkloadIdentityVerifier, error)) (*handler.WorkspaceHelperHandler, error) {
	if !cfg.WorkspaceHelperCapabilityEnabled {
		return nil, nil
	}
	verifier, err := newVerifier(cfg.WorkspaceHelperKubeconfig, cfg.WorkspaceHelperServiceAccount)
	if err != nil {
		return nil, err
	}
	capabilities, err := service.NewWorkspaceHelperCapabilityService(executionJobs, verifier, cfg.WorkspaceHelperCapabilitySecret)
	if err != nil {
		return nil, err
	}
	return handler.NewWorkspaceHelperHandler(capabilities), nil
}

func bootstrapAdminsAtStartup(ctx context.Context, users *service.UserService, emails map[string]struct{}) error {
	return users.BootstrapAdmins(ctx, emails)
}

func configureSCMStatusRuntime(cfg config.Config, buildRepo repository.BuildRepository, projectRepo repository.ProjectRepository, deliveryRepo repository.SCMStatusDeliveryRepository, connectionRepo repository.SCMConnectionRepository, registrationRepo repository.SCMRepositoryRegistrationRepository) (scmStatusRuntime, error) {
	publisher, err := buildsvc.NewGitHubAppCommitStatusPublisher(buildsvc.GitHubAppCommitStatusPublisherConfig{
		Connections:   connectionRepo,
		Registrations: registrationRepo,
		Secrets:       platformsecret.NewEnvResolver(),
		GitHubApps:    platformgithubapp.NewClient(nil),
	})
	if err != nil {
		return scmStatusRuntime{}, err
	}
	reporterImpl, err := buildsvc.NewSCMStatusReporter(buildsvc.SCMStatusReporterConfig{
		BuildRepo:     buildRepo,
		ProjectRepo:   projectRepo,
		DeliveryRepo:  deliveryRepo,
		Publisher:     publisher,
		PublicBaseURL: cfg.PublicURL,
		ClaimOwner:    defaultServerNotificationClaimOwner(),
	})
	if err != nil {
		return scmStatusRuntime{}, err
	}

	recoveryDrain, err := buildsvc.NewSCMStatusRecoveryDrain(buildsvc.SCMStatusRecoveryDrainConfig{
		Reporter:  reporterImpl,
		Interval:  cfg.SCMStatusRecoveryInterval,
		BatchSize: cfg.SCMStatusRecoveryBatchSize,
	})
	if err != nil {
		return scmStatusRuntime{}, err
	}

	return scmStatusRuntime{reporter: reporterImpl, reporterImpl: reporterImpl, recoveryDrain: recoveryDrain}, nil
}

func defaultServerNotificationClaimOwner() string {
	hostname, err := osServerHostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	return "server-" + hostname + "-" + fmt.Sprintf("%d", os.Getpid())
}
