package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	nethttp "net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
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
	"github.com/radiation/coyote-ci/backend/internal/runner"
	dockerrunner "github.com/radiation/coyote-ci/backend/internal/runner/docker"
	"github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	executionsvc "github.com/radiation/coyote-ci/backend/internal/service/execution"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
	"github.com/radiation/coyote-ci/backend/internal/source"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

const defaultPollInterval = 10 * time.Second

type workerIterationService interface {
	ClaimRunnableStep(ctx context.Context) (workersvc.WorkerRunnableStep, bool, error)
	ExecuteRunnableStep(ctx context.Context, step workersvc.WorkerRunnableStep) (workersvc.WorkerStepExecutionReport, error)
}

type workerStatusProvider interface {
	RecoveryStats() workersvc.WorkerLeaseRecoveryStats
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

func main() {
	if handled, commandErr := runWorkspaceHelperCommand(context.Background(), os.Args[1:]); handled {
		if commandErr != nil {
			log.Fatalf("workspace helper command failed: %v", commandErr)
		}
		return
	}
	cfg := config.Load()
	log.Printf("database config: %s", dbopen.ConfigMode(cfg))
	logEmailNotificationConfig(cfg)
	logNotificationLinkConfig(cfg.PublicURL)

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
	artifactRepo := repositorypostgres.NewArtifactRepository(db)
	artifactTriggerDeliveryRepo := repositorypostgres.NewArtifactTriggerDeliveryRepository(db)
	cacheEntryRepo := repositorypostgres.NewCacheEntryRepository(db)
	workerRepo := repositorypostgres.NewWorkerRepository(db)
	jobRepo := repositorypostgres.NewJobRepository(db)
	jobManagedImageConfigRepo := repositorypostgres.NewJobManagedImageConfigRepository(db)
	projectRepo := repositorypostgres.NewProjectRepository(db)
	sourceCredentialRepo := repositorypostgres.NewSourceCredentialRepository(db)
	scmConnectionRepo := repositorypostgres.NewSCMConnectionRepository(db)
	scmRepositoryRegistrationRepo := repositorypostgres.NewSCMRepositoryRegistrationRepository(db)
	userRepo := repositorypostgres.NewUserRepository(db)
	versionTagRepo := repositorypostgres.NewVersionTagRepository(db)
	artifactLabelRepo := repositorypostgres.NewArtifactLabelRepository(db)
	notificationDeliveryRepo := repositorypostgres.NewNotificationDeliveryRepository(db)
	notificationSubscriptionRepo := repositorypostgres.NewNotificationSubscriptionRepository(db)
	notificationPreferenceRepo := repositorypostgres.NewUserNotificationPreferenceRepository(db)
	userSlackIdentityRepo := repositorypostgres.NewUserSlackIdentityRepository(db)
	slackWorkspaceIntegrationRepo := repositorypostgres.NewSlackWorkspaceIntegrationRepository(db)
	notificationMetrics := observability.NewExpvarNotificationDeliveryMetrics()
	buildNotificationService := buildWorkerNotificationService(cfg, buildRepo, artifactRepo, jobRepo, projectRepo, userRepo, notificationPreferenceRepo, userSlackIdentityRepo, slackWorkspaceIntegrationRepo, notificationDeliveryRepo, notificationSubscriptionRepo, notificationMetrics, log.Fatalf)
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
	cacheStore, err := cachepkg.ResolveStore(cachepkg.StoreConfig{
		Provider:    cfg.WorkerCacheStorageProvider,
		StorageRoot: cfg.WorkerCacheStorageRoot,
		MaxSizeMB:   cfg.WorkerCacheMaxSizeMB,
		GCSBucket:   cfg.WorkerCacheGCSBucket,
		GCSPrefix:   cfg.WorkerCacheGCSPrefix,
		GCSProject:  cfg.WorkerCacheGCSProject,
		Strict:      cfg.WorkerCacheStorageStrict,
	})
	if err != nil {
		log.Fatalf("failed to resolve cache store: %v", err)
	}
	workspaceRevisionStore := workspaceRevisionStoreFromConfig(cfg)
	stepRunner := resolveStepRunnerWithWorkspaceRevisions(cfg, workspaceRevisionRepo, workspaceRevisionStore)
	logSink := logs.NewPostgresSink(db)
	versionTagService := newWorkerVersionTagService(versionTagRepo, artifactLabelRepo)
	checkoutResolver, checkoutResolverErr := newRepositoryAwareCheckoutResolver(scmConnectionRepo, scmRepositoryRegistrationRepo)
	if checkoutResolverErr != nil {
		log.Fatalf("failed to configure repository-aware checkout: %v", checkoutResolverErr)
	}
	buildService := buildsvc.NewBuildServiceFromConfig(buildRepo, stepRunner, logSink, newWorkerBuildServiceConfig(cfg, buildsvc.BuildServiceConfig{
		ExecutionJobRepo:    executionJobRepo,
		ExecutionOutputRepo: executionJobOutputRepo,
		BuildNotifier:       buildNotificationService,
		DefaultImage:        cfg.ExecutionDefaultImage,
		ExecutionWorkspace:  cfg.ExecutionWorkspaceRoot,
		RepositoryCheckout:  checkoutResolver,
		CacheStore:          cacheStore,
		CacheEntryRepo:      cacheEntryRepo,
		VersionTagger:       versionTagService,
		ArtifactRepo:        artifactRepo,
		ArtifactLabelRepo:   artifactLabelRepo,
		ArtifactResolver:    artifactResolver,
		ArtifactWorkspace:   cfg.ExecutionWorkspaceRoot,
	}, workspaceRevisionRepo, workspaceRevisionStore))
	jobService := service.NewJobService(jobRepo, buildService).
		WithProjectRepository(projectRepo).
		WithManagedImageConfigRepository(jobManagedImageConfigRepo, sourceCredentialRepo).
		WithArtifactTriggerDeliveryRepository(artifactTriggerDeliveryRepo)
	buildService.SetArtifactTriggerDispatcher(jobService)
	leaseDuration := time.Duration(cfg.StepLeaseSeconds) * time.Second
	workerService := workersvc.NewExecutionWorkerServiceWithLease(buildService, defaultWorkerID(), leaseDuration)
	workerService.SetWorkerRepository(workerRepo)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startWorkerStatusServer(ctx, cfg.WorkerStatusAddr, workerService)

	log.Printf("starting worker loop")
	controller, controllerErr := resolveExecutionController(cfg, workerService, logSink)
	if controllerErr != nil {
		log.Fatalf("failed to configure execution controller: %v", controllerErr)
	}
	if err := runWorkerLoop(ctx, controller, defaultPollInterval); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker loop failed: %v", err)
	}
	log.Printf("worker stopped")
}

