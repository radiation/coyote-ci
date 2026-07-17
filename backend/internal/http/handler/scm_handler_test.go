package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestSCMHandler_CRUDFoundationRoutes(t *testing.T) {
	connectionRepo := repositorymemory.NewSCMConnectionRepository()
	repositoryRepo := repositorymemory.NewSCMRepositoryRegistrationRepository()
	h := NewSCMHandler(service.NewSCMAdminService(connectionRepo, repositoryRepo))

	createConnectionReq := httptest.NewRequest(http.MethodPost, "/settings/scm/connections/github-app-installations", bytes.NewBufferString(`{"display_name":"octo cloud","deployment_kind":"cloud","app_id":"12345","private_key_secret_ref":"secret/github/private-key","webhook_secret_ref":"secret/github/webhook","installation_id":"999","account_login":"octo","account_type":"organization","account_id":"42"}`))
	createConnectionRes := httptest.NewRecorder()
	h.CreateGitHubAppInstallationConnection(createConnectionRes, createConnectionReq)
	if createConnectionRes.Code != http.StatusCreated {
		t.Fatalf("expected create connection status %d, got %d body=%s", http.StatusCreated, createConnectionRes.Code, createConnectionRes.Body.String())
	}
	createdConnection := decodeDataMap(t, createConnectionRes)
	connectionID, ok := createdConnection["id"].(string)
	if !ok || connectionID == "" {
		t.Fatalf("expected connection id, got %v", createdConnection["id"])
	}
	githubApp, ok := createdConnection["github_app"].(map[string]any)
	if !ok || githubApp["private_key_secret_ref"] != "secret/github/private-key" {
		t.Fatalf("expected github app secret refs in response, got %v", createdConnection["github_app"])
	}
	if _, ok := githubApp["private_key"]; ok {
		t.Fatal("did not expect raw private key field in response")
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
	if enabled, ok := patchedConnection["enabled"].(bool); !ok || enabled {
		t.Fatalf("expected connection to be disabled, got %v", patchedConnection["enabled"])
	}

	createRepositoryReq := httptest.NewRequest(http.MethodPost, "/settings/scm/repositories", bytes.NewBufferString(`{"connection_id":"`+connectionID+`","provider_repository_id":"1001","owner":"octo","name":"widgets","full_name":"octo/widgets","clone_url":"https://github.com/octo/widgets.git","web_url":"https://github.com/octo/widgets","default_branch":"main"}`))
	createRepositoryRes := httptest.NewRecorder()
	h.CreateRegisteredRepository(createRepositoryRes, createRepositoryReq)
	if createRepositoryRes.Code != http.StatusCreated {
		t.Fatalf("expected create repository status %d, got %d body=%s", http.StatusCreated, createRepositoryRes.Code, createRepositoryRes.Body.String())
	}
	createdRepository := decodeDataMap(t, createRepositoryRes)
	repositoryID, ok := createdRepository["id"].(string)
	if !ok || repositoryID == "" {
		t.Fatalf("expected repository id, got %v", createdRepository["id"])
	}

	listRepositoriesReq := httptest.NewRequest(http.MethodGet, "/settings/scm/repositories", nil)
	listRepositoriesRes := httptest.NewRecorder()
	h.ListRegisteredRepositories(listRepositoriesRes, listRepositoriesReq)
	if listRepositoriesRes.Code != http.StatusOK {
		t.Fatalf("expected list repositories status %d, got %d", http.StatusOK, listRepositoriesRes.Code)
	}

	getRepositoryReq := addURLParam(httptest.NewRequest(http.MethodGet, "/settings/scm/repositories/"+repositoryID, nil), "repositoryID", repositoryID)
	getRepositoryRes := httptest.NewRecorder()
	h.GetRegisteredRepository(getRepositoryRes, getRepositoryReq)
	if getRepositoryRes.Code != http.StatusOK {
		t.Fatalf("expected get repository status %d, got %d", http.StatusOK, getRepositoryRes.Code)
	}
}
