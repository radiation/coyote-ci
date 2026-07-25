package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestSCMHandler_CRUDFoundationRoutes(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	h := NewSCMHandler(service.NewSCMAdminService(connectionRepo, repositoryRepo))

	listEmptyRegistrationsReq := httptest.NewRequest(http.MethodGet, "/settings/scm/github-apps", nil)
	listEmptyRegistrationsRes := httptest.NewRecorder()
	h.ListGitHubAppRegistrations(listEmptyRegistrationsRes, listEmptyRegistrationsReq)
	if listEmptyRegistrationsRes.Code != http.StatusOK {
		t.Fatalf("expected empty registration list status %d, got %d body=%s", http.StatusOK, listEmptyRegistrationsRes.Code, listEmptyRegistrationsRes.Body.String())
	}
	if body := listEmptyRegistrationsRes.Body.String(); !bytes.Contains([]byte(body), []byte(`"github_apps":[]`)) {
		t.Fatalf("expected empty github app list, got %s", body)
	}

	createRegistrationReq := httptest.NewRequest(http.MethodPost, "/settings/scm/github-apps", bytes.NewBufferString(`{"app_id":"12345","private_key_secret_ref":"secret/github/private-key","webhook_secret_ref":"secret/github/webhook"}`))
	createRegistrationRes := httptest.NewRecorder()
	h.CreateGitHubAppRegistration(createRegistrationRes, createRegistrationReq)
	if createRegistrationRes.Code != http.StatusCreated {
		t.Fatalf("expected create registration status %d, got %d body=%s", http.StatusCreated, createRegistrationRes.Code, createRegistrationRes.Body.String())
	}
	createdRegistration := decodeDataMap(t, createRegistrationRes)
	registrationID, hasRegistrationID := createdRegistration["id"].(string)
	if !hasRegistrationID || registrationID == "" {
		t.Fatalf("expected registration id, got %v", createdRegistration["id"])
	}
	if _, hasPrivateKeySecretRef := createdRegistration["private_key_secret_ref"]; hasPrivateKeySecretRef {
		t.Fatalf("expected registration response to omit secret refs, got %v", createdRegistration)
	}
	if _, hasWebhookSecretRef := createdRegistration["webhook_secret_ref"]; hasWebhookSecretRef {
		t.Fatalf("expected registration response to omit secret refs, got %v", createdRegistration)
	}
	privateKeyConfigured, hasPrivateKeyConfigured := createdRegistration["private_key_configured"].(bool)
	if !hasPrivateKeyConfigured || !privateKeyConfigured {
		t.Fatalf("expected private_key_configured to be true, got %v", createdRegistration["private_key_configured"])
	}
	webhookConfigured, hasWebhookConfigured := createdRegistration["webhook_configured"].(bool)
	if !hasWebhookConfigured || !webhookConfigured {
		t.Fatalf("expected webhook_configured to be true, got %v", createdRegistration["webhook_configured"])
	}

	createSecondRegistrationReq := httptest.NewRequest(http.MethodPost, "/settings/scm/github-apps", bytes.NewBufferString(`{"app_id":"54321","api_base_url":"https://ghe.example/api/v3","web_base_url":"https://ghe.example","private_key_secret_ref":"secret/github/private-key-2","webhook_secret_ref":"secret/github/webhook-2"}`))
	createSecondRegistrationRes := httptest.NewRecorder()
	h.CreateGitHubAppRegistration(createSecondRegistrationRes, createSecondRegistrationReq)
	if createSecondRegistrationRes.Code != http.StatusCreated {
		t.Fatalf("expected second registration status %d, got %d body=%s", http.StatusCreated, createSecondRegistrationRes.Code, createSecondRegistrationRes.Body.String())
	}
	createdSecondRegistration := decodeDataMap(t, createSecondRegistrationRes)
	secondRegistrationID, hasSecondRegistrationID := createdSecondRegistration["id"].(string)
	if !hasSecondRegistrationID || secondRegistrationID == "" {
		t.Fatalf("expected second registration id, got %v", createdSecondRegistration["id"])
	}

	listRegistrationsReq := httptest.NewRequest(http.MethodGet, "/settings/scm/github-apps", nil)
	listRegistrationsRes := httptest.NewRecorder()
	h.ListGitHubAppRegistrations(listRegistrationsRes, listRegistrationsReq)
	if listRegistrationsRes.Code != http.StatusOK {
		t.Fatalf("expected list registration status %d, got %d body=%s", http.StatusOK, listRegistrationsRes.Code, listRegistrationsRes.Body.String())
	}
	listRegistrations := decodeDataMap(t, listRegistrationsRes)
	items, hasRegistrations := listRegistrations["github_apps"].([]any)
	if !hasRegistrations || len(items) != 2 {
		t.Fatalf("expected two github app registrations, got %v", listRegistrations["github_apps"])
	}

	getRegistrationReq := addURLParam(httptest.NewRequest(http.MethodGet, "/settings/scm/github-apps/"+registrationID, nil), "registrationID", registrationID)
	getRegistrationRes := httptest.NewRecorder()
	h.GetGitHubAppRegistration(getRegistrationRes, getRegistrationReq)
	if getRegistrationRes.Code != http.StatusOK {
		t.Fatalf("expected get registration status %d, got %d body=%s", http.StatusOK, getRegistrationRes.Code, getRegistrationRes.Body.String())
	}
	gotRegistration := decodeDataMap(t, getRegistrationRes)
	if _, hasPrivateKeySecretRef := gotRegistration["private_key_secret_ref"]; hasPrivateKeySecretRef {
		t.Fatalf("expected get registration response to omit private key secret ref, got %v", gotRegistration)
	}
	if _, hasWebhookSecretRef := gotRegistration["webhook_secret_ref"]; hasWebhookSecretRef {
		t.Fatalf("expected get registration response to omit webhook secret ref, got %v", gotRegistration)
	}

	getMissingRegistrationReq := addURLParam(httptest.NewRequest(http.MethodGet, "/settings/scm/github-apps/missing", nil), "registrationID", "missing")
	getMissingRegistrationRes := httptest.NewRecorder()
	h.GetGitHubAppRegistration(getMissingRegistrationRes, getMissingRegistrationReq)
	if getMissingRegistrationRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing registration status %d, got %d body=%s", http.StatusNotFound, getMissingRegistrationRes.Code, getMissingRegistrationRes.Body.String())
	}

	createConnectionReq := httptest.NewRequest(http.MethodPost, "/settings/scm/connections/github-app-installations", bytes.NewBufferString(`{"app_registration_id":"`+registrationID+`","display_name":"octo cloud","enabled":true,"installation_id":"999","account_login":"octo","account_type":"organization","target_id":"42"}`))
	createConnectionRes := httptest.NewRecorder()
	h.CreateGitHubAppInstallationConnection(createConnectionRes, createConnectionReq)
	if createConnectionRes.Code != http.StatusCreated {
		t.Fatalf("expected create connection status %d, got %d body=%s", http.StatusCreated, createConnectionRes.Code, createConnectionRes.Body.String())
	}
	createdConnection := decodeDataMap(t, createConnectionRes)
	connectionID, hasConnectionID := createdConnection["id"].(string)
	if !hasConnectionID || connectionID == "" {
		t.Fatalf("expected connection id, got %v", createdConnection["id"])
	}
	githubApp, hasGitHubApp := createdConnection["github_app"].(map[string]any)
	if !hasGitHubApp {
		t.Fatalf("expected github app metadata in response, got %v", createdConnection["github_app"])
	}
	if _, hasPrivateKeySecretRef := githubApp["private_key_secret_ref"]; hasPrivateKeySecretRef {
		t.Fatalf("expected connection response to omit private key secret ref, got %v", githubApp)
	}
	if _, hasWebhookSecretRef := githubApp["webhook_secret_ref"]; hasWebhookSecretRef {
		t.Fatalf("expected connection response to omit webhook secret ref, got %v", githubApp)
	}

	listConnectionsReq := httptest.NewRequest(http.MethodGet, "/settings/scm/connections", nil)
	listConnectionsRes := httptest.NewRecorder()
	h.ListConnections(listConnectionsRes, listConnectionsReq)
	if listConnectionsRes.Code != http.StatusOK {
		t.Fatalf("expected list connections status %d, got %d", http.StatusOK, listConnectionsRes.Code)
	}

	getConnectionReq := addURLParam(httptest.NewRequest(http.MethodGet, "/settings/scm/connections/"+connectionID, nil), "connectionID", connectionID)
	getConnectionRes := httptest.NewRecorder()
	h.GetConnection(getConnectionRes, getConnectionReq)
	if getConnectionRes.Code != http.StatusOK {
		t.Fatalf("expected get connection status %d, got %d", http.StatusOK, getConnectionRes.Code)
	}

	patchConnectionReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/settings/scm/connections/"+connectionID, bytes.NewBufferString(`{"enabled":false}`)), "connectionID", connectionID)
	patchConnectionRes := httptest.NewRecorder()
	h.PatchConnection(patchConnectionRes, patchConnectionReq)
	if patchConnectionRes.Code != http.StatusOK {
		t.Fatalf("expected patch connection status %d, got %d", http.StatusOK, patchConnectionRes.Code)
	}
	patchedConnection := decodeDataMap(t, patchConnectionRes)
	if enabled, hasEnabledFlag := patchedConnection["enabled"].(bool); !hasEnabledFlag || enabled {
		t.Fatalf("expected connection to be disabled, got %v", patchedConnection["enabled"])
	}

	createSiblingReq := httptest.NewRequest(http.MethodPost, "/settings/scm/connections/github-app-installations", bytes.NewBufferString(`{"app_registration_id":"`+registrationID+`","display_name":"octo sibling","enabled":true,"installation_id":"1000","account_login":"octo-two","account_type":"organization","target_id":"84"}`))
	createSiblingRes := httptest.NewRecorder()
	h.CreateGitHubAppInstallationConnection(createSiblingRes, createSiblingReq)
	if createSiblingRes.Code != http.StatusCreated {
		t.Fatalf("expected create sibling connection status %d, got %d body=%s", http.StatusCreated, createSiblingRes.Code, createSiblingRes.Body.String())
	}
	createdSibling := decodeDataMap(t, createSiblingRes)
	siblingID, hasSiblingID := createdSibling["id"].(string)
	if !hasSiblingID || siblingID == "" {
		t.Fatalf("expected sibling connection id, got %v", createdSibling["id"])
	}
	getSiblingReq := addURLParam(httptest.NewRequest(http.MethodGet, "/settings/scm/connections/"+siblingID, nil), "connectionID", siblingID)
	getSiblingRes := httptest.NewRecorder()
	h.GetConnection(getSiblingRes, getSiblingReq)
	if getSiblingRes.Code != http.StatusOK {
		t.Fatalf("expected get sibling connection status %d, got %d body=%s", http.StatusOK, getSiblingRes.Code, getSiblingRes.Body.String())
	}
	siblingConnection := decodeDataMap(t, getSiblingRes)
	if enabled, hasEnabledFlag := siblingConnection["enabled"].(bool); !hasEnabledFlag || !enabled {
		t.Fatalf("expected sibling connection to remain enabled, got %v", siblingConnection["enabled"])
	}

	duplicateConnectionReq := httptest.NewRequest(http.MethodPost, "/settings/scm/connections/github-app-installations", bytes.NewBufferString(`{"app_registration_id":"`+registrationID+`","display_name":"duplicate","enabled":true,"installation_id":"999","account_login":"octo-dup","account_type":"organization","target_id":"126"}`))
	duplicateConnectionRes := httptest.NewRecorder()
	h.CreateGitHubAppInstallationConnection(duplicateConnectionRes, duplicateConnectionReq)
	if duplicateConnectionRes.Code != http.StatusConflict {
		t.Fatalf("expected duplicate connection status %d, got %d body=%s", http.StatusConflict, duplicateConnectionRes.Code, duplicateConnectionRes.Body.String())
	}

	missingRegistrationReq := httptest.NewRequest(http.MethodPost, "/settings/scm/connections/github-app-installations", bytes.NewBufferString(`{"app_registration_id":"missing","display_name":"missing","enabled":true,"installation_id":"1001","account_login":"octo-missing","account_type":"organization","target_id":"168"}`))
	missingRegistrationRes := httptest.NewRecorder()
	h.CreateGitHubAppInstallationConnection(missingRegistrationRes, missingRegistrationReq)
	if missingRegistrationRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing registration status %d, got %d body=%s", http.StatusNotFound, missingRegistrationRes.Code, missingRegistrationRes.Body.String())
	}

	listRepositoriesReq := httptest.NewRequest(http.MethodGet, "/settings/scm/repositories", nil)
	listRepositoriesRes := httptest.NewRecorder()
	h.ListRegisteredRepositories(listRepositoriesRes, listRepositoriesReq)
	if listRepositoriesRes.Code != http.StatusOK {
		t.Fatalf("expected list repositories status %d, got %d", http.StatusOK, listRepositoriesRes.Code)
	}
}

