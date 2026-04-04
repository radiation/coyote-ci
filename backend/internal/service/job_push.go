package service

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"path"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type PushEventInput struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
}

type WebhookMatchedBuild struct {
	Job   domain.Job
	Build domain.Build
}

type WebhookTriggerInput struct {
	SCMProvider     string
	EventType       string
	RepositoryOwner string
	RepositoryName  string
	RepositoryURL   string
	Ref             string
	RefType         string
	CommitSHA       string
	DeliveryID      string
	Actor           string
}

type WebhookTriggerResult struct {
	SCMProvider   string
	EventType     string
	RepositoryURL string
	Ref           string
	RefType       string
	CommitSHA     string
	MatchedJobs   int
	Builds        []WebhookMatchedBuild
}

type PushEventMatchedBuild = WebhookMatchedBuild

type PushEventResult struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
	MatchedJobs   int
	Builds        []PushEventMatchedBuild
}

func (s *JobService) TriggerWebhookEvent(ctx context.Context, input WebhookTriggerInput) (WebhookTriggerResult, error) {
	if s.buildService == nil {
		return WebhookTriggerResult{}, ErrJobBuildServiceNotConfigured
	}

	scmProvider := strings.ToLower(strings.TrimSpace(input.SCMProvider))
	eventType := strings.ToLower(strings.TrimSpace(input.EventType))
	repoURL := strings.TrimSpace(input.RepositoryURL)
	repositoryOwner := strings.TrimSpace(input.RepositoryOwner)
	repositoryName := strings.TrimSpace(input.RepositoryName)
	if repoURL == "" && scmProvider == "github" && repositoryOwner != "" && repositoryName != "" {
		repoURL = "https://github.com/" + repositoryOwner + "/" + repositoryName + ".git"
	}
	if repoURL == "" {
		return WebhookTriggerResult{}, ErrPushEventRepositoryURLRequired
	}

	ref := normalizePushRef(input.Ref)
	if ref == "" {
		return WebhookTriggerResult{}, ErrPushEventRefRequired
	}

	commitSHA := strings.TrimSpace(input.CommitSHA)
	if commitSHA == "" {
		return WebhookTriggerResult{}, ErrPushEventCommitSHARequired
	}

	jobs, err := s.jobRepo.ListPushEnabledByRepository(ctx, repoURL)
	if err != nil {
		return WebhookTriggerResult{}, err
	}

	result := WebhookTriggerResult{
		SCMProvider:   scmProvider,
		EventType:     eventType,
		RepositoryURL: repoURL,
		Ref:           ref,
		RefType:       strings.TrimSpace(input.RefType),
		CommitSHA:     commitSHA,
		Builds:        make([]WebhookMatchedBuild, 0),
	}

	for _, job := range jobs {
		if !matchesSCMRepositoryIdentity(job.RepositoryURL, scmProvider, repositoryOwner, repositoryName) {
			continue
		}
		if !matchesPushBranch(job, ref) {
			continue
		}
		result.MatchedJobs++
		log.Printf("INFO webhook job matched provider=%s owner=%s repository=%s job_id=%s ref=%s", scmProvider, repositoryOwner, repositoryName, job.ID, ref)

		var (
			build    domain.Build
			buildErr error
		)
		triggerInput := &CreateBuildTriggerInput{
			Kind:            string(domain.BuildTriggerKindWebhook),
			SCMProvider:     scmProvider,
			EventType:       eventType,
			RepositoryOwner: repositoryOwner,
			RepositoryName:  repositoryName,
			RepositoryURL:   repoURL,
			Ref:             ref,
			RefType:         strings.TrimSpace(input.RefType),
			DeliveryID:      strings.TrimSpace(input.DeliveryID),
			Actor:           strings.TrimSpace(input.Actor),
		}
		if job.PipelinePath != nil && strings.TrimSpace(*job.PipelinePath) != "" {
			build, buildErr = s.buildService.CreateBuildFromRepo(ctx, CreateRepoBuildInput{
				ProjectID:    job.ProjectID,
				JobID:        &job.ID,
				RepoURL:      job.RepositoryURL,
				Ref:          ref,
				CommitSHA:    commitSHA,
				PipelinePath: strings.TrimSpace(*job.PipelinePath),
				Trigger:      triggerInput,
			})
		} else {
			build, buildErr = s.buildService.CreateBuildFromPipeline(ctx, CreatePipelineBuildInput{
				ProjectID:    job.ProjectID,
				JobID:        &job.ID,
				PipelineYAML: job.PipelineYAML,
				Source: &CreateBuildSourceInput{
					RepositoryURL: job.RepositoryURL,
					Ref:           ref,
					CommitSHA:     commitSHA,
				},
				Trigger: triggerInput,
			})
		}
		if buildErr != nil {
			return WebhookTriggerResult{}, fmt.Errorf("creating build from webhook event for job %s: %w", job.ID, buildErr)
		}
		log.Printf("INFO webhook build queued provider=%s event_type=%s job_id=%s build_id=%s", scmProvider, eventType, job.ID, build.ID)

		result.Builds = append(result.Builds, WebhookMatchedBuild{Job: job, Build: build})
	}

	if result.MatchedJobs == 0 {
		log.Printf("INFO webhook job not matched provider=%s owner=%s repository=%s ref=%s", scmProvider, repositoryOwner, repositoryName, ref)
	}

	return result, nil
}

func (s *JobService) TriggerPushEvent(ctx context.Context, input PushEventInput) (PushEventResult, error) {
	result, err := s.TriggerWebhookEvent(ctx, WebhookTriggerInput{
		SCMProvider:   "github",
		EventType:     "push",
		RepositoryURL: input.RepositoryURL,
		Ref:           input.Ref,
		RefType:       "branch",
		CommitSHA:     input.CommitSHA,
	})
	if err != nil {
		return PushEventResult{}, err
	}

	return PushEventResult{
		RepositoryURL: result.RepositoryURL,
		Ref:           result.Ref,
		CommitSHA:     result.CommitSHA,
		MatchedJobs:   result.MatchedJobs,
		Builds:        result.Builds,
	}, nil
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

func matchesSCMRepositoryIdentity(jobRepositoryURL string, scmProvider string, owner string, name string) bool {
	if scmProvider == "" || owner == "" || name == "" {
		return true
	}

	jobProvider, jobOwner, jobName := parseSCMRepositoryIdentity(jobRepositoryURL)
	return jobProvider == scmProvider && jobOwner == strings.ToLower(strings.TrimSpace(owner)) && jobName == strings.ToLower(strings.TrimSpace(name))
}

func parseSCMRepositoryIdentity(rawURL string) (provider string, owner string, name string) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", "", ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", "", ""
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "github.com" {
		provider = "github"
	}

	if provider == "" {
		return "", "", ""
	}

	cleanPath := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	segments := strings.Split(cleanPath, "/")
	if len(segments) < 2 {
		return provider, "", ""
	}

	owner = strings.ToLower(strings.TrimSpace(segments[0]))
	name = strings.ToLower(strings.TrimSpace(path.Clean(segments[1])))
	name = strings.TrimSuffix(name, ".git")
	if name == "." || name == ".." {
		name = ""
	}

	return provider, owner, name
}
