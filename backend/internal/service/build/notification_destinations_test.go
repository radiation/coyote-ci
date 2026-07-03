package build

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestBuildNotificationService_NotifyTerminalBuild_PersonalSlackDMDistinctFromEmailDelivery(t *testing.T) {
	emailSender := &recordingEmailSender{}
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
	mustEnsureOwnedNotificationTarget(t, subscriptionRepo, authorUser.ID, authorEmail, true)
	if _, err := preferenceRepo.Upsert(context.Background(), domain.UserNotificationPreference{
		UserID:                          authorUser.ID,
		CommitAuthorFailureEmailEnabled: true,
		CommitAuthorFailureSlackEnabled: true,
		CommitAuthorFailureEmailSource:  domain.UserNotificationPreferenceSourceUser,
		CreatedAt:                       time.Now().UTC(),
		UpdatedAt:                       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert notification preference failed: %v", err)
	}
	now := time.Now().UTC()
	if _, err := workspaceRepo.ConnectOrReplace(context.Background(), domain.SlackWorkspaceIntegration{
		ID:             "workspace-integration-1",
		WorkspaceID:    "T123",
		BotTokenSecret: "xoxb-secret",
		Enabled:        true,
		ConnectedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, true); err != nil {
		t.Fatalf("connect slack workspace failed: %v", err)
	}
	if _, err := identityRepo.Upsert(context.Background(), domain.UserSlackIdentity{
		ID:                          "identity-1",
		UserID:                      authorUser.ID,
		SlackWorkspaceIntegrationID: "workspace-integration-1",
		SlackUserID:                 "U123",
		Enabled:                     true,
		LinkedAt:                    now,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}); err != nil {
		t.Fatalf("upsert slack identity failed: %v", err)
	}

	notifier, err := NewBuildNotificationService(BuildNotificationConfig{
		Enabled:          true,
		Sender:           emailSender,
		SlackClient:      slackClient,
		DeliveryRepo:     deliveryRepo,
		SubscriptionRepo: subscriptionRepo,
		UserRepo:         userRepo,
		PreferenceRepo:   preferenceRepo,
		IdentityRepo:     identityRepo,
		WorkspaceRepo:    workspaceRepo,
	})
	if err != nil {
		t.Fatalf("create notifier failed: %v", err)
	}

	if notifyErr := notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-dm-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &authorEmail}); notifyErr != nil {
		t.Fatalf("notify terminal build failed: %v", notifyErr)
	}
	if len(emailSender.messages) != 1 || emailSender.messages[0].To != "<author@example.com>" {
		t.Fatalf("expected one email delivery, got %+v", emailSender.messages)
	}
	if len(slackClient.userIDs) != 1 || slackClient.userIDs[0] != "U123" {
		t.Fatalf("expected one slack dm delivery to U123, got %+v", slackClient.userIDs)
	}
	if len(slackClient.tokens) != 1 || slackClient.tokens[0] != "xoxb-secret" {
		t.Fatalf("expected slack bot token to be used, got %+v", slackClient.tokens)
	}
	if !strings.Contains(slackClient.messages[0].Text, "Build failed") {
		t.Fatalf("expected slack dm message to contain build status, got %q", slackClient.messages[0].Text)
	}
	emailDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-dm-1", domain.NotificationEventTypeBuildFailed, "<author@example.com>")
	if emailDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent email delivery, got %q", emailDelivery.Status)
	}
	slackDelivery := mustGetNotificationDelivery(t, deliveryRepo, "build-dm-1", domain.NotificationEventTypeBuildFailed, "slack_dm:workspace-integration-1:U123")
	if slackDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected sent slack dm delivery, got %q", slackDelivery.Status)
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalCommitAuthorPreferenceMatrix(t *testing.T) {
	tests := []struct {
		name         string
		status       domain.BuildStatus
		buildID      string
		options      personalCommitAuthorNotificationOptions
		wantEmails   int
		wantSlackDMs int
		eventType    domain.NotificationEventType
	}{
		{name: "failure neither", status: domain.BuildStatusFailed, buildID: "build-failure-neither", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: false, failureSlackEnabled: false, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 0, wantSlackDMs: 0, eventType: domain.NotificationEventTypeBuildFailed},
		{name: "failure email only", status: domain.BuildStatusFailed, buildID: "build-failure-email", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: false, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 1, wantSlackDMs: 0, eventType: domain.NotificationEventTypeBuildFailed},
		{name: "failure slack only", status: domain.BuildStatusFailed, buildID: "build-failure-slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: false, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 0, wantSlackDMs: 1, eventType: domain.NotificationEventTypeBuildFailed},
		{name: "failure both", status: domain.BuildStatusFailed, buildID: "build-failure-both", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 1, wantSlackDMs: 1, eventType: domain.NotificationEventTypeBuildFailed},
		{name: "success neither", status: domain.BuildStatusSuccess, buildID: "build-success-neither", options: personalCommitAuthorNotificationOptions{successEmailEnabled: false, successSlackEnabled: false, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 0, wantSlackDMs: 0, eventType: domain.NotificationEventTypeBuildSucceeded},
		{name: "success email only", status: domain.BuildStatusSuccess, buildID: "build-success-email", options: personalCommitAuthorNotificationOptions{successEmailEnabled: true, successSlackEnabled: false, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 1, wantSlackDMs: 0, eventType: domain.NotificationEventTypeBuildSucceeded},
		{name: "success slack only", status: domain.BuildStatusSuccess, buildID: "build-success-slack", options: personalCommitAuthorNotificationOptions{successEmailEnabled: false, successSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 0, wantSlackDMs: 1, eventType: domain.NotificationEventTypeBuildSucceeded},
		{name: "success both", status: domain.BuildStatusSuccess, buildID: "build-success-both", options: personalCommitAuthorNotificationOptions{successEmailEnabled: true, successSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 1, wantSlackDMs: 1, eventType: domain.NotificationEventTypeBuildSucceeded},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPersonalCommitAuthorNotificationFixture(t, tc.options)
			build := domain.Build{ID: tc.buildID, Status: tc.status, SourceAuthorEmail: &fixture.authorEmail}
			if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
				t.Fatalf("notify terminal build failed: %v", notifyErr)
			}
			if len(fixture.emailSender.messages) != tc.wantEmails {
				t.Fatalf("expected %d emails, got %d", tc.wantEmails, len(fixture.emailSender.messages))
			}
			if len(fixture.slackClient.userIDs) != tc.wantSlackDMs {
				t.Fatalf("expected %d slack dms, got %d", tc.wantSlackDMs, len(fixture.slackClient.userIDs))
			}

			emailRecipient := "<" + fixture.authorEmail + ">"
			slackRecipient := "slack_dm:" + fixture.workspaceID + ":" + fixture.slackUserID
			if tc.wantEmails == 1 {
				if delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, tc.buildID, tc.eventType, emailRecipient); delivery.Status != domain.NotificationDeliveryStatusSent {
					t.Fatalf("expected email delivery to be sent, got %q", delivery.Status)
				}
			} else {
				assertNotificationDeliveryAbsent(t, fixture.deliveryRepo, tc.buildID, tc.eventType, emailRecipient)
			}
			if tc.wantSlackDMs == 1 {
				if delivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, tc.buildID, tc.eventType, slackRecipient); delivery.Status != domain.NotificationDeliveryStatusSent {
					t.Fatalf("expected slack dm delivery to be sent, got %q", delivery.Status)
				}
			} else {
				assertNotificationDeliveryAbsent(t, fixture.deliveryRepo, tc.buildID, tc.eventType, slackRecipient)
			}
		})
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalChannelAvailabilityMatrix(t *testing.T) {
	tests := []struct {
		name         string
		options      personalCommitAuthorNotificationOptions
		wantEmails   int
		wantSlackDMs int
	}{
		{name: "email target missing only blocks email", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 0, wantSlackDMs: 1},
		{name: "email target disabled only blocks email", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: false, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 0, wantSlackDMs: 1},
		{name: "workspace missing only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createIdentity: true, identityEnabled: true}, wantEmails: 1, wantSlackDMs: 0},
		{name: "workspace disabled only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: false, createIdentity: true, identityEnabled: true}, wantEmails: 1, wantSlackDMs: 0},
		{name: "identity missing only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true}, wantEmails: 1, wantSlackDMs: 0},
		{name: "identity disabled only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: false}, wantEmails: 1, wantSlackDMs: 0},
		{name: "workspace mismatch only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true, identityWorkspaceID: "workspace-integration-2"}, wantEmails: 1, wantSlackDMs: 0},
		{name: "malformed slack user id only blocks slack", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true, slackUserID: "invalid user"}, wantEmails: 1, wantSlackDMs: 0},
		{name: "both unavailable blocks both personal channels", options: personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true}, wantEmails: 0, wantSlackDMs: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPersonalCommitAuthorNotificationFixture(t, tc.options)
			if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-availability-" + strings.ReplaceAll(tc.name, " ", "-"), Status: domain.BuildStatusFailed, SourceAuthorEmail: &fixture.authorEmail}); notifyErr != nil {
				t.Fatalf("notify terminal build failed: %v", notifyErr)
			}
			if len(fixture.emailSender.messages) != tc.wantEmails {
				t.Fatalf("expected %d emails, got %d", tc.wantEmails, len(fixture.emailSender.messages))
			}
			if len(fixture.slackClient.userIDs) != tc.wantSlackDMs {
				t.Fatalf("expected %d slack dms, got %d", tc.wantSlackDMs, len(fixture.slackClient.userIDs))
			}
		})
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalChannelsFailIndependently(t *testing.T) {
	tests := []struct {
		name                  string
		emailErr              error
		slackErr              error
		wantEmailStatus       domain.NotificationDeliveryStatus
		wantSlackStatus       domain.NotificationDeliveryStatus
		wantEmailDeliveries   int
		wantSlackDMDeliveries int
	}{
		{name: "email fails slack succeeds", emailErr: errors.New("smtp unavailable"), wantEmailStatus: domain.NotificationDeliveryStatusRetryWaiting, wantSlackStatus: domain.NotificationDeliveryStatusSent, wantEmailDeliveries: 1, wantSlackDMDeliveries: 1},
		{name: "slack fails email succeeds", slackErr: errors.New("chat.postMessage unavailable"), wantEmailStatus: domain.NotificationDeliveryStatusSent, wantSlackStatus: domain.NotificationDeliveryStatusRetryWaiting, wantEmailDeliveries: 1, wantSlackDMDeliveries: 1},
		{name: "both fail", emailErr: errors.New("smtp unavailable"), slackErr: errors.New("chat.postMessage unavailable"), wantEmailStatus: domain.NotificationDeliveryStatusRetryWaiting, wantSlackStatus: domain.NotificationDeliveryStatusRetryWaiting, wantEmailDeliveries: 1, wantSlackDMDeliveries: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPersonalCommitAuthorNotificationFixture(t, personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true})
			fixture.emailSender.err = tc.emailErr
			fixture.slackClient.err = tc.slackErr

			err := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: "build-transport-" + strings.ReplaceAll(tc.name, " ", "-"), Status: domain.BuildStatusFailed, SourceAuthorEmail: &fixture.authorEmail})
			if tc.emailErr != nil || tc.slackErr != nil {
				if err == nil {
					t.Fatal("expected notify terminal build to return an error")
				}
			} else if err != nil {
				t.Fatalf("notify terminal build failed: %v", err)
			}

			if len(fixture.emailSender.messages) != tc.wantEmailDeliveries {
				t.Fatalf("expected %d email attempts, got %d", tc.wantEmailDeliveries, len(fixture.emailSender.messages))
			}
			if len(fixture.slackClient.userIDs) != tc.wantSlackDMDeliveries {
				t.Fatalf("expected %d slack dm attempts, got %d", tc.wantSlackDMDeliveries, len(fixture.slackClient.userIDs))
			}

			emailDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-transport-"+strings.ReplaceAll(tc.name, " ", "-"), domain.NotificationEventTypeBuildFailed, "<"+fixture.authorEmail+">")
			if emailDelivery.Status != tc.wantEmailStatus {
				t.Fatalf("expected email delivery status %q, got %q", tc.wantEmailStatus, emailDelivery.Status)
			}
			slackDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, "build-transport-"+strings.ReplaceAll(tc.name, " ", "-"), domain.NotificationEventTypeBuildFailed, "slack_dm:"+fixture.workspaceID+":"+fixture.slackUserID)
			if slackDelivery.Status != tc.wantSlackStatus {
				t.Fatalf("expected slack dm delivery status %q, got %q", tc.wantSlackStatus, slackDelivery.Status)
			}
		})
	}
}

