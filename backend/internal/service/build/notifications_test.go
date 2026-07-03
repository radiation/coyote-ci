package build

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	steprunner "github.com/radiation/coyote-ci/backend/internal/runner"
	"github.com/radiation/coyote-ci/backend/internal/source"
)

func TestBuildService_FailBuild_SendsNotificationWhenConfigured(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, BuildNumber: 42, Status: domain.BuildStatusRunning, CreatedAt: now}}
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: "project-1", Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, JobRepo: jobRepo, ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email, got %d", len(sender.messages))
	}
	message := sender.messages[0]
	if message.To != "<dev@example.com>" {
		t.Fatalf("expected normalized recipient <dev@example.com>, got %q", message.To)
	}
	if !strings.Contains(message.Subject, "backend-ci (job-1)") || !strings.Contains(message.Subject, "build-1") || !strings.Contains(message.Subject, "failed") {
		t.Fatalf("expected subject to include job/build/status context, got %q", message.Subject)
	}
	for _, want := range []string{"A Coyote CI build failed.", "Build ID: build-1", "Status: failed", "Project: Payments API (project-1)", "Build number: 42", "Job: backend-ci (job-1)"} {
		if !strings.Contains(message.Body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, message.Body)
		}
	}

	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent delivery status, got %q", delivery.Status)
	}
	if delivery.SentAt == nil || delivery.SentAt.IsZero() {
		t.Fatal("expected sent_at to be recorded")
	}
}

func TestBuildService_CompleteBuild_SendsNotificationWhenConfigured(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, BuildNumber: 42, Status: domain.BuildStatusRunning, CreatedAt: now}}
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: "project-1", Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, JobRepo: jobRepo, ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CompleteBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected successful build, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email, got %d", len(sender.messages))
	}
	message := sender.messages[0]
	if message.To != "<dev@example.com>" {
		t.Fatalf("expected normalized recipient <dev@example.com>, got %q", message.To)
	}
	if !strings.Contains(message.Subject, "backend-ci (job-1)") || !strings.Contains(message.Subject, "build-1") || !strings.Contains(message.Subject, "succeeded") {
		t.Fatalf("expected subject to include job/build/success context, got %q", message.Subject)
	}
	for _, want := range []string{"A Coyote CI build succeeded.", "Build ID: build-1", "Status: success", "Project: Payments API (project-1)", "Build number: 42", "Job: backend-ci (job-1)"} {
		if !strings.Contains(message.Body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, message.Body)
		}
	}

	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildSucceeded, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent delivery status, got %q", delivery.Status)
	}
	if delivery.SentAt == nil || delivery.SentAt.IsZero() {
		t.Fatal("expected sent_at to be recorded")
	}
}

func TestBuildService_FailBuild_EmailIncludesCoyoteEntityLinksWhenPublicURLSet(t *testing.T) {
	now := time.Now().UTC()
	jobID := "job-1"
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", JobID: &jobID, BuildNumber: 42, Status: domain.BuildStatusRunning, CreatedAt: now}}
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: "project-1", Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	sender := &recordingEmailSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, JobRepo: jobRepo, ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email, got %d", len(sender.messages))
	}
	body := sender.messages[0].Body
	for _, want := range []string{"Project: Payments API (project-1)", "Project detail: https://ci.example.com/projects/project-1", "Job: backend-ci (job-1)", "Job detail: https://ci.example.com/jobs/job-1", "Build detail: https://ci.example.com/builds/build-1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %q", want, body)
		}
	}
}

func TestBuildService_FailBuild_DoesNotSendNotificationWhenDisabled(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: false, Recipients: "dev@example.com", Sender: sender})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), "build-1"); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails when disabled, got %d", len(sender.messages))
	}
}

