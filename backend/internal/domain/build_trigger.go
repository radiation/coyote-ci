package domain

import "strings"

type BuildTriggerType string

const (
	BuildTriggerTypeManual   BuildTriggerType = "manual"
	BuildTriggerTypeRerun    BuildTriggerType = "rerun"
	BuildTriggerTypeWebhook  BuildTriggerType = "webhook"
	BuildTriggerTypeArtifact BuildTriggerType = "artifact"
	BuildTriggerTypeSchedule BuildTriggerType = "schedule"
	BuildTriggerTypeAPI      BuildTriggerType = "api"
)

type BuildTriggerKind string

const (
	BuildTriggerKindManual   BuildTriggerKind = "manual"
	BuildTriggerKindWebhook  BuildTriggerKind = "webhook"
	BuildTriggerKindArtifact BuildTriggerKind = "artifact"
)

type BuildTrigger struct {
	Kind                   BuildTriggerKind
	SCMProvider            *string
	EventType              *string
	RepositoryOwner        *string
	RepositoryName         *string
	RepositoryURL          *string
	RawRef                 *string
	Ref                    *string
	RefType                *string
	RefName                *string
	Deleted                *bool
	CommitSHA              *string
	DeliveryID             *string
	Actor                  *string
	ProducerProjectID      *string
	ProducerJobID          *string
	ProducerBuildID        *string
	ArtifactID             *string
	ArtifactPath           *string
	ArtifactName           *string
	ArtifactSizeBytes      *int64
	ArtifactChecksumSHA256 *string
}

func NormalizeBuildTrigger(in BuildTrigger) BuildTrigger {
	trimmedKind := strings.TrimSpace(string(in.Kind))
	if trimmedKind == "" {
		in.Kind = BuildTriggerKindManual
	} else {
		in.Kind = BuildTriggerKind(trimmedKind)
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
	in.ProducerProjectID = trimOptional(in.ProducerProjectID)
	in.ProducerJobID = trimOptional(in.ProducerJobID)
	in.ProducerBuildID = trimOptional(in.ProducerBuildID)
	in.ArtifactID = trimOptional(in.ArtifactID)
	in.ArtifactPath = trimOptional(in.ArtifactPath)
	in.ArtifactName = trimOptional(in.ArtifactName)
	in.ArtifactChecksumSHA256 = trimOptional(in.ArtifactChecksumSHA256)
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

func NormalizeBuildMetadata(in Build) Build {
	trigger := NormalizeBuildTrigger(in.Trigger)
	in.Trigger = trigger
	in.SourceRef = trimOptional(in.SourceRef)
	in.SourceSHA = trimOptional(in.SourceSHA)
	in.SourceAuthorName = trimOptional(in.SourceAuthorName)
	in.SourceAuthorEmail = trimOptional(in.SourceAuthorEmail)
	in.SourceCommitterName = trimOptional(in.SourceCommitterName)
	in.SourceCommitterEmail = trimOptional(in.SourceCommitterEmail)
	in.TriggeredBy = trimOptional(in.TriggeredBy)

	if in.SourceRef == nil {
		switch {
		case in.Source != nil && in.Source.Ref != nil:
			in.SourceRef = trimOptional(in.Source.Ref)
		case in.Ref != nil:
			in.SourceRef = trimOptional(in.Ref)
		case trigger.Ref != nil:
			in.SourceRef = trimOptional(trigger.Ref)
		}
	}

	if in.SourceSHA == nil {
		switch {
		case in.Source != nil && in.Source.CommitSHA != nil:
			in.SourceSHA = trimOptional(in.Source.CommitSHA)
		case in.CommitSHA != nil:
			in.SourceSHA = trimOptional(in.CommitSHA)
		case trigger.CommitSHA != nil:
			in.SourceSHA = trimOptional(trigger.CommitSHA)
		}
	}

	if in.TriggeredBy == nil {
		in.TriggeredBy = trimOptional(trigger.Actor)
	}

	trimmedTriggerType := strings.TrimSpace(string(in.TriggerType))
	if trimmedTriggerType == "" {
		switch {
		case in.RerunOfBuildID != nil:
			in.TriggerType = BuildTriggerTypeRerun
		case trigger.Kind == BuildTriggerKindWebhook:
			in.TriggerType = BuildTriggerTypeWebhook
		case strings.TrimSpace(string(trigger.Kind)) != "":
			in.TriggerType = BuildTriggerType(strings.TrimSpace(string(trigger.Kind)))
		default:
			in.TriggerType = BuildTriggerTypeManual
		}
	} else {
		in.TriggerType = BuildTriggerType(trimmedTriggerType)
	}

	return in
}
