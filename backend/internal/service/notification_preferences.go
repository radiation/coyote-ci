package service

import (
	"context"
	"errors"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *NotificationService) GetCommitAuthorFailureNotificationPreference(ctx context.Context, user domain.User) (CommitAuthorNotificationPreferenceState, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return CommitAuthorNotificationPreferenceState{}, ErrNotificationPersonalUserIDRequired
	}

	preference, err := s.getUserNotificationPreference(ctx, ownerUserID)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	emailState, err := s.resolveEmailPreferenceState(ctx, ownerUserID)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	emailState.Enabled = preference.CommitAuthorFailureEmailEnabled
	emailState.DeliveryActive = emailState.Enabled && emailState.DeliveryActive
	slackState, err := s.resolveSlackPreferenceState(ctx, ownerUserID, false)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	slackState.Enabled = preference.CommitAuthorFailureSlackEnabled
	slackState.DeliveryActive = slackState.Enabled && slackState.DeliveryActive
	return CommitAuthorNotificationPreferenceState{
		Email: emailState,
		Slack: slackState,
	}, nil
}

func (s *NotificationService) GetCommitAuthorSuccessNotificationPreference(ctx context.Context, user domain.User) (CommitAuthorNotificationPreferenceState, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return CommitAuthorNotificationPreferenceState{}, ErrNotificationPersonalUserIDRequired
	}

	preference, err := s.getUserNotificationPreference(ctx, ownerUserID)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	emailState, err := s.resolveEmailPreferenceState(ctx, ownerUserID)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	emailState.Enabled = preference.CommitAuthorSuccessEmailEnabled
	emailState.DeliveryActive = emailState.Enabled && emailState.DeliveryActive
	slackState, err := s.resolveSlackPreferenceState(ctx, ownerUserID, false)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	slackState.Enabled = preference.CommitAuthorSuccessSlackEnabled
	slackState.DeliveryActive = slackState.Enabled && slackState.DeliveryActive
	return CommitAuthorNotificationPreferenceState{
		Email: emailState,
		Slack: slackState,
	}, nil
}

func (s *NotificationService) SetCommitAuthorFailureNotificationPreference(ctx context.Context, user domain.User, input UpdateCommitAuthorNotificationPreferenceInput) (CommitAuthorNotificationPreferenceState, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return CommitAuthorNotificationPreferenceState{}, ErrNotificationPersonalUserIDRequired
	}
	if input.EmailEnabled == nil || input.SlackEnabled == nil {
		return CommitAuthorNotificationPreferenceState{}, ErrNotificationPreferenceChannelEnabledRequired
	}
	if err := s.validateCommitAuthorPreferenceChannels(ctx, ownerUserID, *input.EmailEnabled, *input.SlackEnabled); err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	if s.preferencesRepo == nil {
		return CommitAuthorNotificationPreferenceState{}, errors.New("notification preference repository is not configured")
	}

	now := s.now().UTC()
	preference, err := s.getUserNotificationPreference(ctx, ownerUserID)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	preference.UpdatedAt = now
	preference.CommitAuthorFailureEmailEnabled = *input.EmailEnabled
	preference.CommitAuthorFailureSlackEnabled = *input.SlackEnabled
	preference.CommitAuthorFailureEmailSource = domain.UserNotificationPreferenceSourceUser
	_, err = s.preferencesRepo.Upsert(ctx, preference)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}

	return s.GetCommitAuthorFailureNotificationPreference(ctx, user)
}

func (s *NotificationService) SetCommitAuthorSuccessNotificationPreference(ctx context.Context, user domain.User, input UpdateCommitAuthorNotificationPreferenceInput) (CommitAuthorNotificationPreferenceState, error) {
	ownerUserID := strings.TrimSpace(user.ID)
	if ownerUserID == "" {
		return CommitAuthorNotificationPreferenceState{}, ErrNotificationPersonalUserIDRequired
	}
	if input.EmailEnabled == nil || input.SlackEnabled == nil {
		return CommitAuthorNotificationPreferenceState{}, ErrNotificationPreferenceChannelEnabledRequired
	}
	if err := s.validateCommitAuthorPreferenceChannels(ctx, ownerUserID, *input.EmailEnabled, *input.SlackEnabled); err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	if s.preferencesRepo == nil {
		return CommitAuthorNotificationPreferenceState{}, errors.New("notification preference repository is not configured")
	}

	now := s.now().UTC()
	preference, err := s.getUserNotificationPreference(ctx, ownerUserID)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}
	successSource := domain.UserNotificationPreferenceSourceUser
	preference.UpdatedAt = now
	preference.CommitAuthorSuccessEmailEnabled = *input.EmailEnabled
	preference.CommitAuthorSuccessSlackEnabled = *input.SlackEnabled
	preference.CommitAuthorSuccessEmailSource = &successSource
	_, err = s.preferencesRepo.Upsert(ctx, preference)
	if err != nil {
		return CommitAuthorNotificationPreferenceState{}, err
	}

	return s.GetCommitAuthorSuccessNotificationPreference(ctx, user)
}

