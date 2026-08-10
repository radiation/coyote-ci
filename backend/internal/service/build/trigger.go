package build

import "github.com/radiation/coyote-ci/backend/internal/domain"

type CreateBuildTriggerInput struct {
	Kind                   string
	SCMProvider            string
	EventType              string
	RepositoryOwner        string
	RepositoryName         string
	RepositoryURL          string
	RawRef                 string
	Ref                    string
	RefType                string
	RefName                string
	Deleted                *bool
	CommitSHA              string
	DeliveryID             string
	Actor                  string
	ProducerProjectID      string
	ProducerJobID          string
	ProducerBuildID        string
	ArtifactID             string
	ArtifactPath           string
	ArtifactName           string
	ArtifactSizeBytes      *int64
	ArtifactChecksumSHA256 string
	PullRequest            *domain.PullRequestSnapshot
}

func toDomainBuildTrigger(input *CreateBuildTriggerInput) domain.BuildTrigger {
	if input == nil {
		return domain.BuildTrigger{Kind: domain.BuildTriggerKindManual}
	}

	trigger := domain.BuildTrigger{
		Kind:                   domain.BuildTriggerKind(input.Kind),
		SCMProvider:            buildOptionalStringPtr(input.SCMProvider),
		EventType:              buildOptionalStringPtr(input.EventType),
		RepositoryOwner:        buildOptionalStringPtr(input.RepositoryOwner),
		RepositoryName:         buildOptionalStringPtr(input.RepositoryName),
		RepositoryURL:          buildOptionalStringPtr(input.RepositoryURL),
		RawRef:                 buildOptionalStringPtr(input.RawRef),
		Ref:                    buildOptionalStringPtr(input.Ref),
		RefType:                buildOptionalStringPtr(input.RefType),
		RefName:                buildOptionalStringPtr(input.RefName),
		Deleted:                input.Deleted,
		CommitSHA:              buildOptionalStringPtr(input.CommitSHA),
		DeliveryID:             buildOptionalStringPtr(input.DeliveryID),
		Actor:                  buildOptionalStringPtr(input.Actor),
		ProducerProjectID:      buildOptionalStringPtr(input.ProducerProjectID),
		ProducerJobID:          buildOptionalStringPtr(input.ProducerJobID),
		ProducerBuildID:        buildOptionalStringPtr(input.ProducerBuildID),
		ArtifactID:             buildOptionalStringPtr(input.ArtifactID),
		ArtifactPath:           buildOptionalStringPtr(input.ArtifactPath),
		ArtifactName:           buildOptionalStringPtr(input.ArtifactName),
		ArtifactSizeBytes:      input.ArtifactSizeBytes,
		ArtifactChecksumSHA256: buildOptionalStringPtr(input.ArtifactChecksumSHA256),
		PullRequest:            clonePullRequestSnapshot(input.PullRequest),
	}

	return domain.NormalizeBuildTrigger(trigger)
}

func clonePullRequestSnapshot(snapshot *domain.PullRequestSnapshot) *domain.PullRequestSnapshot {
	if snapshot == nil {
		return nil
	}
	copySnapshot := *snapshot
	return &copySnapshot
}
