package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository/memory"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestAPITokenHandler_HeaderAndOIDCMethodsCreateListAndRevokeMyTokens(t *testing.T) {
	tests := []struct {
		name   string
		method auth.Method
	}{
		{name: "header", method: auth.MethodHeader},
		{name: "oidc_session", method: auth.MethodOIDC},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runAPITokenManagementFlow(t, tc.method)
		})
	}
}

func runAPITokenManagementFlow(t *testing.T, method auth.Method) {
	t.Helper()
	ctx := context.Background()
	userRepo := memory.NewUserRepository()
	tokenRepo := memory.NewAPITokenRepository()
	userService := service.NewUserService(userRepo)
	user, err := userService.CreateUser(ctx, service.CreateUserInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	tokenService := service.NewAPITokenService(tokenRepo, userRepo)
	h := NewAPITokenHandler(tokenService)
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)

	createReq := httptest.NewRequest(http.MethodPost, "/api/me/tokens", bytes.NewBufferString(`{"name":"fixtures","expires_at":"`+expiresAt+`"}`))
	createReq = withUserAndAuthMethod(createReq, user, method)
	createRes := httptest.NewRecorder()
	h.CreateMyToken(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d body=%s", http.StatusCreated, createRes.Code, createRes.Body.String())
	}
	var createBody struct {
		Data struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			TokenPrefix string `json:"token_prefix"`
			Token       string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createRes.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	if createBody.Data.ID == "" || createBody.Data.Token == "" || !strings.HasPrefix(createBody.Data.Token, service.APITokenPrefix) {
		t.Fatalf("unexpected create token response: %+v", createBody.Data)
	}
	if strings.Contains(createRes.Body.String(), "token_hash") {
		t.Fatalf("create response exposed token_hash: %s", createRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/me/tokens", nil)
	listReq = withUserAndAuthMethod(listReq, user, method)
	listRes := httptest.NewRecorder()
	h.ListMyTokens(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d body=%s", http.StatusOK, listRes.Code, listRes.Body.String())
	}
	if strings.Contains(listRes.Body.String(), "token_hash") || strings.Contains(listRes.Body.String(), createBody.Data.Token) {
		t.Fatalf("list response exposed sensitive token data: %s", listRes.Body.String())
	}

	revokeReq := addURLParam(httptest.NewRequest(http.MethodDelete, "/api/me/tokens/"+createBody.Data.ID, nil), "token_id", createBody.Data.ID)
	revokeReq = withUserAndAuthMethod(revokeReq, user, method)
	revokeRes := httptest.NewRecorder()
	h.RevokeMyToken(revokeRes, revokeReq)
	if revokeRes.Code != http.StatusNoContent {
		t.Fatalf("expected revoke status %d, got %d body=%s", http.StatusNoContent, revokeRes.Code, revokeRes.Body.String())
	}
	if _, err := tokenService.AuthenticateAPIToken(context.Background(), createBody.Data.Token); err == nil {
		t.Fatalf("expected revoked token to be rejected")
	}
}

func withUserAndAuthMethod(req *http.Request, user domain.User, method auth.Method) *http.Request {
	ctx := auth.WithUser(req.Context(), user)
	ctx = auth.WithAuthMethod(ctx, method)
	return req.WithContext(ctx)
}

func TestAPITokenHandler_RequiresAuthenticatedUser(t *testing.T) {
	h := NewAPITokenHandler(service.NewAPITokenService(memory.NewAPITokenRepository(), memory.NewUserRepository()))
	res := httptest.NewRecorder()
	h.ListMyTokens(res, httptest.NewRequest(http.MethodGet, "/api/me/tokens", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, res.Code)
	}
}

func TestAPITokenHandler_RejectsInvalidExpiresAt(t *testing.T) {
	user := domain.User{ID: "user-1", Email: "dev@example.com", GlobalRole: domain.GlobalRoleUser}
	h := NewAPITokenHandler(service.NewAPITokenService(memory.NewAPITokenRepository(), memory.NewUserRepository()))
	req := httptest.NewRequest(http.MethodPost, "/api/me/tokens", bytes.NewBufferString(`{"name":"bad","expires_at":"soon"}`))
	req = req.WithContext(auth.WithUser(req.Context(), user))
	res := httptest.NewRecorder()
	h.CreateMyToken(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}
}