func TestToGitHubAppRegistrationResponse_ConfiguredBooleans(t *testing.T) {
	now := time.Now().UTC()
	response := toGitHubAppRegistrationResponse(domain.GitHubAppRegistration{
		ID:                  "registration-1",
		AppID:               "12345",
		APIBaseURL:          "https://api.github.com",
		WebBaseURL:          "https://github.com",
		PrivateKeySecretRef: "",
		WebhookSecretRef:    "",
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if response.PrivateKeyConfigured {
		t.Fatal("expected private key configured to be false")
	}
	if response.WebhookConfigured {
		t.Fatal("expected webhook configured to be false")
	}
}

func TestSCMHandler_ErrorPaths(t *testing.T) {
	unavailable := &SCMHandler{}
	unavailableReq := httptest.NewRequest(http.MethodGet, "/settings/scm/github-apps", nil)
	unavailableRes := httptest.NewRecorder()
	unavailable.ListGitHubAppRegistrations(unavailableRes, unavailableReq)
	if unavailableRes.Code != http.StatusNotFound {
		t.Fatalf("expected unavailable handler status %d, got %d body=%s", http.StatusNotFound, unavailableRes.Code, unavailableRes.Body.String())
	}

	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	h := NewSCMHandler(service.NewSCMAdminService(connectionRepo, repositoryRepo))

	badCreateRegistrationReq := httptest.NewRequest(http.MethodPost, "/settings/scm/github-apps", bytes.NewBufferString(`{"app_id":`))
	badCreateRegistrationRes := httptest.NewRecorder()
	h.CreateGitHubAppRegistration(badCreateRegistrationRes, badCreateRegistrationReq)
	if badCreateRegistrationRes.Code != http.StatusBadRequest {
		t.Fatalf("expected bad registration request status %d, got %d body=%s", http.StatusBadRequest, badCreateRegistrationRes.Code, badCreateRegistrationRes.Body.String())
	}

	badCreateConnectionReq := httptest.NewRequest(http.MethodPost, "/settings/scm/connections/github-app-installations", bytes.NewBufferString(`{"app_registration_id":`))
	badCreateConnectionRes := httptest.NewRecorder()
	h.CreateGitHubAppInstallationConnection(badCreateConnectionRes, badCreateConnectionReq)
	if badCreateConnectionRes.Code != http.StatusBadRequest {
		t.Fatalf("expected bad connection request status %d, got %d body=%s", http.StatusBadRequest, badCreateConnectionRes.Code, badCreateConnectionRes.Body.String())
	}

	badPatchReq := addURLParam(httptest.NewRequest(http.MethodPatch, "/settings/scm/connections/connection-1", bytes.NewBufferString(`{"enabled":`)), "connectionID", "connection-1")
	badPatchRes := httptest.NewRecorder()
	h.PatchConnection(badPatchRes, badPatchReq)
	if badPatchRes.Code != http.StatusBadRequest {
		t.Fatalf("expected bad patch request status %d, got %d body=%s", http.StatusBadRequest, badPatchRes.Code, badPatchRes.Body.String())
	}

	badCreateRepositoryReq := httptest.NewRequest(http.MethodPost, "/settings/scm/repositories", bytes.NewBufferString(`{"connection_id":`))
	badCreateRepositoryRes := httptest.NewRecorder()
	h.CreateRegisteredRepository(badCreateRepositoryRes, badCreateRepositoryReq)
	if badCreateRepositoryRes.Code != http.StatusBadRequest {
		t.Fatalf("expected bad repository request status %d, got %d body=%s", http.StatusBadRequest, badCreateRepositoryRes.Code, badCreateRepositoryRes.Body.String())
	}

	missingConnectionReq := addURLParam(httptest.NewRequest(http.MethodGet, "/settings/scm/connections/missing", nil), "connectionID", "missing")
	missingConnectionRes := httptest.NewRecorder()
	h.GetConnection(missingConnectionRes, missingConnectionReq)
	if missingConnectionRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing connection status %d, got %d body=%s", http.StatusNotFound, missingConnectionRes.Code, missingConnectionRes.Body.String())
	}

	missingRepositoryReq := addURLParam(httptest.NewRequest(http.MethodGet, "/settings/scm/repositories/missing", nil), "repositoryID", "missing")
	missingRepositoryRes := httptest.NewRecorder()
	h.GetRegisteredRepository(missingRepositoryRes, missingRepositoryReq)
	if missingRepositoryRes.Code != http.StatusNotFound {
		t.Fatalf("expected missing repository status %d, got %d body=%s", http.StatusNotFound, missingRepositoryRes.Code, missingRepositoryRes.Body.String())
	}
}

func TestSCMHandler_CreateRegisteredRepositoryRoute_UsesSelectorPayload(t *testing.T) {
	now := time.Now().UTC()
	admin := &fakeSCMAdminServiceForHandler{
		createRegisteredRepository: domain.SCMRepositoryRegistration{
			ID:                   "repository-1",
			ConnectionID:         "connection-1",
			ProviderRepositoryID: "1001",
			Owner:                "octo",
			Name:                 "widgets",
			FullName:             "octo/widgets",
			CloneURL:             "https://github.com/octo/widgets.git",
			WebURL:               "https://github.com/octo/widgets",
			DefaultBranch:        handlerTestStringPtr("main"),
			MetadataRefreshedAt:  now,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
	}
	h := NewSCMHandler(admin)
	req := httptest.NewRequest(http.MethodPost, "/settings/scm/repositories", bytes.NewBufferString(`{"connection_id":"connection-1","owner":"octo","name":"widgets"}`))
	res := httptest.NewRecorder()
	h.CreateRegisteredRepository(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("expected create repository status %d, got %d body=%s", http.StatusCreated, res.Code, res.Body.String())
	}
	if admin.createRegisteredRepositoryInput.ConnectionID != "connection-1" || admin.createRegisteredRepositoryInput.Owner != "octo" || admin.createRegisteredRepositoryInput.Name != "widgets" || admin.createRegisteredRepositoryInput.ProviderRepositoryID != "" {
		t.Fatalf("expected selector input to pass through, got %+v", admin.createRegisteredRepositoryInput)
	}
	createdRepository := decodeDataMap(t, res)
	if createdRepository["full_name"] != "octo/widgets" {
		t.Fatalf("expected provider-backed response body, got %v", createdRepository)
	}
	if _, hasCloneURL := createdRepository["clone_url"]; !hasCloneURL {
		t.Fatalf("expected create response to include canonical metadata, got %v", createdRepository)
	}
}

func TestSCMHandler_CreateRegisteredRepositoryRoute_ErrorMappings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{name: "selector invalid", err: service.ErrSCMRegisteredRepositorySelectorInvalid, wantCode: http.StatusBadRequest, wantBody: `"code":"invalid_request"`},
		{name: "provider inaccessible", err: service.ErrSCMGitHubRepositoryNotAccessible, wantCode: http.StatusNotFound, wantBody: `"code":"not_found"`},
		{name: "duplicate", err: repository.ErrSCMRepositoryRegistrationDuplicate, wantCode: http.StatusConflict, wantBody: `"code":"conflict"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewSCMHandler(&fakeSCMAdminServiceForHandler{createRegisteredRepositoryErr: tc.err})
			req := httptest.NewRequest(http.MethodPost, "/settings/scm/repositories", bytes.NewBufferString(`{"connection_id":"connection-1","provider_repository_id":"1001"}`))
			res := httptest.NewRecorder()
			h.CreateRegisteredRepository(res, req)
			if res.Code != tc.wantCode {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantCode, res.Code, res.Body.String())
			}
			if !bytes.Contains(res.Body.Bytes(), []byte(tc.wantBody)) {
				t.Fatalf("expected body to contain %s, got %s", tc.wantBody, res.Body.String())
			}
		})
	}
}

func TestSCMHandler_RefreshRegisteredRepositoryRoute(t *testing.T) {
	now := time.Now().UTC()
	admin := &fakeSCMAdminServiceForHandler{
		refreshRegisteredRepository: domain.SCMRepositoryRegistration{
			ID:                   "repository-1",
			ConnectionID:         "connection-1",
			ProviderRepositoryID: "1001",
			Owner:                "acme",
			Name:                 "platform",
			FullName:             "acme/platform",
			CloneURL:             "https://github.com/acme/platform.git",
			WebURL:               "https://github.com/acme/platform",
			DefaultBranch:        handlerTestStringPtr("trunk"),
			Archived:             true,
			Disabled:             true,
			MetadataRefreshedAt:  now,
			CreatedAt:            now.Add(-time.Hour),
			UpdatedAt:            now,
		},
	}
	h := NewSCMHandler(admin)
	req := addURLParam(httptest.NewRequest(http.MethodPost, "/settings/scm/repositories/repository-1/refresh", nil), "repositoryID", "repository-1")
	res := httptest.NewRecorder()
	h.RefreshRegisteredRepository(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected refresh status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	if admin.refreshRegisteredRepositoryID != "repository-1" {
		t.Fatalf("expected refresh repository id to pass through, got %q", admin.refreshRegisteredRepositoryID)
	}
	refreshed := decodeDataMap(t, res)
	if refreshed["full_name"] != "acme/platform" {
		t.Fatalf("expected refreshed repository metadata, got %v", refreshed)
	}

	admin.refreshRegisteredRepositoryErr = service.ErrSCMGitHubRepositoryNotAccessible
	res = httptest.NewRecorder()
	h.RefreshRegisteredRepository(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected refresh not found status %d, got %d body=%s", http.StatusNotFound, res.Code, res.Body.String())
	}
}

func TestSCMHandler_TestConnectionRoute(t *testing.T) {
	now := time.Now().UTC()
	admin := &fakeSCMAdminServiceForHandler{testConnection: domain.SCMConnectionDetail{Connection: domain.SCMConnection{ID: "connection-1", Provider: domain.SCMProviderGitHub, DisplayName: "octo", DeploymentKind: domain.SCMDeploymentKindCloud, APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", Enabled: true, HealthStatus: domain.SCMConnectionHealthStatusHealthy, CreatedAt: now, UpdatedAt: now}, GitHubAppRegistration: &domain.GitHubAppRegistration{ID: "registration-1", AppID: "12345", APIBaseURL: "https://api.github.com", WebBaseURL: "https://github.com", PrivateKeySecretRef: "secret/private", WebhookSecretRef: "secret/webhook", CreatedAt: now, UpdatedAt: now}, GitHubAppInstallation: &domain.GitHubAppInstallation{ConnectionID: "connection-1", AppRegistrationID: "registration-1", InstallationID: "999", AccountLogin: "octo", AccountType: "organization", AccountID: "42", CreatedAt: now, UpdatedAt: now}}}
	h := NewSCMHandler(admin)
	req := addURLParam(httptest.NewRequest(http.MethodPost, "/settings/scm/connections/connection-1/test", nil), "connectionID", "connection-1")
	res := httptest.NewRecorder()
	h.TestConnection(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected test connection status %d, got %d body=%s", http.StatusOK, res.Code, res.Body.String())
	}
	body := res.Body.String()
	if bytes.Contains([]byte(body), []byte("secret/private")) || bytes.Contains([]byte(body), []byte("ghs_")) {
		t.Fatalf("expected sanitized response, got %s", body)
	}

	admin.testConnectionErr = service.ErrSCMGitHubAuthenticationFailed
	res = httptest.NewRecorder()
	h.TestConnection(res, req)
	if res.Code != http.StatusBadGateway {
		t.Fatalf("expected auth failure status %d, got %d body=%s", http.StatusBadGateway, res.Code, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte(`"code":"provider_auth_failed"`)) {
		t.Fatalf("expected provider_auth_failed code, got %s", res.Body.String())
	}
}

func TestSCMHandler_TestConnectionRoute_ErrorMappings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{name: "secret resolution failed", err: service.ErrSCMGitHubPrivateKeyResolveFailed, wantCode: http.StatusBadGateway, wantBody: `"code":"secret_resolution_failed"`},
		{name: "installation unavailable", err: service.ErrSCMGitHubInstallationUnavailable, wantCode: http.StatusBadGateway, wantBody: `"code":"installation_unavailable"`},
		{name: "rate limited", err: service.ErrSCMGitHubRateLimited, wantCode: http.StatusBadGateway, wantBody: `"code":"rate_limited"`},
		{name: "provider unavailable", err: service.ErrSCMGitHubProviderUnavailable, wantCode: http.StatusBadGateway, wantBody: `"code":"provider_unavailable"`},
		{name: "provider malformed", err: service.ErrSCMGitHubProviderMalformedResponse, wantCode: http.StatusBadGateway, wantBody: `"code":"provider_unavailable"`},
		{name: "internal", err: errors.New("boom"), wantCode: http.StatusInternalServerError, wantBody: `"code":"internal_error"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewSCMHandler(&fakeSCMAdminServiceForHandler{testConnectionErr: tc.err})
			req := addURLParam(httptest.NewRequest(http.MethodPost, "/settings/scm/connections/connection-1/test", nil), "connectionID", "connection-1")
			res := httptest.NewRecorder()
			h.TestConnection(res, req)
			if res.Code != tc.wantCode {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantCode, res.Code, res.Body.String())
			}
			if !bytes.Contains(res.Body.Bytes(), []byte(tc.wantBody)) {
				t.Fatalf("expected body to contain %s, got %s", tc.wantBody, res.Body.String())
			}
		})
	}
}