func TestBuildNotificationService_NotifyTerminalBuild_PersonalSlackDMDedupesSeparatelyFromEmailAndWebhook(t *testing.T) {
	fixture := newPersonalCommitAuthorNotificationFixture(t, personalCommitAuthorNotificationOptions{failureEmailEnabled: true, failureSlackEnabled: true, createEmailTarget: true, emailTargetEnabled: true, createWorkspace: true, workspaceEnabled: true, createIdentity: true, identityEnabled: true, sharedSlackWebhook: true})
	build := domain.Build{ID: "build-dedupe-1", ProjectID: "project-1", Status: domain.BuildStatusFailed, SourceAuthorEmail: &fixture.authorEmail}

	if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("first notify terminal build failed: %v", notifyErr)
	}
	if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), build); notifyErr != nil {
		t.Fatalf("replayed notify terminal build failed: %v", notifyErr)
	}

	if len(fixture.emailSender.messages) != 1 {
		t.Fatalf("expected one deduped email attempt, got %d", len(fixture.emailSender.messages))
	}
	if len(fixture.slackClient.userIDs) != 1 {
		t.Fatalf("expected one deduped personal slack dm attempt, got %d", len(fixture.slackClient.userIDs))
	}
	if len(fixture.slackSender.webhookURLs) != 1 {
		t.Fatalf("expected one shared slack webhook attempt, got %d", len(fixture.slackSender.webhookURLs))
	}

	emailDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, build.ID, domain.NotificationEventTypeBuildFailed, "<"+fixture.authorEmail+">")
	if emailDelivery.Attempts != 1 || emailDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected one successful email delivery attempt, got status=%q attempts=%d", emailDelivery.Status, emailDelivery.Attempts)
	}
	personalSlackDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, build.ID, domain.NotificationEventTypeBuildFailed, "slack_dm:"+fixture.workspaceID+":"+fixture.slackUserID)
	if personalSlackDelivery.Attempts != 1 || personalSlackDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected one successful personal slack delivery attempt, got status=%q attempts=%d", personalSlackDelivery.Status, personalSlackDelivery.Attempts)
	}
	sharedSlackDelivery := mustGetNotificationDelivery(t, fixture.deliveryRepo, build.ID, domain.NotificationEventTypeBuildFailed, "slack_webhook:"+fixture.sharedSlackTarget.ID)
	if sharedSlackDelivery.Attempts != 1 || sharedSlackDelivery.Status != domain.NotificationDeliveryStatusSent {
		t.Fatalf("expected one successful shared slack webhook delivery attempt, got status=%q attempts=%d", sharedSlackDelivery.Status, sharedSlackDelivery.Attempts)
	}

	if notifyErr := fixture.notifier.NotifyTerminalBuild(context.Background(), domain.Build{ID: build.ID, ProjectID: build.ProjectID, Status: domain.BuildStatusSuccess, SourceAuthorEmail: &fixture.authorEmail}); notifyErr != nil {
		t.Fatalf("success notify terminal build failed: %v", notifyErr)
	}
	if len(fixture.emailSender.messages) != 1 {
		t.Fatalf("expected success event with disabled success preferences not to resend email, got %d", len(fixture.emailSender.messages))
	}
}
