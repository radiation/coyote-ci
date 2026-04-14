package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestJobService_CreateListGetUpdate(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}
	if !job.Enabled {
		t.Fatal("expected created job enabled=true")
	}
	if !job.PushEnabled {
		t.Fatal("expected created job push_enabled=true")
	}
	if job.PushBranch == nil || *job.PushBranch != "main" {
		t.Fatalf("expected created job push_branch=main, got %v", job.PushBranch)
	}

	list, err := jobService.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("list jobs failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 job, got %d", len(list))
	}

	got, err := jobService.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if got.Name != "backend-ci" {
		t.Fatalf("expected backend-ci, got %q", got.Name)
	}

	updated, err := jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		Name:        strPtr("backend-ci-updated"),
		Enabled:     boolPtr(false),
		PushEnabled: boolPtr(false),
		PushBranch:  strPtr(""),
	})
	if err != nil {
		t.Fatalf("update job failed: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected updated enabled=false")
	}
	if updated.Name != "backend-ci-updated" {
		t.Fatalf("expected updated name, got %q", updated.Name)
	}
	if updated.PushEnabled {
		t.Fatal("expected updated push_enabled=false")
	}
	if updated.PushBranch != nil {
		t.Fatalf("expected updated push_branch=nil, got %v", updated.PushBranch)
	}
}

func TestJobService_CreateRejectsInvalidPipelineYAML(t *testing.T) {
	jobService := NewJobService(memory.NewJobRepository(), NewBuildService(memory.NewBuildRepository(), nil, nil))

	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 2\nsteps:\n  - name: bad\n    run: echo bad\n",
	})
	if err == nil {
		t.Fatal("expected invalid pipeline error")
	}
}

func TestJobService_CreateAllowsRepoPipelinePathWithoutInlineYAML(t *testing.T) {
	jobService := NewJobService(memory.NewJobRepository(), NewBuildService(memory.NewBuildRepository(), nil, nil))

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:        "project-1",
		Name:             "backend-path",
		RepositoryURL:    "https://github.com/example/backend.git",
		DefaultRef:       "main",
		DefaultCommitSHA: "",
		PipelinePath:     "scenarios/success-basic/coyote.yml",
		PipelineYAML:     "",
	})
	if err != nil {
		t.Fatalf("expected path-based job create to succeed, got %v", err)
	}
	if job.PipelinePath == nil || *job.PipelinePath != "scenarios/success-basic/coyote.yml" {
		t.Fatalf("expected persisted pipeline_path, got %v", job.PipelinePath)
	}
	if strings.TrimSpace(job.PipelineYAML) != "" {
		t.Fatalf("expected empty inline pipeline yaml, got %q", job.PipelineYAML)
	}
}

func TestJobService_RunNowCreatesNormalBuildAndSnapshotsPipeline(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	build, err := jobService.RunJobNow(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("run job now failed: %v", err)
	}
	if build.RepoURL == nil || *build.RepoURL != "https://github.com/example/backend.git" {
		t.Fatalf("expected build source repository_url, got %v", build.RepoURL)
	}
	if build.Ref == nil || *build.Ref != "main" {
		t.Fatalf("expected build source ref main, got %v", build.Ref)
	}
	if build.PipelineConfigYAML == nil || !strings.Contains(*build.PipelineConfigYAML, "go test ./...") {
		t.Fatalf("expected build pipeline snapshot, got %v", build.PipelineConfigYAML)
	}

	_, err = jobService.UpdateJob(context.Background(), job.ID, UpdateJobInput{
		PipelineYAML: strPtr("version: 1\nsteps:\n  - name: lint\n    run: golangci-lint run\n"),
	})
	if err != nil {
		t.Fatalf("update job failed: %v", err)
	}

	storedBuild, err := buildService.GetBuild(context.Background(), build.ID)
	if err != nil {
		t.Fatalf("get build failed: %v", err)
	}
	if storedBuild.PipelineConfigYAML == nil || !strings.Contains(*storedBuild.PipelineConfigYAML, "go test ./...") {
		t.Fatalf("expected old build snapshot unchanged, got %v", storedBuild.PipelineConfigYAML)
	}
}

