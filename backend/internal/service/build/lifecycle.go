package build

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

	return s.persistBuildStatus(ctx, id, toStatus, errorMessage)
}

func (s *BuildService) persistBuildStatus(ctx context.Context, id string, toStatus domain.BuildStatus, errorMessage *string) (domain.Build, error) {
	build, err := s.buildRepo.UpdateStatus(ctx, id, toStatus, errorMessage)
	if err != nil {
		return domain.Build{}, err
	}
	s.notifyTerminalBuild(ctx, build)
	return build, nil
}

func (s *BuildService) notifyTerminalBuild(ctx context.Context, build domain.Build) {
	if s.buildNotifier == nil || !domain.IsTerminalBuildStatus(build.Status) {
		return
	}
	if notifyErr := s.buildNotifier.NotifyTerminalBuild(ctx, build); notifyErr != nil {
		log.Printf("WARNING: build notification failed: build_id=%s status=%s err=%v", build.ID, build.Status, notifyErr)
	}
}

func (s *BuildService) CancelBuild(ctx context.Context, id string) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	if !domain.CanCancelBuild(build.Status) {
		log.Printf("cancel rejected: build not cancelable build_id=%s status=%s", id, build.Status)
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	now := time.Now().UTC()
	reason := "build canceled by operator request"
	if repoWithAtomicCancel, ok := s.buildRepo.(interface {
		CancelBuild(ctx context.Context, id string, reason string, canceledAt time.Time) (domain.Build, int, error)
	}); ok {
		canceled, updatedSteps, cancelErr := repoWithAtomicCancel.CancelBuild(ctx, id, reason, now)
		if cancelErr != nil {
			return domain.Build{}, mapRepoErr(cancelErr)
		}
		if !cancelBuildIncludesExecutionJobs(s.buildRepo) {
			s.cancelExecutionJobsForBuild(ctx, id, reason, now)
		}
		log.Printf("cancel applied: build_id=%s status=%s updated_steps=%d", id, canceled.Status, updatedSteps)
		return canceled, nil
	}

	steps, err := s.buildRepo.GetStepsByBuildID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	updatedSteps := 0
	for _, step := range steps {
		if !domain.CanCancelStep(step.Status) {
			continue
		}

		update := repository.StepUpdate{
			Status:       domain.BuildStepStatusCanceled,
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

	canceled, err := s.persistBuildStatus(ctx, id, domain.BuildStatusCanceled, &reason)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}
	s.cancelExecutionJobsForBuild(ctx, id, reason, now)

	log.Printf("cancel applied: build_id=%s status=%s updated_steps=%d", id, canceled.Status, updatedSteps)
	return canceled, nil
}

func cancelBuildIncludesExecutionJobs(repo any) bool {
	repoWithJobCancel, ok := repo.(interface {
		CancelBuildIncludesExecutionJobs() bool
	})
	return ok && repoWithJobCancel.CancelBuildIncludesExecutionJobs()
}

func (s *BuildService) cancelExecutionJobsForBuild(ctx context.Context, id string, reason string, canceledAt time.Time) {
	if s.executionJobRepo == nil {
		return
	}
	repoWithCancel, ok := s.executionJobRepo.(interface {
		CancelJobsForBuild(ctx context.Context, buildID string, reason string, canceledAt time.Time) (int, error)
	})
	if !ok {
		return
	}
	updatedJobs, err := repoWithCancel.CancelJobsForBuild(ctx, id, reason, canceledAt)
	if err != nil {
		log.Printf("cancel jobs failed: build_id=%s err=%v", id, err)
		return
	}
	if updatedJobs > 0 {
		log.Printf("cancel jobs applied: build_id=%s updated_jobs=%d", id, updatedJobs)
	}
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repository.ErrBuildNotFound) {
		return ErrBuildNotFound
	}
	if errors.Is(err, repository.ErrInvalidBuildStatusTransition) {
		return ErrInvalidBuildStatusTransition
	}
	if errors.Is(err, repository.ErrInvalidBuildStepTransition) {
		return ErrInvalidBuildStepTransition
	}
	return err
}
