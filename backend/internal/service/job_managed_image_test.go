package service

import (
	"context"
	"errors"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestJobManagedImageConfigLifecycleHelpers(t *testing.T) {
	ctx := context.Background()
	configRepo := memory.NewJobManagedImageConfigRepository()
	credentialRepo := memory.NewSourceCredentialRepository()
	_, err := credentialRepo.Create(ctx, domain.SourceCredential{
		ID:        "cred-1",
		Name:      "bot",
		Kind:      domain.SourceCredentialKindHTTPSToken,
		SecretRef: "COYOTE_TOKEN",
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}

	jobService := NewJobService(memory.NewJobRepository(), nil).WithManagedImageConfigRepository(configRepo, credentialRepo)
	job := domain.Job{ID: "job-1"}

	config, err := jobService.upsertManagedImageConfig(ctx, job, &ManagedImageConfigInput{
		Enabled:           true,
		ManagedImageName:  " go ",
		PipelinePath:      " .coyote/pipeline.yml ",
		WriteCredentialID: " cred-1 ",
	})
	if err != nil {
		t.Fatalf("upsert managed image config: %v", err)
	}
	if config.ManagedImageName != "go" || config.PipelinePath != ".coyote/pipeline.yml" || config.WriteCredentialID != "cred-1" {
		t.Fatalf("expected trimmed config fields, got %+v", config)
	}
	if config.BotBranchPrefix != "coyote/managed-image-refresh" || config.CommitAuthorName != "Coyote CI Bot" || config.CommitAuthorEmail != "bot@coyote-ci.local" {
		t.Fatalf("expected default author/bot fields, got %+v", config)
	}

	loadedJob := domain.Job{ID: "job-1"}
	if attachErr := jobService.attachManagedImageConfig(ctx, &loadedJob); attachErr != nil {
		t.Fatalf("attach managed image config: %v", attachErr)
	}
	if loadedJob.ManagedImageConfig == nil || loadedJob.ManagedImageConfig.ID != config.ID {
		t.Fatalf("expected attached config %q, got %+v", config.ID, loadedJob.ManagedImageConfig)
	}

	newName := "rust"
	patched, err := jobService.patchManagedImageConfig(ctx, job, &ManagedImageConfigPatch{ManagedImageName: &newName})
	if err != nil {
		t.Fatalf("patch managed image config: %v", err)
	}
	if patched == nil || patched.ManagedImageName != "rust" {
		t.Fatalf("expected patched managed image name rust, got %+v", patched)
	}

	disabled := false
	removed, err := jobService.patchManagedImageConfig(ctx, job, &ManagedImageConfigPatch{Enabled: &disabled})
	if err != nil {
		t.Fatalf("disable managed image config: %v", err)
	}
	if removed != nil {
		t.Fatalf("expected nil config after disable, got %+v", removed)
	}
	if _, err := configRepo.GetByJobID(ctx, job.ID); !errors.Is(err, repository.ErrJobManagedImageConfigNotFound) {
		t.Fatalf("expected config deleted, got %v", err)
	}
}

func TestValidateManagedImageConfigErrors(t *testing.T) {
	base := domain.JobManagedImageConfig{
		ManagedImageName:  "go",
		PipelinePath:      ".coyote/pipeline.yml",
		WriteCredentialID: "cred-1",
	}

	tests := []struct {
		name   string
		mutate func(*domain.JobManagedImageConfig)
		want   error
	}{
		{name: "missing name", mutate: func(config *domain.JobManagedImageConfig) { config.ManagedImageName = " " }, want: ErrJobManagedImageNameRequired},
		{name: "missing pipeline path", mutate: func(config *domain.JobManagedImageConfig) { config.PipelinePath = " " }, want: ErrJobManagedImagePipelinePathRequired},
		{name: "missing credential", mutate: func(config *domain.JobManagedImageConfig) { config.WriteCredentialID = " " }, want: ErrJobManagedImageWriteCredentialIDRequired},
		{name: "valid", mutate: func(_ *domain.JobManagedImageConfig) {}, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := base
			tc.mutate(&config)
			if err := validateManagedImageConfig(config); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestJobManagedImageConfigErrorBranches(t *testing.T) {
	ctx := context.Background()
	job := domain.Job{ID: "job-1"}

	unconfigured := NewJobService(memory.NewJobRepository(), nil)
	if _, err := unconfigured.upsertManagedImageConfig(ctx, job, &ManagedImageConfigInput{}); !errors.Is(err, ErrJobManagedImageConfigNotConfigured) {
		t.Fatalf("expected unconfigured upsert error, got %v", err)
	}
	if _, err := unconfigured.patchManagedImageConfig(ctx, job, &ManagedImageConfigPatch{}); !errors.Is(err, ErrJobManagedImageConfigNotConfigured) {
		t.Fatalf("expected unconfigured patch error, got %v", err)
	}

	configRepo := memory.NewJobManagedImageConfigRepository()
	credentialRepo := memory.NewSourceCredentialRepository()
	jobService := NewJobService(memory.NewJobRepository(), nil).WithManagedImageConfigRepository(configRepo, credentialRepo)
	if _, err := jobService.upsertManagedImageConfig(ctx, job, nil); !errors.Is(err, repository.ErrJobManagedImageConfigNotFound) {
		t.Fatalf("expected nil input not found error, got %v", err)
	}
	if _, err := jobService.upsertManagedImageConfig(ctx, job, &ManagedImageConfigInput{WriteCredentialID: "missing"}); !errors.Is(err, repository.ErrSourceCredentialNotFound) {
		t.Fatalf("expected missing credential error, got %v", err)
	}

	if err := jobService.attachManagedImageConfig(ctx, nil); err != nil {
		t.Fatalf("nil job attach should be ignored, got %v", err)
	}

	_, credentialErr := credentialRepo.Create(ctx, domain.SourceCredential{ID: "cred-1", Name: "bot", Kind: domain.SourceCredentialKindHTTPSToken, SecretRef: "COYOTE_TOKEN"})
	if credentialErr != nil {
		t.Fatalf("create credential: %v", credentialErr)
	}
	name := "go"
	pipelinePath := ".coyote/pipeline.yml"
	credentialID := "cred-1"
	created, patchErr := jobService.patchManagedImageConfig(ctx, job, &ManagedImageConfigPatch{ManagedImageName: &name, PipelinePath: &pipelinePath, WriteCredentialID: &credentialID})
	if patchErr != nil {
		t.Fatalf("patch missing config should create one: %v", patchErr)
	}
	if created == nil || created.ManagedImageName != "go" || created.WriteCredentialID != "cred-1" {
		t.Fatalf("expected created managed image config, got %+v", created)
	}
	removed, patchErr := jobService.patchManagedImageConfig(ctx, job, nil)
	if patchErr != nil {
		t.Fatalf("nil patch should delete config: %v", patchErr)
	}
	if removed != nil {
		t.Fatalf("expected nil config after delete patch, got %+v", removed)
	}
}

func TestStringOrFallback(t *testing.T) {
	if got := stringOrFallback(nil, "fallback"); got != "fallback" {
		t.Fatalf("expected fallback for nil, got %q", got)
	}
	blank := "  "
	if got := stringOrFallback(&blank, "fallback"); got != "fallback" {
		t.Fatalf("expected fallback for blank, got %q", got)
	}
	value := " custom "
	if got := stringOrFallback(&value, "fallback"); got != "custom" {
		t.Fatalf("expected trimmed custom value, got %q", got)
	}
}
