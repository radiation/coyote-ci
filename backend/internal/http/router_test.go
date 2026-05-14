package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/http/handler"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
	buildsvc "github.com/radiation/coyote-ci/backend/internal/service/build"
	webhooksvc "github.com/radiation/coyote-ci/backend/internal/service/webhook"
)

func TestNewRouter_HealthAndNotFound(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, nil, jh, nil, nil, nil, eh, "")

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
		body       string
	}{
		{name: "health", method: http.MethodGet, path: "/health", statusCode: http.StatusOK, body: "ok"},
		{name: "healthz", method: http.MethodGet, path: "/healthz", statusCode: http.StatusOK, body: "ok"},
		{name: "not found", method: http.MethodGet, path: "/missing", statusCode: http.StatusNotFound},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, rr.Code)
			}
			if tc.body != "" && rr.Body.String() != tc.body {
				t.Fatalf("expected body %q, got %q", tc.body, rr.Body.String())
			}
		})
	}
}

func TestNewRouter_HeaderModeUsersRouteRequiresIdentityHeader(t *testing.T) {
	r := newIdentityTestRouter(auth.ModeHeader, "push-secret", "github-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusUnauthorized, res.Code, res.Body.String())
	}
}

func TestNewRouter_HeaderModeMeRouteResolvesUser(t *testing.T) {
	r := newIdentityTestRouter(auth.ModeHeader, "push-secret", "github-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Coyote-User-Email", "ADMIN@Example.COM")
	req.Header.Set("X-Coyote-User-Name", "Admin User")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}

	var body struct {
		Data struct {
			AuthMode string `json:"auth_mode"`
			User     struct {
				Email       string  `json:"email"`
				DisplayName *string `json:"display_name"`
				GlobalRole  string  `json:"global_role"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Data.AuthMode != string(auth.ModeHeader) {
		t.Fatalf("expected auth mode %q, got %q", auth.ModeHeader, body.Data.AuthMode)
	}
	if body.Data.User.Email != "admin@example.com" {
		t.Fatalf("expected normalized email, got %q", body.Data.User.Email)
	}
	if body.Data.User.DisplayName == nil || *body.Data.User.DisplayName != "Admin User" {
		t.Fatalf("expected display name to be preserved, got %v", body.Data.User.DisplayName)
	}
	if body.Data.User.GlobalRole != string(domain.GlobalRoleUser) {
		t.Fatalf("expected default global role %q, got %q", domain.GlobalRoleUser, body.Data.User.GlobalRole)
	}
}

func TestNewRouter_AuthConfigRouteIsPublic(t *testing.T) {
	tests := []struct {
		name          string
		mode          auth.Mode
		expectedLogin *string
	}{
		{name: "disabled mode", mode: auth.ModeDisabled},
		{name: "header mode", mode: auth.ModeHeader},
		{name: "oidc mode", mode: auth.ModeOIDC, expectedLogin: stringPointer("/auth/login")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r := newIdentityTestRouter(tc.mode, "push-secret", "github-secret")
			res := httptest.NewRecorder()
			r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))

			if res.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
			}

			var body struct {
				Data struct {
					AuthMode string  `json:"auth_mode"`
					LoginURL *string `json:"login_url"`
				} `json:"data"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if body.Data.AuthMode != string(tc.mode) {
				t.Fatalf("expected auth mode %q, got %q", tc.mode, body.Data.AuthMode)
			}
			if !equalStringPointers(body.Data.LoginURL, tc.expectedLogin) {
				t.Fatalf("expected login url %v, got %v", tc.expectedLogin, body.Data.LoginURL)
			}
		})
	}
}

func TestNewRouter_AuthRoutesAreRegisteredWhenHandlerIsInjected(t *testing.T) {
	sessions, err := auth.NewCookieSessionManager(auth.CookieSessionConfig{Secret: "test-session-secret"})
	if err != nil {
		t.Fatalf("create session manager failed: %v", err)
	}
	userService := service.NewUserService(repositorymemory.NewUserRepository())
	authHandler := handler.NewAuthHandler(nil, sessions, userService, handler.AuthHandlerConfig{})
	r := newIdentityTestRouter(auth.ModeDisabled, "push-secret", "github-secret", WithAuthHandler(authHandler))
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	response := httptest.NewRecorder()

	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected auth logout route status %d, got %d body=%s", http.StatusOK, response.Code, response.Body.String())
	}
}

func TestNewRouter_OIDCModeMeRouteRequiresSession(t *testing.T) {
	r, _ := newOIDCTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusUnauthorized, res.Code, res.Body.String())
	}
}