func TestBuildService_NotificationsDisabled_DoesNotSendForSuccessOrFailure(t *testing.T) {
	tests := []struct {
		name   string
		action func(*BuildService, context.Context, string) (domain.Build, error)
		want   domain.BuildStatus
	}{
		{name: "success", action: (*BuildService).CompleteBuild, want: domain.BuildStatusSuccess},
		{name: "failure", action: (*BuildService).FailBuild, want: domain.BuildStatusFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
			sender := &recordingEmailSender{}
			notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: false, Recipients: "dev@example.com", Sender: sender})
			if err != nil {
				t.Fatalf("create notifier failed: %v", err)
			}

			svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
			build, err := tc.action(svc, context.Background(), "build-1")
			if err != nil {
				t.Fatalf("terminal transition returned error: %v", err)
			}
			if build.Status != tc.want {
				t.Fatalf("expected build status %q, got %q", tc.want, build.Status)
			}
			if len(sender.messages) != 0 {
				t.Fatalf("expected no notification emails when disabled, got %d", len(sender.messages))
			}
		})
	}
}

func TestBuildService_FailBuild_DoesNotSendNotificationWithoutRecipients(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Sender: sender})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build, got %q", build.Status)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails without recipients, got %d", len(sender.messages))
	}
}

func TestBuildService_FailBuild_SenderFailureDoesNotBreakPersistence(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.FailBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build to remain persisted, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected attempted notification email, got %d", len(sender.messages))
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "<dev@example.com>")
	if delivery.Status != domain.NotificationDeliveryStatusRetryWaiting {
		t.Fatalf("expected retry-waiting delivery status, got %q", delivery.Status)
	}
	if delivery.LastError == nil || !strings.Contains(*delivery.LastError, "smtp unavailable") {
		t.Fatalf("expected last_error to contain smtp failure, got %v", delivery.LastError)
	}
}

func TestBuildService_CompleteBuild_SenderFailureDoesNotBreakPersistence(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{err: errors.New("smtp unavailable")}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CompleteBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusSuccess {
		t.Fatalf("expected successful build to remain persisted, got %q", build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected attempted notification email, got %d", len(sender.messages))
	}
}

func TestBuildService_CancelBuild_DoesNotSendNotificationWhenConfigured(t *testing.T) {
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: time.Now().UTC()}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	build, err := svc.CancelBuild(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("cancel build returned error: %v", err)
	}
	if build.Status != domain.BuildStatusCanceled {
		t.Fatalf("expected canceled build, got %q", build.Status)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no notification emails for canceled builds, got %d", len(sender.messages))
	}
}

