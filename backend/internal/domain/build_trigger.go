package domain

import "strings"

type BuildTriggerKind string

const (
	BuildTriggerKindManual  BuildTriggerKind = "manual"
	BuildTriggerKindWebhook BuildTriggerKind = "webhook"
)

type BuildTrigger struct {
	Kind            BuildTriggerKind
	SCMProvider     *string
	EventType       *string
	RepositoryOwner *string
	RepositoryName  *string
	RepositoryURL   *string
	Ref             *string
	RefType         *string
	CommitSHA       *string
	DeliveryID      *string
	Actor           *string
}

func NormalizeBuildTrigger(in BuildTrigger) BuildTrigger {
	if strings.TrimSpace(string(in.Kind)) == "" {
		in.Kind = BuildTriggerKindManual
	}
	in.SCMProvider = trimOptional(in.SCMProvider)
	in.EventType = trimOptional(in.EventType)
	in.RepositoryOwner = trimOptional(in.RepositoryOwner)
	in.RepositoryName = trimOptional(in.RepositoryName)
	in.RepositoryURL = trimOptional(in.RepositoryURL)
	in.Ref = trimOptional(in.Ref)
	in.RefType = trimOptional(in.RefType)
	in.CommitSHA = trimOptional(in.CommitSHA)
	in.DeliveryID = trimOptional(in.DeliveryID)
	in.Actor = trimOptional(in.Actor)
	return in
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
