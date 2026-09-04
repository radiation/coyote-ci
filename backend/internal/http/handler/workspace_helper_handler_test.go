package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

func TestWorkspaceHelperHandlerExchangeCapability(t *testing.T) {
	exchanger := &workspaceHelperExchangerStub{token: "capability-token", capability: domain.WorkspaceHelperCapability{ExecutionJobID: "job-1", PodUID: "pod-1", Role: domain.WorkspaceHelperRolePrepare, ExpiresAt: time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)}}
	handler := NewWorkspaceHelperHandler(exchanger)
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader(`{"execution_job_id":"job-1","pod_uid":"pod-1","role":"prepare"}`))
	request.Header.Set("Authorization", "Bearer projected-token")
	response := httptest.NewRecorder()

	handler.ExchangeCapability(response, request)
	if response.Code != http.StatusCreated || exchanger.projectedToken != "projected-token" {
		t.Fatalf("status=%d token=%q", response.Code, exchanger.projectedToken)
	}
	var body struct {
		Data struct {
			Capability string `json:"capability"`
		} `json:"data"`
	}
	if decodeErr := json.NewDecoder(response.Body).Decode(&body); decodeErr != nil || body.Data.Capability != "capability-token" {
		t.Fatalf("body=%q err=%v", response.Body.String(), decodeErr)
	}
}

func TestWorkspaceHelperHandlerRejectsUnauthorizedWithoutTokenLeakage(t *testing.T) {
	rawToken := "projected-token-secret"
	handler := NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{err: service.ErrWorkspaceHelperUnauthorized})
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader(`{"execution_job_id":"job-1","pod_uid":"pod-1","role":"prepare"}`))
	request.Header.Set("Authorization", "Bearer "+rawToken)
	response := httptest.NewRecorder()

	handler.ExchangeCapability(response, request)
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), rawToken) {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestWorkspaceHelperHandlerRejectsMissingIdentity(t *testing.T) {
	handler := NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{})
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	handler.ExchangeCapability(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestWorkspaceHelperHandlerRejectsInvalidRequestBody(t *testing.T) {
	handler := NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{})
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader("{"))
	request.Header.Set("Authorization", "Bearer projected-token")
	response := httptest.NewRecorder()

	handler.ExchangeCapability(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestWorkspaceHelperHandlerHandlesUnavailableAndOperationalFailures(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/capabilities", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer projected-token")

	unavailableResponse := httptest.NewRecorder()
	NewWorkspaceHelperHandler(nil).ExchangeCapability(unavailableResponse, request)
	if unavailableResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d", unavailableResponse.Code)
	}

	internalResponse := httptest.NewRecorder()
	NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{err: errors.New("upstream failure")}).ExchangeCapability(internalResponse, request)
	if internalResponse.Code != http.StatusInternalServerError {
		t.Fatalf("internal status=%d", internalResponse.Code)
	}
}

func TestWorkspaceHelperHandlerPreparePreservesAuthoritativePublication(t *testing.T) {
	size := int64(len("corrupt bytes"))
	publication := domain.WorkspaceRevisionPublication{ContentDigest: "sha256:" + strings.Repeat("a", 64), StorageKey: "workspace-revisions/revision.tar.gz", SizeBytes: &size}
	prepare := &workspacePrepareOpenerStub{payload: service.WorkspacePreparePayload{Archive: io.NopCloser(bytes.NewBufferString("corrupt bytes")), Publication: publication}}
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetPrepareService(prepare)
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/prepare", strings.NewReader(`{"execution_job_id":"job-1","pod_uid":"pod-1"}`))
	request.Header.Set("Authorization", "Bearer capability")
	response := httptest.NewRecorder()

	handler.PrepareWorkspace(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Digest") != publication.ContentDigest || response.Body.String() != "corrupt bytes" {
		t.Fatalf("status=%d digest=%q body=%q", response.Code, response.Header().Get("Content-Digest"), response.Body.String())
	}
}

func TestWorkspaceHelperHandlerPrepareRejectsAuthoritativeSizeMismatch(t *testing.T) {
	size := int64(99)
	prepare := &workspacePrepareOpenerStub{payload: service.WorkspacePreparePayload{Archive: io.NopCloser(bytes.NewBufferString("short")), Publication: domain.WorkspaceRevisionPublication{ContentDigest: "sha256:" + strings.Repeat("a", 64), StorageKey: "workspace-revisions/revision.tar.gz", SizeBytes: &size}}}
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetPrepareService(prepare)
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/prepare", strings.NewReader(`{"execution_job_id":"job-1","pod_uid":"pod-1"}`))
	request.Header.Set("Authorization", "Bearer capability")
	response := httptest.NewRecorder()

	handler.PrepareWorkspace(response, request)
	if response.Code != http.StatusInternalServerError || response.Header().Get("Content-Digest") != "" {
		t.Fatalf("status=%d digest=%q", response.Code, response.Header().Get("Content-Digest"))
	}
}

func TestWorkspaceHelperHandlerPrepareRejectsInvalidRequests(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		handler *WorkspaceHelperHandler
		body    string
		token   string
		want    int
	}{
		{name: "unavailable", handler: NewWorkspaceHelperHandler(nil), body: `{}`, want: http.StatusServiceUnavailable},
		{name: "missing capability", handler: workspacePrepareHandlerForTest(service.WorkspacePreparePayload{}), body: `{}`, want: http.StatusUnauthorized},
		{name: "invalid body", handler: workspacePrepareHandlerForTest(service.WorkspacePreparePayload{}), body: `{`, token: "capability", want: http.StatusBadRequest},
		{name: "invalid payload", handler: workspacePrepareHandlerForTest(service.WorkspacePreparePayload{}), body: `{}`, token: "capability", want: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/prepare", strings.NewReader(testCase.body))
			if testCase.token != "" {
				request.Header.Set("Authorization", "Bearer "+testCase.token)
			}
			response := httptest.NewRecorder()
			testCase.handler.PrepareWorkspace(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status=%d, want %d", response.Code, testCase.want)
			}
		})
	}
}

