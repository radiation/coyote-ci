package service

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
)

func TestNormalizeCreateJobInput_NormalizesRefsAndAllowlists(t *testing.T) {
	priority := 7
	triggerMode := " TAGS "
	pushBranch := " refs/heads/main "

	input, err := normalizeCreateJobInput(CreateJobInput{
		ProjectID:       " project-1 ",
		ProjectSlug:     " platform ",
		Name:            " backend ",
		Priority:        &priority,
		RepositoryURL:   " https://example.com/repo.git ",
		DefaultRef:      " main ",
		PushBranch:      &pushBranch,
		TriggerMode:     &triggerMode,
		BranchAllowlist: []string{" refs/heads/main ", "main", " develop "},
		TagAllowlist:    []string{" refs/tags/v1 ", "v1", " v2 "},
		PipelineYAML:    " version: 1\nsteps:\n  - name: test\n    run: go test ./...\n ",
	})
	if err != nil {
		t.Fatalf("normalize returned error: %v", err)
	}

	if input.ProjectID != "project-1" || input.ProjectSlug != "platform" || input.Name != "backend" {
		t.Fatalf("expected trimmed identity fields, got %+v", input)
	}
	if input.PushBranch == nil || *input.PushBranch != "main" {
		t.Fatalf("expected normalized push branch main, got %v", input.PushBranch)
	}
	if input.TriggerMode == nil || *input.TriggerMode != "tags" {
		t.Fatalf("expected trigger mode tags, got %v", input.TriggerMode)
	}
	if got := input.BranchAllowlist; len(got) != 2 || got[0] != "main" || got[1] != "develop" {
		t.Fatalf("expected de-duplicated branch allowlist, got %+v", got)
	}
	if got := input.TagAllowlist; len(got) != 3 || got[0] != "refs/tags/v1" || got[1] != "v1" || got[2] != "v2" {
		t.Fatalf("expected de-duplicated tag allowlist, got %+v", got)
	}
}