func TestBuildService_FailBuild_UsesPersistedProvenanceForCommitAuthorNotifications(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	sender := &recordingEmailSender{}
	userRepo := memoryrepo.NewUserRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	user := mustCreateNotificationUser(t, userRepo, "author@example.com")
	mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, "author@example.com", true)
	mustUpsertNotificationPreference(t, preferenceRepo, user.ID, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, UserRepo: userRepo, PreferenceRepo: preferenceRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	queuedBuild, err := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued, RepoURL: readOptionalStringPtrForTest("https://github.com/example/repo.git"), Ref: readOptionalStringPtrForTest("main")}, nil)
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if queuedBuild.SourceAuthorEmail != nil {
		t.Fatalf("expected original build value to lack author email, got %v", queuedBuild.SourceAuthorEmail)
	}

	originalReadMetadata := readWorkspaceCommitMetadata
	t.Cleanup(func() { readWorkspaceCommitMetadata = originalReadMetadata })
	readWorkspaceCommitMetadata = func(context.Context, string) (source.CommitMetadata, error) {
		return source.CommitMetadata{AuthorName: "Author Example", AuthorEmail: "author@example.com"}, nil
	}

	resolver := &fakeWorkspaceSourceResolver{resolvedCommit: "deadbeef"}
	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	svc.SetSourceResolver(resolver)
	svc.SetExecutionWorkspaceRoot(t.TempDir())

	preparedBuild, err := svc.PrepareBuildExecution(context.Background(), queuedBuild.ID)
	if err != nil {
		t.Fatalf("prepare build execution returned error: %v", err)
	}
	if preparedBuild.Status != domain.BuildStatusRunning {
		t.Fatalf("expected prepared build to be running, got %q", preparedBuild.Status)
	}
	if preparedBuild.SourceAuthorEmail == nil || *preparedBuild.SourceAuthorEmail != "author@example.com" {
		t.Fatalf("expected prepare build execution to persist author email, got %v", preparedBuild.SourceAuthorEmail)
	}

	failedBuild, err := svc.FailBuild(context.Background(), queuedBuild.ID)
	if err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if failedBuild.Status != domain.BuildStatusFailed {
		t.Fatalf("expected failed build, got %q", failedBuild.Status)
	}
	if failedBuild.SourceAuthorEmail == nil || *failedBuild.SourceAuthorEmail != "author@example.com" {
		t.Fatalf("expected failed build returned from repo to include author email, got %v", failedBuild.SourceAuthorEmail)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("expected default plus author notification, got %d", len(sender.messages))
	}

	seen := map[string]bool{}
	for _, message := range sender.messages {
		seen[message.To] = true
	}
	for _, recipient := range []string{"<dev@example.com>", "<author@example.com>"} {
		if !seen[recipient] {
			t.Fatalf("expected notification for %s, got %v", recipient, seen)
		}
		if delivery := mustGetNotificationDelivery(t, deliveryRepo, queuedBuild.ID, domain.NotificationEventTypeBuildFailed, recipient); delivery.Status != domain.NotificationDeliveryStatusSent {
			t.Fatalf("expected sent delivery for %s, got %q", recipient, delivery.Status)
		}
	}
	if queuedBuild.SourceAuthorEmail != nil {
		t.Fatalf("expected stale original build value to remain unchanged, got %v", queuedBuild.SourceAuthorEmail)
	}
	storedBuild, err := buildRepo.GetByID(context.Background(), queuedBuild.ID)
	if err != nil {
		t.Fatalf("reload persisted build failed: %v", err)
	}
	if storedBuild.SourceAuthorEmail == nil || *storedBuild.SourceAuthorEmail != "author@example.com" {
		t.Fatalf("expected persisted build author email, got %v", storedBuild.SourceAuthorEmail)
	}
	if failedBuild.SourceAuthorEmail == nil || *failedBuild.SourceAuthorEmail != "author@example.com" {
		t.Fatalf("expected terminal build used for notification to include persisted author email, got %v", failedBuild.SourceAuthorEmail)
	}
}

func TestBuildService_FailBuild_SendsCommitAuthorNotificationWhenUserOptedIn(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	userRepo := memoryrepo.NewUserRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{}
	authorEmail := "author@example.com"
	user := mustCreateNotificationUser(t, userRepo, authorEmail)
	mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, authorEmail, true)
	mustUpsertNotificationPreference(t, preferenceRepo, user.ID, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, UserRepo: userRepo, PreferenceRepo: preferenceRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	queuedBuild, err := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued}, nil)
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if _, err := buildRepo.UpdateSourceProvenance(context.Background(), queuedBuild.ID, repository.SourceProvenanceUpdate{CommitSHA: "deadbeef", AuthorEmail: "author@example.com"}); err != nil {
		t.Fatalf("persist source provenance failed: %v", err)
	}
	if _, err := buildRepo.UpdateStatus(context.Background(), queuedBuild.ID, domain.BuildStatusRunning, nil); err != nil {
		t.Fatalf("transition to running failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), queuedBuild.ID); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("expected configured recipient plus opted-in author recipient, got %d", len(sender.messages))
	}
	seen := map[string]bool{}
	for _, message := range sender.messages {
		seen[message.To] = true
	}
	for _, recipient := range []string{"<dev@example.com>", "<author@example.com>"} {
		if !seen[recipient] {
			t.Fatalf("expected recipient %s, got %+v", recipient, sender.messages)
		}
	}
	if delivery := mustGetNotificationDelivery(t, deliveryRepo, queuedBuild.ID, domain.NotificationEventTypeBuildFailed, "<author@example.com>"); delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected author delivery, got %q", delivery.Status)
	}
	if delivery := mustGetNotificationDelivery(t, deliveryRepo, queuedBuild.ID, domain.NotificationEventTypeBuildFailed, "<dev@example.com>"); delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected configured recipient delivery, got %q", delivery.Status)
	}
}

