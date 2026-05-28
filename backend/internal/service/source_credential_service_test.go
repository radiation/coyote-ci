package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

func TestSourceCredentialService_CreateTrimsAndValidatesInput(t *testing.T) {
	ctx := context.Background()
	service := NewSourceCredentialService(memoryrepo.NewSourceCredentialRepository())
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	username := " ci-user "

	credential, createErr := service.CreateSourceCredential(ctx, CreateSourceCredentialInput{
		Name:      " GitHub PAT ",
		Kind:      "https_token",
		Username:  &username,
		SecretRef: " secrets/github-token ",
	})
	if createErr != nil {
		t.Fatalf("create source credential: %v", createErr)
	}
	if credential.Name != "GitHub PAT" || credential.Kind != domain.SourceCredentialKindHTTPSToken || credential.SecretRef != "secrets/github-token" {
		t.Fatalf("expected normalized credential, got %+v", credential)
	}
	if credential.Username == nil || *credential.Username != "ci-user" {
		t.Fatalf("expected trimmed username, got %+v", credential.Username)
	}
	if !credential.CreatedAt.Equal(now) || !credential.UpdatedAt.Equal(now) {
		t.Fatalf("expected deterministic timestamps, got created=%s updated=%s", credential.CreatedAt, credential.UpdatedAt)
	}

	invalidCases := []struct {
		name    string
		input   CreateSourceCredentialInput
		wantErr error
	}{
		{name: "invalid kind", input: CreateSourceCredentialInput{Name: "cred", Kind: "password", SecretRef: "secret"}, wantErr: ErrSourceCredentialKindInvalid},
		{name: "missing name", input: CreateSourceCredentialInput{Name: " ", Kind: "ssh_key", SecretRef: "secret"}, wantErr: ErrSourceCredentialNameRequired},
		{name: "missing secret", input: CreateSourceCredentialInput{Name: "cred", Kind: "ssh_key", SecretRef: " "}, wantErr: ErrSourceCredentialSecretRefRequired},
	}
	for _, testCase := range invalidCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, validationErr := service.CreateSourceCredential(ctx, testCase.input)
			if !errors.Is(validationErr, testCase.wantErr) {
				t.Fatalf("expected %v, got %v", testCase.wantErr, validationErr)
			}
		})
	}
}

func TestSourceCredentialService_UpdateAppliesTriStatePatch(t *testing.T) {
	ctx := context.Background()
	repo := memoryrepo.NewSourceCredentialRepository()
	service := NewSourceCredentialService(repo)
	createdAt := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	service.now = func() time.Time { return createdAt }
	username := "git"
	credential, createErr := service.CreateSourceCredential(ctx, CreateSourceCredentialInput{
		Name:      "repo key",
		Kind:      "ssh_key",
		Username:  &username,
		SecretRef: "secret/old",
	})
	if createErr != nil {
		t.Fatalf("create source credential: %v", createErr)
	}

	service.now = func() time.Time { return updatedAt }
	newName := " repo token "
	newKind := "https_token"
	newSecret := " secret/new "
	updated, updateErr := service.UpdateSourceCredential(ctx, " "+credential.ID+" ", UpdateSourceCredentialInput{
		Name:      &newName,
		Kind:      &newKind,
		Username:  OptionalStringPatch{Set: true, Value: nil},
		SecretRef: &newSecret,
	})
	if updateErr != nil {
		t.Fatalf("update source credential: %v", updateErr)
	}
	if updated.Name != "repo token" || updated.Kind != domain.SourceCredentialKindHTTPSToken || updated.SecretRef != "secret/new" {
		t.Fatalf("expected updated fields, got %+v", updated)
	}
	if updated.Username != nil {
		t.Fatalf("expected username to be cleared, got %+v", updated.Username)
	}
	if !updated.CreatedAt.Equal(createdAt) || !updated.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("expected updated timestamp only, got created=%s updated=%s", updated.CreatedAt, updated.UpdatedAt)
	}

	emptyName := " "
	_, nameErr := service.UpdateSourceCredential(ctx, credential.ID, UpdateSourceCredentialInput{Name: &emptyName})
	if !errors.Is(nameErr, ErrSourceCredentialNameRequired) {
		t.Fatalf("expected name validation error, got %v", nameErr)
	}
}

func TestSourceCredentialService_GetAndDeleteTrimIDs(t *testing.T) {
	ctx := context.Background()
	service := NewSourceCredentialService(memoryrepo.NewSourceCredentialRepository())
	credential, createErr := service.CreateSourceCredential(ctx, CreateSourceCredentialInput{Name: "repo key", Kind: "ssh_key", SecretRef: "secret/ref"})
	if createErr != nil {
		t.Fatalf("create source credential: %v", createErr)
	}

	got, getErr := service.GetSourceCredential(ctx, " "+credential.ID+" ")
	if getErr != nil {
		t.Fatalf("get source credential: %v", getErr)
	}
	if got.ID != credential.ID {
		t.Fatalf("expected credential %q, got %q", credential.ID, got.ID)
	}

	deleteErr := service.DeleteSourceCredential(ctx, " "+credential.ID+" ")
	if deleteErr != nil {
		t.Fatalf("delete source credential: %v", deleteErr)
	}
	_, getDeletedErr := service.GetSourceCredential(ctx, credential.ID)
	if !errors.Is(getDeletedErr, repository.ErrSourceCredentialNotFound) {
		t.Fatalf("expected not found after delete, got %v", getDeletedErr)
	}
	if deleteBlankErr := service.DeleteSourceCredential(ctx, " "); !errors.Is(deleteBlankErr, repository.ErrSourceCredentialNotFound) {
		t.Fatalf("expected blank delete to be not found, got %v", deleteBlankErr)
	}
}