func TestJobService_RunNowDisabledJobRejected(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	jobService := NewJobService(jobRepo, NewBuildService(buildRepo, nil, nil))

	job, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	_, err = jobService.RunJobNow(context.Background(), job.ID)
	if !errors.Is(err, ErrJobDisabled) {
		t.Fatalf("expected ErrJobDisabled, got %v", err)
	}
}

func TestJobService_TriggerPushEvent_MatchesAndCreatesBuilds(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	jobA, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-main",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job A failed: %v", err)
	}

	_, err = jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-any-branch",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PipelineYAML:  "version: 1\nsteps:\n  - name: lint\n    run: golangci-lint run\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job B failed: %v", err)
	}

	_, err = jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-disabled",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: skip\n    run: echo skip\n",
		Enabled:       boolPtr(false),
	})
	if err != nil {
		t.Fatalf("create disabled job failed: %v", err)
	}

	_, err = jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-push-disabled",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(false),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: skip\n    run: echo skip\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create push-disabled job failed: %v", err)
	}

	result, err := jobService.TriggerPushEvent(context.Background(), PushEventInput{
		RepositoryURL: "https://github.com/example/backend.git",
		Ref:           "refs/heads/main",
		CommitSHA:     "abc123",
	})
	if err != nil {
		t.Fatalf("trigger push event failed: %v", err)
	}

	if result.MatchedJobs != 2 {
		t.Fatalf("expected 2 matched jobs, got %d", result.MatchedJobs)
	}
	if len(result.Builds) != 2 {
		t.Fatalf("expected 2 created builds, got %d", len(result.Builds))
	}

	for _, item := range result.Builds {
		if item.Build.RepoURL == nil || *item.Build.RepoURL != "https://github.com/example/backend.git" {
			t.Fatalf("expected build repo_url from job, got %v", item.Build.RepoURL)
		}
		if item.Build.Ref == nil || *item.Build.Ref != "main" {
			t.Fatalf("expected build ref=main, got %v", item.Build.Ref)
		}
		if item.Build.CommitSHA == nil || *item.Build.CommitSHA != "abc123" {
			t.Fatalf("expected build commit_sha=abc123, got %v", item.Build.CommitSHA)
		}
		if item.Build.PipelineConfigYAML == nil || *item.Build.PipelineConfigYAML == "" {
			t.Fatal("expected build pipeline snapshot")
		}
		if item.Build.Trigger.Kind != domain.BuildTriggerKindWebhook {
			t.Fatalf("expected webhook trigger kind, got %q", item.Build.Trigger.Kind)
		}
		if item.Build.Trigger.SCMProvider == nil || *item.Build.Trigger.SCMProvider != "github" {
			t.Fatalf("expected trigger scm_provider=github, got %v", item.Build.Trigger.SCMProvider)
		}
		if item.Build.Trigger.EventType == nil || *item.Build.Trigger.EventType != "push" {
			t.Fatalf("expected trigger event_type=push, got %v", item.Build.Trigger.EventType)
		}
	}

	_, err = jobService.UpdateJob(context.Background(), jobA.ID, UpdateJobInput{
		PipelineYAML: strPtr("version: 1\nsteps:\n  - name: changed\n    run: echo changed\n"),
	})
	if err != nil {
		t.Fatalf("update job after trigger failed: %v", err)
	}

	storedBuild, err := buildService.GetBuild(context.Background(), result.Builds[0].Build.ID)
	if err != nil {
		t.Fatalf("get triggered build failed: %v", err)
	}
	if storedBuild.PipelineConfigYAML == nil || strings.Contains(*storedBuild.PipelineConfigYAML, "changed") {
		t.Fatalf("expected triggered build snapshot unchanged, got %v", storedBuild.PipelineConfigYAML)
	}
}

func TestJobService_TriggerPushEvent_NoMatches(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	result, err := jobService.TriggerPushEvent(context.Background(), PushEventInput{
		RepositoryURL: "https://github.com/example/backend.git",
		Ref:           "main",
		CommitSHA:     "abc123",
	})
	if err != nil {
		t.Fatalf("trigger push event failed: %v", err)
	}
	if result.MatchedJobs != 0 {
		t.Fatalf("expected 0 matched jobs, got %d", result.MatchedJobs)
	}
	if len(result.Builds) != 0 {
		t.Fatalf("expected 0 builds, got %d", len(result.Builds))
	}
}

