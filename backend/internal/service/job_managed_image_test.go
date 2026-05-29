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