type fakeSCMAdminServiceForHandler struct {
	testConnection                  domain.SCMConnectionDetail
	testConnectionErr               error
	createRegisteredRepository      domain.SCMRepositoryRegistration
	createRegisteredRepositoryErr   error
	createRegisteredRepositoryInput service.CreateSCMRepositoryRegistrationInput
	refreshRegisteredRepository     domain.SCMRepositoryRegistration
	refreshRegisteredRepositoryErr  error
	refreshRegisteredRepositoryID   string
}

func (f *fakeSCMAdminServiceForHandler) ListConnections(context.Context) ([]domain.SCMConnectionDetail, error) {
	return nil, nil
}

func (f *fakeSCMAdminServiceForHandler) GetConnection(context.Context, string) (domain.SCMConnectionDetail, error) {
	return domain.SCMConnectionDetail{}, nil
}

func (f *fakeSCMAdminServiceForHandler) ListGitHubAppRegistrations(context.Context) ([]domain.GitHubAppRegistration, error) {
	return nil, nil
}

func (f *fakeSCMAdminServiceForHandler) GetGitHubAppRegistration(context.Context, string) (domain.GitHubAppRegistration, error) {
	return domain.GitHubAppRegistration{}, nil
}

func (f *fakeSCMAdminServiceForHandler) CreateGitHubAppRegistration(context.Context, service.CreateGitHubAppRegistrationInput) (domain.GitHubAppRegistration, error) {
	return domain.GitHubAppRegistration{}, nil
}