func TestBuildService_CompleteBuild_DoesNotIncludeCommitAuthorOnSuccess(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	queuedBuild, err := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued}, nil)
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if _, err := buildRepo.UpdateSourceProvenance(context.Background(), queuedBuild.ID, repository.SourceProvenanceUpdate{CommitSHA: "deadbeef", AuthorEmail: "author@example.com"}); err != nil {
		t.Fatalf("persist source provenance failed: %v", err)
	}
	if _, err := buildRepo.UpdateStatus(context.Background(), queuedBuild.ID, domain.BuildStatusRunning, nil); err != nil {
		t.Fatalf("transition to running failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.CompleteBuild(context.Background(), queuedBuild.ID); err != nil {
		t.Fatalf("complete build returned error: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected only configured success recipient, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<dev@example.com>" {
		t.Fatalf("expected configured success recipient, got %q", sender.messages[0].To)
	}
	if _, err := deliveryRepo.GetByBuildEventRecipient(context.Background(), queuedBuild.ID, domain.NotificationEventTypeBuildSucceeded, "<author@example.com>"); !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected no author success delivery record, got %v", err)
	}
}

func TestBuildService_FailBuild_DedupesCommitAuthorAgainstConfiguredRecipients(t *testing.T) {
	buildRepo := memoryrepo.NewBuildRepository()
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	queuedBuild, err := buildRepo.CreateQueuedBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusQueued}, nil)
	if err != nil {
		t.Fatalf("create queued build failed: %v", err)
	}
	if _, err := buildRepo.UpdateSourceProvenance(context.Background(), queuedBuild.ID, repository.SourceProvenanceUpdate{CommitSHA: "deadbeef", AuthorEmail: "dev@example.com"}); err != nil {
		t.Fatalf("persist source provenance failed: %v", err)
	}
	if _, err := buildRepo.UpdateStatus(context.Background(), queuedBuild.ID, domain.BuildStatusRunning, nil); err != nil {
		t.Fatalf("transition to running failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	if _, err := svc.FailBuild(context.Background(), queuedBuild.ID); err != nil {
		t.Fatalf("fail build returned error: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected deduped configured/author recipient, got %d", len(sender.messages))
	}
	if sender.messages[0].To != "<dev@example.com>" {
		t.Fatalf("expected deduped recipient, got %q", sender.messages[0].To)
	}
	if delivery := mustGetNotificationDelivery(t, deliveryRepo, queuedBuild.ID, domain.NotificationEventTypeBuildFailed, "<dev@example.com>"); delivery.Attempts != 1 {
		t.Fatalf("expected one deduped delivery attempt, got %d", delivery.Attempts)
	}
	if _, err := deliveryRepo.GetByBuildEventRecipient(context.Background(), queuedBuild.ID, domain.NotificationEventTypeBuildFailed, "<author@example.com>"); !errors.Is(err, repository.ErrNotificationDeliveryNotFound) {
		t.Fatalf("expected no second author delivery after dedupe, got %v", err)
	}
}

func TestBuildService_HandleStepResult_FailedStepThenFailBuild_DoesNotDoubleSendEmail(t *testing.T) {
	claimToken := "claim-active"
	buildRepo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CurrentStepIndex: 0, CreatedAt: time.Now().UTC()}, steps: []domain.BuildStep{{StepIndex: 0, Name: "step-1", Status: domain.BuildStepStatusRunning, ClaimToken: &claimToken}, {StepIndex: 1, Name: "step-2", Status: domain.BuildStepStatusPending}}}
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	svc := NewBuildServiceFromConfig(buildRepo, nil, nil, BuildServiceConfig{BuildNotifier: notifier})
	report, err := svc.HandleStepResult(context.Background(), steprunner.RunStepRequest{BuildID: "build-1", StepIndex: 0, StepName: "step-1", ClaimToken: claimToken}, steprunner.RunStepResult{Status: steprunner.RunStepStatusFailed, ExitCode: 7, Stderr: "boom", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("handle step result returned error: %v", err)
	}
	if report.CompletionOutcome != repository.StepCompletionCompleted {
		t.Fatalf("expected completion to persist, got %q", report.CompletionOutcome)
	}
	if buildRepo.build.Status != domain.BuildStatusFailed {
		t.Fatalf("expected build failed after step completion, got %q", buildRepo.build.Status)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one notification email after failed step completion, got %d", len(sender.messages))
	}

	if _, err := svc.FailBuild(context.Background(), "build-1"); !errors.Is(err, ErrInvalidBuildStatusTransition) {
		t.Fatalf("expected invalid transition when failing an already failed build, got %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected no second notification email after invalid FailBuild, got %d", len(sender.messages))
	}
}

