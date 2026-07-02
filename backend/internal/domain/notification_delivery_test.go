package domain

import (
	"testing"
	"time"
)

func TestNotificationDeliveryValidateIdentity(t *testing.T) {
	tests := []struct {
		name     string
		delivery NotificationDelivery
		wantErr  bool
	}{
		{
			name: "shared email valid",
			delivery: NotificationDelivery{
				BuildID:         "build-1",
				EventType:       NotificationEventTypeBuildFailed,
				Transport:       NotificationTransportEmail,
				DestinationKind: NotificationDestinationKindSharedTarget,
				DestinationKey:  "email-target:target-1",
			},
		},
		{
			name: "personal email valid",
			delivery: NotificationDelivery{
				BuildID:         "build-1",
				EventType:       NotificationEventTypeBuildFailed,
				Transport:       NotificationTransportEmail,
				DestinationKind: NotificationDestinationKindPersonalEmail,
				DestinationKey:  "email-personal:target-1",
			},
		},
		{
			name: "slack webhook valid",
			delivery: NotificationDelivery{
				BuildID:         "build-1",
				EventType:       NotificationEventTypeBuildFailed,
				Transport:       NotificationTransportSlackWebhook,
				DestinationKind: NotificationDestinationKindSharedTarget,
				DestinationKey:  "slack-webhook-target:target-1",
			},
		},
		{
			name: "slack dm valid",
			delivery: NotificationDelivery{
				BuildID:         "build-1",
				EventType:       NotificationEventTypeBuildFailed,
				Transport:       NotificationTransportSlackDM,
				DestinationKind: NotificationDestinationKindSlackIdentity,
				DestinationKey:  "slack-dm:workspace-1:U123",
			},
		},
		{
			name: "blank destination key invalid",
			delivery: NotificationDelivery{
				BuildID:         "build-1",
				EventType:       NotificationEventTypeBuildFailed,
				Transport:       NotificationTransportEmail,
				DestinationKind: NotificationDestinationKindSharedTarget,
			},
			wantErr: true,
		},
		{
			name: "invalid transport kind combination",
			delivery: NotificationDelivery{
				BuildID:         "build-1",
				EventType:       NotificationEventTypeBuildFailed,
				Transport:       NotificationTransportSlackWebhook,
				DestinationKind: NotificationDestinationKindSlackIdentity,
				DestinationKey:  "slack-dm:workspace-1:U123",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.delivery.ValidateIdentity()
			if tc.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected valid delivery identity, got %v", err)
			}
		})
	}
}

func TestNotificationDeliveryDestinationKeyBuilders(t *testing.T) {
	_, sharedEmailKey, err := NotificationSharedEmailTargetKey("target-1")
	if err != nil {
		t.Fatalf("shared email key failed: %v", err)
	}
	if sharedEmailKey != "email-target:target-1" {
		t.Fatalf("unexpected shared email key %q", sharedEmailKey)
	}

	_, personalEmailKey, err := NotificationPersonalEmailTargetKey("target-2")
	if err != nil {
		t.Fatalf("personal email key failed: %v", err)
	}
	if personalEmailKey != "email-personal:target-2" {
		t.Fatalf("unexpected personal email key %q", personalEmailKey)
	}

	_, slackWebhookKey, err := NotificationSharedSlackWebhookTargetKey("target-3")
	if err != nil {
		t.Fatalf("shared slack webhook key failed: %v", err)
	}
	if slackWebhookKey != "slack-webhook-target:target-3" {
		t.Fatalf("unexpected shared slack webhook key %q", slackWebhookKey)
	}

	_, slackDMKey, err := NotificationSlackDMDestinationKey("workspace-1", "U123")
	if err != nil {
		t.Fatalf("slack dm key failed: %v", err)
	}
	if slackDMKey != "slack-dm:workspace-1:U123" {
		t.Fatalf("unexpected slack dm key %q", slackDMKey)
	}
	if slackDMKey == "xoxb-secret" {
		t.Fatal("slack dm key unexpectedly includes secret data")
	}
}