func TestWorkspaceHelperHandlerPrepareMapsOpenErrors(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want int
	}{
		{name: "unauthorized", err: service.ErrWorkspaceHelperUnauthorized, want: http.StatusUnauthorized},
		{name: "internal", err: errors.New("open failed"), want: http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := NewWorkspaceHelperHandler(nil)
			handler.SetPrepareService(&workspacePrepareOpenerStub{err: testCase.err})
			request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/prepare", strings.NewReader(`{"execution_job_id":"job-1","pod_uid":"pod-1"}`))
			request.Header.Set("Authorization", "Bearer capability")
			response := httptest.NewRecorder()
			handler.PrepareWorkspace(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status=%d, want %d", response.Code, testCase.want)
			}
		})
	}
}

func TestWorkspaceHelperHandlerExposesPrepareCapabilityAuthorizer(t *testing.T) {
	var nilHandler *WorkspaceHelperHandler
	if nilHandler.PrepareCapabilityAuthorizer() != nil {
		t.Fatal("nil handler returned an authorizer")
	}
	handler := NewWorkspaceHelperHandler(&workspaceHelperExchangerStub{})
	if handler.PrepareCapabilityAuthorizer() == nil {
		t.Fatal("expected prepare capability authorizer")
	}
}

func TestWorkspaceHelperHandlerPublishRejectsOversizedArchive(t *testing.T) {
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetPublishService(&workspacePublisherStub{err: service.ErrWorkspacePublishArchiveTooLarge})
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/publish", strings.NewReader("archive"))
	request.Header.Set("Authorization", "Bearer capability")
	response := httptest.NewRecorder()

	handler.PublishWorkspace(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestWorkspaceHelperHandlerPublishMapsOutcomes(t *testing.T) {
	size := int64(7)
	digest := "sha256:digest"
	for _, testCase := range []struct {
		name      string
		publisher workspacePublisher
		token     string
		want      int
	}{
		{name: "unavailable", want: http.StatusServiceUnavailable},
		{name: "missing capability", publisher: &workspacePublisherStub{}, want: http.StatusUnauthorized},
		{name: "unauthorized", publisher: &workspacePublisherStub{err: service.ErrWorkspaceHelperUnauthorized}, token: "capability", want: http.StatusUnauthorized},
		{name: "invalid archive", publisher: &workspacePublisherStub{err: service.ErrWorkspacePublishInvalidArchive}, token: "capability", want: http.StatusBadRequest},
		{name: "conflict", publisher: &workspacePublisherStub{err: repository.ErrWorkspaceRevisionConflict}, token: "capability", want: http.StatusConflict},
		{name: "workspace object conflict", publisher: &workspacePublisherStub{err: workspace.ErrWorkspaceRevisionConflict}, token: "capability", want: http.StatusConflict},
		{name: "internal failure", publisher: &workspacePublisherStub{err: errors.New("publish failed")}, token: "capability", want: http.StatusInternalServerError},
		{name: "invalid publication", publisher: &workspacePublisherStub{}, token: "capability", want: http.StatusInternalServerError},
		{name: "published", publisher: &workspacePublisherStub{published: domain.WorkspaceRevision{ID: "revision-1", ContentDigest: &digest, SizeBytes: &size}}, token: "capability", want: http.StatusOK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := NewWorkspaceHelperHandler(nil)
			handler.SetPublishService(testCase.publisher)
			request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/publish", strings.NewReader("archive"))
			if testCase.token != "" {
				request.Header.Set("Authorization", "Bearer "+testCase.token)
			}
			response := httptest.NewRecorder()
			handler.PublishWorkspace(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status=%d, want %d", response.Code, testCase.want)
			}
		})
	}
}

type workspaceHelperExchangerStub struct {
	token          string
	capability     domain.WorkspaceHelperCapability
	err            error
	projectedToken string
}

func (s *workspaceHelperExchangerStub) Exchange(_ context.Context, projectedToken string, _ domain.WorkspaceHelperCapability) (string, domain.WorkspaceHelperCapability, error) {
	s.projectedToken = projectedToken
	return s.token, s.capability, s.err
}

func (*workspaceHelperExchangerStub) Authorize(context.Context, string, string, string, domain.WorkspaceHelperRole) (domain.WorkspaceHelperCapability, error) {
	return domain.WorkspaceHelperCapability{}, nil
}

var _ workspaceHelperCapabilityExchanger = (*workspaceHelperExchangerStub)(nil)

type workspacePrepareOpenerStub struct {
	payload service.WorkspacePreparePayload
	err     error
}

func (s *workspacePrepareOpenerStub) Open(context.Context, string, string, string) (service.WorkspacePreparePayload, error) {
	return s.payload, s.err
}

var _ workspacePrepareOpener = (*workspacePrepareOpenerStub)(nil)

type workspacePublisherStub struct {
	err       error
	published domain.WorkspaceRevision
}

func (s *workspacePublisherStub) Publish(context.Context, string, string, string, io.Reader) (domain.WorkspaceRevision, error) {
	return s.published, s.err
}

var _ workspacePublisher = (*workspacePublisherStub)(nil)

func workspacePrepareHandlerForTest(payload service.WorkspacePreparePayload) *WorkspaceHelperHandler {
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetPrepareService(&workspacePrepareOpenerStub{payload: payload})
	return handler
}
