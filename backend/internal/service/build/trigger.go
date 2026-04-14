package build

import "github.com/radiation/coyote-ci/backend/internal/domain"

type CreateBuildTriggerInput struct {
	Kind            string
	SCMProvider     string
	EventType       string
	RepositoryOwner string
	RepositoryName  string
	RepositoryURL   string
	RawRef          string
	Ref             string
	RefType         string
	RefName         string
	Deleted         *bool
	CommitSHA       string
	DeliveryID      string
	Actor           string
}

func toDomainBuildTrigger(input *CreateBuildTriggerInput) domain.BuildTrigger {
	if input == nil {
		return domain.BuildTrigger{Kind: domain.BuildTriggerKindManual}
	}

	trigger := domain.BuildTrigger{
		Kind:            domain.BuildTriggerKind(input.Kind),
		SCMProvider:     buildOptionalStringPtr(input.SCMProvider),
		EventType:       buildOptionalStringPtr(input.EventType),
		RepositoryOwner: buildOptionalStringPtr(input.RepositoryOwner),
		RepositoryName:  buildOptionalStringPtr(input.RepositoryName),
		RepositoryURL:   buildOptionalStringPtr(input.RepositoryURL),
		RawRef:          buildOptionalStringPtr(input.RawRef),
		Ref:             buildOptionalStringPtr(input.Ref),
		RefType:         buildOptionalStringPtr(input.RefType),
		RefName:         buildOptionalStringPtr(input.RefName),
		Deleted:         input.Deleted,
		CommitSHA:       buildOptionalStringPtr(input.CommitSHA),
		DeliveryID:      buildOptionalStringPtr(input.DeliveryID),
		Actor:           buildOptionalStringPtr(input.Actor),
	}

	return domain.NormalizeBuildTrigger(trigger)
}