func TestBuildNotificationService_SendSampleBuildFailure(t *testing.T) {
	sender := &recordingEmailSender{}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com, qa@example.com", Sender: sender})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	recipients, err := notifier.SendSampleBuildFailure(context.Background())
	if err != nil {
		t.Fatalf("sample email returned error: %v", err)
	}
	if len(recipients) != 2 || recipients[0] != "<dev@example.com>" || recipients[1] != "<qa@example.com>" {
		t.Fatalf("unexpected recipients: %v", recipients)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("expected one message per recipient, got %d", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0].Subject, "sample build failure") {
		t.Fatalf("expected sample subject, got %q", sender.messages[0].Subject)
	}
	if !strings.Contains(sender.messages[0].Body, "POST /api/dev/notifications/sample-build") {
		t.Fatalf("expected sample endpoint hint in body, got %q", sender.messages[0].Body)
	}
}

func TestBuildNotificationService_SendSampleBuildFailureRequiresEnabledConfig(t *testing.T) {
	disabledNotifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: false, Recipients: "dev@example.com", Sender: &recordingEmailSender{}})
	if err != nil {
		t.Fatalf("create disabled notifier failed: %v", err)
	}
	if _, sendErr := disabledNotifier.SendSampleBuildFailure(context.Background()); !errors.Is(sendErr, ErrEmailNotificationsDisabled) {
		t.Fatalf("expected disabled error, got %v", sendErr)
	}

	noRecipientsNotifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Sender: &recordingEmailSender{}})
	if err != nil {
		t.Fatalf("create no-recipient notifier failed: %v", err)
	}
	if _, sendErr := noRecipientsNotifier.SendSampleBuildFailure(context.Background()); !errors.Is(sendErr, ErrEmailNotificationRecipientsNotConfigured) {
		t.Fatalf("expected no recipient error, got %v", sendErr)
	}
}

func TestNewBuildNotificationService_DisabledIgnoresInvalidRecipients(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: false, Recipients: "not-an-email", Sender: &recordingEmailSender{}})
	if err != nil {
		t.Fatalf("expected disabled notifier to ignore invalid recipients, got %v", err)
	}
	if notifier == nil {
		t.Fatal("expected notifier")
	}
	if len(notifier.defaultRecipients) != 0 {
		t.Fatalf("expected disabled notifier to keep no parsed recipients, got %v", notifier.defaultRecipients)
	}
}

func TestNewBuildNotificationService_EnabledRejectsInvalidRecipients(t *testing.T) {
	_, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "not-an-email", Sender: &recordingEmailSender{}})
	if err == nil {
		t.Fatal("expected invalid recipient error")
	}
}