func TestNewRouter_OIDCModeMeRouteUsesSession(t *testing.T) {
	r, sessions := newOIDCTestRouter(t)
	loginRes := httptest.NewRecorder()
	if err := sessions.CreateSession(loginRes, "user-1"); err != nil {
		t.Fatalf("create session failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for _, cookie := range loginRes.Result().Cookies() {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	var body struct {
		Data struct {
			AuthMode string `json:"auth_mode"`
			User     struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Data.AuthMode != string(auth.ModeOIDC) || body.Data.User.Email != "user@example.com" {
		t.Fatalf("unexpected me response: %+v", body.Data)
	}
}

func TestNewRouter_OIDCModeHealthAndIngressBypassSession(t *testing.T) {
	r, _ := newOIDCTestRouter(t)

	healthReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	healthRes := httptest.NewRecorder()
	r.ServeHTTP(healthRes, healthReq)
	if healthRes.Code != http.StatusOK {
		t.Fatalf("expected api health status %d, got %d", http.StatusOK, healthRes.Code)
	}

	pushReq := httptest.NewRequest(http.MethodPost, "/api/events/push", bytes.NewBufferString(`{"repository_url":"https://github.com/example/backend.git","ref":"refs/heads/main","commit_sha":"abc123"}`))
	pushRes := httptest.NewRecorder()
	r.ServeHTTP(pushRes, pushReq)
	if pushRes.Code != http.StatusOK {
		t.Fatalf("expected push ingress status %d, got %d body=%s", http.StatusOK, pushRes.Code, pushRes.Body.String())
	}
}

func TestNewRouter_DisabledModePreservesNormalAPIRoutes(t *testing.T) {
	r := newIdentityTestRouter(auth.ModeDisabled, "push-secret", "github-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
}

func TestNewRouter_HeaderModeHealthAndIngressBypassIdentityHeaders(t *testing.T) {
	r := newIdentityTestRouter(auth.ModeHeader, "push-secret", "github-secret")

	healthReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	healthRes := httptest.NewRecorder()
	r.ServeHTTP(healthRes, healthReq)
	if healthRes.Code != http.StatusOK {
		t.Fatalf("expected api health status %d, got %d", http.StatusOK, healthRes.Code)
	}

	pushReq := httptest.NewRequest(http.MethodPost, "/api/events/push", bytes.NewBufferString(`{"repository_url":"https://github.com/example/backend.git","ref":"refs/heads/main","commit_sha":"abc123"}`))
	pushReq.Header.Set("X-Coyote-Secret", "push-secret")
	pushRes := httptest.NewRecorder()
	r.ServeHTTP(pushRes, pushReq)
	if pushRes.Code != http.StatusOK {
		t.Fatalf("expected push ingress status %d, got %d body=%s", http.StatusOK, pushRes.Code, pushRes.Body.String())
	}

	body := []byte(`{}`)
	webhookReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(body))
	webhookReq.Header.Set("X-GitHub-Event", "pull_request")
	webhookReq.Header.Set("X-GitHub-Delivery", "delivery-router-test")
	webhookReq.Header.Set("X-Hub-Signature-256", githubRouterTestSignature("github-secret", body))
	webhookRes := httptest.NewRecorder()
	r.ServeHTTP(webhookRes, webhookReq)
	if webhookRes.Code != http.StatusAccepted {
		t.Fatalf("expected webhook ingress status %d, got %d body=%s", http.StatusAccepted, webhookRes.Code, webhookRes.Body.String())
	}
}

func TestNewRouter_HeaderModeProjectMembershipRouteRequiresIdentityHeader(t *testing.T) {
	r := newProjectMembershipTestRouter(t, auth.ModeHeader)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/members", nil)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusUnauthorized, res.Code, res.Body.String())
	}
}

func TestNewRouter_HeaderModeUsersRouteForbiddenForNonAdmin(t *testing.T) {
	r := newIdentityTestRouter(auth.ModeHeader, "push-secret", "github-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("X-Coyote-User-Email", "user@example.com")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusForbidden, res.Code, res.Body.String())
	}
}

func TestNewRouter_HeaderModeProjectMembershipMutationForbiddenForViewer(t *testing.T) {
	r := newProjectMembershipTestRouter(t, auth.ModeHeader)

	req := httptest.NewRequest(http.MethodPut, "/api/projects/project-1/members/target-1", bytes.NewBufferString(`{"role":"viewer"}`))
	req.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusForbidden, res.Code, res.Body.String())
	}
}

func TestNewRouter_HeaderModeProjectMembershipMutationForbiddenForMaintainer(t *testing.T) {
	r := newProjectMembershipTestRouter(t, auth.ModeHeader)

	req := httptest.NewRequest(http.MethodPut, "/api/projects/project-1/members/target-1", bytes.NewBufferString(`{"role":"viewer"}`))
	req.Header.Set("X-Coyote-User-Email", "maintainer@example.com")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusForbidden, res.Code, res.Body.String())
	}
}

func TestNewRouter_DisabledModeProjectMembershipRouteUsesSyntheticIdentity(t *testing.T) {
	r := newProjectMembershipTestRouter(t, auth.ModeDisabled)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/members", nil)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
}

func TestNewRouter_RBACJobMutationRequiresMaintainer(t *testing.T) {
	fixture := newRBACTestRouter(t)
	body := `{"project_id":"` + fixture.projectID + `","name":"backend-ci","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","enabled":true}`

	viewerReq := httptest.NewRequest(http.MethodPost, "/api/jobs/", bytes.NewBufferString(body))
	viewerReq.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	viewerRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(viewerRes, viewerReq)
	if viewerRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer create status %d, got %d body=%s", http.StatusForbidden, viewerRes.Code, viewerRes.Body.String())
	}

	maintainerReq := httptest.NewRequest(http.MethodPost, "/api/jobs/", bytes.NewBufferString(body))
	maintainerReq.Header.Set("X-Coyote-User-Email", "maintainer@example.com")
	maintainerRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(maintainerRes, maintainerReq)
	if maintainerRes.Code != http.StatusCreated {
		t.Fatalf("expected maintainer create status %d, got %d body=%s", http.StatusCreated, maintainerRes.Code, maintainerRes.Body.String())
	}
}

