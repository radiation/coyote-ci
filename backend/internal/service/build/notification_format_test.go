package build

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildNotificationService_NotifyTerminalBuild_SendsSlackWebhookSubscription(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	jobID := "job-1"
	now := time.Now().UTC()
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: projectID, Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: projectID, Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, JobRepo: jobRepo, ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	ref := "refs/heads/main"
	sha := "deadbeefcafebabedeadbeefcafebabedeadbeef"
	authorName := "Octo Cat"
	authorEmail := "octo@example.com"
	startedAt := time.Now().Add(-3 * time.Minute).UTC()
	finishedAt := time.Now().UTC()
	build := domain.Build{ID: "build-1", ProjectID: projectID, JobID: &jobID, Status: domain.BuildStatusFailed, BuildNumber: 42, SourceRef: &ref, SourceSHA: &sha, SourceAuthorName: &authorName, SourceAuthorEmail: &authorEmail, StartedAt: &startedAt, FinishedAt: &finishedAt}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	if len(slackSender.messages) != 1 {
		t.Fatalf("expected one slack message, got %d", len(slackSender.messages))
	}
	if len(slackSender.webhookURLs) != 1 || slackSender.webhookURLs[0] != "https://hooks.slack.example/services/T/B/X" {
		t.Fatalf("unexpected slack webhook urls %v", slackSender.webhookURLs)
	}
	message := slackSender.messages[0].Text
	for _, want := range []string{":x: Build failed: backend-ci", "Project: <https://ci.example.com/projects/project-1|Payments API>", "Job: <https://ci.example.com/jobs/job-1|backend-ci>", "Build: <https://ci.example.com/builds/build-1|#42 (build-1)>", "Commit: <https://github.com/example/payments/commit/deadbeefcafebabedeadbeefcafebabedeadbeef|main @ deadbee>", "Author: Octo Cat (octo@example.com)", "Duration: 3m0s", "Diagnostic: <https://ci.example.com/builds/build-1|View build details>", "CLI:", "`coyote build status build-1`", "`coyote build logs build-1 --failed --tail 200`", "`coyote build retry build-1 --yes`", "Build details: <https://ci.example.com/builds/build-1|View build>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
		}
	}
	for _, unwanted := range []string{"coyote build logs build-1 --step", "coyote build rerun build-1 --yes", "step retry", "artifact download"} {
		if strings.Contains(message, unwanted) {
			t.Fatalf("did not expect slack text to contain %q, got %q", unwanted, message)
		}
	}
	delivery := mustGetNotificationDelivery(t, deliveryRepo, "build-1", domain.NotificationEventTypeBuildFailed, "slack_webhook:"+target.ID)
	if delivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent slack delivery status, got %q", delivery.Status)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackFailureIncludesFailedStepContext(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	jobID := "job-1"
	now := time.Now().UTC()
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: projectID, Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: projectID, Name: "backend-ci", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	exitCode := 7
	stepError := strings.Repeat("deploy <prod> failed after verifying the release artifact manifest. ", 4)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, BuildRepo: &fakeBuildRepository{steps: []domain.BuildStep{{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "deploy <prod>", Status: domain.BuildStepStatusFailed, ExitCode: &exitCode, ErrorMessage: &stepError}}}, JobRepo: jobRepo, ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	ref := "refs/heads/main"
	sha := "deadbeefcafebabedeadbeefcafebabedeadbeef"
	authorName := "Octo Cat"
	authorEmail := "octo@example.com"
	startedAt := time.Now().Add(-3 * time.Minute).UTC()
	finishedAt := time.Now().UTC()
	buildError := "build level failure text should not replace the failed step"
	build := domain.Build{ID: "build-1", ProjectID: projectID, JobID: &jobID, Status: domain.BuildStatusFailed, BuildNumber: 42, SourceRef: &ref, SourceSHA: &sha, SourceAuthorName: &authorName, SourceAuthorEmail: &authorEmail, StartedAt: &startedAt, FinishedAt: &finishedAt, ErrorMessage: &buildError}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	message := slackSender.messages[0].Text
	wantReason := truncateNotificationText(stepError, maxNotificationFailureMessageLength)
	for _, want := range []string{"Failed step: <https://ci.example.com/builds/build-1?step=0|Step 1 deploy &lt;prod&gt;>", "Reason: " + slackEscapeMrkdwnLabel(wantReason), "Exit code: 7", "Diagnostic: <https://ci.example.com/builds/build-1?step=0|Open failed step logs>", "`coyote build status build-1`", "`coyote build logs build-1 --step 0 --tail 200`", "`coyote build retry build-1 --yes`"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "`coyote build logs build-1 --failed --tail 200`") {
		t.Fatalf("expected known failed step to use step-specific logs command, got %q", message)
	}
	if strings.Contains(message, "build level failure text") {
		t.Fatalf("expected failed step message to take precedence, got %q", message)
	}
	if strings.Contains(message, "hooks.slack.example") {
		t.Fatalf("expected slack payload not to contain the webhook url, got %q", message)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalSlackFailureUsesFailedStepDiagnosticLabel(t *testing.T) {
	slackClient := &recordingSlackDMClient{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	userRepo := memoryrepo.NewUserRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	identityRepo := memoryrepo.NewUserSlackIdentityRepository()
	workspaceRepo := memoryrepo.NewSlackWorkspaceIntegrationRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)

	authorEmail := "author@example.com"
	authorUser := mustCreateNotificationUser(t, userRepo, authorEmail)
	if _, err := preferenceRepo.Upsert(context.Background(), domain.UserNotificationPreference{UserID: authorUser.ID, CommitAuthorFailureSlackEnabled: true, CommitAuthorFailureEmailSource: domain.UserNotificationPreferenceSourceUser, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert notification preference failed: %v", err)
	}
	now := time.Now().UTC()
	if _, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{ID: "workspace-integration-1", WorkspaceID: "T123", BotTokenSecret: "xoxb-secret", Enabled: true, ConnectedAt: now, CreatedAt: now, UpdatedAt: now}, true); err != nil {
		t.Fatalf("connect slack workspace failed: %v", err)
	}
	if _, err := identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{ID: "identity-1", UserID: authorUser.ID, SlackWorkspaceIntegrationID: "workspace-integration-1", SlackUserID: "U123", Enabled: true, LinkedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert slack identity failed: %v", err)
	}
	exitCode := 7
	stepError := "deploy failed"
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackClient: slackClient, BuildRepo: &fakeBuildRepository{steps: []domain.BuildStep{{ID: "step-1", BuildID: "build-1", StepIndex: 0, Name: "deploy", Status: domain.BuildStepStatusFailed, ExitCode: &exitCode, ErrorMessage: &stepError}}}, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, UserRepo: userRepo, PreferenceRepo: preferenceRepo, IdentityRepo: identityRepo, WorkspaceRepo: workspaceRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	message := slackClient.messages[0].Text
	if !strings.Contains(message, "Next: <https://ci.example.com/builds/build-1?step=0|Open failed step logs>") {
		t.Fatalf("expected personal slack dm to use failed-step diagnostic label, got %q", message)
	}
	for _, want := range []string{"CLI:", "`coyote build status build-1`", "`coyote build logs build-1 --step 0 --tail 200`", "`coyote build retry build-1 --yes`"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected personal slack dm to contain %q, got %q", want, message)
		}
	}
	if !strings.Contains(message, "Build: <https://ci.example.com/builds/build-1|View build>") {
		t.Fatalf("expected personal slack dm to keep a separate build link when diagnostic differs, got %q", message)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalSlackFailureFallbackUsesBuildDetailsLabelWithoutDuplicateBuildLink(t *testing.T) {
	slackClient := &recordingSlackDMClient{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	userRepo := memoryrepo.NewUserRepository()
	preferenceRepo := memoryrepo.NewUserNotificationPreferenceRepository()
	identityRepo := memoryrepo.NewUserSlackIdentityRepository()
	workspaceRepo := memoryrepo.NewSlackWorkspaceIntegrationRepository()
	workspaceRepo.SetUserSlackIdentityRepository(identityRepo)

	authorEmail := "author@example.com"
	authorUser := mustCreateNotificationUser(t, userRepo, authorEmail)
	if _, err := preferenceRepo.Upsert(context.Background(), domain.UserNotificationPreference{UserID: authorUser.ID, CommitAuthorFailureSlackEnabled: true, CommitAuthorFailureEmailSource: domain.UserNotificationPreferenceSourceUser, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("upsert notification preference failed: %v", err)
	}
	now := time.Now().UTC()
	if _, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{ID: "workspace-integration-1", WorkspaceID: "T123", BotTokenSecret: "xoxb-secret", Enabled: true, ConnectedAt: now, CreatedAt: now, UpdatedAt: now}, true); err != nil {
		t.Fatalf("connect slack workspace failed: %v", err)
	}
	if _, err := identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{ID: "identity-1", UserID: authorUser.ID, SlackWorkspaceIntegrationID: "workspace-integration-1", SlackUserID: "U123", Enabled: true, LinkedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsert slack identity failed: %v", err)
	}
	buildError := "build level failure"
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackClient: slackClient, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, UserRepo: userRepo, PreferenceRepo: preferenceRepo, IdentityRepo: identityRepo, WorkspaceRepo: workspaceRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, ErrorMessage: &buildError, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	message := slackClient.messages[0].Text
	if !strings.Contains(message, "Next: <https://ci.example.com/builds/build-1|View build details>") {
		t.Fatalf("expected personal slack dm fallback label to use build details wording, got %q", message)
	}
	if strings.Contains(message, "Build: <https://ci.example.com/builds/build-1|View build>") {
		t.Fatalf("expected personal slack dm to omit duplicate build link when diagnostic falls back to build url, got %q", message)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackSuccessArtifactsRemainConcise(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	artifactRepo := memoryrepo.NewArtifactRepository()
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)
	versionArtifactID := "artifact-a"
	artifactRepo.SeedBuilds(domain.Build{ID: "build-1", ProjectID: projectID})
	for _, artifact := range []domain.BuildArtifact{{ID: versionArtifactID, BuildID: "build-1", Name: "pkg-a.tgz", LogicalPath: "dist/pkg-a.tgz", VersionTags: []domain.VersionTag{{ID: "tag-1", Kind: domain.VersionTagKindVersion, Version: "1.2.3"}}, CreatedAt: time.Now().UTC()}, {ID: "artifact-b", BuildID: "build-1", Name: "pkg-b.tgz", LogicalPath: "dist/pkg-b.tgz", CreatedAt: time.Now().UTC()}, {ID: "artifact-c", BuildID: "build-1", Name: "pkg-c.tgz", LogicalPath: "dist/pkg-c.tgz", CreatedAt: time.Now().UTC()}, {ID: "artifact-d", BuildID: "build-1", Name: "pkg-d.tgz", LogicalPath: "dist/pkg-d.tgz", CreatedAt: time.Now().UTC()}} {
		if _, err := artifactRepo.Create(context.Background(), artifact); err != nil {
			t.Fatalf("create artifact failed: %v", err)
		}
	}
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, ArtifactRepo: artifactRepo, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	buildID := "build-1"
	finishedAt := time.Now().UTC()
	startedAt := finishedAt.Add(-2 * time.Minute)
	build := domain.Build{ID: buildID, ProjectID: projectID, Status: domain.BuildStatusSuccess, BuildNumber: 42, StartedAt: &startedAt, FinishedAt: &finishedAt}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	message := slackSender.messages[0].Text
	for _, want := range []string{"Artifacts: <https://ci.example.com/artifacts/artifact-a|pkg-a.tgz (1.2.3)>", "<https://ci.example.com/artifacts/artifact-b|pkg-b.tgz>", "<https://ci.example.com/artifacts/artifact-c|pkg-c.tgz>", "<https://ci.example.com/artifacts?build_id=build-1|+1 more>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
		}
	}
	if !strings.Contains(message, "CLI: `coyote build status build-1`") {
		t.Fatalf("expected success slack text to contain status command, got %q", message)
	}
	for _, unwanted := range []string{"coyote build retry build-1 --yes", "coyote build logs build-1"} {
		if strings.Contains(message, unwanted) {
			t.Fatalf("did not expect success slack text to contain %q, got %q", unwanted, message)
		}
	}
	if strings.Contains(message, "artifact-d") {
		t.Fatalf("expected overflow artifacts to stay concise, got %q", message)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackFailureIncludesArtifactTriggerContext(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	jobID := "job-downstream"
	producerProjectID := "project-upstream"
	producerJobID := "job-upstream"
	producerBuildID := "build-upstream"
	now := time.Now().UTC()
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: projectID, Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create downstream project failed: %v", err)
	}
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: producerProjectID, Name: "Artifact Producer", Slug: "artifact-producer", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create producer project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: projectID, Name: "package-consumer", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create downstream job failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: producerJobID, ProjectID: producerProjectID, Name: "package-producer", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create producer job failed: %v", err)
	}
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	buildError := "artifact consumer failed"
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, JobRepo: jobRepo, ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	build := domain.Build{
		ID:        "build-downstream",
		ProjectID: projectID,
		JobID:     &jobID,
		Status:    domain.BuildStatusFailed,
		Trigger: domain.BuildTrigger{
			Kind:              domain.BuildTriggerKindArtifact,
			ProducerProjectID: &producerProjectID,
			ProducerJobID:     &producerJobID,
			ProducerBuildID:   &producerBuildID,
			ArtifactPath:      stringPtr("dist/service-client.jar"),
			ArtifactName:      stringPtr("service-client.jar"),
		},
		ErrorMessage: &buildError,
	}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	message := slackSender.messages[0].Text
	for _, want := range []string{
		"Triggered by: <https://ci.example.com/projects/project-upstream|Artifact Producer> / <https://ci.example.com/jobs/job-upstream|package-producer> / <https://ci.example.com/builds/build-upstream|build-upstream>",
		"Artifact: service-client.jar (dist/service-client.jar)",
		"`coyote build watch build-downstream`",
		"`coyote build artifact-triggers build-upstream`",
		"`coyote build status build-downstream --json`",
		"`coyote build retry build-downstream --yes`",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected artifact-trigger slack text to contain %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "`coyote build status build-downstream`") {
		t.Fatalf("expected artifact-trigger message to prefer json status command, got %q", message)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackSuccessIncludesArtifactTriggerContext(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	jobRepo := memoryrepo.NewJobRepository()
	projectRepo := memoryrepo.NewProjectRepository(jobRepo)
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	jobID := "job-downstream"
	producerProjectID := "project-upstream"
	producerJobID := "job-upstream"
	producerBuildID := "build-upstream"
	now := time.Now().UTC()
	if _, err := projectRepo.Create(context.Background(), domain.Project{ID: projectID, Name: "Payments API", Slug: "payments-api", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create downstream project failed: %v", err)
	}
	if _, err := jobRepo.Create(context.Background(), domain.Job{ID: jobID, ProjectID: projectID, Name: "package-consumer", RepositoryURL: "https://github.com/example/payments.git", DefaultRef: "main", PipelineYAML: "version: 1", Enabled: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create downstream job failed: %v", err)
	}
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, JobRepo: jobRepo, ProjectRepo: projectRepo, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}
	build := domain.Build{
		ID:        "build-downstream",
		ProjectID: projectID,
		JobID:     &jobID,
		Status:    domain.BuildStatusSuccess,
		Trigger: domain.BuildTrigger{
			Kind:              domain.BuildTriggerKindArtifact,
			ProducerProjectID: &producerProjectID,
			ProducerJobID:     &producerJobID,
			ProducerBuildID:   &producerBuildID,
			ArtifactPath:      stringPtr("dist/service-client.jar"),
		},
	}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	message := slackSender.messages[0].Text
	for _, want := range []string{
		"Triggered by: <https://ci.example.com/projects/project-upstream|project-upstream> / <https://ci.example.com/jobs/job-upstream|job-upstream> / <https://ci.example.com/builds/build-upstream|build-upstream>",
		"Artifact: dist/service-client.jar",
		"CLI: `coyote build watch build-downstream`",
		"`coyote build artifact-triggers build-upstream`",
		"`coyote build status build-downstream --json`",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected artifact-trigger success slack text to contain %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "`coyote build status build-downstream`") {
		t.Fatalf("expected artifact-trigger success message to prefer json status command, got %q", message)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackOrdinaryBuildOmitsArtifactTriggerContext(t *testing.T) {
	slackSender := &recordingSlackSender{}
	deliveryRepo := memoryrepo.NewNotificationDeliveryRepository()
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildFailed, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, DeliveryRepo: deliveryRepo, SubscriptionRepo: subscriptionRepo, PublicBaseURL: "https://ci.example.com/"})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-ordinary", ProjectID: projectID, Status: domain.BuildStatusFailed}); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	message := slackSender.messages[0].Text
	for _, unwanted := range []string{"Triggered by:", "Artifact:", "coyote build watch build-ordinary", "coyote build artifact-triggers"} {
		if strings.Contains(message, unwanted) {
			t.Fatalf("did not expect ordinary slack message to contain %q, got %q", unwanted, message)
		}
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_SlackMessageOmitsMissingOptionalFields(t *testing.T) {
	slackSender := &recordingSlackSender{}
	subscriptionRepo := memoryrepo.NewNotificationSubscriptionRepository()
	target := mustCreateSlackNotificationTarget(t, subscriptionRepo, "https://hooks.slack.example/services/T/B/X", true)
	projectID := "project-1"
	mustCreateNotificationSubscription(t, subscriptionRepo, target.ID, &projectID, nil, domain.NotificationEventTypeBuildSucceeded, true)
	notifier, err := NewBuildNotificationService(BuildNotificationConfig{Enabled: true, SlackSender: slackSender, SubscriptionRepo: subscriptionRepo})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusSuccess}); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	message := slackSender.messages[0].Text
	for _, unwanted := range []string{"Git:", "Commit author:", "Build detail:"} {
		if strings.Contains(message, unwanted) {
			t.Fatalf("did not expect slack text to contain %q, got %q", unwanted, message)
		}
	}
}

func TestBuildNotificationHelpers_FrontendEntityURLs(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		projectID string
		jobID     string
		buildID   string
		wantProj  string
		wantJob   string
		wantBuild string
	}{
		{name: "base url without trailing slash", baseURL: "https://ci.example.com", projectID: "project-1", jobID: "job-1", buildID: "build-1", wantProj: "https://ci.example.com/projects/project-1", wantJob: "https://ci.example.com/jobs/job-1", wantBuild: "https://ci.example.com/builds/build-1"},
		{name: "base url with trailing slash", baseURL: "https://ci.example.com/", projectID: "project-1", jobID: "job-1", buildID: "build-1", wantProj: "https://ci.example.com/projects/project-1", wantJob: "https://ci.example.com/jobs/job-1", wantBuild: "https://ci.example.com/builds/build-1"},
		{name: "empty base url falls back", baseURL: "", projectID: "project-1", jobID: "job-1", buildID: "build-1", wantProj: "", wantJob: "", wantBuild: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildProjectDetailURL(tc.baseURL, tc.projectID); got != tc.wantProj {
				t.Fatalf("project url mismatch: got %q want %q", got, tc.wantProj)
			}
			if got := buildJobDetailURL(tc.baseURL, tc.jobID); got != tc.wantJob {
				t.Fatalf("job url mismatch: got %q want %q", got, tc.wantJob)
			}
			if got := buildBuildDetailURL(tc.baseURL, tc.buildID); got != tc.wantBuild {
				t.Fatalf("build url mismatch: got %q want %q", got, tc.wantBuild)
			}
		})
	}
}

func TestBuildNotificationHelpers_GitHubCommitURLs(t *testing.T) {
	fullSHA := "1fccc2972f530f59642f8f88f2f818ca1d2f0f99"
	tests := []struct {
		name   string
		remote string
		sha    string
		want   string
		ok     bool
	}{
		{name: "github https with .git", remote: "https://github.com/owner/repo.git", sha: fullSHA, want: "https://github.com/owner/repo/commit/" + fullSHA, ok: true},
		{name: "github https without .git", remote: "https://github.com/owner/repo", sha: fullSHA, want: "https://github.com/owner/repo/commit/" + fullSHA, ok: true},
		{name: "github scp ssh", remote: "git@github.com:owner/repo.git", sha: fullSHA, want: "https://github.com/owner/repo/commit/" + fullSHA, ok: true},
		{name: "github ssh url", remote: "ssh://git@github.com/owner/repo.git", sha: fullSHA, want: "https://github.com/owner/repo/commit/" + fullSHA, ok: true},
		{name: "unknown host fallback", remote: "https://gitlab.com/owner/repo.git", sha: fullSHA, want: "", ok: false},
		{name: "missing repository remote", remote: "", sha: fullSHA, want: "", ok: false},
		{name: "missing commit sha", remote: "https://github.com/owner/repo.git", sha: "", want: "", ok: false},
		{name: "non-full commit sha fallback", remote: "https://github.com/owner/repo.git", sha: "1fccc29", want: "", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := buildRepositoryCommitURL(tc.remote, tc.sha)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got %v (url=%q)", tc.ok, ok, got)
			}
			if got != tc.want {
				t.Fatalf("commit url mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestFormatBuildStatusSlackText_LinkPolish(t *testing.T) {
	fullSHA := "1fccc2972f530f59642f8f88f2f818ca1d2f0f99"
	shortSHA := shortNotificationSHA(fullSHA)

	t.Run("links and escaped labels with configured urls", func(t *testing.T) {
		details := buildNotificationDetails{statusSummary: "failed", projectName: "Core & <Proj>", projectLabel: "Core & <Proj> (project-1)", projectURL: "https://ci.example.com/projects/project-1", jobName: "Build > Job", jobLabel: "Build > Job (job-1)", jobURL: "https://ci.example.com/jobs/job-1", buildNumber: 42, buildLabel: "#42 (build-1)", buildURL: "https://ci.example.com/builds/build-1", refLabel: "refs/heads/main", shaLabel: shortSHA, commitURL: "https://github.com/owner/repo/commit/" + fullSHA, authorName: "Bryan & <Choate>", authorEmail: "bryan.choate@gmail.com"}

		message := formatBuildStatusSlackText(details)
		for _, want := range []string{":x: Build failed: Build &gt; Job", "Project: <https://ci.example.com/projects/project-1|Core &amp; &lt;Proj&gt;>", "Job: <https://ci.example.com/jobs/job-1|Build &gt; Job>", "Build: <https://ci.example.com/builds/build-1|#42>", "Commit: <https://github.com/owner/repo/commit/" + fullSHA + "|main @ " + shortSHA + ">", "Author: Bryan &amp; &lt;Choate&gt; (bryan.choate@gmail.com)", "Build details: <https://ci.example.com/builds/build-1|View build>"} {
			if !strings.Contains(message, want) {
				t.Fatalf("expected slack text to contain %q, got %q", want, message)
			}
		}
		if strings.Contains(message, "<bryan.choate@gmail.com>") {
			t.Fatalf("expected author to use parentheses, got %q", message)
		}
	})

	t.Run("plain text fallback without urls", func(t *testing.T) {
		details := buildNotificationDetails{statusSummary: "succeeded", projectLabel: "project-1", jobLabel: "job-1", buildLabel: "#42 (build-1)", refLabel: "refs/heads/main", shaLabel: shortSHA, authorName: "Bryan Choate", authorEmail: "bryan.choate@gmail.com"}

		message := formatBuildStatusSlackText(details)
		for _, want := range []string{":white_check_mark: Build succeeded: job-1", "Project: project-1", "Job: job-1", "Build: #42 (build-1)", "Commit: main @ " + shortSHA} {
			if !strings.Contains(message, want) {
				t.Fatalf("expected slack text to contain %q, got %q", want, message)
			}
		}
		if strings.Contains(message, "<http") {
			t.Fatalf("did not expect mrkdwn links without urls, got %q", message)
		}
	})

	t.Run("cli hint helper uses failed-step fallback rules", func(t *testing.T) {
		failedDetails := buildNotificationDetails{statusSummary: "failed", buildID: "build-1", failedStep: &buildNotificationStep{index: 2}}
		joinedFailed := strings.Join(notificationSlackCLIHintLines(failedDetails), "\n")
		for _, want := range []string{"CLI:", "`coyote build status build-1`", "`coyote build logs build-1 --step 2 --tail 200`", "`coyote build retry build-1 --yes`"} {
			if !strings.Contains(joinedFailed, want) {
				t.Fatalf("expected failed cli hints to contain %q, got %q", want, joinedFailed)
			}
		}

		joinedFallback := strings.Join(notificationSlackCLIHintLines(buildNotificationDetails{statusSummary: "failed", buildID: "build-2"}), "\n")
		if !strings.Contains(joinedFallback, "`coyote build logs build-2 --failed --tail 200`") {
			t.Fatalf("expected fallback failed cli hint to use --failed, got %q", joinedFallback)
		}

		joinedSuccess := strings.Join(notificationSlackCLIHintLines(buildNotificationDetails{statusSummary: "succeeded", buildID: "build-3"}), "\n")
		if joinedSuccess != "CLI: `coyote build status build-3`" {
			t.Fatalf("unexpected success cli hint output: %q", joinedSuccess)
		}

		if got := notificationSlackCLIHintLines(buildNotificationDetails{statusSummary: "failed"}); got != nil {
			t.Fatalf("expected missing build id to omit cli hints, got %q", strings.Join(got, "\n"))
		}
	})
}

func TestFormatBuildStatusSlackText_NonFullSHAFallbackStillLinksCoyoteEntities(t *testing.T) {
	details := buildNotificationDetails{statusSummary: "failed", projectName: "Payments API", projectLabel: "Payments API (project-1)", projectURL: "https://ci.example.com/projects/project-1", jobName: "backend-ci", jobLabel: "backend-ci (job-1)", jobURL: "https://ci.example.com/jobs/job-1", buildNumber: 42, buildLabel: "#42 (build-1)", buildURL: "https://ci.example.com/builds/build-1", refLabel: "refs/heads/main", shaLabel: "deadbee", commitURL: "", authorName: "Octo Cat", authorEmail: "octo@example.com"}

	message := formatBuildStatusSlackText(details)
	for _, want := range []string{"Project: <https://ci.example.com/projects/project-1|Payments API>", "Job: <https://ci.example.com/jobs/job-1|backend-ci>", "Build: <https://ci.example.com/builds/build-1|#42>", "Commit: main @ deadbee", "Author: Octo Cat (octo@example.com)"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "github.com") {
		t.Fatalf("did not expect commit link for non-full sha, got %q", message)
	}
}

func TestBuildNotificationHelpers_GitRefLabel(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "refs/heads/ prefix", ref: "refs/heads/main", want: "main"},
		{name: "refs/tags/ prefix", ref: "refs/tags/v1.2.3", want: "v1.2.3"},
		{name: "plain branch name", ref: "develop", want: "develop"},
		{name: "empty ref", ref: "", want: ""},
		{name: "whitespace-only ref", ref: "   ", want: ""},
		{name: "refs/heads/ with special chars", ref: "refs/heads/feature/PROJ-123", want: "feature/PROJ-123"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := slackGitRefLabel(tc.ref); got != tc.want {
				t.Fatalf("git ref label mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestBuildNotificationHelpers_RepositoryRemoteParsing(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		wantHost  string
		wantOwner string
		wantRepo  string
		wantOk    bool
	}{
		{name: "https url with trailing .git", remote: "https://github.com/acme/myrepo.git", wantHost: "github.com", wantOwner: "acme", wantRepo: "myrepo", wantOk: true},
		{name: "scp-style git url", remote: "git@github.com:acme/myrepo.git", wantHost: "github.com", wantOwner: "acme", wantRepo: "myrepo", wantOk: true},
		{name: "ssh:// url with .git", remote: "ssh://git@github.com/acme/myrepo.git", wantHost: "github.com", wantOwner: "acme", wantRepo: "myrepo", wantOk: true},
		{name: "missing owner", remote: "https://github.com//myrepo.git", wantHost: "", wantOwner: "", wantRepo: "", wantOk: false},
		{name: "malformed git@ url without colon", remote: "git@github.com/acme/myrepo", wantHost: "", wantOwner: "", wantRepo: "", wantOk: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, owner, repo, ok := parseRepositoryRemote(tc.remote)
			if ok != tc.wantOk {
				t.Fatalf("expected ok=%v, got %v", tc.wantOk, ok)
			}
			if host != tc.wantHost {
				t.Fatalf("host mismatch: got %q want %q", host, tc.wantHost)
			}
			if owner != tc.wantOwner {
				t.Fatalf("owner mismatch: got %q want %q", owner, tc.wantOwner)
			}
			if repo != tc.wantRepo {
				t.Fatalf("repo mismatch: got %q want %q", repo, tc.wantRepo)
			}
		})
	}
}

func TestBuildNotificationHelpers_NewBranches(t *testing.T) {
	t.Run("headline and build labels fall back cleanly", func(t *testing.T) {
		headline := formatBuildStatusHeadline(buildNotificationDetails{statusSummary: "failed", buildNumber: 42})
		if headline != ":x: Build failed: #42" {
			t.Fatalf("unexpected headline fallback: %q", headline)
		}
		headline = formatBuildStatusHeadline(buildNotificationDetails{statusSummary: "succeeded", buildID: "build-1"})
		if headline != ":white_check_mark: Build succeeded: build-1" {
			t.Fatalf("unexpected build-id headline fallback: %q", headline)
		}
		if got := notificationBuildLinkLabel(buildNotificationDetails{buildNumber: 7}); got != "#7" {
			t.Fatalf("unexpected build link label without build id: %q", got)
		}
		if got := notificationBuildLinkLabel(buildNotificationDetails{buildLabel: " build-9 "}); got != "build-9" {
			t.Fatalf("unexpected build label fallback: %q", got)
		}
	})

	t.Run("step helpers select sorted failed step and handle misses", func(t *testing.T) {
		stepError := "boom"
		exitCode := 7
		steps := []domain.BuildStep{
			{StepIndex: 3, Name: "deploy", Status: domain.BuildStepStatusFailed, ErrorMessage: &stepError, ExitCode: &exitCode},
			{StepIndex: 1, Name: "setup", Status: domain.BuildStepStatusSuccess},
			{StepIndex: 2, Name: "verify", Status: domain.BuildStepStatusFailed},
		}
		failed := buildNotificationFailedStep("https://ci.example.com", "build-1", steps)
		if failed == nil || failed.index != 2 || failed.label != "Step 3 verify" || failed.url != "https://ci.example.com/builds/build-1?step=2" {
			t.Fatalf("unexpected failed step details: %+v", failed)
		}
		if got := notificationStepSlackText(buildNotificationStep{label: "Step <3>"}); got != "Step &lt;3&gt;" {
			t.Fatalf("unexpected plain-text step label: %q", got)
		}
		if got := formatNotificationStepLabel(domain.BuildStep{StepIndex: 0, Name: "   "}); got != "Step 1" {
			t.Fatalf("unexpected blank-name step label: %q", got)
		}
		if got := stepErrorPointer(steps, 3); got == nil || *got != "boom" {
			t.Fatalf("expected step error pointer, got %v", got)
		}
		if got := stepExitCodePointer(steps, 3); got == nil || *got != 7 {
			t.Fatalf("expected step exit pointer, got %v", got)
		}
		if got := stepErrorPointer(steps, 99); got != nil {
			t.Fatalf("expected missing step error to return nil, got %v", *got)
		}
		if got := stepExitCodePointer(steps, 99); got != nil {
			t.Fatalf("expected missing step exit code to return nil, got %v", *got)
		}
		if got := buildNotificationFailedStep("https://ci.example.com", "build-1", []domain.BuildStep{{StepIndex: 0, Status: domain.BuildStepStatusSuccess}}); got != nil {
			t.Fatalf("expected no failed step, got %+v", got)
		}
	})

	t.Run("artifact helpers handle fallback labels sorting and overflow", func(t *testing.T) {
		if links := buildNotificationArtifactLinks("", []domain.BuildArtifact{{ID: "artifact-1"}}); links != nil {
			t.Fatalf("expected nil links without base url, got %+v", links)
		}
		artifacts := []domain.BuildArtifact{
			{ID: "artifact-3", Name: "", LogicalPath: "", VersionTags: []domain.VersionTag{{Kind: domain.VersionTagKindChannel, Version: "latest"}}},
			{ID: "artifact-2", Name: "pkg-b.tgz", LogicalPath: "dist/pkg-b.tgz"},
			{ID: "artifact-1", Name: "pkg-a.tgz", LogicalPath: "dist/pkg-a.tgz", VersionTags: []domain.VersionTag{{Kind: domain.VersionTagKindVersion, Version: "1.2.3"}}},
			{ID: "", Name: "ignored"},
		}
		links := buildNotificationArtifactLinks("https://ci.example.com", artifacts)
		if len(links) != 3 {
			t.Fatalf("expected three artifact links, got %+v", links)
		}
		if links[0].url != "https://ci.example.com/artifacts/artifact-3" || links[0].label != "artifact-3 (latest)" {
			t.Fatalf("unexpected first artifact link: %+v", links[0])
		}
		if links[1].url != "https://ci.example.com/artifacts/artifact-1" || links[1].label != "pkg-a.tgz (1.2.3)" {
			t.Fatalf("unexpected second artifact link: %+v", links[1])
		}
		if links[2].label != "pkg-b.tgz" {
			t.Fatalf("unexpected third artifact label: %+v", links[2])
		}
		line := formatNotificationArtifactSlackLine(buildNotificationDetails{artifacts: links[:2], artifactCount: 4})
		if !strings.Contains(line, "Artifacts: ") || !strings.Contains(line, "+2 more") || strings.Contains(line, "artifacts?") {
			t.Fatalf("unexpected artifact slack overflow line: %q", line)
		}
		if got := notificationArtifactSortKey(domain.BuildArtifact{ID: "artifact-9", Name: "  ", LogicalPath: "  "}); got != "artifact-9" {
			t.Fatalf("unexpected artifact sort fallback: %q", got)
		}
	})

	t.Run("truncate and minimum helpers cover edge cases", func(t *testing.T) {
		if got := truncateNotificationText("  keep me  ", 0); got != "keep me" {
			t.Fatalf("unexpected zero-limit truncate result: %q", got)
		}
		if got := truncateNotificationText("abcd", 1); got != "a" {
			t.Fatalf("unexpected one-rune truncate result: %q", got)
		}
		want := "ab" + string(rune(0x2026))
		if got := truncateNotificationText("abcd", 3); got != want {
			t.Fatalf("unexpected truncated label: got %q want %q", got, want)
		}
		if got := minNotificationInt(2, 5); got != 2 {
			t.Fatalf("unexpected min result for left-smaller case: %d", got)
		}
		if got := minNotificationInt(7, 3); got != 3 {
			t.Fatalf("unexpected min result for right-smaller case: %d", got)
		}
	})
}

func TestBuildNotificationHelpers_SlackStatusIndicator(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{status: "succeeded", want: ":white_check_mark:"},
		{status: "failed", want: ":x:"},
		{status: "cancelled", want: ":information_source:"},
		{status: "", want: ":information_source:"},
	}

	for _, tc := range tests {
		if got := slackStatusIndicator(tc.status); got != tc.want {
			t.Fatalf("slackStatusIndicator(%q): got %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestBuildNotificationHelpers_ShortNotificationSHA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "abc1234def5678", want: "abc1234"},
		{input: "abc", want: "abc"},
		{input: "   deadbeef   ", want: "deadbee"},
		{input: "", want: ""},
	}

	for _, tc := range tests {
		if got := shortNotificationSHA(tc.input); got != tc.want {
			t.Fatalf("shortNotificationSHA(%q): got %q, want %q", tc.input, got, tc.want)
		}
	}
}
