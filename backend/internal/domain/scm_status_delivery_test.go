package domain

import (
	"testing"
	"time"
)

func TestSCMStatusDeliveryNormalizeAndHelpers(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	delivery := SCMStatusDelivery{
		BuildID:         " build-1 ",
		Provider:        " GitHub ",
		RepositoryOwner: " octo ",
		RepositoryName:  " repo ",
		CommitSHA:       " abcdef ",
		Context:         " coyote/project/job-1 ",
		DesiredState:    SCMCommitStatusStatePending,
		Status:          SCMStatusDeliveryStatusRetryWaiting,
		Attempts:        1,
		MaxAttempts:     3,
		NextAttemptAt:   &now,
		FailureCategory: scmFailureCategoryPtr(SCMStatusDeliveryFailureCategoryRetryable),
	}

	normalized := delivery.Normalize()
	if normalized.BuildID != "build-1" || normalized.Provider != "github" {
		t.Fatalf("unexpected normalized delivery: %+v", normalized)
	}
	if !normalized.CanAttempt(now) {
		t.Fatal("expected retry waiting delivery to be due at now")
	}
	if err := normalized.Validate(); err != nil {
		t.Fatalf("expected normalized delivery to validate: %v", err)
	}
	if normalized.IsTerminal() {
		t.Fatal("expected retry waiting delivery to be non-terminal")
	}
}

func TestSCMStatusDeliveryValidateSentAndSuperseded(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	sentState := SCMCommitStatusStateSuccess
	sent := SCMStatusDelivery{
		BuildID:         "build-1",
		Provider:        "github",
		RepositoryOwner: "octo",
		RepositoryName:  "repo",
		CommitSHA:       "abcdef",
		Context:         "coyote/project/job-1",
		DesiredState:    SCMCommitStatusStateSuccess,
		LastSentState:   &sentState,
		Status:          SCMStatusDeliveryStatusSent,
		Attempts:        1,
		MaxAttempts:     1,
		SentAt:          &now,
	}
	if err := sent.Validate(); err != nil {
		t.Fatalf("expected sent delivery to validate: %v", err)
	}

	superseded := SCMStatusDelivery{
		BuildID:         "build-2",
		Provider:        "github",
		RepositoryOwner: "octo",
		RepositoryName:  "repo",
		CommitSHA:       "abcdef",
		Context:         "coyote/project/job-1",
		DesiredState:    SCMCommitStatusStateFailure,
		Status:          SCMStatusDeliveryStatusSuperseded,
		Attempts:        1,
		MaxAttempts:     3,
		SupersededAt:    &now,
	}
	if err := superseded.Validate(); err != nil {
		t.Fatalf("expected superseded delivery to validate: %v", err)
	}
	if !superseded.IsTerminal() {
		t.Fatal("expected superseded delivery to be terminal")
	}
}

func scmFailureCategoryPtr(value SCMStatusDeliveryFailureCategory) *SCMStatusDeliveryFailureCategory {
	return &value
}