func TestJobService_TriggerPushEvent_Validation(t *testing.T) {
	jobService := NewJobService(memory.NewJobRepository(), NewBuildService(memory.NewBuildRepository(), nil, nil))

	_, err := jobService.TriggerPushEvent(context.Background(), PushEventInput{})
	if !errors.Is(err, ErrPushEventRepositoryURLRequired) {
		t.Fatalf("expected ErrPushEventRepositoryURLRequired, got %v", err)
	}

	_, err = jobService.TriggerPushEvent(context.Background(), PushEventInput{RepositoryURL: "https://github.com/example/backend.git"})
	if !errors.Is(err, ErrPushEventRefRequired) {
		t.Fatalf("expected ErrPushEventRefRequired, got %v", err)
	}

	_, err = jobService.TriggerPushEvent(context.Background(), PushEventInput{RepositoryURL: "https://github.com/example/backend.git", Ref: "main"})
	if !errors.Is(err, ErrPushEventCommitSHARequired) {
		t.Fatalf("expected ErrPushEventCommitSHARequired, got %v", err)
	}
}

func TestJobService_TriggerWebhookEvent_TagOnlyJob(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	triggerMode := "tags"
	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-tags",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		TriggerMode:   &triggerMode,
		TagAllowlist:  []string{"v*"},
		PipelineYAML:  "version: 1\nsteps:\n  - name: release\n    run: echo release\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	branchResult, err := jobService.TriggerWebhookEvent(context.Background(), WebhookTriggerInput{
		SCMProvider:   "github",
		EventType:     "push",
		RepositoryURL: "https://github.com/example/backend.git",
		RawRef:        "refs/heads/main",
		CommitSHA:     "abc123",
	})
	if err != nil {
		t.Fatalf("branch trigger failed: %v", err)
	}
	if branchResult.MatchedJobs != 0 {
		t.Fatalf("expected no branch matches for tag-only job, got %d", branchResult.MatchedJobs)
	}

	tagResult, err := jobService.TriggerWebhookEvent(context.Background(), WebhookTriggerInput{
		SCMProvider:   "github",
		EventType:     "push",
		RepositoryURL: "https://github.com/example/backend.git",
		RawRef:        "refs/tags/v1.2.3",
		CommitSHA:     "def456",
	})
	if err != nil {
		t.Fatalf("tag trigger failed: %v", err)
	}
	if tagResult.MatchedJobs != 1 {
		t.Fatalf("expected one tag match, got %d", tagResult.MatchedJobs)
	}
}

func TestJobService_TriggerWebhookEvent_DeletePushIgnored(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	buildRepo := memory.NewBuildRepository()
	buildService := NewBuildService(buildRepo, nil, nil)
	jobService := NewJobService(jobRepo, buildService)

	_, err := jobService.CreateJob(context.Background(), CreateJobInput{
		ProjectID:     "project-1",
		Name:          "backend-main",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PushEnabled:   boolPtr(true),
		PushBranch:    strPtr("main"),
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
		Enabled:       boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	result, err := jobService.TriggerWebhookEvent(context.Background(), WebhookTriggerInput{
		SCMProvider:   "github",
		EventType:     "push",
		RepositoryURL: "https://github.com/example/backend.git",
		RawRef:        "refs/heads/main",
		Deleted:       true,
	})
	if err != nil {
		t.Fatalf("delete push should be ignored cleanly, got error: %v", err)
	}
	if result.MatchedJobs != 0 {
		t.Fatalf("expected no matches for deleted ref, got %d", result.MatchedJobs)
	}
	if result.NoMatchReason == nil || *result.NoMatchReason != string(WebhookFilterDecisionDeletedRef) {
		t.Fatalf("expected no_match_reason deleted_ref, got %v", result.NoMatchReason)
	}
}

func strPtr(v string) *string {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}
