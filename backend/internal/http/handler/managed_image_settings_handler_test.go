package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestManagedImageSettingsHandler_SourceCredentialCRUD(t *testing.T) {
	credRepo := repositorymemory.NewSourceCredentialRepository()
	cfgRepo := repositorymemory.NewRepoWritebackConfigRepository()
	h := NewManagedImageSettingsHandler(service.NewManagedImageSettingsService(credRepo, cfgRepo))

	createReq := httptest.NewRequest(http.MethodPost, "/source-credentials", bytes.NewBufferString(`{"project_id":"proj-1","name":"gh-token","kind":"https_token","username":"x-access-token","secret_ref":"COYOTE_TOKEN"}`))
	createRes := httptest.NewRecorder()
	h.CreateSourceCredential(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d", http.StatusCreated, createRes.Code)
	}
	created := decodeDataMap(t, createRes)
	credentialID, ok := created["id"].(string)
	if !ok || credentialID == "" {
		t.Fatalf("expected credential id, got %v", created["id"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/source-credentials?project_id=proj-1", nil)
	listRes := httptest.NewRecorder()
	h.ListSourceCredentials(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d", http.StatusOK, listRes.Code)
	}
	listData := decodeDataMap(t, listRes)
	credentials, ok := listData["credentials"].([]any)
	if !ok || len(credentials) != 1 {
		t.Fatalf("expected one credential, got %v", listData["credentials"])
	}

	updateReq := addURLParam(httptest.NewRequest(http.MethodPut, "/source-credentials/"+credentialID, bytes.NewBufferString(`{"name":"github-token"}`)), "credentialID", credentialID)
	updateRes := httptest.NewRecorder()
	h.UpdateSourceCredential(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("expected update status %d, got %d", http.StatusOK, updateRes.Code)
	}
	updated := decodeDataMap(t, updateRes)
	if updated["name"] != "github-token" {
		t.Fatalf("expected updated name, got %v", updated["name"])
	}

	deleteReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/source-credentials/"+credentialID, nil), "credentialID", credentialID)
	deleteRes := httptest.NewRecorder()
	h.DeleteSourceCredential(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("expected delete status %d, got %d", http.StatusNoContent, deleteRes.Code)
	}
}

func TestManagedImageSettingsHandler_RepoWritebackConfigCRUD(t *testing.T) {
	credRepo := repositorymemory.NewSourceCredentialRepository()
	cfgRepo := repositorymemory.NewRepoWritebackConfigRepository()
	h := NewManagedImageSettingsHandler(service.NewManagedImageSettingsService(credRepo, cfgRepo))

	createCredReq := httptest.NewRequest(http.MethodPost, "/source-credentials", bytes.NewBufferString(`{"project_id":"proj-1","name":"gh-token","kind":"https_token","secret_ref":"COYOTE_TOKEN"}`))
	createCredRes := httptest.NewRecorder()
	h.CreateSourceCredential(createCredRes, createCredReq)
	if createCredRes.Code != http.StatusCreated {
		t.Fatalf("expected credential create status %d, got %d", http.StatusCreated, createCredRes.Code)
	}
	credData := decodeDataMap(t, createCredRes)
	credentialID, ok := credData["id"].(string)
	if !ok || credentialID == "" {
		t.Fatalf("expected credential id, got %v", credData["id"])
	}

	createCfgReq := httptest.NewRequest(http.MethodPost, "/repo-writeback-configs", bytes.NewBufferString(`{"project_id":"proj-1","repository_url":"https://github.com/example/repo.git","pipeline_path":".coyote/pipeline.yml","managed_image_name":"go","write_credential_id":"`+credentialID+`","enabled":true}`))
	createCfgRes := httptest.NewRecorder()
	h.CreateRepoWritebackConfig(createCfgRes, createCfgReq)
	if createCfgRes.Code != http.StatusCreated {
		t.Fatalf("expected config create status %d, got %d", http.StatusCreated, createCfgRes.Code)
	}
	cfgData := decodeDataMap(t, createCfgRes)
	configID, ok := cfgData["id"].(string)
	if !ok || configID == "" {
		t.Fatalf("expected config id, got %v", cfgData["id"])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/repo-writeback-configs?project_id=proj-1", nil)
	listRes := httptest.NewRecorder()
	h.ListRepoWritebackConfigs(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected config list status %d, got %d", http.StatusOK, listRes.Code)
	}
	listData := decodeDataMap(t, listRes)
	configs, ok := listData["configs"].([]any)
	if !ok || len(configs) != 1 {
		t.Fatalf("expected one config, got %v", listData["configs"])
	}

	updateReq := addURLParam(httptest.NewRequest(http.MethodPut, "/repo-writeback-configs/"+configID, bytes.NewBufferString(`{"enabled":false}`)), "configID", configID)
	updateRes := httptest.NewRecorder()
	h.UpdateRepoWritebackConfig(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("expected config update status %d, got %d", http.StatusOK, updateRes.Code)
	}
	updated := decodeDataMap(t, updateRes)
	if updated["enabled"] != false {
		t.Fatalf("expected enabled false, got %v", updated["enabled"])
	}

	deleteReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/repo-writeback-configs/"+configID, nil), "configID", configID)
	deleteRes := httptest.NewRecorder()
	h.DeleteRepoWritebackConfig(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("expected config delete status %d, got %d", http.StatusNoContent, deleteRes.Code)
	}
}
