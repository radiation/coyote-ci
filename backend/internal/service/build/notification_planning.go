package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *BuildNotificationService) resolveTerminalDestinations(ctx context.Context, build domain.Build, eventType domain.NotificationEventType) ([]notificationDestination, error) {
	var destinations []notificationDestination
	if s.subscriptionRepo == nil {
		for _, recipient := range s.defaultRecipients {
			destination, err := s.resolveConfiguredEmailDestination(ctx, recipient)
			if err != nil {
				return nil, err
			}
			destinations = append(destinations, destination)
		}
	} else {
		matches, err := s.subscriptionRepo.ListEnabledMatchesForBuildEvent(ctx, build, eventType)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			for _, recipient := range s.defaultRecipients {
				destination, destinationErr := s.resolveConfiguredEmailDestination(ctx, recipient)
				if destinationErr != nil {
					return nil, destinationErr
				}
				destinations = append(destinations, destination)
			}
		} else {
			destinations = make([]notificationDestination, 0, len(matches))
			for _, match := range matches {
				target := match.Target
				destination, destinationErr := notificationTargetDestination(target)
				if destinationErr != nil {
					return nil, destinationErr
				}
				if destination.recipient == "" && destination.emailRecipient == "" && destination.webhookURL == "" {
					continue
				}
				destinations = append(destinations, destination)
			}
		}
	}

	if eventType == domain.NotificationEventTypeBuildFailed || eventType == domain.NotificationEventTypeBuildSucceeded {
		personalDestinations, err := s.resolveCommitAuthorDestinations(ctx, build, eventType)
		if err != nil {
			return nil, err
		}
		destinations = append(destinations, personalDestinations...)
	}

	return dedupeDestinations(destinations), nil
}

func (s *BuildNotificationService) resolveCommitAuthorDestinations(ctx context.Context, build domain.Build, eventType domain.NotificationEventType) ([]notificationDestination, error) {
	if s.userRepo == nil || s.preferenceRepo == nil || s.subscriptionRepo == nil {
		return nil, nil
	}

	authorEmail := normalizeCommitAuthorEmail(build.SourceAuthorEmail)
	if authorEmail == "" {
		return nil, nil
	}

	user, err := s.userRepo.GetByEmail(ctx, authorEmail)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			log.Printf("build notification skipped commit author recipient: build_id=%s reason=author_unmatched", build.ID)
			return nil, nil
		}
		return nil, err
	}

	preference, err := s.preferenceRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var destinations []notificationDestination
	switch eventType {
	case domain.NotificationEventTypeBuildFailed:
		if preference.CommitAuthorFailureEmailEnabled {
			emailDestination, ok, destinationErr := s.resolveCommitAuthorEmailDestination(ctx, user.ID, build.ID)
			if destinationErr != nil {
				return nil, destinationErr
			}
			if ok {
				destinations = append(destinations, emailDestination)
			}
		}
		if preference.CommitAuthorFailureSlackEnabled {
			slackDestination, ok, destinationErr := s.resolveCommitAuthorSlackDestination(ctx, user)
			if destinationErr != nil {
				return nil, destinationErr
			}
			if ok {
				destinations = append(destinations, slackDestination)
			}
		}
	case domain.NotificationEventTypeBuildSucceeded:
		if preference.CommitAuthorSuccessEmailEnabled {
			emailDestination, ok, destinationErr := s.resolveCommitAuthorEmailDestination(ctx, user.ID, build.ID)
			if destinationErr != nil {
				return nil, destinationErr
			}
			if ok {
				destinations = append(destinations, emailDestination)
			}
		}
		if preference.CommitAuthorSuccessSlackEnabled {
			slackDestination, ok, destinationErr := s.resolveCommitAuthorSlackDestination(ctx, user)
			if destinationErr != nil {
				return nil, destinationErr
			}
			if ok {
				destinations = append(destinations, slackDestination)
			}
		}
	}
	return destinations, nil
}