func runWorkspaceHelperCommand(ctx context.Context, args []string) (bool, error) {
	if len(args) != 2 || args[0] != "workspace" {
		return false, nil
	}
	switch args[1] {
	case "prepare":
		return true, runWorkspacePrepare(ctx)
	case "publish":
		return true, runWorkspacePublish(ctx)
	case "publish-after-build":
		return true, runWorkspacePublishAfterBuild(ctx)
	default:
		return false, nil
	}
}

var newKubernetesClient = kubernetesexec.NewClient

func resolveExecutionController(cfg config.Config, workerService *workersvc.ExecutionWorkerService, logSink logs.LogSink) (executionsvc.Controller, error) {
	if strings.ToLower(strings.TrimSpace(cfg.ExecutionBackend)) != "kubernetes" {
		return workersvc.NewSynchronousController(workerService), nil
	}
	helperConfig, helpersEnabled, helperErr := kubernetesWorkspaceHelperConfig(cfg)
	if helperErr != nil {
		return nil, helperErr
	}
	client, err := newKubernetesClient(cfg.WorkerKubernetesKubeconfig)
	if err != nil {
		return nil, err
	}
	controller := kubernetesexec.NewController(client, workerService, logSink, cfg.WorkerKubernetesNamespace)
	workerService.SetKubernetesWorkspaceLifecycleEnabled(helpersEnabled)
	if helpersEnabled {
		controller.WithWorkspaceHelper(helperConfig)
	}
	return controller, nil
}