func (s *NotificationService) getCommitAuthorFailurePreferenceEnabled(ctx context.Context, userID string) (bool, error) {
	if s.preferencesRepo == nil {
		return false, nil
	}
	preference, err := s.preferencesRepo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
			return false, nil
		}
		return false, err
	}
	return preference.CommitAuthorFailureEmailEnabled, nil
}

func (s *NotificationService) getCommitAuthorSuccessPreferenceEnabled(ctx context.Context, userID string) (bool, error) {
	if s.preferencesRepo == nil {
		return false, nil
	}
	preference, err := s.preferencesRepo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
			return false, nil
		}
		return false, err
	}
	return preference.CommitAuthorSuccessEmailEnabled, nil
}

func (s *NotificationService) getUserNotificationPreference(ctx context.Context, userID string) (domain.UserNotificationPreference, error) {
	if s.preferencesRepo == nil {
		return domain.UserNotificationPreference{UserID: userID}, nil
	}
	now := s.now().UTC()
	preference := domain.UserNotificationPreference{
		UserID:                         userID,
		CommitAuthorFailureEmailSource: domain.UserNotificationPreferenceSourceUser,
		CreatedAt:                      now,
		UpdatedAt:                      now,
	}
	stored, err := s.preferencesRepo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
			return preference, nil
		}
		return domain.UserNotificationPreference{}, err
	}
	return stored, nil
}

func (s *NotificationService) resolveEmailPreferenceState(ctx context.Context, userID string) (CommitAuthorEmailNotificationPreferenceState, error) {
	state := CommitAuthorEmailNotificationPreferenceState{}
	target, err := s.targetRepo.GetOwnedEmailTargetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationTargetNotFound) {
			reason := NotificationPreferenceUnavailableReasonPersonalTargetRequired
			state.UnavailableReason = &reason
			return state, nil
		}
		return CommitAuthorEmailNotificationPreferenceState{}, err
	}
	state.Target = &target
	state.DeliveryActive = target.Enabled
	if !target.Enabled {
		reason := NotificationPreferenceUnavailableReasonPersonalTargetDisabled
		state.UnavailableReason = &reason
	}
	return state, nil
}

func (s *NotificationService) resolveSlackPreferenceState(ctx context.Context, userID string, requireDeliverable bool) (CommitAuthorSlackNotificationPreferenceState, error) {
	state := CommitAuthorSlackNotificationPreferenceState{}
	if s.workspaceRepo == nil || s.identityRepo == nil {
		reason := NotificationPreferenceUnavailableReasonSlackWorkspaceNotConfigured
		state.UnavailableReason = &reason
		return state, nil
	}
	integration, err := s.workspaceRepo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
			reason := NotificationPreferenceUnavailableReasonSlackWorkspaceNotConfigured
			state.UnavailableReason = &reason
			return state, nil
		}
		return CommitAuthorSlackNotificationPreferenceState{}, err
	}
	if strings.TrimSpace(integration.BotTokenSecret) == "" {
		reason := NotificationPreferenceUnavailableReasonSlackWorkspaceNotConfigured
		state.UnavailableReason = &reason
		return state, nil
	}
	if !integration.Enabled {
		reason := NotificationPreferenceUnavailableReasonSlackWorkspaceDisabled
		state.UnavailableReason = &reason
		return state, nil
	}
	identity, err := s.identityRepo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
			reason := NotificationPreferenceUnavailableReasonSlackIdentityRequired
			state.UnavailableReason = &reason
			return state, nil
		}
		return CommitAuthorSlackNotificationPreferenceState{}, err
	}
	if strings.TrimSpace(identity.SlackUserID) == "" {
		reason := NotificationPreferenceUnavailableReasonSlackIdentityRequired
		state.UnavailableReason = &reason
		return state, nil
	}
	if !identity.Enabled {
		reason := NotificationPreferenceUnavailableReasonSlackIdentityDisabled
		state.UnavailableReason = &reason
		return state, nil
	}
	if identity.SlackWorkspaceIntegrationID != integration.ID {
		reason := NotificationPreferenceUnavailableReasonSlackWorkspaceMismatch
		state.UnavailableReason = &reason
		return state, nil
	}
	state.DeliveryActive = true
	if requireDeliverable {
		return state, nil
	}
	return state, nil
}

func (s *NotificationService) validateCommitAuthorPreferenceChannels(ctx context.Context, userID string, emailEnabled bool, slackEnabled bool) error {
	if emailEnabled {
		emailState, err := s.resolveEmailPreferenceState(ctx, userID)
		if err != nil {
			return err
		}
		if !emailState.DeliveryActive {
			return ErrNotificationPreferencePersonalTargetRequired
		}
	}
	if slackEnabled {
		state, err := s.resolveSlackPreferenceState(ctx, userID, true)
		if err != nil {
			return err
		}
		if !state.DeliveryActive {
			return ErrNotificationPreferencePersonalSlackRequired
		}
	}
	return nil
}