func TestNewRouter_RBACBuildMutationRequiresMaintainer(t *testing.T) {
	fixture := newRBACTestRouter(t)

	viewerReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+fixture.buildID+"/queue", bytes.NewBufferString(`{}`))
	viewerReq.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	viewerRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(viewerRes, viewerReq)
	if viewerRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer queue status %d, got %d body=%s", http.StatusForbidden, viewerRes.Code, viewerRes.Body.String())
	}

	maintainerReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+fixture.buildID+"/queue", bytes.NewBufferString(`{}`))
	maintainerReq.Header.Set("X-Coyote-User-Email", "maintainer@example.com")
	maintainerRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(maintainerRes, maintainerReq)
	if maintainerRes.Code != http.StatusOK {
		t.Fatalf("expected maintainer queue status %d, got %d body=%s", http.StatusOK, maintainerRes.Code, maintainerRes.Body.String())
	}
}

func TestNewRouter_RBACProjectAndArtifactReadsRequireMembership(t *testing.T) {
	fixture := newRBACTestRouter(t)

	outsiderProjectReq := httptest.NewRequest(http.MethodGet, "/api/projects/"+fixture.projectID, nil)
	outsiderProjectReq.Header.Set("X-Coyote-User-Email", "outsider@example.com")
	outsiderProjectRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(outsiderProjectRes, outsiderProjectReq)
	if outsiderProjectRes.Code != http.StatusForbidden {
		t.Fatalf("expected outsider project status %d, got %d body=%s", http.StatusForbidden, outsiderProjectRes.Code, outsiderProjectRes.Body.String())
	}

	viewerProjectReq := httptest.NewRequest(http.MethodGet, "/api/projects/"+fixture.projectID, nil)
	viewerProjectReq.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	viewerProjectRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(viewerProjectRes, viewerProjectReq)
	if viewerProjectRes.Code != http.StatusOK {
		t.Fatalf("expected viewer project status %d, got %d body=%s", http.StatusOK, viewerProjectRes.Code, viewerProjectRes.Body.String())
	}

	outsiderArtifactReq := httptest.NewRequest(http.MethodGet, "/api/builds/"+fixture.buildID+"/artifacts/missing/download", nil)
	outsiderArtifactReq.Header.Set("X-Coyote-User-Email", "outsider@example.com")
	outsiderArtifactRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(outsiderArtifactRes, outsiderArtifactReq)
	if outsiderArtifactRes.Code != http.StatusForbidden {
		t.Fatalf("expected outsider artifact status %d, got %d body=%s", http.StatusForbidden, outsiderArtifactRes.Code, outsiderArtifactRes.Body.String())
	}

	viewerArtifactReq := httptest.NewRequest(http.MethodGet, "/api/builds/"+fixture.buildID+"/artifacts/missing/download", nil)
	viewerArtifactReq.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	viewerArtifactRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(viewerArtifactRes, viewerArtifactReq)
	if viewerArtifactRes.Code != http.StatusNotFound {
		t.Fatalf("expected viewer artifact status %d, got %d body=%s", http.StatusNotFound, viewerArtifactRes.Code, viewerArtifactRes.Body.String())
	}
}

func TestNewRouter_BuildRoutes(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, nil, jh, nil, nil, nil, eh, "")

	createReq := httptest.NewRequest(http.MethodPost, "/api/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createBody map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create data envelope, got %v", createBody)
	}
	id, ok := createData["id"].(string)
	if !ok || id == "" {
		t.Fatalf("expected create response id, got %v", createData["id"])
	}

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
	}{
		{name: "list builds", method: http.MethodGet, path: "/api/builds/", statusCode: http.StatusOK},
		{name: "get build", method: http.MethodGet, path: "/api/builds/" + id, statusCode: http.StatusOK},
		{name: "build steps", method: http.MethodGet, path: "/api/builds/" + id + "/steps", statusCode: http.StatusOK},
		{name: "build step logs", method: http.MethodGet, path: "/api/builds/" + id + "/steps/0/logs", statusCode: http.StatusOK},
		{name: "build logs", method: http.MethodGet, path: "/api/builds/" + id + "/logs", statusCode: http.StatusOK},
		{name: "build artifacts", method: http.MethodGet, path: "/api/builds/" + id + "/artifacts", statusCode: http.StatusOK},
		{name: "build artifact download missing", method: http.MethodGet, path: "/api/builds/" + id + "/artifacts/missing/download", statusCode: http.StatusNotFound},
		{name: "queue build", method: http.MethodPost, path: "/api/builds/" + id + "/queue", statusCode: http.StatusOK},
		{name: "start build", method: http.MethodPost, path: "/api/builds/" + id + "/start", statusCode: http.StatusOK},
		{name: "complete build", method: http.MethodPost, path: "/api/builds/" + id + "/complete", statusCode: http.StatusOK},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != tc.statusCode {
				t.Fatalf("expected status %d, got %d", tc.statusCode, rr.Code)
			}
		})
	}

	failReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+id+"/fail", nil)
	failRes := httptest.NewRecorder()
	r.ServeHTTP(failRes, failReq)
	if failRes.Code != http.StatusConflict {
		t.Fatalf("expected fail status %d after completion, got %d", http.StatusConflict, failRes.Code)
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+id+"/cancel", nil)
	cancelRes := httptest.NewRecorder()
	r.ServeHTTP(cancelRes, cancelReq)
	if cancelRes.Code != http.StatusOK {
		t.Fatalf("expected cancel status %d, got %d", http.StatusOK, cancelRes.Code)
	}
}