func kubernetesWorkspaceHelperConfig(cfg config.Config) (kubernetesexec.WorkspaceHelperConfig, bool, error) {
	helper := kubernetesexec.WorkspaceHelperConfig{
		Image:              strings.TrimSpace(cfg.WorkerKubernetesHelperImage),
		InternalAPIURL:     strings.TrimSpace(cfg.WorkerKubernetesInternalAPIURL),
		ServiceAccountName: strings.TrimSpace(cfg.WorkspaceHelperServiceAccount),
	}
	if helper.Image == "" && helper.InternalAPIURL == "" {
		return kubernetesexec.WorkspaceHelperConfig{}, false, nil
	}
	if helper.Image == "" || helper.InternalAPIURL == "" || helper.ServiceAccountName == "" {
		return kubernetesexec.WorkspaceHelperConfig{}, false, errors.New("kubernetes workspace helper configuration requires helper image, internal API URL, and service account")
	}
	return helper, true, nil
}

func workspaceRevisionStoreFromConfig(cfg config.Config) workspacepkg.WorkspaceRevisionStore {
	if strings.TrimSpace(cfg.WorkspaceRevisionStorageRoot) == "" {
		return nil
	}
	return workspacepkg.NewFilesystemWorkspaceRevisionStore(cfg.WorkspaceRevisionStorageRoot)
}

func newWorkerBuildServiceConfig(cfg config.Config, serviceConfig buildsvc.BuildServiceConfig, workspaceRevisionRepo repository.WorkspaceRevisionRepository, workspaceRevisionStore workspacepkg.WorkspaceRevisionStore) buildsvc.BuildServiceConfig {
	serviceConfig.WorkspaceRevisionRepo = workspaceRevisionRepo
	serviceConfig.WorkspaceRevisionStore = workspaceRevisionStore
	return serviceConfig
}

var errConfigureEmailSender = errors.New("configure email sender")

func logEmailNotificationConfig(cfg config.Config) {
	if cfg.EmailNotificationsEnabled {
		log.Printf("email notifications enabled via smtp %s:%s", cfg.SMTPHost, cfg.SMTPPort)
		if strings.TrimSpace(cfg.EmailNotificationRecipients) == "" {
			log.Printf("email notifications enabled but no recipients configured")
		}
		return
	}
	log.Printf("email notifications disabled")
}

func logNotificationLinkConfig(publicURL string) {
	if strings.TrimSpace(publicURL) == "" {
		log.Printf("public url is not configured; slack project/job/build links are disabled")
		return
	}
	log.Printf("public url configured for notification links: %s", publicURL)
}

func newWorkerNotificationService(cfg config.Config, buildRepo repository.BuildRepository, artifactRepo repository.ArtifactBuildListRepository, jobRepo repository.JobRepository, projectRepo repository.ProjectRepository, userRepo repository.UserRepository, preferenceRepo repository.UserNotificationPreferenceRepository, identityRepo repository.UserSlackIdentityRepository, workspaceRepo repository.SlackWorkspaceIntegrationRepository, deliveryRepo repository.NotificationDeliveryRepository, subscriptionRepo repository.NotificationSubscriptionRepository, metrics observability.NotificationDeliveryMetrics) (*buildsvc.BuildNotificationService, error) {
	emailSender, emailSenderErr := platformemail.NewSender(platformemail.Config{
		Enabled:     cfg.EmailNotificationsEnabled,
		Host:        cfg.SMTPHost,
		Port:        cfg.SMTPPort,
		Username:    cfg.SMTPUsername,
		Password:    cfg.SMTPPassword,
		FromAddress: cfg.SMTPFromAddress,
	})
	if emailSenderErr != nil {
		return nil, fmt.Errorf("%w: %v", errConfigureEmailSender, emailSenderErr)
	}

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
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
		UserRepo:         userRepo,
		PreferenceRepo:   preferenceRepo,
		IdentityRepo:     identityRepo,
		WorkspaceRepo:    workspaceRepo,
		PublicBaseURL:    cfg.PublicURL,
		ClaimOwner:       defaultWorkerID(),
		DeliveryMetrics:  metrics,
	})
	if buildNotificationErr != nil {
		return nil, buildNotificationErr
	}

	return buildNotificationService, nil
}