func (s *BuildNotificationService) resolveCommitAuthorEmailDestination(ctx context.Context, userID string, buildID string) (notificationDestination, bool, error) {
	target, err := s.subscriptionRepo.GetOwnedEmailTargetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationTargetNotFound) {
			log.Printf("build notification skipped commit author recipient: build_id=%s reason=personal_target_missing user_id=%s", buildID, userID)
			return notificationDestination{}, false, nil
		}
		return notificationDestination{}, false, err
	}
	if !target.Enabled {
		return notificationDestination{}, false, nil
	}

	recipient := strings.TrimSpace(target.Recipient)
	if recipient == "" {
		return notificationDestination{}, false, nil
	}
	destination, err := personalEmailTargetDestination(target)
	if err != nil {
		return notificationDestination{}, false, err
	}
	return destination, true, nil
}

func (s *BuildNotificationService) resolveCommitAuthorSlackDestination(ctx context.Context, user domain.User) (notificationDestination, bool, error) {
	if s.identityRepo == nil || s.workspaceRepo == nil || s.slackClient == nil {
		return notificationDestination{}, false, nil
	}
	identity, err := s.identityRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
			return notificationDestination{}, false, nil
		}
		return notificationDestination{}, false, err
	}
	if !identity.Enabled {
		return notificationDestination{}, false, nil
	}
	integration, err := s.workspaceRepo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
			return notificationDestination{}, false, nil
		}
		return notificationDestination{}, false, err
	}
	if !integration.Enabled || integration.ID != identity.SlackWorkspaceIntegrationID {
		return notificationDestination{}, false, nil
	}
	if strings.TrimSpace(integration.BotTokenSecret) == "" || !platformslack.IsSlackUserID(strings.TrimSpace(identity.SlackUserID)) {
		return notificationDestination{}, false, nil
	}

	destination, destinationErr := slackDMDestination(user.ID, integration.ID, identity.SlackUserID, integration.BotTokenSecret)
	if destinationErr != nil {
		return notificationDestination{}, false, destinationErr
	}
	return destination, true, nil
}

func (s *BuildNotificationService) resolveConfiguredEmailDestination(ctx context.Context, recipient string) (notificationDestination, error) {
	if s.subscriptionRepo == nil {
		parsedRecipient, ok := parseNotificationRecipient(&recipient)
		if !ok {
			trimmedRecipient := strings.TrimSpace(recipient)
			return notificationDestination{}, fmt.Errorf("invalid email notification recipient %q", trimmedRecipient)
		}
		parsedAddress, err := mail.ParseAddress(parsedRecipient)
		if err != nil {
			return notificationDestination{}, fmt.Errorf("invalid email notification recipient %q: %w", strings.TrimSpace(recipient), err)
		}
		return notificationDestination{
			transport:       domain.NotificationTransportEmail,
			destinationKind: domain.NotificationDestinationKindSharedTarget,
			destinationKey:  "email-config:" + strings.ToLower(strings.TrimSpace(parsedAddress.Address)),
			recipient:       parsedRecipient,
			emailRecipient:  parsedRecipient,
		}, nil
	}
	now := s.now().UTC()
	target, err := s.subscriptionRepo.EnsureConfigEmailTarget(ctx, repository.EnsureConfigNotificationEmailTargetInput{
		Name:      recipient,
		Recipient: recipient,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return notificationDestination{}, err
	}
	return notificationTargetDestination(target)
}

func dedupeDestinations(destinations []notificationDestination) []notificationDestination {
	if len(destinations) == 0 {
		return nil
	}
	result := make([]notificationDestination, 0, len(destinations))
	seen := make(map[string]struct{}, len(destinations))
	for _, destination := range destinations {
		key := strings.TrimSpace(string(destination.transport)) + "|" + strings.TrimSpace(destination.destinationKey)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, destination)
	}
	return result
}

func normalizeCommitAuthorEmail(value *string) string {
	if value == nil {
		return ""
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return ""
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Address))
}