func TestNewRouter_QueueBuild_WithTemplate_PersistsTemplateSteps(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, nil, jh, nil, nil, nil, eh, "")

	createReq := httptest.NewRequest(http.MethodPost, "/api/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createBody map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create data envelope, got %v", createBody)
	}
	buildID, ok := createData["id"].(string)
	if !ok || buildID == "" {
		t.Fatalf("expected create response id, got %v", createData["id"])
	}

	queueReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+buildID+"/queue", bytes.NewBufferString(`{"template":"test"}`))
	queueRes := httptest.NewRecorder()
	r.ServeHTTP(queueRes, queueReq)
	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := httptest.NewRequest(http.MethodGet, "/api/builds/"+buildID+"/steps", nil)
	stepsRes := httptest.NewRecorder()
	r.ServeHTTP(stepsRes, stepsReq)
	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected steps status %d, got %d", http.StatusOK, stepsRes.Code)
	}

	var stepsBody map[string]any
	if err := json.Unmarshal(stepsRes.Body.Bytes(), &stepsBody); err != nil {
		t.Fatalf("failed to parse steps response: %v", err)
	}
	stepsData, ok := stepsBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected steps data envelope, got %v", stepsBody)
	}
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}

	expectedNames := []string{"setup", "test", "teardown"}
	if len(steps) != len(expectedNames) {
		t.Fatalf("expected %d steps, got %d", len(expectedNames), len(steps))
	}

	for idx, expectedName := range expectedNames {
		step, ok := steps[idx].(map[string]any)
		if !ok {
			t.Fatalf("expected step object at index %d, got %T", idx, steps[idx])
		}
		if step["step_index"] != float64(idx) {
			t.Fatalf("expected step_index %d, got %v", idx, step["step_index"])
		}
		if step["name"] != expectedName {
			t.Fatalf("expected step name %q, got %v", expectedName, step["name"])
		}
	}
}

func newIdentityTestRouter(mode auth.Mode, pushEventSecret string, githubWebhookSecret string, opts ...RouterOption) http.Handler {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	buildHandler := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jobHandler := handler.NewJobHandler(jobSvc)
	webhookSvc := webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc)
	eventHandler := handler.NewEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), githubWebhookSecret)
	userService := service.NewUserService(repositorymemory.NewUserRepository())
	userHandler := handler.NewUserHandler(userService, mode)
	authMiddleware := auth.Middleware(auth.MiddlewareConfig{Mode: mode}, userService)
	routerOptions := []RouterOption{
		WithAuthMiddleware(authMiddleware),
		WithUserHandler(userHandler),
	}
	routerOptions = append(routerOptions, opts...)

	return NewRouter(
		buildHandler,
		nil,
		jobHandler,
		nil,
		nil,
		nil,
		eventHandler,
		pushEventSecret,
		routerOptions...,
	)
}

