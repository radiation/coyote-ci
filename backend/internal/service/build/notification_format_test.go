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

	if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	if len(slackSender.messages) != 1 {
		t.Fatalf("expected one slack message, got %d", len(slackSender.messages))
	}
	if len(slackSender.webhookURLs) != 1 || slackSender.webhookURLs[0] != "https://hooks.slack.example/services/T/B/X" {
		t.Fatalf("unexpected slack webhook urls %v", slackSender.webhookURLs)
	}
	message := slackSender.messages[0].Text
	for _, want := range []string{":x: Build failed: backend-ci", "Project: <https://ci.example.com/projects/project-1|Payments API>", "Job: <https://ci.example.com/jobs/job-1|backend-ci>", "Build: <https://ci.example.com/builds/build-1|#42 (build-1)>", "Commit: <https://github.com/example/payments/commit/deadbeefcafebabedeadbeefcafebabedeadbeef|main @ deadbee>", "Author: Octo Cat (octo@example.com)", "Duration: 3m0s", "Diagnostic: <https://ci.example.com/builds/build-1|View build details>", "Build details: <https://ci.example.com/builds/build-1|View build>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
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

	if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	message := slackSender.messages[0].Text
	wantReason := truncateNotificationText(stepError, maxNotificationFailureMessageLength)
	for _, want := range []string{"Failed step: <https://ci.example.com/builds/build-1?step=0|Step 1 deploy &lt;prod&gt;>", "Reason: " + slackEscapeMrkdwnLabel(wantReason), "Exit code: 7", "Diagnostic: <https://ci.example.com/builds/build-1?step=0|Open failed step logs>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
		}
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

	if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	message := slackClient.messages[0].Text
	if !strings.Contains(message, "Next: <https://ci.example.com/builds/build-1?step=0|Open failed step logs>") {
		t.Fatalf("expected personal slack dm to use failed-step diagnostic label, got %q", message)
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

	if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", Status: domain.BuildStatusFailed, ErrorMessage: &buildError, SourceAuthorEmail: &authorEmail}); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
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

	if err := notifier.NotifyTerminalBuild(context.Background(), build); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
	}
	message := slackSender.messages[0].Text
	for _, want := range []string{"Artifacts: <https://ci.example.com/artifacts/artifact-a|pkg-a.tgz (1.2.3)>", "<https://ci.example.com/artifacts/artifact-b|pkg-b.tgz>", "<https://ci.example.com/artifacts/artifact-c|pkg-c.tgz>", "<https://ci.example.com/artifacts?build_id=build-1|+1 more>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected slack text to contain %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "artifact-d") {
		t.Fatalf("expected overflow artifacts to stay concise, got %q", message)
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

	if err := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-1", ProjectID: projectID, Status: domain.BuildStatusSuccess}); err != nil {
		t.Fatalf("notify terminal build failed: %v", err)
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
