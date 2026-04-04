package service

import "github.com/radiation/coyote-ci/backend/internal/domain"

type CreateBuildTriggerInput struct {
	Kind            string
	SCMProvider     string
	EventType       string
	RepositoryOwner string
	RepositoryName  string
	RepositoryURL   string
	Ref             string
	RefType         string
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
		Ref:             optionalStringPtr(input.Ref),
		RefType:         optionalStringPtr(input.RefType),
		CommitSHA:       optionalStringPtr(input.CommitSHA),
		DeliveryID:      optionalStringPtr(input.DeliveryID),
		Actor:           optionalStringPtr(input.Actor),
	}

	return domain.NormalizeBuildTrigger(trigger)
}