func newOIDCTestRouter(t *testing.T) (http.Handler, *auth.CookieSessionManager) {
	t.Helper()
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	buildHandler := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jobHandler := handler.NewJobHandler(jobSvc)
	webhookSvc := webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc)
	eventHandler := handler.NewEventHandler(jobSvc, webhookSvc, observability.NewNoopWebhookIngressMetrics(), "")
	userRepo := repositorymemory.NewUserRepository()
	if _, err := userRepo.Create(context.Background(), domain.User{ID: "user-1", Email: "user@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	userService := service.NewUserService(userRepo)
	sessions, err := auth.NewCookieSessionManager(auth.CookieSessionConfig{Secret: "test-session-secret"})
	if err != nil {
		t.Fatalf("create session manager failed: %v", err)
	}
	authMiddleware := auth.Middleware(auth.MiddlewareConfig{Mode: auth.ModeOIDC, Sessions: sessions}, userService)
	userHandler := handler.NewUserHandler(userService, auth.ModeOIDC)

	return NewRouter(
		buildHandler,
		nil,
		jobHandler,
		nil,
		nil,
		nil,
		eventHandler,
		"",
		WithAuthMiddleware(authMiddleware),
		WithUserHandler(userHandler),
	), sessions
}

func newProjectMembershipTestRouter(t *testing.T, mode auth.Mode) http.Handler {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	buildHandler := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)
	jobHandler := handler.NewJobHandler(jobSvc)
	projectHandler := handler.NewProjectHandler(service.NewProjectService(projectRepo), jobSvc)
	eventHandler := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "github-secret")
	userRepo := repositorymemory.NewUserRepository()
	userService := service.NewUserService(userRepo)
	membershipRepo := repositorymemory.NewProjectMembershipRepository(projectRepo, userRepo)
	membershipService := service.NewProjectMembershipService(projectRepo, membershipRepo)
	membershipHandler := handler.NewProjectMembershipHandler(membershipService, mode)
	authMiddleware := auth.Middleware(auth.MiddlewareConfig{Mode: mode}, userService)

	if _, err := projectRepo.Create(ctx, domain.Project{ID: "project-1", Name: "Platform", Slug: "platform", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	viewer, err := userRepo.Create(ctx, domain.User{ID: "viewer-1", Email: "viewer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create viewer failed: %v", err)
	}
	owner, err := userRepo.Create(ctx, domain.User{ID: "owner-1", Email: "owner@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create owner failed: %v", err)
	}
	maintainer, err := userRepo.Create(ctx, domain.User{ID: "maintainer-1", Email: "maintainer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create maintainer failed: %v", err)
	}
	if _, err := userRepo.Create(ctx, domain.User{ID: "target-1", Email: "target@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create target failed: %v", err)
	}
	if _, err := membershipService.UpsertProjectMembership(ctx, service.UpsertProjectMembershipInput{ProjectID: "project-1", UserID: viewer.ID, Role: "viewer"}); err != nil {
		t.Fatalf("create viewer membership failed: %v", err)
	}
	if _, err := membershipService.UpsertProjectMembership(ctx, service.UpsertProjectMembershipInput{ProjectID: "project-1", UserID: owner.ID, Role: "owner"}); err != nil {
		t.Fatalf("create owner membership failed: %v", err)
	}
	if _, err := membershipService.UpsertProjectMembership(ctx, service.UpsertProjectMembershipInput{ProjectID: "project-1", UserID: maintainer.ID, Role: "maintainer"}); err != nil {
		t.Fatalf("create maintainer membership failed: %v", err)
	}

	return NewRouter(
		buildHandler,
		nil,
		jobHandler,
		projectHandler,
		nil,
		nil,
		eventHandler,
		"push-secret",
		WithAuthMiddleware(authMiddleware),
		WithProjectMembershipHandler(membershipHandler),
	)
}

type rbacRouterFixture struct {
	router    http.Handler
	projectID string
	buildID   string
	tokens    map[string]string
	tokenIDs  map[string]string
}

func newRBACTestRouter(t *testing.T) rbacRouterFixture {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	userRepo := repositorymemory.NewUserRepository()
	membershipRepo := repositorymemory.NewProjectMembershipRepository(projectRepo, userRepo)
	membershipService := service.NewProjectMembershipService(projectRepo, membershipRepo)
	projectService := service.NewProjectService(projectRepo)
	userService := service.NewUserService(userRepo)
	apiTokenService := service.NewAPITokenService(repositorymemory.NewAPITokenRepository(), userRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)

	project, err := projectRepo.Create(ctx, domain.Project{ID: "11111111-1111-1111-1111-111111111111", Name: "Platform", Slug: "platform-rbac", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	users := []domain.User{
		{ID: "owner-rbac", Email: "owner@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
		{ID: "maintainer-rbac", Email: "maintainer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
		{ID: "viewer-rbac", Email: "viewer@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
		{ID: "outsider-rbac", Email: "outsider@example.com", GlobalRole: domain.GlobalRoleUser, CreatedAt: now, UpdatedAt: now},
	}
	for _, user := range users {
		if _, createErr := userRepo.Create(ctx, user); createErr != nil {
			t.Fatalf("create user %s failed: %v", user.Email, createErr)
		}
	}
	memberships := []service.UpsertProjectMembershipInput{
		{ProjectID: project.ID, UserID: "owner-rbac", Role: "owner"},
		{ProjectID: project.ID, UserID: "maintainer-rbac", Role: "maintainer"},
		{ProjectID: project.ID, UserID: "viewer-rbac", Role: "viewer"},
	}
	for _, membership := range memberships {
		if _, membershipErr := membershipService.UpsertProjectMembership(ctx, membership); membershipErr != nil {
			t.Fatalf("create membership failed: %v", membershipErr)
		}
	}
	tokens := map[string]string{}
	tokenIDs := map[string]string{}
	for _, user := range users {
		created, tokenErr := apiTokenService.CreateAPIToken(ctx, service.CreateAPITokenInput{UserID: user.ID, Name: "test-token"})
		if tokenErr != nil {
			t.Fatalf("create api token for %s failed: %v", user.ID, tokenErr)
		}
		tokens[user.ID] = created.PlaintextToken
		tokenIDs[user.ID] = created.Token.ID
	}
	build, err := buildSvc.CreateBuild(ctx, buildsvc.CreateBuildInput{ProjectID: project.ID})
	if err != nil {
		t.Fatalf("create build failed: %v", err)
	}

	buildHandler := handler.NewBuildHandler(buildSvc)
	buildHandler.SetProjectService(projectService)
	buildHandler.SetAuthorization(auth.ModeHeader, membershipService)
	jobHandler := handler.NewJobHandler(jobSvc)
	jobHandler.SetAuthorization(auth.ModeHeader, membershipService)
	projectHandler := handler.NewProjectHandler(projectService, jobSvc)
	projectHandler.SetAuthorization(auth.ModeHeader, membershipService)
	eventHandler := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	authMiddleware := auth.Middleware(auth.MiddlewareConfig{Mode: auth.ModeHeader, APITokens: apiTokenService}, userService)

	router := NewRouter(
		buildHandler,
		nil,
		jobHandler,
		projectHandler,
		nil,
		nil,
		eventHandler,
		"",
		WithAuthMiddleware(authMiddleware),
		WithUserHandler(handler.NewUserHandler(userService, auth.ModeHeader)),
		WithAPITokenHandler(handler.NewAPITokenHandler(apiTokenService)),
	)
	return rbacRouterFixture{router: router, projectID: project.ID, buildID: build.ID, tokens: tokens, tokenIDs: tokenIDs}
}

func TestNewRouter_APITokenCannotManageAPITokens(t *testing.T) {
	fixture := newRBACTestRouter(t)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/me/tokens"},
		{name: "create", method: http.MethodPost, path: "/api/me/tokens", body: `{"name":"chained"}`},
		{name: "revoke", method: http.MethodDelete, path: "/api/me/tokens/" + fixture.tokenIDs["viewer-rbac"]},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer "+fixture.tokens["viewer-rbac"])
			res := httptest.NewRecorder()
			fixture.router.ServeHTTP(res, req)
			if res.Code != http.StatusForbidden {
				t.Fatalf("expected status %d, got %d body=%s", http.StatusForbidden, res.Code, res.Body.String())
			}
			if !strings.Contains(res.Body.String(), "api tokens cannot manage api tokens") {
				t.Fatalf("expected clear token-management error, got %s", res.Body.String())
			}
		})
	}
}

func TestNewRouter_HeaderUserCanManageOwnAPITokens(t *testing.T) {
	fixture := newRBACTestRouter(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/me/tokens", bytes.NewBufferString(`{"name":"header-managed"}`))
	createReq.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	createRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d body=%s", http.StatusCreated, createRes.Code, createRes.Body.String())
	}
	var createBody struct {
		Data struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	if createBody.Data.ID == "" || createBody.Data.Token == "" {
		t.Fatalf("expected created token id and raw token, got %+v", createBody.Data)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/me/tokens", nil)
	listReq.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	listRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/me/tokens/"+createBody.Data.ID, nil)
	revokeReq.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	revokeRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(revokeRes, revokeReq)
	if revokeRes.Code != http.StatusNoContent {
		t.Fatalf("expected revoke status %d, got %d body=%s", http.StatusNoContent, revokeRes.Code, revokeRes.Body.String())
	}
}

func TestNewRouter_APITokenUserInheritsRBACPermissions(t *testing.T) {
	fixture := newRBACTestRouter(t)

	adminReq := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	adminReq.Header.Set("Authorization", "Bearer "+fixture.tokens["viewer-rbac"])
	adminRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(adminRes, adminReq)
	if adminRes.Code != http.StatusForbidden {
		t.Fatalf("expected non-admin token status %d, got %d body=%s", http.StatusForbidden, adminRes.Code, adminRes.Body.String())
	}

	projectReq := httptest.NewRequest(http.MethodGet, "/api/projects/"+fixture.projectID, nil)
	projectReq.Header.Set("Authorization", "Bearer "+fixture.tokens["viewer-rbac"])
	projectRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(projectRes, projectReq)
	if projectRes.Code != http.StatusOK {
		t.Fatalf("expected viewer token project read status %d, got %d body=%s", http.StatusOK, projectRes.Code, projectRes.Body.String())
	}
	headerProjectReq := httptest.NewRequest(http.MethodGet, "/api/projects/"+fixture.projectID, nil)
	headerProjectReq.Header.Set("X-Coyote-User-Email", "viewer@example.com")
	headerProjectRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(headerProjectRes, headerProjectReq)
	if headerProjectRes.Code != projectRes.Code {
		t.Fatalf("expected bearer and header auth to share RBAC result, bearer=%d header=%d", projectRes.Code, headerProjectRes.Code)
	}

	outsiderReq := httptest.NewRequest(http.MethodGet, "/api/projects/"+fixture.projectID, nil)
	outsiderReq.Header.Set("Authorization", "Bearer "+fixture.tokens["outsider-rbac"])
	outsiderRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(outsiderRes, outsiderReq)
	if outsiderRes.Code != http.StatusForbidden {
		t.Fatalf("expected outsider token status %d, got %d body=%s", http.StatusForbidden, outsiderRes.Code, outsiderRes.Body.String())
	}
}

func TestNewRouter_APITokenViewerCannotMutateProjectJobOrBuild(t *testing.T) {
	fixture := newRBACTestRouter(t)
	jobBody := `{"project_id":"` + fixture.projectID + `","name":"viewer-job","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","enabled":true}`

	projectReq := httptest.NewRequest(http.MethodPatch, "/api/projects/"+fixture.projectID, bytes.NewBufferString(`{"name":"blocked"}`))
	projectReq.Header.Set("Authorization", "Bearer "+fixture.tokens["viewer-rbac"])
	projectRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(projectRes, projectReq)
	if projectRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer project mutation status %d, got %d body=%s", http.StatusForbidden, projectRes.Code, projectRes.Body.String())
	}

	jobReq := httptest.NewRequest(http.MethodPost, "/api/jobs/", bytes.NewBufferString(jobBody))
	jobReq.Header.Set("Authorization", "Bearer "+fixture.tokens["viewer-rbac"])
	jobRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(jobRes, jobReq)
	if jobRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer job mutation status %d, got %d body=%s", http.StatusForbidden, jobRes.Code, jobRes.Body.String())
	}

	buildReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+fixture.buildID+"/queue", bytes.NewBufferString(`{}`))
	buildReq.Header.Set("Authorization", "Bearer "+fixture.tokens["viewer-rbac"])
	buildRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(buildRes, buildReq)
	if buildRes.Code != http.StatusForbidden {
		t.Fatalf("expected viewer build mutation status %d, got %d body=%s", http.StatusForbidden, buildRes.Code, buildRes.Body.String())
	}
}

func TestNewRouter_APITokenMaintainerCanMutateAllowedProjectRoutes(t *testing.T) {
	fixture := newRBACTestRouter(t)
	body := `{"project_id":"` + fixture.projectID + `","name":"maintainer-job","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","enabled":true}`

	jobReq := httptest.NewRequest(http.MethodPost, "/api/jobs/", bytes.NewBufferString(body))
	jobReq.Header.Set("Authorization", "Bearer "+fixture.tokens["maintainer-rbac"])
	jobRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(jobRes, jobReq)
	if jobRes.Code != http.StatusCreated {
		t.Fatalf("expected maintainer token job create status %d, got %d body=%s", http.StatusCreated, jobRes.Code, jobRes.Body.String())
	}

	buildReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+fixture.buildID+"/queue", bytes.NewBufferString(`{}`))
	buildReq.Header.Set("Authorization", "Bearer "+fixture.tokens["maintainer-rbac"])
	buildRes := httptest.NewRecorder()
	fixture.router.ServeHTTP(buildRes, buildReq)
	if buildRes.Code != http.StatusOK {
		t.Fatalf("expected maintainer token build queue status %d, got %d body=%s", http.StatusOK, buildRes.Code, buildRes.Body.String())
	}
}

func githubRouterTestSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func stringPointer(value string) *string {
	return &value
}

func equalStringPointers(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func TestNewRouter_QueueBuild_UnknownTemplate_FallsBackToDefaultStep(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, nil, jh, nil, nil, nil, eh, "")

	createReq := httptest.NewRequest(http.MethodPost, "/api/builds/", bytes.NewBufferString(`{"project_id":"project-1"}`))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createBody map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create data envelope, got %v", createBody)
	}
	buildID, ok := createData["id"].(string)
	if !ok || buildID == "" {
		t.Fatalf("expected create response id, got %v", createData["id"])
	}

	queueReq := httptest.NewRequest(http.MethodPost, "/api/builds/"+buildID+"/queue", bytes.NewBufferString(`{"template":"not-a-template"}`))
	queueRes := httptest.NewRecorder()
	r.ServeHTTP(queueRes, queueReq)
	if queueRes.Code != http.StatusOK {
		t.Fatalf("expected queue status %d, got %d", http.StatusOK, queueRes.Code)
	}

	stepsReq := httptest.NewRequest(http.MethodGet, "/api/builds/"+buildID+"/steps", nil)
	stepsRes := httptest.NewRecorder()
	r.ServeHTTP(stepsRes, stepsReq)
	if stepsRes.Code != http.StatusOK {
		t.Fatalf("expected steps status %d, got %d", http.StatusOK, stepsRes.Code)
	}

	var stepsBody map[string]any
	if err := json.Unmarshal(stepsRes.Body.Bytes(), &stepsBody); err != nil {
		t.Fatalf("failed to parse steps response: %v", err)
	}
	stepsData, ok := stepsBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected steps data envelope, got %v", stepsBody)
	}
	steps, ok := stepsData["steps"].([]any)
	if !ok {
		t.Fatalf("expected steps array, got %T", stepsData["steps"])
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 default step, got %d", len(steps))
	}

	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("expected step object, got %T", steps[0])
	}
	if step["step_index"] != float64(0) {
		t.Fatalf("expected step_index 0, got %v", step["step_index"])
	}
	if step["name"] != "default" {
		t.Fatalf("expected default step name, got %v", step["name"])
	}
}

