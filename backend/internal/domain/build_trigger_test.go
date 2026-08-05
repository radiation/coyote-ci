package domain

import "testing"

func TestValidatePullRequestSnapshot(t *testing.T) {
	valid := PullRequestSnapshot{
		Number:     42,
		Action:     "opened",
		URL:        "https://github.example.com/acme/repo/pull/42",
		BaseRef:    "main",
		BaseSHA:    "base-sha",
		HeadRef:    "feature/pr-42",
		HeadSHA:    "head-sha",
		SourceMode: PullRequestSourceModeHead,
	}
	if err := ValidatePullRequestSnapshot(valid); err != nil {
		t.Fatalf("validate valid snapshot: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*PullRequestSnapshot)
	}{
		{name: "number", mutate: func(snapshot *PullRequestSnapshot) { snapshot.Number = 0 }},
		{name: "action", mutate: func(snapshot *PullRequestSnapshot) { snapshot.Action = "closed" }},
		{name: "URL", mutate: func(snapshot *PullRequestSnapshot) { snapshot.URL = "http://github.example.com/pr/42" }},
		{name: "base ref", mutate: func(snapshot *PullRequestSnapshot) { snapshot.BaseRef = "" }},
		{name: "base SHA", mutate: func(snapshot *PullRequestSnapshot) { snapshot.BaseSHA = "" }},
		{name: "head ref", mutate: func(snapshot *PullRequestSnapshot) { snapshot.HeadRef = "" }},
		{name: "head SHA", mutate: func(snapshot *PullRequestSnapshot) { snapshot.HeadSHA = "" }},
		{name: "source mode", mutate: func(snapshot *PullRequestSnapshot) { snapshot.SourceMode = "merge" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := valid
			test.mutate(&snapshot)
			if err := ValidatePullRequestSnapshot(snapshot); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNormalizeBuildTrigger_NormalizesPullRequestSnapshot(t *testing.T) {
	trigger := NormalizeBuildTrigger(BuildTrigger{PullRequest: &PullRequestSnapshot{
		Number:     42,
		Action:     " OPENED ",
		URL:        " https://github.example.com/acme/repo/pull/42 ",
		BaseRef:    " main ",
		BaseSHA:    " base-sha ",
		HeadRef:    " feature/pr-42 ",
		HeadSHA:    " head-sha ",
		SourceMode: " head ",
	}})
	if trigger.PullRequest == nil {
		t.Fatal("expected pull request snapshot")
	}
	if trigger.PullRequest.Action != "opened" || trigger.PullRequest.BaseRef != "main" || trigger.PullRequest.SourceMode != PullRequestSourceModeHead {
		t.Fatalf("unexpected normalized snapshot: %+v", trigger.PullRequest)
	}
}

func TestBuildTriggerValidatePullRequestContract(t *testing.T) {
	github := "github"
	pullRequest := "pull_request"
	headRef := "feature/pr-42"
	headSHA := "head-sha"
	valid := BuildTrigger{
		Kind:        BuildTriggerKindWebhook,
		SCMProvider: &github,
		EventType:   &pullRequest,
		Ref:         &headRef,
		RefName:     &headRef,
		CommitSHA:   &headSHA,
		PullRequest: &PullRequestSnapshot{
			Number:     42,
			Action:     "opened",
			URL:        "https://github.example.com/acme/repo/pull/42",
			BaseRef:    "main",
			BaseSHA:    "base-sha",
			HeadRef:    headRef,
			HeadSHA:    headSHA,
			SourceMode: PullRequestSourceModeHead,
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("validate valid PR trigger: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*BuildTrigger)
	}{
		{name: "wrong provider", mutate: func(trigger *BuildTrigger) { provider := "gitlab"; trigger.SCMProvider = &provider }},
		{name: "wrong event", mutate: func(trigger *BuildTrigger) { event := "push"; trigger.EventType = &event }},
		{name: "commit SHA mismatch", mutate: func(trigger *BuildTrigger) { sha := "other-sha"; trigger.CommitSHA = &sha }},
		{name: "ref mismatch", mutate: func(trigger *BuildTrigger) { ref := "other-ref"; trigger.Ref = &ref }},
		{name: "ref name mismatch", mutate: func(trigger *BuildTrigger) { refName := "other-ref"; trigger.RefName = &refName }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trigger := valid
			test.mutate(&trigger)
			if err := trigger.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