func buildWorkerNotificationService(cfg config.Config, buildRepo repository.BuildRepository, artifactRepo repository.ArtifactBuildListRepository, jobRepo repository.JobRepository, projectRepo repository.ProjectRepository, userRepo repository.UserRepository, preferenceRepo repository.UserNotificationPreferenceRepository, identityRepo repository.UserSlackIdentityRepository, workspaceRepo repository.SlackWorkspaceIntegrationRepository, deliveryRepo repository.NotificationDeliveryRepository, subscriptionRepo repository.NotificationSubscriptionRepository, metrics observability.NotificationDeliveryMetrics, fatalf func(string, ...any)) *buildsvc.BuildNotificationService {
	buildNotificationService, notificationErr := newWorkerNotificationService(cfg, buildRepo, artifactRepo, jobRepo, projectRepo, userRepo, preferenceRepo, identityRepo, workspaceRepo, deliveryRepo, subscriptionRepo, metrics)
	if notificationErr == nil {
		return buildNotificationService
	}
	if errors.Is(notificationErr, errConfigureEmailSender) {
		fatalf("failed to configure email sender: %v", notificationErr)
		return nil
	}
	fatalf("failed to configure build notifications: %v", notificationErr)
	return nil
}

func resolveStepRunner(cfg config.Config) runner.Runner {
	return resolveStepRunnerWithWorkspaceRevisions(cfg, nil, nil)
}

func resolveStepRunnerWithWorkspaceRevisions(cfg config.Config, workspaceRevisionRepo repository.WorkspaceRevisionRepository, workspaceRevisionStore workspacepkg.WorkspaceRevisionStore) runner.Runner {
	switch strings.ToLower(strings.TrimSpace(cfg.ExecutionBackend)) {
	case "", "docker":
		workspace := source.NewHostWorkspaceMaterializerWithRevisionStore(cfg.ExecutionWorkspaceRoot, workspaceRevisionRepo, workspaceRevisionStore)
		return dockerrunner.New(dockerrunner.Options{
			Workspace:         workspace,
			DefaultImage:      cfg.ExecutionDefaultImage,
			MountDockerSocket: cfg.MountDockerSocket,
		})
	case "inprocess", "local":
		return inprocess.NewWithWorkspaceRoot(cfg.ExecutionWorkspaceRoot)
	default:
		log.Printf("unknown execution backend %q; falling back to inprocess", cfg.ExecutionBackend)
		return inprocess.NewWithWorkspaceRoot(cfg.ExecutionWorkspaceRoot)
	}
}

var osHostname = os.Hostname

func defaultWorkerID() string {
	hostname, err := osHostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}

	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

func runWorkerLoop(ctx context.Context, controller executionsvc.Controller, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := controller.Reconcile(ctx); err != nil {
				log.Printf("worker polling/claiming error: %v", err)
			}
		}
	}
}

func runWorkerIteration(ctx context.Context, worker workerIterationService) error {
	return workersvc.NewSynchronousController(worker).Reconcile(ctx)
}

func newWorkerVersionTagService(versionTagRepo repository.VersionTagRepository, artifactLabelRepo repository.ArtifactLabelRepository) *versiontagsvc.Service {
	return versiontagsvc.NewService(versionTagRepo).WithArtifactLabels(artifactLabelRepo)
}

func startWorkerStatusServer(ctx context.Context, addr string, worker workerStatusProvider) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return
	}

	srv := &nethttp.Server{
		Addr:    trimmed,
		Handler: newWorkerStatusHandler(worker),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("worker status server shutdown error: %v", err)
		}
	}()

	go func() {
		log.Printf("worker status server listening on %s", trimmed)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			log.Printf("worker status server error: %v", err)
		}
	}()
}

func newWorkerStatusHandler(worker workerStatusProvider) nethttp.Handler {
	mux := nethttp.NewServeMux()
	mux.HandleFunc("/healthz", func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/internal/status/worker", func(w nethttp.ResponseWriter, req *nethttp.Request) {
		if req.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}

		resp := struct {
			WorkerRecovery workersvc.WorkerLeaseRecoveryStats `json:"worker_recovery"`
			TimestampUTC   time.Time                          `json:"timestamp_utc"`
		}{
			WorkerRecovery: worker.RecoveryStats(),
			TimestampUTC:   time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			w.WriteHeader(nethttp.StatusInternalServerError)
			return
		}
	})

	return mux
}
