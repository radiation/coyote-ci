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
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	platformdb "github.com/radiation/coyote-ci/backend/internal/platform/db"
	"github.com/radiation/coyote-ci/backend/internal/platform/dbopen"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorypostgres "github.com/radiation/coyote-ci/backend/internal/repository/postgres"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	dockerrunner "github.com/radiation/coyote-ci/backend/internal/runner/docker"
	"github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

const defaultPollInterval = 10 * time.Second

type workerIterationService interface {
	ClaimRunnableStep(ctx context.Context) (workersvc.WorkerRunnableStep, bool, error)
	ExecuteRunnableStep(ctx context.Context, step workersvc.WorkerRunnableStep) (workersvc.WorkerStepExecutionReport, error)
}

type workerStatusProvider interface {
	RecoveryStats() workersvc.WorkerLeaseRecoveryStats
}

func main() {
	cfg := config.Load()
	log.Printf("database config: %s", dbopen.ConfigMode(cfg))
	logEmailNotificationConfig(cfg)
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
	artifactRepo := repositorypostgres.NewArtifactRepository(db)
	cacheEntryRepo := repositorypostgres.NewCacheEntryRepository(db)
	workerRepo := repositorypostgres.NewWorkerRepository(db)
	jobRepo := repositorypostgres.NewJobRepository(db)
	projectRepo := repositorypostgres.NewProjectRepository(db)
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
	stepRunner := resolveStepRunner(cfg)
	logSink := logs.NewPostgresSink(db)
	versionTagService := newWorkerVersionTagService(versionTagRepo, artifactLabelRepo)
	buildService := buildsvc.NewBuildServiceFromConfig(buildRepo, stepRunner, logSink, buildsvc.BuildServiceConfig{
		ExecutionJobRepo:    executionJobRepo,
		ExecutionOutputRepo: executionJobOutputRepo,
		BuildNotifier:       buildNotificationService,
		DefaultImage:        cfg.ExecutionDefaultImage,
		ExecutionWorkspace:  cfg.ExecutionWorkspaceRoot,
		CacheStore:          cacheStore,
		CacheEntryRepo:      cacheEntryRepo,
		VersionTagger:       versionTagService,
		ArtifactRepo:        artifactRepo,
		ArtifactLabelRepo:   artifactLabelRepo,
		ArtifactResolver:    artifactResolver,
		ArtifactWorkspace:   cfg.ExecutionWorkspaceRoot,
	})
	leaseDuration := time.Duration(cfg.StepLeaseSeconds) * time.Second
	workerService := workersvc.NewExecutionWorkerServiceWithLease(buildService, defaultWorkerID(), leaseDuration)
	workerService.SetWorkerRepository(workerRepo)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startWorkerStatusServer(ctx, cfg.WorkerStatusAddr, workerService)

	log.Printf("starting worker loop")
	if err := runWorkerLoop(ctx, workerService, defaultPollInterval); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("worker loop failed: %v", err)
	}
	log.Printf("worker stopped")
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
	switch strings.ToLower(strings.TrimSpace(cfg.ExecutionBackend)) {
	case "", "docker":
		workspace := source.NewHostWorkspaceMaterializer(cfg.ExecutionWorkspaceRoot)
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

func runWorkerLoop(ctx context.Context, worker workerIterationService, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := runWorkerIteration(ctx, worker); err != nil {
				log.Printf("worker polling/claiming error: %v", err)
			}
		}
	}
}

func runWorkerIteration(ctx context.Context, worker workerIterationService) error {
	log.Printf("polling for runnable work")

	step, found, err := worker.ClaimRunnableStep(ctx)
	if err != nil {
		return err
	}
	if !found {
		log.Printf("no runnable work found")
		return nil
	}

	if _, err := worker.ExecuteRunnableStep(ctx, step); err != nil {
		return err
	}
	log.Printf("worker iteration completed for claimed work: build_id=%s step=%s", step.BuildID, step.StepName)

	return nil
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
