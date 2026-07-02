package domain

import (
	"strings"
	"testing"
)

func TestNormalizeNotificationTarget(t *testing.T) {
	blankOwner := "   "
	tests := []struct {
		name       string
		target     NotificationTarget
		wantType   NotificationTargetType
		wantOrigin NotificationTargetOrigin
		wantName   string
		wantOwner  *string
		wantRecip  string
		wantErr    string
	}{
		{
			name: "defaults email and manual origin",
			target: NotificationTarget{
				OwnerUserID: &blankOwner,
				Name:        " Dev Mailbox ",
				Recipient:   " dev@example.com ",
				Enabled:     true,
			},
			wantType:   NotificationTargetTypeEmail,
			wantOrigin: NotificationTargetOriginManual,
			wantName:   "Dev Mailbox",
			wantRecip:  "<dev@example.com>",
		},
		{
			name: "normalizes slack webhook target",
			target: NotificationTarget{
				Type:      NotificationTargetTypeSlackWebhook,
				Origin:    NotificationTargetOriginManual,
				Name:      " Build Alerts ",
				Recipient: " https://hooks.slack.example/services/T/B/X ",
				Enabled:   true,
			},
			wantType:   NotificationTargetTypeSlackWebhook,
			wantOrigin: NotificationTargetOriginManual,
			wantName:   "Build Alerts",
			wantRecip:  "https://hooks.slack.example/services/T/B/X",
		},
		{
			name: "allows ownerless config default email",
			target: NotificationTarget{
				Type:      NotificationTargetTypeEmail,
				Origin:    NotificationTargetOriginConfigDefault,
				Recipient: "alerts@example.com",
				Enabled:   true,
			},
			wantType:   NotificationTargetTypeEmail,
			wantOrigin: NotificationTargetOriginConfigDefault,
			wantRecip:  "<alerts@example.com>",
		},
		{
			name: "rejects unsupported target type",
			target: NotificationTarget{
				Type:      NotificationTargetType("sms"),
				Origin:    NotificationTargetOriginManual,
				Recipient: "dev@example.com",
			},
			wantErr: `unsupported notification target type "sms"`,
		},
		{
			name: "rejects invalid email recipient",
			target: NotificationTarget{
				Type:      NotificationTargetTypeEmail,
				Origin:    NotificationTargetOriginManual,
				Recipient: "bad-email",
			},
			wantErr: `invalid notification target recipient "bad-email"`,
		},
		{
			name: "rejects non-https webhook",
			target: NotificationTarget{
				Type:      NotificationTargetTypeSlackWebhook,
				Origin:    NotificationTargetOriginManual,
				Recipient: "http://hooks.slack.example/services/T/B/X",
			},
			wantErr: "notification target webhook_url must be a valid https URL",
		},
		{
			name: "rejects config default slack target",
			target: NotificationTarget{
				Type:      NotificationTargetTypeSlackWebhook,
				Origin:    NotificationTargetOriginConfigDefault,
				Recipient: "https://hooks.slack.example/services/T/B/X",
			},
			wantErr: "config-default notification targets must be email targets",
		},
		{
			name: "rejects owned config default target",
			target: NotificationTarget{
				Type:        NotificationTargetTypeEmail,
				Origin:      NotificationTargetOriginConfigDefault,
				Recipient:   "owned@example.com",
				OwnerUserID: targetStringPtr("user-1"),
			},
			wantErr: "config-default notification targets must be ownerless",
		},
		{
			name: "rejects unsupported origin",
			target: NotificationTarget{
				Type:      NotificationTargetTypeEmail,
				Origin:    NotificationTargetOrigin("legacy"),
				Recipient: "legacy@example.com",
			},
			wantErr: `unsupported notification target origin "legacy"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			normalized, err := NormalizeNotificationTarget(tc.target)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize target failed: %v", err)
			}
			if normalized.Type != tc.wantType {
				t.Fatalf("expected type %q, got %q", tc.wantType, normalized.Type)
			}
			if normalized.Origin != tc.wantOrigin {
				t.Fatalf("expected origin %q, got %q", tc.wantOrigin, normalized.Origin)
			}
			if normalized.Name != tc.wantName {
				t.Fatalf("expected name %q, got %q", tc.wantName, normalized.Name)
			}
			if normalized.Recipient != tc.wantRecip {
				t.Fatalf("expected recipient %q, got %q", tc.wantRecip, normalized.Recipient)
			}
			if tc.wantOwner == nil {
				if normalized.OwnerUserID != nil {
					t.Fatalf("expected nil owner, got %v", *normalized.OwnerUserID)
				}
			} else if normalized.OwnerUserID == nil || *normalized.OwnerUserID != *tc.wantOwner {
				t.Fatalf("expected owner %v, got %v", tc.wantOwner, normalized.OwnerUserID)
			}
		})
	}
}

func TestValidateExplicitNotificationTarget(t *testing.T) {
	_, err := ValidateExplicitNotificationTarget(NotificationTarget{
		Type:      NotificationTargetTypeEmail,
		Recipient: "dev@example.com",
	})
	if err == nil || err.Error() != "notification target origin is required" {
		t.Fatalf("expected explicit origin error, got %v", err)
	}

	normalized, err := ValidateExplicitNotificationTarget(NotificationTarget{
		Type:      NotificationTargetTypeEmail,
		Origin:    NotificationTargetOriginManual,
		Recipient: "dev@example.com",
	})
	if err != nil {
		t.Fatalf("validate explicit target failed: %v", err)
	}
	if normalized.Recipient != "<dev@example.com>" {
		t.Fatalf("expected normalized explicit recipient, got %q", normalized.Recipient)
	}
}

func targetStringPtr(value string) *string {
	return &value
}
