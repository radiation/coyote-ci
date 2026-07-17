package domain

import (
	"testing"
	"time"
)

func TestSCMRepositoryRegistrationNormalizeAndValidate(t *testing.T) {
	now := time.Now().UTC()
	branch := " main "
	repo := SCMRepositoryRegistration{
		ID:                   " repo-1 ",
		ConnectionID:         " connection-1 ",
		ProviderRepositoryID: " 1001 ",
		Owner:                " octo ",
		Name:                 " widgets ",
		FullName:             " octo/widgets ",
		CloneURL:             " https://github.com/octo/widgets.git ",
		WebURL:               " https://github.com/octo/widgets ",
		DefaultBranch:        &branch,
		MetadataRefreshedAt:  now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	normalized := repo.Normalize()
	if normalized.CloneURL != "https://github.com/octo/widgets.git" {
		t.Fatalf("expected normalized clone url, got %q", normalized.CloneURL)
	}
	if normalized.DefaultBranch == nil || *normalized.DefaultBranch != "main" {
		t.Fatalf("expected normalized default branch, got %+v", normalized.DefaultBranch)
	}
	if err := normalized.Validate(); err != nil {
		t.Fatalf("expected valid repository registration, got %v", err)
	}
}

func TestSCMRepositoryRegistrationValidateRequiresConnectionScopedIdentity(t *testing.T) {
	now := time.Now().UTC()
	repo := SCMRepositoryRegistration{ID: "repo-1", ProviderRepositoryID: "1001", Owner: "octo", Name: "widgets", FullName: "octo/widgets", CloneURL: "https://github.com/octo/widgets.git", WebURL: "https://github.com/octo/widgets", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := repo.Validate(); err == nil {
		t.Fatal("expected missing connection id validation error")
	}
}
