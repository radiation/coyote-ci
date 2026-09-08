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

func TestWorkspaceHelperHandlerRestoreCacheOutcomes(t *testing.T) {
	size := int64(len("cache archive"))
	publication := domain.WorkspaceRevisionPublication{StorageKey: "cache/transport.tar.gz", ContentDigest: "sha256:" + strings.Repeat("a", 64), SizeBytes: &size}
	for _, testCase := range []struct {
		name  string
		cache workspaceCacheHelper
		token string
		want  int
	}{
		{name: "unavailable", want: http.StatusServiceUnavailable},
		{name: "missing capability", cache: &workspaceCacheHelperStub{}, want: http.StatusUnauthorized},
		{name: "invalid body", cache: &workspaceCacheHelperStub{}, token: "capability", want: http.StatusBadRequest},
		{name: "unauthorized", cache: &workspaceCacheHelperStub{restoreErr: service.ErrWorkspaceHelperUnauthorized}, token: "capability", want: http.StatusUnauthorized},
		{name: "invalid request", cache: &workspaceCacheHelperStub{restoreErr: service.ErrWorkspaceHelperCacheInvalidInput}, token: "capability", want: http.StatusBadRequest},
		{name: "internal", cache: &workspaceCacheHelperStub{restoreErr: errors.New("restore failed")}, token: "capability", want: http.StatusInternalServerError},
		{name: "miss", cache: &workspaceCacheHelperStub{}, token: "capability", want: http.StatusNoContent},
		{name: "hit", cache: &workspaceCacheHelperStub{payload: service.WorkspaceHelperCachePayload{Archive: io.NopCloser(strings.NewReader("cache archive")), Publication: publication}, found: true}, token: "capability", want: http.StatusOK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := NewWorkspaceHelperHandler(nil)
			handler.SetCacheService(testCase.cache)
			body := `{"execution_job_id":"job-1","pod_uid":"pod-1","preset":"go","cache_key":"go:key"}`
			if testCase.name == "invalid body" {
				body = "{"
			}
			request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/cache/restore", strings.NewReader(body))
			if testCase.token != "" {
				request.Header.Set("Authorization", "Bearer "+testCase.token)
			}
			response := httptest.NewRecorder()
			handler.RestoreCache(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status=%d, want %d", response.Code, testCase.want)
			}
			if testCase.name == "hit" && (response.Header().Get("Content-Digest") != publication.ContentDigest || response.Body.String() != "cache archive") {
				t.Fatalf("headers=%v body=%q", response.Header(), response.Body.String())
			}
		})
	}
}

func TestWorkspaceHelperHandlerSaveCacheOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		cache workspaceCacheHelper
		token string
		want  int
	}{
		{name: "unavailable", want: http.StatusServiceUnavailable},
		{name: "missing capability", cache: &workspaceCacheHelperStub{}, want: http.StatusUnauthorized},
		{name: "unauthorized", cache: &workspaceCacheHelperStub{saveErr: service.ErrWorkspaceHelperUnauthorized}, token: "capability", want: http.StatusUnauthorized},
		{name: "invalid request", cache: &workspaceCacheHelperStub{saveErr: service.ErrWorkspaceHelperCacheInvalidInput}, token: "capability", want: http.StatusBadRequest},
		{name: "internal", cache: &workspaceCacheHelperStub{saveErr: errors.New("save failed")}, token: "capability", want: http.StatusInternalServerError},
		{name: "saved", cache: &workspaceCacheHelperStub{}, token: "capability", want: http.StatusNoContent},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := NewWorkspaceHelperHandler(nil)
			handler.SetCacheService(testCase.cache)
			request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/cache/save", strings.NewReader("cache archive"))
			request.Header.Set("Content-Digest", "sha256:"+strings.Repeat("a", 64))
			request.Header.Set("Coyote-Execution-Job-ID", "job-1")
			request.Header.Set("Coyote-Pod-UID", "pod-1")
			request.Header.Set("Coyote-Cache-Preset", "go")
			request.Header.Set("Coyote-Cache-Key", "go:key")
			if testCase.token != "" {
				request.Header.Set("Authorization", "Bearer "+testCase.token)
			}
			response := httptest.NewRecorder()
			handler.SaveCache(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status=%d, want %d", response.Code, testCase.want)
			}
		})
	}
}

