package domain

import "testing"

func TestExecutionJobLifecycleHelpers(t *testing.T) {
	terminalStatuses := []ExecutionJobStatus{ExecutionJobStatusSuccess, ExecutionJobStatusFailed, ExecutionJobStatusCanceled}
	for _, status := range terminalStatuses {
		if !IsTerminalExecutionJobStatus(status) {
			t.Fatalf("expected %q to be terminal", status)
		}
	}

	cancelCases := []struct {
		status ExecutionJobStatus
		want   bool
	}{
		{status: ExecutionJobStatusQueued, want: true},
		{status: ExecutionJobStatusRunning, want: true},
		{status: ExecutionJobStatusSuccess, want: false},
		{status: ExecutionJobStatusFailed, want: false},
		{status: ExecutionJobStatusCanceled, want: false},
	}
	for _, testCase := range cancelCases {
		if got := CanCancelExecutionJob(testCase.status); got != testCase.want {
			t.Fatalf("expected CanCancelExecutionJob(%q)=%v, got %v", testCase.status, testCase.want, got)
		}
	}
}

func TestStepCacheConfigCloneAndPolicyNormalization(t *testing.T) {
	if (*StepCacheConfig)(nil).Clone() != nil {
		t.Fatal("expected nil cache config clone to stay nil")
	}
	original := &StepCacheConfig{Preset: " node ", Policy: CachePolicyPull}
	clone := original.Clone()
	if clone == original {
		t.Fatal("expected clone to allocate a new config")
	}
	if clone.Preset != "node" || clone.Policy != CachePolicyPull {
		t.Fatalf("expected trimmed clone, got %+v", clone)
	}

	policyCases := map[CachePolicy]CachePolicy{
		"":             CachePolicyPullPush,
		" PULL ":       CachePolicyPull,
		"push":         CachePolicyPush,
		"off":          CachePolicyOff,
		"pull-push":    CachePolicyPullPush,
		"unsupported":  CachePolicyPullPush,
		" pull-push ":  CachePolicyPullPush,
		"unsupported ": CachePolicyPullPush,
	}
	for input, want := range policyCases {
		if got := NormalizeCachePolicy(input); got != want {
			t.Fatalf("expected NormalizeCachePolicy(%q)=%q, got %q", input, want, got)
		}
	}
}

func TestSourceSpecAndBuildTriggerNormalization(t *testing.T) {
	if NewSourceSpec(" ", "main", "abc") != nil {
		t.Fatal("expected blank repository URL to return nil source spec")
	}
	source := NewSourceSpec(" https://github.com/acme/repo.git ", " main ", " abc123 ")
	if source == nil || source.RepositoryURL != "https://github.com/acme/repo.git" {
		t.Fatalf("expected trimmed source spec, got %+v", source)
	}
	if source.Ref == nil || *source.Ref != "main" || source.CommitSHA == nil || *source.CommitSHA != "abc123" {
		t.Fatalf("expected optional source fields, got %+v", source)
	}

	refName := " main "
	blank := " "
	trigger := NormalizeBuildTrigger(BuildTrigger{RefName: &refName, Actor: &blank})
	if trigger.Kind != BuildTriggerKindManual {
		t.Fatalf("expected default manual trigger, got %q", trigger.Kind)
	}
	if trigger.Ref == nil || *trigger.Ref != "main" || trigger.RefName == nil || *trigger.RefName != "main" {
		t.Fatalf("expected ref/ref_name backfilled from trimmed ref name, got %+v", trigger)
	}
	if trigger.Actor != nil {
		t.Fatalf("expected blank actor to be cleared, got %+v", trigger.Actor)
	}
}

func TestPriorityNodeAndArtifactParsingHelpers(t *testing.T) {
	if NormalizePriority(0) != DefaultPriority || NormalizePriority(9) != 9 {
		t.Fatalf("unexpected priority normalization")
	}
	if !ValidPriority(MinPriority) || !ValidPriority(MaxPriority) || ValidPriority(MinPriority-1) || ValidPriority(MaxPriority+1) {
		t.Fatalf("unexpected priority validation")
	}
	if got := FallbackNodeID(7); got != "node-007" {
		t.Fatalf("expected node-007, got %q", got)
	}
	if artifactType, ok := ParseArtifactType(" docker_image "); !ok || artifactType != ArtifactTypeDockerImage {
		t.Fatalf("expected docker image artifact type, got type=%q ok=%v", artifactType, ok)
	}
	if artifactType, ok := ParseArtifactType("missing"); ok || artifactType != "" {
		t.Fatalf("expected unsupported artifact type, got type=%q ok=%v", artifactType, ok)
	}
}

func TestExecutionJobSpecSerializationAndDigest(t *testing.T) {
	if BuildSpecDigest("  ") != nil {
		t.Fatal("expected blank spec digest to be nil")
	}
	spec := ExecutionJobSpec{Version: 1, Image: "alpine", Command: []string{"echo", "ok"}}
	jsonSpec, marshalErr := spec.ToJSON()
	if marshalErr != nil {
		t.Fatalf("marshal execution job spec: %v", marshalErr)
	}
	if jsonSpec == "" {
		t.Fatal("expected serialized execution job spec")
	}
	digest := BuildSpecDigest(jsonSpec)
	if digest == nil || *digest == "" {
		t.Fatalf("expected spec digest, got %+v", digest)
	}
}