func (f *fakeSCMAdminServiceForHandler) CreateGitHubAppInstallationConnection(context.Context, service.CreateGitHubAppInstallationConnectionInput) (domain.SCMConnectionDetail, error) {
	return domain.SCMConnectionDetail{}, nil
}

func (f *fakeSCMAdminServiceForHandler) SetConnectionEnabled(context.Context, string, *bool) (domain.SCMConnectionDetail, error) {
	return domain.SCMConnectionDetail{}, nil
}

func (f *fakeSCMAdminServiceForHandler) TestConnection(context.Context, string) (domain.SCMConnectionDetail, error) {
	if f.testConnectionErr != nil {
		return domain.SCMConnectionDetail{}, f.testConnectionErr
	}
	return f.testConnection, nil
}

func (f *fakeSCMAdminServiceForHandler) ListRegisteredRepositories(context.Context) ([]domain.SCMRepositoryRegistration, error) {
	return nil, nil
}

func (f *fakeSCMAdminServiceForHandler) GetRegisteredRepository(context.Context, string) (domain.SCMRepositoryRegistration, error) {
	return domain.SCMRepositoryRegistration{}, nil
}

func (f *fakeSCMAdminServiceForHandler) CreateRegisteredRepository(_ context.Context, input service.CreateSCMRepositoryRegistrationInput) (domain.SCMRepositoryRegistration, error) {
	f.createRegisteredRepositoryInput = input
	if f.createRegisteredRepositoryErr != nil {
		return domain.SCMRepositoryRegistration{}, f.createRegisteredRepositoryErr
	}
	return f.createRegisteredRepository, nil
}

func (f *fakeSCMAdminServiceForHandler) RefreshRegisteredRepository(_ context.Context, id string) (domain.SCMRepositoryRegistration, error) {
	f.refreshRegisteredRepositoryID = id
	if f.refreshRegisteredRepositoryErr != nil {
		return domain.SCMRepositoryRegistration{}, f.refreshRegisteredRepositoryErr
	}
	return f.refreshRegisteredRepository, nil
}

func handlerTestStringPtr(value string) *string {
	return &value
}