func TestBuildNotificationService_NotifyTerminalBuild(t *testing.T) {
	t.Run("nil service is noop", func(t *testing.T) {
		var notifier *BuildNotificationService
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); notifyErr != nil {
			t.Fatalf("expected nil service to noop, got %v", notifyErr)
		}
	})

	t.Run("no sender returns error", func(t *testing.T) {
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed}); notifyErr == nil {
			t.Fatal("expected missing sender error")
		}
	})

	t.Run("non failed terminal status is ignored", func(t *testing.T) {
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusCanceled}); notifyErr != nil {
			t.Fatalf("expected canceled status to be ignored, got %v", notifyErr)
		}
		if len(sender.messages) != 0 {
			t.Fatalf("expected no email for canceled status, got %d", len(sender.messages))
		}
	})

	t.Run("successful terminal status sends email", func(t *testing.T) {
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusSuccess}); notifyErr != nil {
			t.Fatalf("expected success status to send, got %v", notifyErr)
		}
		if len(sender.messages) != 1 {
			t.Fatalf("expected one email for success status, got %d", len(sender.messages))
		}
		if !strings.Contains(sender.messages[0].Subject, "succeeded") {
			t.Fatalf("expected success subject, got %q", sender.messages[0].Subject)
		}
	})

	t.Run("configured recipients send without subscription repo", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "Dev <dev@example.com>", Sender: sender, DeliveryRepo: deliveryRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		build := domain.Build{ID: "build-no-subscriptions", Status: domain.BuildStatusSuccess}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
			t.Fatalf("expected configured-recipient notifier without subscriptions to send, got %v", notifyErr)
		}
		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
			t.Fatalf("expected duplicate configured-recipient notify to skip cleanly, got %v", notifyErr)
		}
		if len(sender.messages) != 1 {
			t.Fatalf("expected one sent email after duplicate notify, got %d", len(sender.messages))
		}
		if sender.messages[0].To != "\"Dev\" <dev@example.com>" {
			t.Fatalf("expected normalized configured recipient, got %q", sender.messages[0].To)
		}

		delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-no-subscriptions", domain.NotificationEventTypeBuildSucceeded, "\"Dev\" <dev@example.com>")
		if delivery.DestinationKey != "email-config:dev@example.com" {
			t.Fatalf("expected stable configured-recipient destination key, got %q", delivery.DestinationKey)
		}
		if delivery.Status != domain.NotificationDeliveryStatusSent {
			t.Fatalf("expected sent delivery status, got %q", delivery.Status)
		}
		if delivery.Attempts != 1 {
			t.Fatalf("expected one delivery attempt, got %d", delivery.Attempts)
		}
	})

	t.Run("failed builds can include commit author when enabled", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		userRepo := memoryrepo.NewUserRepository()
		preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		authorEmail := "author@example.com"
		user := mustCreateNotificationUser(t, userRepo, authorEmail)
		mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, authorEmail, true)
		mustUpsertNotificationPreference(t, preferenceRepo, user.ID, true)
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, UserRepo: userRepo, PreferenceRepo: preferenceRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 2 {
			t.Fatalf("expected default plus author recipient, got %d", len(sender.messages))
		}
	})

	t.Run("commit author recipient remains distinct from configured shared target", func(t *testing.T) {
		deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
		sender := &recordingEmailSender{}
		userRepo := memoryrepo.NewUserRepository()
		preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
		subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
		authorEmail := "dev@example.com"
		user := mustCreateNotificationUser(t, userRepo, authorEmail)
		mustEnsureOwnedNotificationTarget(t, subscriptionRepo, user.ID, authorEmail, true)
		mustUpsertNotificationPreference(t, preferenceRepo, user.ID, true)
		notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: sender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, UserRepo: userRepo, PreferenceRepo: preferenceRepo})
		if err != nil {
			t.Fatalf("create notifier failed: %v", err)
		}

		if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
			t.Fatalf("notify terminal build failed: %v", notifyErr)
		}
		if len(sender.messages) != 2 {
			t.Fatalf("expected configured shared and personal author targets to remain distinct, got %d", len(sender.messages))
		}
	})
}

func TestNewBuildNotificationService_RejectsClaimDurationBelowProviderSafetyMargin(t *testing.T) {
	_, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, ClaimDuration: minimumNotificationClaimDuration() - time.Second})
	if err == nil {
		t.Fatal("expected claim duration validation error")
	}
	if !strings.Contains(err.Error(), "notification claim duration") {
		t.Fatalf("expected claim duration error, got %v", err)
	}
}

func TestBuildNotificationService_SendSampleBuildFailureRequiresSender(t *testing.T) {
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	if _, sendErr := notifier.SendSampleBuildFailure(context.Background()); sendErr == nil {
		t.Fatal("expected missing sender error")
	}
}

func TestBuildNotificationService_IsActive(t *testing.T) {
	var nilNotifier *BuildNotificationService
	if nilNotifier.isActive() {
		t.Fatal("expected nil notifier to be inactive")
	}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, Recipients: "dev@example.com", Sender: &recordingEmailSender{}, SubscriptionRepo: memoryrepo.NewNotificationSubscriptionRepository()})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	if !notifier.isActive() {
		t.Fatal("expected notifier to be active")
	}
}
