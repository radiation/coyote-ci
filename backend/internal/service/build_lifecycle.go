package service

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *BuildService) transitionBuildStatus(ctx context.Context, id string, toStatus domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	if !domain.CanTransitionBuild(build.Status, toStatus) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	return s.buildRepo.UpdateStatus(ctx, id, toStatus, errorMessage)
}

func (s *BuildService) CancelBuild(ctx context.Context, id string) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	if domain.IsTerminalBuildStatus(build.Status) {
		log.Printf("cancel ignored: build already terminal build_id=%s status=%s", id, build.Status)
		return build, nil
	}

	steps, err := s.buildRepo.GetStepsByBuildID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	now := time.Now().UTC()
	reason := "build canceled by operator request"
	updatedSteps := 0
	for _, step := range steps {
		if domain.IsTerminalStepStatus(step.Status) {
			continue
		}

		update := repository.StepUpdate{
			Status:       domain.BuildStepStatusFailed,
			ErrorMessage: &reason,
			FinishedAt:   &now,
		}
		if step.StartedAt == nil {
			update.StartedAt = &now
		}

		if _, updateErr := s.buildRepo.UpdateStepByIndex(ctx, id, step.StepIndex, update); updateErr != nil {
			return domain.Build{}, mapRepoErr(updateErr)
		}
		updatedSteps++
	}

	failed, err := s.buildRepo.UpdateStatus(ctx, id, domain.BuildStatusFailed, &reason)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	log.Printf("cancel applied: build_id=%s status=%s updated_steps=%d", id, failed.Status, updatedSteps)
	return failed, nil
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repository.ErrBuildNotFound) {
		return ErrBuildNotFound
	}
	if errors.Is(err, repository.ErrInvalidBuildStepTransition) {
		return ErrInvalidBuildStepTransition
	}
	return err
}
