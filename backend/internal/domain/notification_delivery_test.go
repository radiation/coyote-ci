package domain

import "testing"

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