func TestNewRouter_JobRoutes(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, nil, jh, nil, nil, nil, eh, "")

	createBody := `{"project_id":"project-1","name":"backend-ci","repository_url":"https://github.com/example/backend.git","default_ref":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","enabled":true}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/jobs/", bytes.NewBufferString(createBody))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create job status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createPayload map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("parse create job response failed: %v", err)
	}
	data, ok := createPayload["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create response data object, got %T", createPayload["data"])
	}
	jobID, ok := data["id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("expected create response job id string, got %v", data["id"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)
	listRes := httptest.NewRecorder()
	r.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list jobs status %d, got %d", http.StatusOK, listRes.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID, nil)
	getRes := httptest.NewRecorder()
	r.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected get job status %d, got %d", http.StatusOK, getRes.Code)
	}

	runReq := httptest.NewRequest(http.MethodPost, "/api/jobs/"+jobID+"/run", nil)
	runRes := httptest.NewRecorder()
	r.ServeHTTP(runRes, runReq)
	if runRes.Code != http.StatusCreated {
		t.Fatalf("expected run-now status %d, got %d", http.StatusCreated, runRes.Code)
	}
}

func TestNewRouter_PushEventRoute(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc)
	jh := handler.NewJobHandler(jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, nil, jh, nil, nil, nil, eh, "")

	createBody := `{"project_id":"project-1","name":"backend-ci","repository_url":"https://github.com/example/backend.git","default_ref":"main","push_enabled":true,"push_branch":"main","pipeline_yaml":"version: 1\nsteps:\n  - name: test\n    run: go test ./...\n","enabled":true}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/jobs/", bytes.NewBufferString(createBody))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create job status %d, got %d", http.StatusCreated, createRes.Code)
	}

	eventBody := `{"repository_url":"https://github.com/example/backend.git","ref":"main","commit_sha":"abc123"}`
	eventReq := httptest.NewRequest(http.MethodPost, "/api/events/push", bytes.NewBufferString(eventBody))
	eventRes := httptest.NewRecorder()
	r.ServeHTTP(eventRes, eventReq)
	if eventRes.Code != http.StatusOK {
		t.Fatalf("expected push event status %d, got %d body=%s", http.StatusOK, eventRes.Code, eventRes.Body.String())
	}
}

