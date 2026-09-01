package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/runner"
	dockerrunner "github.com/radiation/coyote-ci/backend/internal/runner/docker"
	"github.com/radiation/coyote-ci/backend/internal/runner/inprocess"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	executionsvc "github.com/radiation/coyote-ci/backend/internal/service/execution"
	versiontagsvc "github.com/radiation/coyote-ci/backend/internal/service/versiontag"
	workersvc "github.com/radiation/coyote-ci/backend/internal/service/worker"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

func captureWorkerLogOutput(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()
	fn()
	return buf.String()
}

type fakeWorkerIterationService struct {
	claimStep workersvc.WorkerRunnableStep
	claimOK   bool
	claimErr  error
	claimHook func()

	executeReport workersvc.WorkerStepExecutionReport
	executeErr    error

	executeCalls int
}

type fakeWorkerStatusProvider struct {
	stats workersvc.WorkerLeaseRecoveryStats
}

func (f *fakeWorkerStatusProvider) RecoveryStats() workersvc.WorkerLeaseRecoveryStats {
	return f.stats
}

func (f *fakeWorkerIterationService) ClaimRunnableStep(_ context.Context) (workersvc.WorkerRunnableStep, bool, error) {
	if f.claimHook != nil {
		f.claimHook()
	}
	return f.claimStep, f.claimOK, f.claimErr
}

func (f *fakeWorkerIterationService) ExecuteRunnableStep(_ context.Context, _ workersvc.WorkerRunnableStep) (workersvc.WorkerStepExecutionReport, error) {
	f.executeCalls++
	return f.executeReport, f.executeErr
}

func TestRunWorkerIteration_Success(t *testing.T) {
	worker := &fakeWorkerIterationService{
		claimStep: workersvc.WorkerRunnableStep{BuildID: "build-1", StepName: "default"},
		claimOK:   true,
		executeReport: workersvc.WorkerStepExecutionReport{
			BuildID: "build-1",
			Step: domain.BuildStep{
				Name:   "default",
				Status: domain.BuildStepStatusSuccess,
			},
			Result: runner.RunStepResult{
				Status:     runner.RunStepStatusSuccess,
				ExitCode:   0,
				StartedAt:  time.Now().UTC(),
				FinishedAt: time.Now().UTC(),
			},
		},
	}

	if err := runWorkerIteration(context.Background(), worker); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if worker.executeCalls != 1 {
		t.Fatalf("expected execute to be called once, got %d", worker.executeCalls)
	}
}

func TestRunWorkerIteration_ExecutionFailure(t *testing.T) {
	worker := &fakeWorkerIterationService{
		claimStep:  workersvc.WorkerRunnableStep{BuildID: "build-2", StepName: "default"},
		claimOK:    true,
		executeErr: errors.New("step failed"),
	}

	err := runWorkerIteration(context.Background(), worker)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "step failed" {
		t.Fatalf("expected step failed error, got %v", err)
	}
	if worker.executeCalls != 1 {
		t.Fatalf("expected execute to be called once, got %d", worker.executeCalls)
	}
}

func TestRunWorkerIteration_NoRunnableWork(t *testing.T) {
	worker := &fakeWorkerIterationService{claimOK: false}
	if err := runWorkerIteration(context.Background(), worker); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if worker.executeCalls != 0 {
		t.Fatalf("expected execute not to be called, got %d", worker.executeCalls)
	}
}

func TestRunWorkerIteration_ClaimFailure(t *testing.T) {
	worker := &fakeWorkerIterationService{claimErr: errors.New("claim failed")}
	if err := runWorkerIteration(context.Background(), worker); err == nil || err.Error() != "claim failed" {
		t.Fatalf("expected claim failed error, got %v", err)
	}
}

func TestRunWorkerLoop_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runWorkerLoop(ctx, executionsvc.ControllerFunc(func(context.Context) error { return nil }), time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestRunWorkerLoop_ProcessesTickerUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &fakeWorkerIterationService{
		claimHook: cancel,
		claimErr:  errors.New("claim failed"),
	}
	output := captureWorkerLogOutput(t, func() {
		controller := workersvc.NewSynchronousController(worker)
		if err := runWorkerLoop(ctx, controller, time.Millisecond); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})
	if !strings.Contains(output, "worker polling/claiming error: claim failed") {
		t.Fatalf("expected claim failure log, got %q", output)
	}
	if worker.executeCalls != 0 {
		t.Fatalf("expected execute not to run, got %d", worker.executeCalls)
	}
}

