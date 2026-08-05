package github

import (
	"errors"
	"net/http"
	"testing"
)

func TestParsePullRequestEvent_AcceptsSupportedSameRepositoryActions(t *testing.T) {
	for _, action := range []string{"opened", "reopened", "synchronize"} {
		t.Run(action, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("X-GitHub-Event", "pull_request")
			headers.Set("X-GitHub-Delivery", "delivery-1")
			body := []byte(`{"action":"` + action + `","installation":{"id":999},"repository":{"id":1001,"name":"backend","html_url":"https://github.com/example/backend","owner":{"login":"example"}},"pull_request":{"number":42,"html_url":"https://github.example.com/example/backend/pull/42","base":{"ref":"main","sha":"base-sha"},"head":{"ref":"feature/pr","sha":"head-sha","repo":{"id":1001}}},"sender":{"login":"octocat"}}`)

			event, err := ParsePullRequestEvent(headers, body)
			if err != nil {
				t.Fatalf("parse event: %v", err)
			}
			if !event.SupportedAction || !event.SameRepository || event.Ref != "feature/pr" || event.CommitSHA != "head-sha" || event.PullRequest == nil || event.PullRequest.Number != 42 || event.PullRequest.SourceMode != "head" {
				t.Fatalf("unexpected event: %+v", event)
			}
		})
	}
}

func TestParsePullRequestEvent_RejectsMalformedSupportedPayloads(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-GitHub-Event", "pull_request")
	for _, body := range [][]byte{
		[]byte(`{`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":"1001","name":"backend","owner":{"login":"example"}},"pull_request":{"number":42,"html_url":"https://github.example.com/pr/42","base":{"ref":"main","sha":"base"},"head":{"ref":"feature","sha":"head","repo":{"id":1001}}}}`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"number":0,"html_url":"https://github.example.com/pr/42","base":{"ref":"main","sha":"base"},"head":{"ref":"feature","sha":"head","repo":{"id":1001}}}}`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"number":42,"html_url":"http://github.example.com/pr/42","base":{"ref":"main","sha":"base"},"head":{"ref":"feature","sha":"head","repo":{"id":1001}}}}`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"number":42,"html_url":"https://github.example.com/pr/42","base":{"ref":"","sha":"base"},"head":{"ref":"feature","sha":"head","repo":{"id":1001}}}}`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"number":42,"html_url":"https://github.example.com/pr/42","base":{"ref":"main","sha":""},"head":{"ref":"feature","sha":"head","repo":{"id":1001}}}}`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"number":42,"html_url":"https://github.example.com/pr/42","base":{"ref":"main","sha":"base"},"head":{"ref":"","sha":"head","repo":{"id":1001}}}}`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"number":42,"html_url":"https://github.example.com/pr/42","base":{"ref":"main","sha":"base"},"head":{"ref":"feature","sha":"","repo":{"id":1001}}}}`),
	} {
		_, err := ParsePullRequestEvent(headers, body)
		if !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("expected invalid payload, got %v", err)
		}
	}
}

func TestParsePullRequestEvent_RejectsWrongEventAndInvalidHeadRepositoryID(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-GitHub-Event", "push")
	if _, err := ParsePullRequestEvent(headers, []byte(`{}`)); !errors.Is(err, ErrUnsupportedEvent) {
		t.Fatalf("expected unsupported event, got %v", err)
	}

	headers.Set("X-GitHub-Event", "pull_request")
	body := []byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"head":{"ref":"feature","sha":"head","repo":{"id":1.5}}}}`)
	if _, err := ParsePullRequestEvent(headers, body); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("expected invalid head repository ID, got %v", err)
	}
}

func TestParsePullRequestEvent_UnsupportedForkAndMissingHeadAreNoOps(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-GitHub-Event", "pull_request")
	for _, body := range [][]byte{
		[]byte(`{"action":"closed","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"head":{"ref":"feature","sha":"head","repo":{"id":1001}}}}`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"head":{"ref":"feature","sha":"head","repo":{"id":2002}}}}`),
		[]byte(`{"action":"opened","installation":{"id":999},"repository":{"id":1001,"name":"backend","owner":{"login":"example"}},"pull_request":{"head":{"ref":"feature","sha":"head","repo":null}}}`),
	} {
		event, err := ParsePullRequestEvent(headers, body)
		if err != nil {
			t.Fatalf("parse event: %v", err)
		}
		if event.SupportedAction && event.SameRepository {
			t.Fatalf("expected no-op event, got %+v", event)
		}
	}
}
