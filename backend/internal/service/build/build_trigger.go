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
		SCMProvider:     optionalStringPtr(input.SCMProvider),
		EventType:       optionalStringPtr(input.EventType),
		RepositoryOwner: optionalStringPtr(input.RepositoryOwner),
		RepositoryName:  optionalStringPtr(input.RepositoryName),
		RepositoryURL:   optionalStringPtr(input.RepositoryURL),
		RawRef:          optionalStringPtr(input.RawRef),
		Ref:             optionalStringPtr(input.Ref),
		RefType:         optionalStringPtr(input.RefType),
		RefName:         optionalStringPtr(input.RefName),
		Deleted:         input.Deleted,
		CommitSHA:       optionalStringPtr(input.CommitSHA),
		DeliveryID:      optionalStringPtr(input.DeliveryID),
		Actor:           optionalStringPtr(input.Actor),
	}

	return domain.NormalizeBuildTrigger(trigger)
}
