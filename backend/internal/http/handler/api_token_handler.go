package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type APITokenHandler struct {
	tokens *service.APITokenService
}

func NewAPITokenHandler(tokens *service.APITokenService) *APITokenHandler {
	return &APITokenHandler{tokens: tokens}
}

// ListMyTokens godoc
// @Summary List my API tokens
// @Description Lists API token metadata for the authenticated user.
// @Tags users
// @Produce json
// @Success 200 {object} api.APITokenListEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/tokens [get]
func (h *APITokenHandler) ListMyTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if rejectAPITokenManagement(w, r) {
		return
	}
	tokens, err := h.tokens.ListAPITokens(r.Context(), user.ID)
	if err != nil {
		h.writeAPITokenError(w, err)
		return
	}
	responses := make([]api.APITokenResponse, 0, len(tokens))
	for _, token := range tokens {
		responses = append(responses, toAPITokenResponse(token))
	}
	writeDataJSON(w, http.StatusOK, api.APITokenListResponse{Tokens: responses})
}

// CreateMyToken godoc
// @Summary Create my API token
// @Description Creates a user-owned API token and returns the raw token only once.
// @Tags users
// @Accept json
// @Produce json
// @Param request body api.CreateAPITokenRequest true "API token create request"
// @Success 201 {object} api.APITokenEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/tokens [post]
func (h *APITokenHandler) CreateMyToken(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if rejectAPITokenManagement(w, r) {
		return
	}
	var req api.CreateAPITokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	expiresAt, parseErr := parseOptionalAPITokenTime(req.ExpiresAt)
	if parseErr != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", parseErr.Error())
		return
	}
	created, err := h.tokens.CreateAPIToken(r.Context(), service.CreateAPITokenInput{
		UserID:    user.ID,
		Name:      req.Name,
		ExpiresAt: expiresAt,
		Scopes:    req.Scopes,
	})
	if err != nil {
		h.writeAPITokenError(w, err)
		return
	}
	response := api.CreatedAPITokenResponse{APITokenResponse: toAPITokenResponse(created.Token), Token: created.PlaintextToken}
	writeDataJSON(w, http.StatusCreated, response)
}

// RevokeMyToken godoc
// @Summary Revoke my API token
// @Description Revokes one API token owned by the authenticated user.
// @Tags users
// @Param token_id path string true "API token ID"
// @Success 204
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /me/tokens/{token_id} [delete]
func (h *APITokenHandler) RevokeMyToken(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.CurrentUser(r.Context())
	if !ok {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if rejectAPITokenManagement(w, r) {
		return
	}
	if err := h.tokens.RevokeAPIToken(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "token_id"))); err != nil {
		h.writeAPITokenError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func rejectAPITokenManagement(w http.ResponseWriter, r *http.Request) bool {
	method, ok := auth.CurrentAuthMethod(r.Context())
	if ok && method == auth.MethodAPIToken {
		writeErrorJSON(w, http.StatusForbidden, "forbidden", "api tokens cannot manage api tokens")
		return true
	}
	return false
}

func (h *APITokenHandler) writeAPITokenError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrAPITokenNotFound) || errors.Is(err, repository.ErrUserNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, service.ErrAPITokenNameRequired) || errors.Is(err, service.ErrAPITokenExpirationInvalid) || errors.Is(err, domain.ErrUnknownAPITokenScope) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func parseOptionalAPITokenTime(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*value))
	if err != nil {
		return nil, errors.New("expires_at must be an RFC3339 timestamp")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func toAPITokenResponse(token domain.APIToken) api.APITokenResponse {
	return api.APITokenResponse{
		ID:          token.ID,
		Name:        token.Name,
		Scopes:      toAPITokenScopeStrings(token.Scopes),
		TokenPrefix: token.TokenPrefix,
		ExpiresAt:   formatOptionalAPITokenTime(token.ExpiresAt),
		LastUsedAt:  formatOptionalAPITokenTime(token.LastUsedAt),
		CreatedAt:   formatUserTime(token.CreatedAt),
		RevokedAt:   formatOptionalAPITokenTime(token.RevokedAt),
	}
}

func toAPITokenScopeStrings(scopes []domain.APITokenScope) []string {
	if len(scopes) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, string(scope))
	}
	return out
}

func formatOptionalAPITokenTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
