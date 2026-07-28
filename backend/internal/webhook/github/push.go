package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrUnsupportedEvent = errors.New("unsupported github webhook event")
var ErrInvalidPayload = errors.New("invalid github push payload")

type PushEvent struct {
	EventType            string
	RepositoryOwner      string
	RepositoryName       string
	ProviderRepositoryID string
	RepositoryURL        string
	InstallationID       string
	RawRef               string
	Ref                  string
	RefType              string
	RefName              string
	Deleted              bool
	CommitSHA            string
	DeliveryID           string
	Actor                string
}

type AppEnvelope struct {
	InstallationID string
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

	envelope, envelopeErr := ParseAppEnvelope(body)
	if envelopeErr != nil {
		return PushEvent{}, envelopeErr
	}

	var payload struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		HeadCommit struct {
			ID string `json:"id"`
		} `json:"head_commit"`
		Repository struct {
			ID       json.RawMessage `json:"id"`
			Name     string          `json:"name"`
			HTMLURL  string          `json:"html_url"`
			CloneURL string          `json:"clone_url"`
			URL      string          `json:"url"`
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
	providerRepositoryID, providerRepositoryIDErr := parseProviderRepositoryID(payload.Repository.ID)
	if providerRepositoryIDErr != nil {
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
		EventType:            eventType,
		RepositoryOwner:      repositoryOwner,
		RepositoryName:       repositoryName,
		ProviderRepositoryID: providerRepositoryID,
		RepositoryURL:        repositoryURL,
		InstallationID:       envelope.InstallationID,
		RawRef:               normalizedRef.RawRef,
		Ref:                  normalizedRef.RefName,
		RefType:              string(normalizedRef.RefType),
		RefName:              normalizedRef.RefName,
		Deleted:              normalizedRef.Deleted,
		CommitSHA:            commitSHA,
		DeliveryID:           strings.TrimSpace(headers.Get("X-GitHub-Delivery")),
		Actor:                strings.TrimSpace(payload.Sender.Login),
	}, nil
}

func parseProviderRepositoryID(raw json.RawMessage) (string, error) {
	return parsePositiveIntegerID(raw)
}

func ParseAppEnvelope(body []byte) (AppEnvelope, error) {
	var payload struct {
		Installation struct {
			ID json.RawMessage `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return AppEnvelope{}, ErrInvalidPayload
	}
	installationID, err := parseInstallationID(payload.Installation.ID)
	if err != nil {
		return AppEnvelope{}, ErrInvalidPayload
	}
	return AppEnvelope{InstallationID: installationID}, nil
}

func parseInstallationID(raw json.RawMessage) (string, error) {
	return parsePositiveIntegerID(raw)
}

func parsePositiveIntegerID(raw json.RawMessage) (string, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.HasPrefix(value, `"`) {
		return "", ErrInvalidPayload
	}
	installationID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || installationID <= 0 {
		return "", ErrInvalidPayload
	}
	return strconv.FormatInt(installationID, 10), nil
}
