package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	payload := []byte(`{"hello":"world"}`)
	secret := "topsecret"

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifySignature(secret, payload, sig) {
		t.Fatal("expected valid signature")
	}
	if VerifySignature(secret, payload, "sha256=bad") {
		t.Fatal("expected invalid signature to fail")
	}
}

func TestParsePushEvent(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "push")
	headers.Set("X-GitHub-Delivery", "delivery-1")

	body := []byte(`{
		"ref":"refs/heads/main",
		"after":"abc123",
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"bryan"}
	}`)

	event, err := ParsePushEvent(headers, body)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.RepositoryOwner != "example" || event.RepositoryName != "backend" {
		t.Fatalf("unexpected repository identity: %+v", event)
	}
	if event.Ref != "main" || event.RefType != "branch" {
		t.Fatalf("unexpected ref parsing: %+v", event)
	}
	if event.RawRef != "refs/heads/main" {
		t.Fatalf("expected raw ref refs/heads/main, got %q", event.RawRef)
	}
	if event.CommitSHA != "abc123" {
		t.Fatalf("unexpected commit sha: %s", event.CommitSHA)
	}
}

func TestParsePushEvent_TagRef(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "push")

	body := []byte(`{
		"ref":"refs/tags/v1.2.3",
		"after":"abc123",
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"bryan"}
	}`)

	event, err := ParsePushEvent(headers, body)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.RefType != "tag" || event.RefName != "v1.2.3" {
		t.Fatalf("expected tag ref v1.2.3, got type=%q name=%q", event.RefType, event.RefName)
	}
}

func TestParsePushEvent_UnknownRef(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "push")

	body := []byte(`{
		"ref":"custom/ref/path",
		"after":"abc123",
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"bryan"}
	}`)

	event, err := ParsePushEvent(headers, body)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if event.RefType != "unknown" || event.RefName != "custom/ref/path" {
		t.Fatalf("expected unknown ref custom/ref/path, got type=%q name=%q", event.RefType, event.RefName)
	}
}

func TestParsePushEvent_DeletePushAllowedWithoutCommit(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "push")

	body := []byte(`{
		"ref":"refs/heads/main",
		"deleted":true,
		"after":"",
		"repository":{
			"name":"backend",
			"html_url":"https://github.com/example/backend",
			"owner":{"login":"example"}
		},
		"sender":{"login":"bryan"}
	}`)

	event, err := ParsePushEvent(headers, body)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !event.Deleted {
		t.Fatal("expected deleted=true")
	}
}

func TestParsePushEvent_UnsupportedEvent(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "pull_request")

	_, err := ParsePushEvent(headers, []byte(`{}`))
	if err == nil {
		t.Fatal("expected unsupported event error")
	}
	if err != ErrUnsupportedEvent {
		t.Fatalf("expected ErrUnsupportedEvent, got %v", err)
	}
}