func TestNewRouter_ProjectRoutes(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)
	jh := handler.NewJobHandler(jobSvc)
	ph := handler.NewProjectHandler(service.NewProjectService(projectRepo), jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, nil, jh, ph, nil, nil, eh, "")

	createReq := httptest.NewRequest(http.MethodPost, "/api/projects/", bytes.NewBufferString(`{"name":"Platform","slug":"platform"}`))
	createRes := httptest.NewRecorder()
	r.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create project status %d, got %d", http.StatusCreated, createRes.Code)
	}

	var createBody map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("failed to parse create project response: %v", err)
	}
	createData, ok := createBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected create project data object, got %T", createBody["data"])
	}
	projectID, ok := createData["id"].(string)
	if !ok || projectID == "" {
		t.Fatalf("expected create project id, got %v", createData["id"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/projects/", nil)
	listRes := httptest.NewRecorder()
	r.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list projects status %d, got %d", http.StatusOK, listRes.Code)
	}

	_, err := jobSvc.CreateJob(httptest.NewRequest(http.MethodGet, "/", nil).Context(), service.CreateJobInput{
		ProjectID:     projectID,
		Name:          "backend-ci",
		RepositoryURL: "https://github.com/example/backend.git",
		DefaultRef:    "main",
		PipelineYAML:  "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
	})
	if err != nil {
		t.Fatalf("create job failed: %v", err)
	}

	jobsReq := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/jobs", nil)
	jobsRes := httptest.NewRecorder()
	r.ServeHTTP(jobsRes, jobsReq)
	if jobsRes.Code != http.StatusOK {
		t.Fatalf("expected project jobs status %d, got %d", http.StatusOK, jobsRes.Code)
	}
}

func TestNewRouter_ProjectDuplicateSlugReturnsConflict(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	h := handler.NewBuildHandler(buildSvc)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)
	ph := handler.NewProjectHandler(service.NewProjectService(projectRepo), jobSvc)
	eh := handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), "")
	r := NewRouter(h, nil, handler.NewJobHandler(jobSvc), ph, nil, nil, eh, "")

	body := `{"name":"Platform","slug":"platform"}`
	firstReq := httptest.NewRequest(http.MethodPost, "/api/projects/", bytes.NewBufferString(body))
	firstRes := httptest.NewRecorder()
	r.ServeHTTP(firstRes, firstReq)
	if firstRes.Code != http.StatusCreated {
		t.Fatalf("expected first project create status %d, got %d", http.StatusCreated, firstRes.Code)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/projects/", bytes.NewBufferString(body))
	secondRes := httptest.NewRecorder()
	r.ServeHTTP(secondRes, secondReq)
	if secondRes.Code != http.StatusConflict {
		t.Fatalf("expected duplicate slug project status %d, got %d", http.StatusConflict, secondRes.Code)
	}
}

