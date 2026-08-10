package domain

import (
	"fmt"
	"net/url"
	"strings"
)

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

type PullRequestSourceMode string

const PullRequestSourceModeHead PullRequestSourceMode = "head"

type PullRequestSnapshot struct {
	Number     int64
	Action     string
	URL        string
	BaseRef    string
	BaseSHA    string
	HeadRef    string
	HeadSHA    string
	SourceMode PullRequestSourceMode
}

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
	PullRequest            *PullRequestSnapshot
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
	if in.PullRequest != nil {
		snapshot := normalizePullRequestSnapshot(*in.PullRequest)
		in.PullRequest = &snapshot
	}
	if in.RefName == nil && in.Ref != nil {
		in.RefName = in.Ref
	}
	if in.Ref == nil && in.RefName != nil {
		in.Ref = in.RefName
	}
	return in
}

func (t BuildTrigger) Validate() error {
	if t.PullRequest == nil {
		return nil
	}
	if err := ValidatePullRequestSnapshot(*t.PullRequest); err != nil {
		return err
	}
	if t.Kind != BuildTriggerKindWebhook || t.SCMProvider == nil || *t.SCMProvider != "github" || t.EventType == nil || *t.EventType != "pull_request" {
		return fmt.Errorf("pull request snapshot requires a GitHub pull_request webhook trigger")
	}
	if t.CommitSHA == nil || *t.CommitSHA != t.PullRequest.HeadSHA {
		return fmt.Errorf("pull request trigger commit SHA must match the head SHA")
	}
	if t.Ref == nil || *t.Ref != t.PullRequest.HeadRef {
		return fmt.Errorf("pull request trigger ref must match the head ref")
	}
	if t.RefName == nil || *t.RefName != t.PullRequest.HeadRef {
		return fmt.Errorf("pull request trigger ref name must match the head ref")
	}
	return nil
}

func ValidatePullRequestSnapshot(snapshot PullRequestSnapshot) error {
	snapshot = normalizePullRequestSnapshot(snapshot)
	if snapshot.Number <= 0 {
		return fmt.Errorf("pull request number must be positive")
	}
	if !isSupportedPullRequestAction(snapshot.Action) {
		return fmt.Errorf("pull request action is unsupported")
	}
	parsedURL, err := url.Parse(snapshot.URL)
	if err != nil || !parsedURL.IsAbs() || !strings.EqualFold(parsedURL.Scheme, "https") || strings.TrimSpace(parsedURL.Host) == "" {
		return fmt.Errorf("pull request URL must be an absolute HTTPS URL")
	}
	if snapshot.BaseRef == "" || snapshot.BaseSHA == "" || snapshot.HeadRef == "" || snapshot.HeadSHA == "" {
		return fmt.Errorf("pull request base and head refs and SHAs are required")
	}
	if snapshot.SourceMode != PullRequestSourceModeHead {
		return fmt.Errorf("pull request source mode must be head")
	}
	return nil
}

func normalizePullRequestSnapshot(snapshot PullRequestSnapshot) PullRequestSnapshot {
	snapshot.Action = strings.ToLower(strings.TrimSpace(snapshot.Action))
	snapshot.URL = strings.TrimSpace(snapshot.URL)
	snapshot.BaseRef = strings.TrimSpace(snapshot.BaseRef)
	snapshot.BaseSHA = strings.TrimSpace(snapshot.BaseSHA)
	snapshot.HeadRef = strings.TrimSpace(snapshot.HeadRef)
	snapshot.HeadSHA = strings.TrimSpace(snapshot.HeadSHA)
	snapshot.SourceMode = PullRequestSourceMode(strings.TrimSpace(string(snapshot.SourceMode)))
	return snapshot
}

func isSupportedPullRequestAction(action string) bool {
	switch action {
	case "opened", "reopened", "synchronize":
		return true
	default:
		return false
	}
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
