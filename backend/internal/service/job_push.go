package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type PushEventInput struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
}

type PushEventMatchedBuild struct {
	Job   domain.Job
	Build domain.Build
}

type PushEventResult struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
	MatchedJobs   int
	Builds        []PushEventMatchedBuild
}

func (s *JobService) TriggerPushEvent(ctx context.Context, input PushEventInput) (PushEventResult, error) {
	if s.buildService == nil {
		return PushEventResult{}, ErrJobBuildServiceNotConfigured
	}

	repoURL := strings.TrimSpace(input.RepositoryURL)
	if repoURL == "" {
		return PushEventResult{}, ErrPushEventRepositoryURLRequired
	}

	ref := normalizePushRef(input.Ref)
	if ref == "" {
		return PushEventResult{}, ErrPushEventRefRequired
	}

	commitSHA := strings.TrimSpace(input.CommitSHA)
	if commitSHA == "" {
		return PushEventResult{}, ErrPushEventCommitSHARequired
	}

	jobs, err := s.jobRepo.ListPushEnabledByRepository(ctx, repoURL)
	if err != nil {
		return PushEventResult{}, err
	}

	result := PushEventResult{
		RepositoryURL: repoURL,
		Ref:           ref,
		CommitSHA:     commitSHA,
		Builds:        make([]PushEventMatchedBuild, 0),
	}

	for _, job := range jobs {
		if !matchesPushBranch(job, ref) {
			continue
		}
		result.MatchedJobs++

		build, buildErr := s.buildService.CreateBuildFromPipeline(ctx, CreatePipelineBuildInput{
			ProjectID:    job.ProjectID,
			PipelineYAML: job.PipelineYAML,
			Source: &CreateBuildSourceInput{
				RepositoryURL: job.RepositoryURL,
				Ref:           ref,
				CommitSHA:     commitSHA,
			},
		})
		if buildErr != nil {
			return PushEventResult{}, fmt.Errorf("creating build from push event for job %s: %w", job.ID, buildErr)
		}

		result.Builds = append(result.Builds, PushEventMatchedBuild{Job: job, Build: build})
	}

	return result, nil
}

func normalizePushRef(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.TrimPrefix(trimmed, "refs/heads/")
}

func matchesPushBranch(job domain.Job, ref string) bool {
	if job.PushBranch == nil {
		return true
	}
	return normalizePushRef(*job.PushBranch) == ref
}