func TestWorkspaceHelperHandlerSaveCacheRejectsOversizedUpload(t *testing.T) {
	cache := &workspaceCacheHelperStub{}
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetCacheService(cache)
	handler.SetCacheMaxUploadBytes(2)
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/cache/save", strings.NewReader("archive"))
	request.Header.Set("Authorization", "Bearer capability")
	response := httptest.NewRecorder()

	handler.SaveCache(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWorkspaceHelperHandlerSaveCacheLimitsUnknownLengthUpload(t *testing.T) {
	cache := &workspaceCacheHelperStub{readSaveBody: true}
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetCacheService(cache)
	handler.SetCacheMaxUploadBytes(2)
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/cache/save", io.NopCloser(strings.NewReader("archive")))
	request.Header.Set("Authorization", "Bearer capability")
	response := httptest.NewRecorder()

	handler.SaveCache(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWorkspaceHelperHandlerUploadArtifactRejectsOversizedUpload(t *testing.T) {
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetArtifactService(&workspaceArtifactHelperStub{})
	handler.SetArtifactMaxUploadBytes(2)
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/artifacts/upload", strings.NewReader("artifact"))
	request.Header.Set("Authorization", "Bearer capability")
	response := httptest.NewRecorder()

	handler.UploadArtifact(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWorkspaceHelperHandlerUploadArtifactLimitsUnknownLengthUpload(t *testing.T) {
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetArtifactService(&workspaceArtifactHelperStub{readUploadBody: true})
	handler.SetArtifactMaxUploadBytes(2)
	request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/artifacts/upload", io.NopCloser(strings.NewReader("artifact")))
	request.Header.Set("Authorization", "Bearer capability")
	response := httptest.NewRecorder()

	handler.UploadArtifact(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWorkspaceHelperHandlerPlanArtifactsOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		artifacts workspaceArtifactHelper
		body      string
		token     string
		want      int
	}{
		{name: "unavailable", want: http.StatusServiceUnavailable},
		{name: "missing capability", artifacts: &workspaceArtifactHelperStub{}, want: http.StatusUnauthorized},
		{name: "invalid request", artifacts: &workspaceArtifactHelperStub{}, body: "{", token: "capability", want: http.StatusBadRequest},
		{name: "unauthorized", artifacts: &workspaceArtifactHelperStub{planErr: service.ErrWorkspaceHelperUnauthorized}, token: "capability", want: http.StatusUnauthorized},
		{name: "invalid artifact request", artifacts: &workspaceArtifactHelperStub{planErr: service.ErrWorkspaceHelperArtifactInvalidInput}, token: "capability", want: http.StatusBadRequest},
		{name: "internal", artifacts: &workspaceArtifactHelperStub{planErr: errors.New("plan failed")}, token: "capability", want: http.StatusInternalServerError},
		{name: "planned", artifacts: &workspaceArtifactHelperStub{plan: service.WorkspaceHelperArtifactPlan{Collect: true}}, token: "capability", want: http.StatusOK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := NewWorkspaceHelperHandler(nil)
			handler.SetArtifactService(testCase.artifacts)
			body := testCase.body
			if body == "" {
				body = `{"execution_job_id":"job-1","pod_uid":"pod-1","build_succeeded":true}`
			}
			request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/artifacts/plan", strings.NewReader(body))
			if testCase.token != "" {
				request.Header.Set("Authorization", "Bearer "+testCase.token)
			}
			response := httptest.NewRecorder()

			handler.PlanArtifacts(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status=%d, want %d", response.Code, testCase.want)
			}
		})
	}
}

func TestWorkspaceHelperHandlerUploadArtifactOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		artifacts workspaceArtifactHelper
		token     string
		want      int
	}{
		{name: "unavailable", want: http.StatusServiceUnavailable},
		{name: "missing capability", artifacts: &workspaceArtifactHelperStub{}, want: http.StatusUnauthorized},
		{name: "unauthorized", artifacts: &workspaceArtifactHelperStub{uploadErr: service.ErrWorkspaceHelperUnauthorized}, token: "capability", want: http.StatusUnauthorized},
		{name: "invalid artifact request", artifacts: &workspaceArtifactHelperStub{uploadErr: service.ErrWorkspaceHelperArtifactInvalidInput}, token: "capability", want: http.StatusBadRequest},
		{name: "internal", artifacts: &workspaceArtifactHelperStub{uploadErr: errors.New("upload failed")}, token: "capability", want: http.StatusInternalServerError},
		{name: "uploaded", artifacts: &workspaceArtifactHelperStub{}, token: "capability", want: http.StatusNoContent},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := NewWorkspaceHelperHandler(nil)
			handler.SetArtifactService(testCase.artifacts)
			request := httptest.NewRequest(http.MethodPost, "/api/internal/workspace-helper/artifacts/upload", strings.NewReader("artifact"))
			if testCase.token != "" {
				request.Header.Set("Authorization", "Bearer "+testCase.token)
			}
			response := httptest.NewRecorder()

			handler.UploadArtifact(response, request)
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

type workspaceCacheHelperStub struct {
	payload      service.WorkspaceHelperCachePayload
	found        bool
	restoreErr   error
	saveErr      error
	readSaveBody bool
}

func (s *workspaceCacheHelperStub) Restore(context.Context, string, string, string, string, string) (service.WorkspaceHelperCachePayload, bool, error) {
	return s.payload, s.found, s.restoreErr
}

func (s *workspaceCacheHelperStub) Save(_ context.Context, _ string, _ string, _ string, _ string, _ string, archive io.Reader, _ domain.WorkspaceRevisionPublication) error {
	if s.readSaveBody {
		if _, readErr := io.ReadAll(archive); readErr != nil {
			return readErr
		}
	}
	return s.saveErr
}

var _ workspaceCacheHelper = (*workspaceCacheHelperStub)(nil)

type workspaceArtifactHelperStub struct {
	readUploadBody bool
	plan           service.WorkspaceHelperArtifactPlan
	planErr        error
	uploadErr      error
}

func (s *workspaceArtifactHelperStub) Plan(context.Context, string, string, string, bool) (service.WorkspaceHelperArtifactPlan, error) {
	return s.plan, s.planErr
}

func (s *workspaceArtifactHelperStub) Upload(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ bool, source io.Reader) error {
	if s.readUploadBody {
		_, readErr := io.ReadAll(source)
		return readErr
	}
	return s.uploadErr
}

var _ workspaceArtifactHelper = (*workspaceArtifactHelperStub)(nil)

func workspacePrepareHandlerForTest(payload service.WorkspacePreparePayload) *WorkspaceHelperHandler {
	handler := NewWorkspaceHelperHandler(nil)
	handler.SetPrepareService(&workspacePrepareOpenerStub{payload: payload})
	return handler
}