func TestResolveStepRunner(t *testing.T) {
	dockerRunner := resolveStepRunner(config.Config{ExecutionBackend: "docker", ExecutionWorkspaceRoot: "/tmp/coyote-work", ExecutionDefaultImage: "alpine:3.20"})
	if _, ok := dockerRunner.(*dockerrunner.Runner); !ok {
		t.Fatalf("expected docker runner, got %T", dockerRunner)
	}

	inprocessRunner := resolveStepRunner(config.Config{ExecutionBackend: "local", ExecutionWorkspaceRoot: "/tmp/coyote-work"})
	if _, ok := inprocessRunner.(*inprocess.Runner); !ok {
		t.Fatalf("expected inprocess runner, got %T", inprocessRunner)
	}

	fallbackOutput := captureWorkerLogOutput(t, func() {
		fallbackRunner := resolveStepRunner(config.Config{ExecutionBackend: "weird", ExecutionWorkspaceRoot: "/tmp/coyote-work"})
		if _, ok := fallbackRunner.(*inprocess.Runner); !ok {
			t.Fatalf("expected fallback inprocess runner, got %T", fallbackRunner)
		}
	})
	if !strings.Contains(fallbackOutput, "unknown execution backend \"weird\"; falling back to inprocess") {
		t.Fatalf("expected fallback log, got %q", fallbackOutput)
	}
}

func TestWorkspaceRevisionStoreFromConfig(t *testing.T) {
	if store := workspaceRevisionStoreFromConfig(config.Config{}); store != nil {
		t.Fatalf("expected no revision store when storage root is unset, got %T", store)
	}
	if store := workspaceRevisionStoreFromConfig(config.Config{WorkspaceRevisionStorageRoot: " /tmp/coyote-revisions "}); store == nil {
		t.Fatal("expected revision store when storage root is configured")
	} else if _, ok := store.(*workspacepkg.FilesystemWorkspaceRevisionStore); !ok {
		t.Fatalf("expected filesystem revision store, got %T", store)
	}
}

func TestNewWorkerBuildServiceConfig_WiresWorkspaceRevisionDependencies(t *testing.T) {
	revisionRepo := repositorymemory.NewWorkspaceRevisionRepository(nil)
	revisionStore := workspaceRevisionStoreFromConfig(config.Config{WorkspaceRevisionStorageRoot: "/tmp/coyote-revisions"})
	serviceConfig := newWorkerBuildServiceConfig(config.Config{}, buildsvc.BuildServiceConfig{DefaultImage: "alpine:3.20"}, revisionRepo, revisionStore)

	if serviceConfig.DefaultImage != "alpine:3.20" {
		t.Fatalf("expected existing service config to be preserved, got %#v", serviceConfig)
	}
	if serviceConfig.WorkspaceRevisionRepo != revisionRepo || serviceConfig.WorkspaceRevisionStore != revisionStore {
		t.Fatalf("expected workspace revision dependencies to be wired, got %#v", serviceConfig)
	}
}

func TestDefaultWorkerID(t *testing.T) {
	id := defaultWorkerID()
	if id == "" {
		t.Fatal("expected non-empty worker id")
	}
	if !strings.Contains(id, "-") {
		t.Fatalf("expected worker id to include pid separator, got %q", id)
	}
	hostname, err := os.Hostname()
	if err == nil && hostname != "" && !strings.Contains(id, hostname) {
		t.Fatalf("expected worker id to contain hostname %q, got %q", hostname, id)
	}
}

func TestDefaultWorkerID_FallsBackWhenHostnameFails(t *testing.T) {
	original := osHostname
	osHostname = func() (string, error) {
		return "", errors.New("hostname unavailable")
	}
	defer func() {
		osHostname = original
	}()

	id := defaultWorkerID()
	if !strings.HasPrefix(id, "unknown-host-") {
		t.Fatalf("expected unknown-host fallback, got %q", id)
	}
}

func TestNewRepositoryAwareCheckoutResolver(t *testing.T) {
	resolver, err := newRepositoryAwareCheckoutResolver(repositorymemory.NewSCMConnectionRepository(), repositorymemory.NewSCMRepositoryRegistrationRepository())
	if err != nil || resolver == nil {
		t.Fatalf("expected configured checkout resolver, resolver=%v err=%v", resolver, err)
	}
}

func TestStartWorkerStatusServer_EmptyAddrIsNoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWorkerStatusServer(ctx, "   ", &fakeWorkerStatusProvider{})
}

func TestNewWorkerStatusHandler_Healthz(t *testing.T) {
	h := newWorkerStatusHandler(&fakeWorkerStatusProvider{})
	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Fatalf("expected ok body, got %q", rr.Body.String())
	}
}

