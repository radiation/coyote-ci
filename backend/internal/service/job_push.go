package service

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"path"
	"strings"

	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type PushEventInput struct {
	RepositoryURL string
	Ref           string
	CommitSHA     string
}

type PushEventMatchedBuild = webhooksvc.WebhookMatchedBuild

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

	normalizedRef := normalizeWebhookRefInput(input)
	if normalizedRef.RefName == "" {
		return WebhookTriggerResult{}, ErrPushEventRefRequired
	}

	commitSHA := strings.TrimSpace(input.CommitSHA)

	jobs, err := s.jobRepo.ListPushEnabledByRepository(ctx, repoURL)
	if err != nil {
		return WebhookTriggerResult{}, err
	}

	result := WebhookTriggerResult{
		SCMProvider:   scmProvider,
		EventType:     eventType,
		RepositoryURL: repoURL,
		RawRef:        normalizedRef.RawRef,
		Ref:           normalizedRef.RefName,
		RefType:       string(normalizedRef.RefType),
		RefName:       normalizedRef.RefName,
		Deleted:       normalizedRef.Deleted,
		CommitSHA:     commitSHA,
		Builds:        make([]WebhookMatchedBuild, 0),
	}
	webhookFields := webhooksvc.WebhookLogFields(ctx)
	var firstNoMatchReason *string
	if !webhooksvc.WebhookFilterShouldTriggerBuild(normalizedRef, webhooksvc.WebhookFilterConfig{}).Matched {
		defaultReason := string(webhooksvc.WebhookFilterShouldTriggerBuild(normalizedRef, webhooksvc.WebhookFilterConfig{}).Reason)
		firstNoMatchReason = &defaultReason
	}

	for _, job := range jobs {
		if !matchesSCMRepositoryIdentity(job.RepositoryURL, scmProvider, repositoryOwner, repositoryName) {
			continue
		}

		decision := webhooksvc.WebhookFilterShouldTriggerBuild(normalizedRef, toWebhookJobTriggerConfig(job))
		if !decision.Matched {
			if firstNoMatchReason == nil {
				reason := string(decision.Reason)
				firstNoMatchReason = &reason
			}
			continue
		}
		if commitSHA == "" {
			return WebhookTriggerResult{}, ErrPushEventCommitSHARequired
		}
		result.MatchedJobs++
		log.Printf("INFO webhook job matched %s owner=%s repository=%s job_id=%s ref=%s ref_type=%s", webhookFields, repositoryOwner, repositoryName, job.ID, normalizedRef.RefName, normalizedRef.RefType)

		var (
			build    domain.Build
			buildErr error
		)
		triggerInput := &buildsvc.CreateBuildTriggerInput{
			Kind:            string(domain.BuildTriggerKindWebhook),
			SCMProvider:     scmProvider,
			EventType:       eventType,
			RepositoryOwner: repositoryOwner,
			RepositoryName:  repositoryName,
			RepositoryURL:   repoURL,
			RawRef:          normalizedRef.RawRef,
			Ref:             normalizedRef.RefName,
			RefType:         string(normalizedRef.RefType),
			RefName:         normalizedRef.RefName,
			Deleted:         boolPtrValue(normalizedRef.Deleted),
			CommitSHA:       commitSHA,
			DeliveryID:      strings.TrimSpace(input.DeliveryID),
			Actor:           strings.TrimSpace(input.Actor),
		}
		if job.PipelinePath != nil && strings.TrimSpace(*job.PipelinePath) != "" {
			build, buildErr = s.buildService.CreateBuildFromRepo(ctx, buildsvc.CreateRepoBuildInput{
				ProjectID:    job.ProjectID,
				JobID:        &job.ID,
				RepoURL:      job.RepositoryURL,
				Ref:          normalizedRef.RefName,
				CommitSHA:    commitSHA,
				PipelinePath: strings.TrimSpace(*job.PipelinePath),
				Trigger:      triggerInput,
			})
		} else {
			build, buildErr = s.buildService.CreateBuildFromPipeline(ctx, buildsvc.CreatePipelineBuildInput{
				ProjectID:    job.ProjectID,
				JobID:        &job.ID,
				PipelineYAML: job.PipelineYAML,
				Source: &buildsvc.CreateBuildSourceInput{
					RepositoryURL: job.RepositoryURL,
					Ref:           normalizedRef.RefName,
					CommitSHA:     commitSHA,
				},
				Trigger: triggerInput,
			})
		}
		if buildErr != nil {
			return WebhookTriggerResult{}, fmt.Errorf("creating build from webhook event for job %s: %w", job.ID, buildErr)
		}
		log.Printf("INFO webhook build queued %s job_id=%s build_id=%s", webhookFields, job.ID, build.ID)

		result.Builds = append(result.Builds, WebhookMatchedBuild{Job: job, Build: build})
	}
	result.NoMatchReason = firstNoMatchReason

	if result.MatchedJobs == 0 {
		log.Printf("INFO webhook job not matched %s owner=%s repository=%s ref=%s ref_type=%s reason=%s", webhookFields, repositoryOwner, repositoryName, normalizedRef.RefName, normalizedRef.RefType, readStringPtr(firstNoMatchReason))
	}

	return result, nil
}

