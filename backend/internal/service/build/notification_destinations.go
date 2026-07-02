package build

import (
	"fmt"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func notificationTargetDestination(target domain.NotificationTarget) (notificationDestination, error) {
	recipient := strings.TrimSpace(target.Recipient)
	if recipient == "" {
		return notificationDestination{}, nil
	}
	if target.Type == domain.NotificationTargetTypeEmail && target.OwnerUserID != nil {
		return personalEmailTargetDestination(target)
	}
	targetID := strings.TrimSpace(target.ID)
	if target.Type == domain.NotificationTargetTypeSlackWebhook {
		destinationKind, destinationKey, err := domain.NotificationSharedSlackWebhookTargetKey(targetID)
		if err != nil {
			return notificationDestination{}, err
		}
		return notificationDestination{
			transport:            domain.NotificationTransportSlackWebhook,
			destinationKind:      destinationKind,
			destinationKey:       destinationKey,
			notificationTargetID: &targetID,
			recipient:            fmt.Sprintf("%s:%s", target.Type, targetID),
			webhookURL:           recipient,
		}, nil
	}
	destinationKind, destinationKey, err := domain.NotificationSharedEmailTargetKey(targetID)
	if err != nil {
		return notificationDestination{}, err
	}
	return notificationDestination{
		transport:            domain.NotificationTransportEmail,
		destinationKind:      destinationKind,
		destinationKey:       destinationKey,
		notificationTargetID: &targetID,
		recipient:            recipient,
		emailRecipient:       recipient,
	}, nil
}

func personalEmailTargetDestination(target domain.NotificationTarget) (notificationDestination, error) {
	targetID := strings.TrimSpace(target.ID)
	destinationKind, destinationKey, err := domain.NotificationPersonalEmailTargetKey(targetID)
	if err != nil {
		return notificationDestination{}, err
	}
	recipient := strings.TrimSpace(target.Recipient)
	return notificationDestination{
		transport:            domain.NotificationTransportEmail,
		destinationKind:      destinationKind,
		destinationKey:       destinationKey,
		notificationTargetID: &targetID,
		recipientUserID:      target.OwnerUserID,
		recipient:            recipient,
		emailRecipient:       recipient,
	}, nil
}

func slackDMDestination(userID string, workspaceIntegrationID string, slackUserID string, slackBotToken string) (notificationDestination, error) {
	destinationKind, destinationKey, err := domain.NotificationSlackDMDestinationKey(workspaceIntegrationID, slackUserID)
	if err != nil {
		return notificationDestination{}, err
	}
	trimmedUserID := strings.TrimSpace(userID)
	trimmedWorkspaceID := strings.TrimSpace(workspaceIntegrationID)
	trimmedSlackUserID := strings.TrimSpace(slackUserID)
	return notificationDestination{
		transport:                   domain.NotificationTransportSlackDM,
		destinationKind:             destinationKind,
		destinationKey:              destinationKey,
		recipientUserID:             &trimmedUserID,
		slackWorkspaceIntegrationID: &trimmedWorkspaceID,
		recipient:                   "slack_dm:" + trimmedWorkspaceID + ":" + trimmedSlackUserID,
		slackUserID:                 trimmedSlackUserID,
		slackBotToken:               strings.TrimSpace(slackBotToken),
	}, nil
}
