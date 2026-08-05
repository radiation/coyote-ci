package github

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type PullRequestEvent struct {
	EventType            string
	Action               string
	SupportedAction      bool
	SameRepository       bool
	RepositoryOwner      string
	RepositoryName       string
	ProviderRepositoryID string
	RepositoryURL        string
	InstallationID       string
	RawRef               string
	Ref                  string
	RefType              string
	RefName              string
	CommitSHA            string
	PullRequest          *domain.PullRequestSnapshot
	DeliveryID           string
	Actor                string
}

func ParsePullRequestEvent(headers http.Header, body []byte) (PullRequestEvent, error) {
	if strings.ToLower(strings.TrimSpace(headers.Get("X-GitHub-Event"))) != "pull_request" {
		return PullRequestEvent{}, ErrUnsupportedEvent
	}
	envelope, err := ParseAppEnvelope(body)
	if err != nil {
		return PullRequestEvent{}, err
	}
	var payload struct {
		Action     string `json:"action"`
		Repository struct {
			ID      json.RawMessage `json:"id"`
			Name    string          `json:"name"`
			HTMLURL string          `json:"html_url"`
			Owner   struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
		PullRequest struct {
			Number  json.RawMessage `json:"number"`
			HTMLURL string          `json:"html_url"`
			Base    struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"base"`
			Head struct {
				Ref  string `json:"ref"`
				SHA  string `json:"sha"`
				Repo *struct {
					ID json.RawMessage `json:"id"`
				} `json:"repo"`
			} `json:"head"`
		} `json:"pull_request"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}
	if unmarshalErr := json.Unmarshal(body, &payload); unmarshalErr != nil {
		return PullRequestEvent{}, ErrInvalidPayload
	}
	repositoryID, err := parseProviderRepositoryID(payload.Repository.ID)
	if err != nil || strings.TrimSpace(payload.Action) == "" || strings.TrimSpace(payload.Repository.Owner.Login) == "" || strings.TrimSpace(payload.Repository.Name) == "" {
		return PullRequestEvent{}, ErrInvalidPayload
	}
	action := strings.ToLower(strings.TrimSpace(payload.Action))
	event := PullRequestEvent{
		EventType:            "pull_request",
		Action:               action,
		RepositoryOwner:      strings.TrimSpace(payload.Repository.Owner.Login),
		RepositoryName:       strings.TrimSpace(payload.Repository.Name),
		ProviderRepositoryID: repositoryID,
		RepositoryURL:        strings.TrimSpace(payload.Repository.HTMLURL),
		InstallationID:       envelope.InstallationID,
		DeliveryID:           strings.TrimSpace(headers.Get("X-GitHub-Delivery")),
		Actor:                strings.TrimSpace(payload.Sender.Login),
	}
	event.SupportedAction = event.Action == "opened" || event.Action == "reopened" || event.Action == "synchronize"
	if payload.PullRequest.Head.Repo == nil {
		return event, nil
	}
	headRepositoryID, err := parseProviderRepositoryID(payload.PullRequest.Head.Repo.ID)
	if err != nil {
		return PullRequestEvent{}, ErrInvalidPayload
	}
	event.SameRepository = headRepositoryID == repositoryID
	if !event.SupportedAction || !event.SameRepository {
		return event, nil
	}
	event.Ref = strings.TrimSpace(payload.PullRequest.Head.Ref)
	event.RefName = event.Ref
	event.RawRef = "refs/heads/" + event.Ref
	event.RefType = "branch"
	event.CommitSHA = strings.TrimSpace(payload.PullRequest.Head.SHA)
	prNumberText, numberErr := parsePositiveIntegerID(payload.PullRequest.Number)
	prNumber, parseNumberErr := strconv.ParseInt(prNumberText, 10, 64)
	if numberErr != nil || parseNumberErr != nil || event.Ref == "" || event.CommitSHA == "" {
		return PullRequestEvent{}, ErrInvalidPayload
	}
	snapshot := domain.NormalizeBuildTrigger(domain.BuildTrigger{PullRequest: &domain.PullRequestSnapshot{
		Number:     prNumber,
		Action:     action,
		URL:        payload.PullRequest.HTMLURL,
		BaseRef:    payload.PullRequest.Base.Ref,
		BaseSHA:    payload.PullRequest.Base.SHA,
		HeadRef:    event.Ref,
		HeadSHA:    event.CommitSHA,
		SourceMode: domain.PullRequestSourceModeHead,
	}}).PullRequest
	if snapshot == nil || domain.ValidatePullRequestSnapshot(*snapshot) != nil {
		return PullRequestEvent{}, ErrInvalidPayload
	}
	event.PullRequest = snapshot
	return event, nil
}