func TestNewRouter_ProjectDeleteDefaultReturnsConflict(t *testing.T) {
	buildRepo := repositorymemory.NewBuildRepository()
	jobRepo := repositorymemory.NewJobRepository()
	projectRepo := repositorymemory.NewProjectRepository(jobRepo)
	buildSvc := buildsvc.NewBuildService(buildRepo, nil, nil)
	jobSvc := service.NewJobService(jobRepo, buildSvc).WithProjectRepository(projectRepo)
	projectSvc := service.NewProjectService(projectRepo)
	ph := handler.NewProjectHandler(projectSvc, jobSvc)
	r := NewRouter(handler.NewBuildHandler(buildSvc), nil, handler.NewJobHandler(jobSvc), ph, nil, nil, handler.NewEventHandler(jobSvc, webhooksvc.NewDeliveryIngressService(repositorymemory.NewWebhookDeliveryRepository(), jobSvc), observability.NewNoopWebhookIngressMetrics(), ""), "")

	_, err := projectRepo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), domain.Project{
		ID:   "00000000-0000-0000-0000-000000000001",
		Name: "Default Project",
		Slug: domain.DefaultProjectSlug,
	})
	if err != nil {
		t.Fatalf("create default project failed: %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/projects/00000000-0000-0000-0000-000000000001", nil)
	deleteRes := httptest.NewRecorder()
	r.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusConflict {
		t.Fatalf("expected default project delete status %d, got %d", http.StatusConflict, deleteRes.Code)
	}
}
