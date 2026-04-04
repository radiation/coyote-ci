package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrUnsupportedEvent = errors.New("unsupported github webhook event")
var ErrInvalidPayload = errors.New("invalid github push payload")

type PushEvent struct {
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

func VerifySignature(secret string, payload []byte, signatureHeader string) bool {
	secret = strings.TrimSpace(secret)
	provided := strings.TrimSpace(signatureHeader)
	if secret == "" || provided == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func ParsePushEvent(headers http.Header, body []byte) (PushEvent, error) {
	eventType := strings.ToLower(strings.TrimSpace(headers.Get("X-GitHub-Event")))
	if eventType != "push" {
		return PushEvent{}, ErrUnsupportedEvent
	}

	var payload struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		HeadCommit struct {
			ID string `json:"id"`
		} `json:"head_commit"`
		Repository struct {
			Name     string `json:"name"`
			HTMLURL  string `json:"html_url"`
			CloneURL string `json:"clone_url"`
			URL      string `json:"url"`
			Owner    struct {
				Login string `json:"login"`
				Name  string `json:"name"`
			} `json:"owner"`
		} `json:"repository"`
		Sender struct {
			Login string `json:"login"`
		} `json:"sender"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return PushEvent{}, ErrInvalidPayload
	}

	repositoryOwner := strings.TrimSpace(payload.Repository.Owner.Login)
	if repositoryOwner == "" {
		repositoryOwner = strings.TrimSpace(payload.Repository.Owner.Name)
	}
	repositoryName := strings.TrimSpace(payload.Repository.Name)
	repositoryURL := strings.TrimSpace(payload.Repository.HTMLURL)
	if repositoryURL == "" {
		repositoryURL = strings.TrimSpace(payload.Repository.CloneURL)
	}
	if repositoryURL == "" {
		repositoryURL = strings.TrimSpace(payload.Repository.URL)
	}

	normalizedRef := domain.NormalizeWebhookRef(payload.Ref, payload.Deleted)

	commitSHA := strings.TrimSpace(payload.After)
	if commitSHA == "" {
		commitSHA = strings.TrimSpace(payload.HeadCommit.ID)
	}

	if repositoryOwner == "" || repositoryName == "" || normalizedRef.RefName == "" || (!normalizedRef.Deleted && commitSHA == "") {
		return PushEvent{}, ErrInvalidPayload
	}

	return PushEvent{
		EventType:       eventType,
		RepositoryOwner: repositoryOwner,
		RepositoryName:  repositoryName,
		RepositoryURL:   repositoryURL,
		RawRef:          normalizedRef.RawRef,
		Ref:             normalizedRef.RefName,
		RefType:         string(normalizedRef.RefType),
		RefName:         normalizedRef.RefName,
		Deleted:         normalizedRef.Deleted,
		CommitSHA:       commitSHA,
		DeliveryID:      strings.TrimSpace(headers.Get("X-GitHub-Delivery")),
		Actor:           strings.TrimSpace(payload.Sender.Login),
	}, nil
}
