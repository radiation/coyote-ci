package service

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (s *BuildService) GetBuild(ctx context.Context, id string) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	return build, mapRepoErr(err)
}

func (s *BuildService) ListBuilds(ctx context.Context) ([]domain.Build, error) {
	return s.buildRepo.List(ctx)
}

func (s *BuildService) GetBuildSteps(ctx context.Context, id string) ([]domain.BuildStep, error) {
	steps, err := s.buildRepo.GetStepsByBuildID(ctx, id)
	return steps, mapRepoErr(err)
}

func (s *BuildService) GetBuildLogs(ctx context.Context, id string) ([]logs.BuildLogLine, error) {
	if _, err := s.buildRepo.GetByID(ctx, id); err != nil {
		return nil, mapRepoErr(err)
	}

	reader, ok := s.logSink.(logs.LogReader)
	if !ok {
		return []logs.BuildLogLine{}, nil
	}

	return reader.GetBuildLogs(ctx, id)
}

func (s *BuildService) GetStepLogChunks(ctx context.Context, buildID string, stepIndex int, afterSequence int64, limit int) ([]logs.StepLogChunk, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return nil, mapRepoErr(err)
	}

	reader, ok := s.logSink.(logs.StepLogChunkReader)
	if !ok {
		return []logs.StepLogChunk{}, nil
	}

	return reader.ListStepLogChunks(ctx, buildID, stepIndex, afterSequence, limit)
}

func (s *BuildService) GetBuildArtifacts(ctx context.Context, buildID string) ([]domain.BuildArtifact, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return nil, mapRepoErr(err)
	}
	if s.artifactRepo == nil {
		return []domain.BuildArtifact{}, nil
	}

	artifacts, err := s.artifactRepo.ListByBuildID(ctx, buildID)
	if err != nil {
		return nil, err
	}

	return artifacts, nil
}

func (s *BuildService) OpenBuildArtifact(ctx context.Context, buildID string, artifactID string) (domain.BuildArtifact, io.ReadCloser, error) {
	if _, err := s.buildRepo.GetByID(ctx, buildID); err != nil {
		return domain.BuildArtifact{}, nil, mapRepoErr(err)
	}
	if s.artifactRepo == nil || s.artifactStore == nil {
		return domain.BuildArtifact{}, nil, ErrArtifactNotFound
	}

	meta, err := s.artifactRepo.GetByID(ctx, buildID, artifactID)
	if err != nil {
		if errors.Is(err, repository.ErrArtifactNotFound) {
			return domain.BuildArtifact{}, nil, ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, nil, err
	}

	stream, err := s.artifactStore.Open(ctx, meta.StorageKey)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.BuildArtifact{}, nil, ErrArtifactNotFound
		}
		return domain.BuildArtifact{}, nil, err
	}

	return meta, stream, nil
}

func (s *BuildService) ClaimStepIfPending(ctx context.Context, buildID string, stepIndex int, workerID *string, startedAt time.Time) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ClaimStepIfPending(ctx, buildID, stepIndex, workerID, startedAt)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) ClaimPendingStep(ctx context.Context, buildID string, stepIndex int, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ClaimPendingStep(ctx, buildID, stepIndex, claim)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) ReclaimExpiredStep(ctx context.Context, buildID string, stepIndex int, reclaimBefore time.Time, claim repository.StepClaim) (domain.BuildStep, bool, error) {
	step, claimed, err := s.buildRepo.ReclaimExpiredStep(ctx, buildID, stepIndex, reclaimBefore, claim)
	return step, claimed, mapRepoErr(err)
}

func (s *BuildService) RenewStepLease(ctx context.Context, buildID string, stepIndex int, claimToken string, leaseExpiresAt time.Time) (domain.BuildStep, bool, error) {
	step, outcome, err := s.buildRepo.RenewStepLease(ctx, buildID, stepIndex, claimToken, leaseExpiresAt)
	if err != nil {
		return domain.BuildStep{}, false, mapRepoErr(err)
	}

	if outcome == repository.StepCompletionCompleted {
		return step, true, nil
	}
	if outcome == repository.StepCompletionStaleClaim {
		return step, false, ErrStaleStepClaim
	}
	if outcome == repository.StepCompletionDuplicateTerminal || outcome == repository.StepCompletionInvalidTransition {
		return domain.BuildStep{}, false, ErrInvalidBuildStepTransition
	}

	return domain.BuildStep{}, false, ErrInvalidBuildStepTransition
}

func (s *BuildService) QueueBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.QueueBuildWithTemplate(ctx, id, "")
}

func (s *BuildService) QueueBuildWithTemplate(ctx context.Context, id string, template string) (domain.Build, error) {
	return s.QueueBuildWithTemplateAndCustomSteps(ctx, id, template, nil)
}

func (s *BuildService) QueueBuildWithTemplateAndCustomSteps(ctx context.Context, id string, template string, customSteps []QueueBuildCustomStepInput) (domain.Build, error) {
	build, err := s.buildRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Build{}, mapRepoErr(err)
	}

	if !domain.CanTransitionBuild(build.Status, domain.BuildStatusQueued) {
		return domain.Build{}, ErrInvalidBuildStatusTransition
	}

	normalizedTemplate := strings.ToLower(strings.TrimSpace(template))
	if normalizedTemplate == BuildTemplateCustom {
		steps, err := buildStepsForCustomTemplate(id, customSteps)
		if err != nil {
			return domain.Build{}, err
		}

		return s.buildRepo.QueueBuild(ctx, id, steps)
	}

	steps := buildStepsForTemplate(id, normalizedTemplate)
	return s.buildRepo.QueueBuild(ctx, id, steps)
}

func (s *BuildService) StartBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusRunning, nil)
}

func (s *BuildService) CompleteBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusSuccess, nil)
}

func (s *BuildService) FailBuild(ctx context.Context, id string) (domain.Build, error) {
	return s.transitionBuildStatus(ctx, id, domain.BuildStatusFailed, nil)
}