func TestNewWorkerStatusHandler_RecoveryStatus(t *testing.T) {
	h := newWorkerStatusHandler(&fakeWorkerStatusProvider{stats: workersvc.WorkerLeaseRecoveryStats{
		ClaimsWon:     1,
		ReclaimsWon:   2,
		RenewalsWon:   3,
		RenewalsStale: 4,
		StaleComplete: 5,
		ReclaimMisses: 6,
	}})

	req := httptest.NewRequest(nethttp.MethodGet, "/internal/status/worker", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		WorkerRecovery workersvc.WorkerLeaseRecoveryStats `json:"worker_recovery"`
		TimestampUTC   time.Time                          `json:"timestamp_utc"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if resp.WorkerRecovery.ClaimsWon != 1 || resp.WorkerRecovery.ReclaimsWon != 2 || resp.WorkerRecovery.RenewalsWon != 3 {
		t.Fatalf("unexpected recovery stats payload: %+v", resp.WorkerRecovery)
	}
	if resp.TimestampUTC.IsZero() {
		t.Fatal("expected timestamp_utc to be set")
	}
}

func TestNewWorkerStatusHandler_MethodNotAllowed(t *testing.T) {
	h := newWorkerStatusHandler(&fakeWorkerStatusProvider{})
	req := httptest.NewRequest(nethttp.MethodPost, "/internal/status/worker", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != nethttp.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestNewWorkerVersionTagService_WiresArtifactLabels(t *testing.T) {
	jobID := "job-1"
	buildID := "build-1"
	versionRepo := repositorymemory.NewVersionTagRepository()
	artifactRepo := repositorymemory.NewArtifactLabelRepository()
	artifactRepo.SeedBuilds(domain.Build{ID: buildID, JobID: &jobID})
	artifactRepo.SeedArtifacts(domain.BuildArtifact{ID: "artifact-1", BuildID: buildID, LogicalPath: "dist/app.tgz"})

	svc := newWorkerVersionTagService(versionRepo, artifactRepo)
	tags, err := svc.CreateVersionTags(context.Background(), jobID, versiontagsvc.CreateVersionTagsInput{
		Kind:        string(domain.VersionTagKindChannel),
		Version:     "latest",
		ArtifactIDs: []string{"artifact-1"},
	})
	if err != nil {
		t.Fatalf("expected artifact channel create to succeed, got %v", err)
	}
	if len(tags) != 1 || tags[0].Kind != domain.VersionTagKindChannel {
		t.Fatalf("expected one artifact channel tag, got %#v", tags)
	}
}

func TestLogEmailNotificationConfig(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		output := captureWorkerLogOutput(t, func() {
			logEmailNotificationConfig(config.Config{EmailNotificationsEnabled: false})
		})
		if !strings.Contains(output, "email notifications disabled") {
			t.Fatalf("expected disabled log, got %q", output)
		}
	})

	t.Run("enabled without recipients", func(t *testing.T) {
		output := captureWorkerLogOutput(t, func() {
			logEmailNotificationConfig(config.Config{EmailNotificationsEnabled: true, SMTPHost: "mailpit", SMTPPort: "1025"})
		})
		if !strings.Contains(output, "email notifications enabled via smtp mailpit:1025") {
			t.Fatalf("expected enabled log, got %q", output)
		}
		if !strings.Contains(output, "email notifications enabled but no recipients configured") {
			t.Fatalf("expected no-recipient log, got %q", output)
		}
	})
}

func TestLogNotificationLinkConfig(t *testing.T) {
	t.Run("disabled when public url missing", func(t *testing.T) {
		output := captureWorkerLogOutput(t, func() {
			logNotificationLinkConfig("  ")
		})
		if !strings.Contains(output, "public url is not configured; slack project/job/build links are disabled") {
			t.Fatalf("expected missing-public-url log, got %q", output)
		}
	})

	t.Run("enabled when public url configured", func(t *testing.T) {
		output := captureWorkerLogOutput(t, func() {
			logNotificationLinkConfig("https://ci.example.com")
		})
		if !strings.Contains(output, "public url configured for notification links: https://ci.example.com") {
			t.Fatalf("expected configured-public-url log, got %q", output)
		}
	})
}

func TestNewWorkerNotificationService(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	artifactRepo := repositorymemory.NewArtifactRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	userRepo := repositorymemory.NewUserRepository()
	preferenceRepo := repositorymemory.NewUserNotificationPreferenceRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	deliveryRepo := repositorymemory.NewNotificationDeliveryRepository()
	subscriptionRepo := repositorymemory.NewNotificationSubscriptionRepository()
	metrics := observability.NewNoopNotificationDeliveryMetrics()

	t.Run("disabled ignores invalid recipients", func(t *testing.T) {
		notifier, err := newWorkerNotificationService(config.Config{
			EmailNotificationsEnabled:   false,
			EmailNotificationRecipients: "not-an-email",
		}, buildRepo, artifactRepo, jobRepo, projectRepo, userRepo, preferenceRepo, identityRepo, workspaceRepo, deliveryRepo, subscriptionRepo, metrics)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if notifier == nil {
			t.Fatal("expected notifier")
		}
	})

	t.Run("enabled invalid smtp is wrapped as sender config", func(t *testing.T) {
		_, err := newWorkerNotificationService(config.Config{
			EmailNotificationsEnabled: true,
			SMTPHost:                  "mailpit",
			SMTPPort:                  "1025",
		}, buildRepo, artifactRepo, jobRepo, projectRepo, userRepo, preferenceRepo, identityRepo, workspaceRepo, deliveryRepo, subscriptionRepo, metrics)
		if !errors.Is(err, errConfigureEmailSender) {
			t.Fatalf("expected sender configuration error, got %v", err)
		}
	})

	t.Run("enabled invalid recipients returns notifier error", func(t *testing.T) {
		_, err := newWorkerNotificationService(config.Config{
			EmailNotificationsEnabled:   true,
			EmailNotificationRecipients: "not-an-email",
			SMTPHost:                    "mailpit",
			SMTPPort:                    "1025",
			SMTPFromAddress:             "coyote-ci@localhost",
		}, buildRepo, artifactRepo, jobRepo, projectRepo, userRepo, preferenceRepo, identityRepo, workspaceRepo, deliveryRepo, subscriptionRepo, metrics)
		if err == nil || errors.Is(err, errConfigureEmailSender) {
			t.Fatalf("expected recipient validation error, got %v", err)
		}
	})
}

func TestBuildWorkerNotificationService(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	artifactRepo := repositorymemory.NewArtifactRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	userRepo := repositorymemory.NewUserRepository()
	preferenceRepo := repositorymemory.NewUserNotificationPreferenceRepository()
	identityRepo := repositorymemory.NewUserSlackIdentityRepository()
	workspaceRepo := repositorymemory.NewSlackWorkspaceIntegrationRepository()
	deliveryRepo := repositorymemory.NewNotificationDeliveryRepository()
	subscriptionRepo := repositorymemory.NewNotificationSubscriptionRepository()
	metrics := observability.NewNoopNotificationDeliveryMetrics()

	t.Run("returns notifier on success", func(t *testing.T) {
		called := false
		notifier := buildWorkerNotificationService(config.Config{EmailNotificationsEnabled: false}, buildRepo, artifactRepo, jobRepo, projectRepo, userRepo, preferenceRepo, identityRepo, workspaceRepo, deliveryRepo, subscriptionRepo, metrics, func(string, ...any) {
			called = true
		})
		if called {
			t.Fatal("expected fatalf not to be called")
		}
		if notifier == nil {
			t.Fatal("expected notifier")
		}
	})

	t.Run("reports sender configuration failures", func(t *testing.T) {
		var message string
		notifier := buildWorkerNotificationService(config.Config{
			EmailNotificationsEnabled: true,
			SMTPHost:                  "mailpit",
			SMTPPort:                  "1025",
		}, buildRepo, artifactRepo, jobRepo, projectRepo, userRepo, preferenceRepo, identityRepo, workspaceRepo, deliveryRepo, subscriptionRepo, metrics, func(format string, args ...any) {
			message = fmt.Sprintf(format, args...)
		})
		if notifier != nil {
			t.Fatal("expected nil notifier")
		}
		if !strings.Contains(message, "failed to configure email sender") {
			t.Fatalf("expected email sender fatal message, got %q", message)
		}
	})

	t.Run("reports build notification failures", func(t *testing.T) {
		var message string
		notifier := buildWorkerNotificationService(config.Config{
			EmailNotificationsEnabled:   true,
			EmailNotificationRecipients: "not-an-email",
			SMTPHost:                    "mailpit",
			SMTPPort:                    "1025",
			SMTPFromAddress:             "coyote-ci@localhost",
		}, buildRepo, artifactRepo, jobRepo, projectRepo, userRepo, preferenceRepo, identityRepo, workspaceRepo, deliveryRepo, subscriptionRepo, metrics, func(format string, args ...any) {
			message = fmt.Sprintf(format, args...)
		})
		if notifier != nil {
			t.Fatal("expected nil notifier")
		}
		if !strings.Contains(message, "failed to configure build notifications") {
			t.Fatalf("expected build notification fatal message, got %q", message)
		}
	})
}
