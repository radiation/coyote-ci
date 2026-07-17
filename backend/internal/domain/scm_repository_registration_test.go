package domain

import (
	"strings"
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

func TestSCMRepositoryRegistrationValidationBranches(t *testing.T) {
	now := time.Now().UTC()
	longBranch := strings.Repeat("a", maxSCMRepositoryTextLen+20)
	normalized := SCMRepositoryRegistration{
		ID:                   " repo-1 ",
		ConnectionID:         " connection-1 ",
		ProviderRepositoryID: " 1001 ",
		Owner:                " owner ",
		Name:                 " repo ",
		FullName:             " owner/repo ",
		CloneURL:             " https://github.com/owner/repo.git ",
		WebURL:               " https://github.com/owner/repo ",
		DefaultBranch:        &longBranch,
		MetadataRefreshedAt:  now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}.Normalize()
	if normalized.DefaultBranch == nil || len([]rune(*normalized.DefaultBranch)) != maxSCMRepositoryTextLen {
		t.Fatalf("expected normalized default branch to be truncated to %d", maxSCMRepositoryTextLen)
	}

	cases := []struct {
		name  string
		value SCMRepositoryRegistration
	}{
		{name: "missing id", value: SCMRepositoryRegistration{ConnectionID: "conn", ProviderRepositoryID: "1001", Owner: "owner", Name: "repo", FullName: "owner/repo", CloneURL: "https://github.com/owner/repo.git", WebURL: "https://github.com/owner/repo", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}},
		{name: "missing provider repo id", value: SCMRepositoryRegistration{ID: "id", ConnectionID: "conn", Owner: "owner", Name: "repo", FullName: "owner/repo", CloneURL: "https://github.com/owner/repo.git", WebURL: "https://github.com/owner/repo", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}},
		{name: "missing owner", value: SCMRepositoryRegistration{ID: "id", ConnectionID: "conn", ProviderRepositoryID: "1001", Name: "repo", FullName: "owner/repo", CloneURL: "https://github.com/owner/repo.git", WebURL: "https://github.com/owner/repo", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}},
		{name: "missing name", value: SCMRepositoryRegistration{ID: "id", ConnectionID: "conn", ProviderRepositoryID: "1001", Owner: "owner", FullName: "owner/repo", CloneURL: "https://github.com/owner/repo.git", WebURL: "https://github.com/owner/repo", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}},
		{name: "missing full name", value: SCMRepositoryRegistration{ID: "id", ConnectionID: "conn", ProviderRepositoryID: "1001", Owner: "owner", Name: "repo", CloneURL: "https://github.com/owner/repo.git", WebURL: "https://github.com/owner/repo", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}},
		{name: "bad clone url", value: SCMRepositoryRegistration{ID: "id", ConnectionID: "conn", ProviderRepositoryID: "1001", Owner: "owner", Name: "repo", FullName: "owner/repo", CloneURL: "bad", WebURL: "https://github.com/owner/repo", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}},
		{name: "bad web url", value: SCMRepositoryRegistration{ID: "id", ConnectionID: "conn", ProviderRepositoryID: "1001", Owner: "owner", Name: "repo", FullName: "owner/repo", CloneURL: "https://github.com/owner/repo.git", WebURL: "bad", MetadataRefreshedAt: now, CreatedAt: now, UpdatedAt: now}},
		{name: "missing metadata refreshed at", value: SCMRepositoryRegistration{ID: "id", ConnectionID: "conn", ProviderRepositoryID: "1001", Owner: "owner", Name: "repo", FullName: "owner/repo", CloneURL: "https://github.com/owner/repo.git", WebURL: "https://github.com/owner/repo", CreatedAt: now, UpdatedAt: now}},
		{name: "missing timestamps", value: SCMRepositoryRegistration{ID: "id", ConnectionID: "conn", ProviderRepositoryID: "1001", Owner: "owner", Name: "repo", FullName: "owner/repo", CloneURL: "https://github.com/owner/repo.git", WebURL: "https://github.com/owner/repo", MetadataRefreshedAt: now}},
	}
	for _, test := range cases {
		if err := test.value.Validate(); err == nil {
			t.Fatalf("expected validation error for %s", test.name)
		}
	}
}
