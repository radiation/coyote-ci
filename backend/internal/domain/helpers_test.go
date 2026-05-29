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

	webhookKind := BuildTriggerKind(" webhook ")
	webhookTrigger := NormalizeBuildTrigger(BuildTrigger{Kind: webhookKind})
	if webhookTrigger.Kind != BuildTriggerKindWebhook {
		t.Fatalf("expected trimmed webhook trigger kind, got %q", webhookTrigger.Kind)
	}
}

func TestNormalizeBuildMetadata(t *testing.T) {
	rerunOfBuildID := "build-1"
	actor := " octocat "

	tests := []struct {
		name                string
		in                  Build
		wantSourceRef       *string
		wantSourceSHA       *string
		wantTriggeredBy     *string
		wantTriggerType     BuildTriggerType
		wantNormalizedRef   *string
		wantNormalizedActor *string
	}{
		{
			name: "prefers explicit metadata and trims provided trigger type",
			in: Build{
				SourceRef:   stringPtr(" feature "),
				SourceSHA:   stringPtr(" deadbeef "),
				TriggeredBy: stringPtr(" cli-user "),
				TriggerType: BuildTriggerType(" api "),
				Trigger: BuildTrigger{
					Kind:  BuildTriggerKindWebhook,
					Actor: &actor,
				},
			},
			wantSourceRef:       stringPtr("feature"),
			wantSourceSHA:       stringPtr("deadbeef"),
			wantTriggeredBy:     stringPtr("cli-user"),
			wantTriggerType:     BuildTriggerTypeAPI,
			wantNormalizedActor: stringPtr("octocat"),
		},
		{
			name: "derives metadata from source spec first",
			in: Build{
				Source: NewSourceSpec("https://github.com/acme/repo.git", " main ", " abc123 "),
				Trigger: BuildTrigger{
					Kind:  BuildTriggerKindManual,
					Actor: &actor,
				},
			},
			wantSourceRef:       stringPtr("main"),
			wantSourceSHA:       stringPtr("abc123"),
			wantTriggeredBy:     stringPtr("octocat"),
			wantTriggerType:     BuildTriggerTypeManual,
			wantNormalizedActor: stringPtr("octocat"),
		},
		{
			name: "falls back to repo ref and commit sha",
			in: Build{
				Ref:       stringPtr(" release/v1 "),
				CommitSHA: stringPtr(" cafe123 "),
				Trigger:   BuildTrigger{Kind: BuildTriggerKindWebhook},
			},
			wantSourceRef:   stringPtr("release/v1"),
			wantSourceSHA:   stringPtr("cafe123"),
			wantTriggerType: BuildTriggerTypeWebhook,
		},
		{
			name: "falls back to trigger ref and commit and marks rerun",
			in: Build{
				RerunOfBuildID: &rerunOfBuildID,
				Trigger: BuildTrigger{
					Kind:      BuildTriggerKindWebhook,
					Ref:       stringPtr(" refs/heads/main "),
					CommitSHA: stringPtr(" ff00aa "),
					Actor:     &actor,
				},
			},
			wantSourceRef:       stringPtr("refs/heads/main"),
			wantSourceSHA:       stringPtr("ff00aa"),
			wantTriggeredBy:     stringPtr("octocat"),
			wantTriggerType:     BuildTriggerTypeRerun,
			wantNormalizedRef:   stringPtr("refs/heads/main"),
			wantNormalizedActor: stringPtr("octocat"),
		},
		{
			name:            "defaults to manual when no signal is available",
			in:              Build{},
			wantTriggerType: BuildTriggerTypeManual,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeBuildMetadata(tc.in)

			assertOptionalStringEqual(t, "source_ref", got.SourceRef, tc.wantSourceRef)
			assertOptionalStringEqual(t, "source_sha", got.SourceSHA, tc.wantSourceSHA)
			assertOptionalStringEqual(t, "triggered_by", got.TriggeredBy, tc.wantTriggeredBy)
			if got.TriggerType != tc.wantTriggerType {
				t.Fatalf("expected trigger type %q, got %q", tc.wantTriggerType, got.TriggerType)
			}
			if tc.wantNormalizedRef != nil {
				assertOptionalStringEqual(t, "trigger.ref", got.Trigger.Ref, tc.wantNormalizedRef)
			}
			if tc.wantNormalizedActor != nil {
				assertOptionalStringEqual(t, "trigger.actor", got.Trigger.Actor, tc.wantNormalizedActor)
			}
		})
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

func assertOptionalStringEqual(t *testing.T, field string, got *string, want *string) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("expected %s nil, got %v", field, got)
		}
		return
	}
	if got == nil || *got != *want {
		t.Fatalf("expected %s=%q, got %v", field, *want, got)
	}
}

func stringPtr(value string) *string {
	return &value
}