func TestValidateCreateJobRequiredFields_Errors(t *testing.T) {
	validPriority := 5
	invalidPriority := 11
	invalidTrigger := "pull_requests"

	tests := []struct {
		name  string
		input CreateJobInput
		want  error
	}{
		{name: "missing name", input: CreateJobInput{RepositoryURL: "https://example.com/repo.git", DefaultRef: "main", PipelineYAML: "version: 1"}, want: ErrJobNameRequired},
		{name: "missing repo", input: CreateJobInput{Name: "backend", DefaultRef: "main", PipelineYAML: "version: 1"}, want: ErrJobRepositorySourceRequired},
		{name: "missing source target", input: CreateJobInput{Name: "backend", RepositoryURL: "https://example.com/repo.git", PipelineYAML: "version: 1"}, want: ErrJobSourceTargetRequired},
		{name: "missing pipeline", input: CreateJobInput{Name: "backend", RepositoryURL: "https://example.com/repo.git", DefaultRef: "main"}, want: ErrJobPipelineDefinitionRequired},
		{name: "conflicting repository source", input: CreateJobInput{Name: "backend", RepositoryID: "repo-1", RepositoryURL: "https://example.com/repo.git", DefaultRef: "main", PipelineYAML: "version: 1"}, want: ErrJobRepositoryAssignmentConflict},
		{name: "invalid trigger", input: CreateJobInput{Name: "backend", RepositoryURL: "https://example.com/repo.git", DefaultRef: "main", PipelineYAML: "version: 1", TriggerMode: &invalidTrigger}, want: ErrJobInvalidTriggerMode},
		{name: "invalid priority", input: CreateJobInput{Name: "backend", RepositoryURL: "https://example.com/repo.git", DefaultRef: "main", PipelineYAML: "version: 1", Priority: &invalidPriority}, want: ErrJobPriorityOutOfRange},
		{name: "valid", input: CreateJobInput{Name: "backend", RepositoryURL: "https://example.com/repo.git", DefaultRef: "main", PipelineYAML: "version: 1", Priority: &validPriority}, want: nil},
		{name: "valid registered repository", input: CreateJobInput{Name: "backend", RepositoryID: "repo-1", DefaultRef: "main", PipelineYAML: "version: 1", Priority: &validPriority}, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCreateJobRequiredFields(tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateJobRequiredFields(t *testing.T) {
	validJob := domain.Job{
		Name:          "backend",
		Priority:      5,
		RepositoryID:  stringPtr("repo-1"),
		RepositoryURL: "https://github.com/octo/widgets.git",
		DefaultRef:    "main",
		TriggerMode:   domain.JobTriggerModeBranches,
		PipelineYAML:  "version: 1",
	}

	tests := []struct {
		name string
		job  domain.Job
		want error
	}{
		{name: "valid mapped job", job: validJob},
		{name: "missing name", job: domain.Job{Priority: 5, RepositoryURL: validJob.RepositoryURL, DefaultRef: "main", PipelineYAML: "version: 1"}, want: ErrJobNameRequired},
		{name: "missing repository source", job: domain.Job{Name: "backend", Priority: 5, DefaultRef: "main", PipelineYAML: "version: 1"}, want: ErrJobRepositorySourceRequired},
		{name: "missing source target", job: domain.Job{Name: "backend", Priority: 5, RepositoryURL: validJob.RepositoryURL, PipelineYAML: "version: 1"}, want: ErrJobSourceTargetRequired},
		{name: "missing pipeline", job: domain.Job{Name: "backend", Priority: 5, RepositoryURL: validJob.RepositoryURL, DefaultRef: "main"}, want: ErrJobPipelineDefinitionRequired},
		{name: "invalid trigger mode", job: domain.Job{Name: "backend", Priority: 5, RepositoryURL: validJob.RepositoryURL, DefaultRef: "main", TriggerMode: "invalid", PipelineYAML: "version: 1"}, want: ErrJobInvalidTriggerMode},
		{name: "invalid priority", job: domain.Job{Name: "backend", Priority: 11, RepositoryURL: validJob.RepositoryURL, DefaultRef: "main", PipelineYAML: "version: 1"}, want: ErrJobPriorityOutOfRange},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJobRequiredFields(tc.job)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}

	if got := optionalJobTriggerMode(""); got != string(domain.JobTriggerModeBranches) {
		t.Fatalf("expected default trigger mode branches, got %q", got)
	}
	if got := optionalJobTriggerMode(" tags "); got != "tags" {
		t.Fatalf("expected trimmed trigger mode tags, got %q", got)
	}
}

func TestNormalizeCreateJobInput_InvalidArtifactTriggersAreNotSilentlyDropped(t *testing.T) {
	tests := []struct {
		name  string
		input CreateJobInput
		want  error
	}{
		{
			name: "missing producer job id",
			input: CreateJobInput{
				ProjectID:     "project-1",
				Name:          "backend",
				RepositoryURL: "https://example.com/repo.git",
				DefaultRef:    "main",
				PipelineYAML:  "version: 1",
				ArtifactTriggers: []domain.JobArtifactTrigger{{
					ProducerJobID: " ",
					Path:          "dist/app.tgz",
				}},
			},
			want: ErrJobArtifactTriggerProducerJobIDRequired,
		},
		{
			name: "missing path",
			input: CreateJobInput{
				ProjectID:     "project-1",
				Name:          "backend",
				RepositoryURL: "https://example.com/repo.git",
				DefaultRef:    "main",
				PipelineYAML:  "version: 1",
				ArtifactTriggers: []domain.JobArtifactTrigger{{
					ProducerJobID: "job-upstream",
					Path:          " ",
				}},
			},
			want: ErrJobArtifactTriggerPathRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeCreateJobInput(tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestJobServiceResolveProjectID_DefaultsAndSlugLookup(t *testing.T) {
	jobRepo := memory.NewJobRepository()
	projectRepo := memory.NewProjectRepository(jobRepo)
	jobService := NewJobService(jobRepo, buildsvc.NewBuildService(memory.NewBuildRepository(), nil, nil)).WithProjectRepository(projectRepo)

	project, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-1", Name: "Default", Slug: domain.DefaultProjectSlug})
	if err != nil {
		t.Fatalf("create default project: %v", err)
	}
	got, resolveErr := jobService.resolveProjectID(context.Background(), "", "")
	if resolveErr != nil || got != project.ID {
		t.Fatalf("expected default project %q, got %q err=%v", project.ID, got, resolveErr)
	}

	slugProject, err := projectRepo.Create(context.Background(), domain.Project{ID: "project-2", Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create slug project: %v", err)
	}
	got, resolveErr = jobService.resolveProjectID(context.Background(), "platform", "")
	if resolveErr != nil || got != slugProject.ID {
		t.Fatalf("expected slug project %q, got %q err=%v", slugProject.ID, got, resolveErr)
	}
}

func TestJobValidationHelperBranches(t *testing.T) {
	if err := validatePipelineDefinition("", nil); !errors.Is(err, ErrJobPipelineDefinitionRequired) {
		t.Fatalf("expected pipeline definition required, got %v", err)
	}
	pipelinePath := " .coyote/pipeline.yml "
	if err := validatePipelineDefinition("", &pipelinePath); err != nil {
		t.Fatalf("expected pipeline path to satisfy definition, got %v", err)
	}
	if got := normalizedPriority(nil); got != domain.DefaultPriority {
		t.Fatalf("expected default priority, got %d", got)
	}
	outOfRangePriority := 99
	if got := normalizedPriority(&outOfRangePriority); got != outOfRangePriority {
		t.Fatalf("expected explicit priority to be preserved, got %d", got)
	}
	if got := optionalTrimmedStringPtr("  "); got != nil {
		t.Fatalf("expected nil optional string for blank, got %v", got)
	}
	if got := optionalTrimmedStringPtr(" refs/heads/main "); got == nil || *got != "refs/heads/main" {
		t.Fatalf("expected trimmed optional string, got %v", got)
	}
	if got := normalizeBranchAllowlist([]string{" ", "refs/heads/main", "main"}); len(got) != 1 || got[0] != "main" {
		t.Fatalf("expected normalized single branch, got %+v", got)
	}
	if got := normalizeBranchAllowlist([]string{" ", "\t"}); got != nil {
		t.Fatalf("expected nil branch allowlist, got %+v", got)
	}
	if got := normalizeTagAllowlist([]string{" refs/tags/v1 ", "v1", " "}); len(got) != 2 || got[0] != "refs/tags/v1" || got[1] != "v1" {
		t.Fatalf("expected normalized tag allowlist preserving current trim-prefix behavior, got %+v", got)
	}
	if got := normalizeTagAllowlist([]string{" ", "\t"}); got != nil {
		t.Fatalf("expected nil tag allowlist, got %+v", got)
	}
}
