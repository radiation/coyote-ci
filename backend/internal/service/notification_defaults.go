package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *NotificationService) GetNotificationDefaults(ctx context.Context) (NotificationDefaultsState, error) {
	failureEnabled, successEnabled, err := s.getNotificationDefaultsEnabled(ctx)
	if err != nil {
		return NotificationDefaultsState{}, err
	}
	return NotificationDefaultsState{
		DefaultCommitAuthorFailureEmailEnabled: failureEnabled,
		DefaultCommitAuthorSuccessEmailEnabled: successEnabled,
	}, nil
}

func (s *NotificationService) SetNotificationDefaults(ctx context.Context, failureEnabled *bool, successEnabled *bool) (NotificationDefaultsState, error) {
	if failureEnabled == nil && successEnabled == nil {
		return NotificationDefaultsState{}, ErrNotificationDefaultsUpdateRequired
	}
	if s.settingsRepo == nil {
		return NotificationDefaultsState{}, errors.New("notification instance settings repository is not configured")
	}
	now := s.now().UTC()
	settings := domain.NotificationInstanceSettings{
		DefaultCommitAuthorFailureEmailEnabled: true,
		DefaultCommitAuthorSuccessEmailEnabled: false,
		CreatedAt:                              now,
		UpdatedAt:                              now,
	}
	createdAt := now
	if existing, err := s.settingsRepo.Get(ctx); err == nil {
		settings = existing
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, repository.ErrNotificationInstanceSettingsNotFound) {
		return NotificationDefaultsState{}, err
	}
	if failureEnabled != nil {
		settings.DefaultCommitAuthorFailureEmailEnabled = *failureEnabled
	}
	if successEnabled != nil {
		settings.DefaultCommitAuthorSuccessEmailEnabled = *successEnabled
	}
	settings.CreatedAt = createdAt
	settings.UpdatedAt = now
	settings, err := s.settingsRepo.Upsert(ctx, settings)
	if err != nil {
		return NotificationDefaultsState{}, err
	}
	return NotificationDefaultsState{
		DefaultCommitAuthorFailureEmailEnabled: settings.DefaultCommitAuthorFailureEmailEnabled,
		DefaultCommitAuthorSuccessEmailEnabled: settings.DefaultCommitAuthorSuccessEmailEnabled,
	}, nil
}

func (s *NotificationService) getDefaultCommitAuthorFailureEmailEnabled(ctx context.Context) (bool, error) {
	enabled, _, err := s.getNotificationDefaultsEnabled(ctx)
	return enabled, err
}

func (s *NotificationService) getNotificationDefaultsEnabled(ctx context.Context) (bool, bool, error) {
	if s.settingsRepo == nil {
		return true, false, nil
	}
	settings, err := s.settingsRepo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationInstanceSettingsNotFound) {
			return true, false, nil
		}
		return false, false, fmt.Errorf("get notification instance settings: %w", err)
	}
	return settings.DefaultCommitAuthorFailureEmailEnabled, settings.DefaultCommitAuthorSuccessEmailEnabled, nil
}
