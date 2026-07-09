package domain

import "testing"

func TestNormalizeJobArtifactTrigger_TrimsFields(t *testing.T) {
	trigger := NormalizeJobArtifactTrigger(JobArtifactTrigger{
		ProducerJobID: "  producer-job  ",
		Path:          "  dist/app.tgz  ",
	})

	if trigger.ProducerJobID != "producer-job" {
		t.Fatalf("expected trimmed producer job id, got %q", trigger.ProducerJobID)
	}
	if trigger.Path != "dist/app.tgz" {
		t.Fatalf("expected trimmed path, got %q", trigger.Path)
	}
}

func TestNormalizeJobArtifactTriggers_DedupesAndDropsInvalid(t *testing.T) {
	triggers := NormalizeJobArtifactTriggers([]JobArtifactTrigger{
		{ProducerJobID: " producer-a ", Path: " dist/app.tgz "},
		{ProducerJobID: "producer-a", Path: "dist/app.tgz"},
		{ProducerJobID: "producer-b", Path: "release/app.tgz"},
		{ProducerJobID: " ", Path: "release/app.tgz"},
		{ProducerJobID: "producer-c", Path: " "},
	})

	if len(triggers) != 2 {
		t.Fatalf("expected two normalized triggers, got %#v", triggers)
	}
	if triggers[0].ProducerJobID != "producer-a" || triggers[0].Path != "dist/app.tgz" {
		t.Fatalf("unexpected first trigger: %#v", triggers[0])
	}
	if triggers[1].ProducerJobID != "producer-b" || triggers[1].Path != "release/app.tgz" {
		t.Fatalf("unexpected second trigger: %#v", triggers[1])
	}

	if got := NormalizeJobArtifactTriggers(nil); got != nil {
		t.Fatalf("expected nil for nil input, got %#v", got)
	}
	if got := NormalizeJobArtifactTriggers([]JobArtifactTrigger{{ProducerJobID: " ", Path: " "}}); got != nil {
		t.Fatalf("expected nil when all triggers are invalid, got %#v", got)
	}
}
