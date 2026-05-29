package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *JobService) attachManagedImageConfig(ctx context.Context, job *domain.Job) error {
	if s.managedImageConfigs == nil || job == nil || strings.TrimSpace(job.ID) == "" {
		return nil
	}
	config, err := s.managedImageConfigs.GetByJobID(ctx, job.ID)
	if err != nil {
		if errors.Is(err, repository.ErrJobManagedImageConfigNotFound) {
			return nil
		}
		return err
	}
	job.ManagedImageConfig = &config
	return nil
}

func (s *JobService) upsertManagedImageConfig(ctx context.Context, job domain.Job, input *ManagedImageConfigInput) (domain.JobManagedImageConfig, error) {
	if s.managedImageConfigs == nil || s.credentials == nil {
		return domain.JobManagedImageConfig{}, ErrJobManagedImageConfigNotConfigured
	}
	if input == nil {
		return domain.JobManagedImageConfig{}, repository.ErrJobManagedImageConfigNotFound
	}
	credentialID := strings.TrimSpace(input.WriteCredentialID)
	if _, err := s.credentials.GetByID(ctx, credentialID); err != nil {
		return domain.JobManagedImageConfig{}, err
	}
	now := time.Now().UTC()
	config := domain.JobManagedImageConfig{
		ID:                uuid.NewString(),
		JobID:             job.ID,
		ManagedImageName:  strings.TrimSpace(input.ManagedImageName),
		PipelinePath:      strings.TrimSpace(input.PipelinePath),
		WriteCredentialID: credentialID,
		BotBranchPrefix:   stringOrFallback(input.BotBranchPrefix, "coyote/managed-image-refresh"),
		CommitAuthorName:  stringOrFallback(input.CommitAuthorName, "Coyote CI Bot"),
		CommitAuthorEmail: stringOrFallback(input.CommitAuthorEmail, "bot@coyote-ci.local"),
		Enabled:           input.Enabled,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := validateManagedImageConfig(config); err != nil {
		return domain.JobManagedImageConfig{}, err
	}
	return s.managedImageConfigs.UpsertByJobID(ctx, config)
}

func (s *JobService) patchManagedImageConfig(ctx context.Context, job domain.Job, patch *ManagedImageConfigPatch) (*domain.JobManagedImageConfig, error) {
	if s.managedImageConfigs == nil {
		return nil, ErrJobManagedImageConfigNotConfigured
	}
	if patch == nil {
		if err := s.managedImageConfigs.DeleteByJobID(ctx, job.ID); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if patch.Enabled != nil && !*patch.Enabled {
		if err := s.managedImageConfigs.DeleteByJobID(ctx, job.ID); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if s.credentials == nil {
		return nil, ErrJobManagedImageConfigNotConfigured
	}

	current, err := s.managedImageConfigs.GetByJobID(ctx, job.ID)
	if err != nil {
		if !errors.Is(err, repository.ErrJobManagedImageConfigNotFound) {
			return nil, err
		}
		current = domain.JobManagedImageConfig{
			ID:                uuid.NewString(),
			JobID:             job.ID,
			BotBranchPrefix:   "coyote/managed-image-refresh",
			CommitAuthorName:  "Coyote CI Bot",
			CommitAuthorEmail: "bot@coyote-ci.local",
			Enabled:           true,
			CreatedAt:         time.Now().UTC(),
		}
	}

	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.ManagedImageName != nil {
		current.ManagedImageName = strings.TrimSpace(*patch.ManagedImageName)
	}
	if patch.PipelinePath != nil {
		current.PipelinePath = strings.TrimSpace(*patch.PipelinePath)
	}
	if patch.WriteCredentialID != nil {
		credentialID := strings.TrimSpace(*patch.WriteCredentialID)
		if credentialID == "" {
			return nil, ErrJobManagedImageWriteCredentialIDRequired
		}
		if _, credentialErr := s.credentials.GetByID(ctx, credentialID); credentialErr != nil {
			return nil, credentialErr
		}
		current.WriteCredentialID = credentialID
	}
	if patch.BotBranchPrefix != nil {
		current.BotBranchPrefix = strings.TrimSpace(*patch.BotBranchPrefix)
	}
	if patch.CommitAuthorName != nil {
		current.CommitAuthorName = strings.TrimSpace(*patch.CommitAuthorName)
	}
	if patch.CommitAuthorEmail != nil {
		current.CommitAuthorEmail = strings.TrimSpace(*patch.CommitAuthorEmail)
	}
	current.UpdatedAt = time.Now().UTC()

	if validateErr := validateManagedImageConfig(current); validateErr != nil {
		return nil, validateErr
	}
	updated, err := s.managedImageConfigs.UpsertByJobID(ctx, current)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func validateManagedImageConfig(config domain.JobManagedImageConfig) error {
	if strings.TrimSpace(config.ManagedImageName) == "" {
		return ErrJobManagedImageNameRequired
	}
	if strings.TrimSpace(config.PipelinePath) == "" {
		return ErrJobManagedImagePipelinePathRequired
	}
	if strings.TrimSpace(config.WriteCredentialID) == "" {
		return ErrJobManagedImageWriteCredentialIDRequired
	}
	return nil
}

func stringOrFallback(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
