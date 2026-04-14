package webhook

import "github.com/radiation/coyote-ci/backend/internal/domain"

type WebhookMatchedBuild struct {
	Job   domain.Job
	Build domain.Build
}

type WebhookTriggerInput struct {
	SCMProvider     string
	EventType       string
	RepositoryOwner string
	RepositoryName  string
	RepositoryURL   string
	RawRef          string
	Ref             string
	RefType         string
	RefName         string
	Deleted         bool
	CommitSHA       string
	DeliveryID      string
	Actor           string
}

type WebhookTriggerResult struct {
	SCMProvider   string
	EventType     string
	RepositoryURL string
	RawRef        string
	Ref           string
	RefType       string
	RefName       string
	Deleted       bool
	CommitSHA     string
	MatchedJobs   int
	NoMatchReason *string
	Builds        []WebhookMatchedBuild
}