func TestNotificationDeliveryNormalizeAndHelpers(t *testing.T) {
	blank := "   "
	trimmed := " user-1 "
	lastError := " smtp failed "
	delivery := NotificationDelivery{
		BuildID:                     " build-1 ",
		EventType:                   NotificationEventType(" build_failed "),
		Transport:                   NotificationTransport(" email "),
		DestinationKind:             NotificationDestinationKind(" shared_target "),
		DestinationKey:              " email-target:target-1 ",
		NotificationTargetID:        &trimmed,
		RecipientUserID:             &blank,
		SlackWorkspaceIntegrationID: &blank,
		Recipient:                   " <dev@example.com> ",
		Status:                      NotificationDeliveryStatus(" pending "),
		LastError:                   &lastError,
	}

	normalized := delivery.Normalize()
	if normalized.BuildID != "build-1" || normalized.DestinationKey != "email-target:target-1" {
		t.Fatalf("expected normalized identity fields, got %+v", normalized)
	}
	if normalized.NotificationTargetID == nil || *normalized.NotificationTargetID != "user-1" {
		t.Fatalf("expected trimmed target id, got %v", normalized.NotificationTargetID)
	}
	if normalized.RecipientUserID != nil || normalized.SlackWorkspaceIntegrationID != nil {
		t.Fatalf("expected blank optional ids to normalize to nil, got recipient=%v slack=%v", normalized.RecipientUserID, normalized.SlackWorkspaceIntegrationID)
	}
	if normalized.LastError == nil || *normalized.LastError != "smtp failed" {
		t.Fatalf("expected trimmed last error, got %v", normalized.LastError)
	}

	if !NotificationTransportEmail.IsValid() || NotificationTransport("sms").IsValid() {
		t.Fatal("unexpected transport validity result")
	}
	if !NotificationDestinationKindSharedTarget.IsValid() || NotificationDestinationKind("pager").IsValid() {
		t.Fatal("unexpected destination kind validity result")
	}

	if !(NotificationDelivery{Transport: NotificationTransportEmail, DestinationKind: NotificationDestinationKindSharedTarget}).IsTransportDestinationKindValid() {
		t.Fatal("expected email/shared_target to be valid")
	}
	if !(NotificationDelivery{Transport: NotificationTransportEmail, DestinationKind: NotificationDestinationKindPersonalEmail}).IsTransportDestinationKindValid() {
		t.Fatal("expected email/personal_email to be valid")
	}
	if !(NotificationDelivery{Transport: NotificationTransportSlackWebhook, DestinationKind: NotificationDestinationKindSharedTarget}).IsTransportDestinationKindValid() {
		t.Fatal("expected slack_webhook/shared_target to be valid")
	}
	if !(NotificationDelivery{Transport: NotificationTransportSlackDM, DestinationKind: NotificationDestinationKindSlackIdentity}).IsTransportDestinationKindValid() {
		t.Fatal("expected slack_dm/slack_identity to be valid")
	}
	if (NotificationDelivery{Transport: NotificationTransportSlackDM, DestinationKind: NotificationDestinationKindSharedTarget}).IsTransportDestinationKindValid() {
		t.Fatal("expected slack_dm/shared_target to be invalid")
	}

	if got := trimNotificationOptionalString(nil); got != nil {
		t.Fatalf("expected nil optional string, got %v", got)
	}
	if got := trimNotificationOptionalString(&blank); got != nil {
		t.Fatalf("expected blank optional string to normalize to nil, got %v", got)
	}
	if got := trimNotificationOptionalString(&trimmed); got == nil || *got != "user-1" {
		t.Fatalf("expected trimmed optional string, got %v", got)
	}
}

func TestNotificationDeliveryDestinationKeyBuilderErrors(t *testing.T) {
	if _, _, err := NotificationSharedEmailTargetKey("   "); err == nil {
		t.Fatal("expected blank shared email target id to fail")
	}
	if _, _, err := NotificationSharedSlackWebhookTargetKey("   "); err == nil {
		t.Fatal("expected blank shared slack webhook target id to fail")
	}
	if _, _, err := NotificationPersonalEmailTargetKey("   "); err == nil {
		t.Fatal("expected blank personal email target id to fail")
	}
	if _, _, err := NotificationSlackDMDestinationKey("", "U123"); err == nil {
		t.Fatal("expected blank workspace id to fail")
	}
	if _, _, err := NotificationSlackDMDestinationKey("workspace-1", "   "); err == nil {
		t.Fatal("expected blank slack user id to fail")
	}
}

func TestNotificationDeliveryValidateStateInvariants(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	nextAttempt := now.Add(time.Minute)
	claimExpires := now.Add(2 * time.Minute)
	retryable := NotificationDeliveryFailureCategoryRetryable
	permanent := NotificationDeliveryFailureCategoryPermanent
	claimOwner := "worker-a"

	base := NotificationDelivery{
		BuildID:         "build-1",
		EventType:       NotificationEventTypeBuildFailed,
		Transport:       NotificationTransportEmail,
		DestinationKind: NotificationDestinationKindSharedTarget,
		DestinationKey:  "email-target:target-1",
		Recipient:       "<dev@example.com>",
		MaxAttempts:     3,
	}

	tests := []struct {
		name     string
		delivery NotificationDelivery
		wantErr  bool
	}{
		{name: "sending valid", delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusSending
			d.Attempts = 1
			d.LastAttemptAt = &now
			d.ClaimedAt = &now
			d.ClaimExpiresAt = &claimExpires
			d.ClaimedBy = &claimOwner
		})},
		{name: "attempts cannot exceed max", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusPending
			d.Attempts = 4
		})},
		{name: "sending requires attempt", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusSending
			d.Attempts = 0
			d.ClaimedAt = &now
			d.ClaimExpiresAt = &claimExpires
			d.ClaimedBy = &claimOwner
		})},
		{name: "retry waiting requires retryable category", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusRetryWaiting
			d.Attempts = 1
			d.NextAttemptAt = &nextAttempt
			d.FailureCategory = &permanent
		})},
		{name: "retry waiting requires attempts below max", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusRetryWaiting
			d.Attempts = 3
			d.NextAttemptAt = &nextAttempt
			d.FailureCategory = &retryable
		})},
		{name: "exhausted requires exact max attempts", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusFailedExhausted
			d.Attempts = 2
			d.FailureCategory = &retryable
		})},
		{name: "exhausted requires retryable category", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusFailedExhausted
			d.Attempts = 3
			d.FailureCategory = &permanent
		})},
		{name: "permanent requires permanent category", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusFailedPermanent
			d.Attempts = 1
			d.FailureCategory = &retryable
		})},
		{name: "sent requires attempt", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusSent
			d.Attempts = 0
			d.SentAt = &now
		})},
		{name: "terminal status cannot keep retry scheduling", wantErr: true, delivery: withNotificationDelivery(base, func(d *NotificationDelivery) {
			d.Status = NotificationDeliveryStatusFailedPermanent
			d.Attempts = 1
			d.FailureCategory = &permanent
			d.NextAttemptAt = &nextAttempt
		})},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.delivery.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected valid delivery, got %v", err)
			}
		})
	}
}

func withNotificationDelivery(base NotificationDelivery, mutate func(*NotificationDelivery)) NotificationDelivery {
	delivery := base
	mutate(&delivery)
	return delivery
}