func (s *JobService) TriggerPushEvent(ctx context.Context, input PushEventInput) (PushEventResult, error) {
	repoURL := strings.TrimSpace(input.RepositoryURL)
	if repoURL == "" {
		return PushEventResult{}, ErrPushEventRepositoryURLRequired
	}
	ref := strings.TrimSpace(input.Ref)
	if ref == "" {
		return PushEventResult{}, ErrPushEventRefRequired
	}
	commitSHA := strings.TrimSpace(input.CommitSHA)
	if commitSHA == "" {
		return PushEventResult{}, ErrPushEventCommitSHARequired
	}

	result, err := s.TriggerWebhookEvent(ctx, WebhookTriggerInput{
		SCMProvider:   "github",
		EventType:     "push",
		RepositoryURL: repoURL,
		RawRef:        ref,
		Ref:           ref,
		RefType:       string(domain.WebhookRefTypeBranch),
		CommitSHA:     commitSHA,
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
	return strings.TrimPrefix(strings.TrimPrefix(trimmed, "refs/heads/"), "refs/tags/")
}

func normalizeWebhookRefInput(input WebhookTriggerInput) domain.WebhookRef {
	raw := strings.TrimSpace(input.RawRef)
	if raw == "" {
		raw = strings.TrimSpace(input.Ref)
	}

	ref := domain.NormalizeWebhookRef(raw, input.Deleted)
	if ref.RefType == domain.WebhookRefTypeUnknown {
		inputRefType := strings.ToLower(strings.TrimSpace(input.RefType))
		switch inputRefType {
		case string(domain.WebhookRefTypeBranch):
			ref.RefType = domain.WebhookRefTypeBranch
		case string(domain.WebhookRefTypeTag):
			ref.RefType = domain.WebhookRefTypeTag
		}
	}
	if ref.RefName == "" {
		ref.RefName = normalizePushRef(input.RefName)
	}
	if ref.RefName == "" {
		ref.RefName = normalizePushRef(input.Ref)
	}
	return ref
}

func toWebhookJobTriggerConfig(job domain.Job) webhooksvc.WebhookFilterConfig {
	allowBranches := make([]string, 0, len(job.BranchAllowlist)+1)
	for _, item := range job.BranchAllowlist {
		branch := normalizePushRef(item)
		if branch != "" {
			allowBranches = append(allowBranches, branch)
		}
	}
	if len(allowBranches) == 0 && job.PushBranch != nil {
		legacyBranch := normalizePushRef(*job.PushBranch)
		if legacyBranch != "" {
			allowBranches = append(allowBranches, legacyBranch)
		}
	}

	tagAllowlist := make([]string, 0, len(job.TagAllowlist))
	for _, item := range job.TagAllowlist {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			tagAllowlist = append(tagAllowlist, strings.TrimPrefix(trimmed, "refs/tags/"))
		}
	}

	return webhooksvc.WebhookFilterConfig{
		Mode:            webhooksvc.NormalizeWebhookFilterMode(job.TriggerMode),
		BranchAllowlist: allowBranches,
		TagAllowlist:    tagAllowlist,
	}
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

func boolPtrValue(v bool) *bool {
	return &v
}
