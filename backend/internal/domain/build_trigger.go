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
	RawRef          *string
	Ref             *string
	RefType         *string
	RefName         *string
	Deleted         *bool
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
	in.RawRef = trimOptional(in.RawRef)
	in.Ref = trimOptional(in.Ref)
	in.RefType = trimOptional(in.RefType)
	in.RefName = trimOptional(in.RefName)
	in.CommitSHA = trimOptional(in.CommitSHA)
	in.DeliveryID = trimOptional(in.DeliveryID)
	in.Actor = trimOptional(in.Actor)
	if in.RefName == nil && in.Ref != nil {
		in.RefName = in.Ref
	}
	if in.Ref == nil && in.RefName != nil {
		in.Ref = in.RefName
	}
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
