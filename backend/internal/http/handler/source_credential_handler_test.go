package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	repositorymemory "github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestSourceCredentialHandler_CRUD(t *testing.T) {
	credRepo := repositorymemory.NewSourceCredentialRepository()
	h := NewSourceCredentialHandler(service.NewSourceCredentialService(credRepo))

	createReq := httptest.NewRequest(http.MethodPost, "/source-credentials", bytes.NewBufferString(`{"name":"gh-token","kind":"https_token","username":"x-access-token","secret_ref":"COYOTE_TOKEN"}`))
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

	listReq := httptest.NewRequest(http.MethodGet, "/source-credentials", nil)
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
