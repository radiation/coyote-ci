package mcp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type principalContextKey struct{}

type apiTokenAuthenticationFailure string

const (
	apiTokenAuthenticationFailureInvalidFormat apiTokenAuthenticationFailure = "invalid_token_format"
	apiTokenAuthenticationFailureTokenNotFound apiTokenAuthenticationFailure = "token_not_found"
	apiTokenAuthenticationFailureTokenRevoked  apiTokenAuthenticationFailure = "token_revoked"
	apiTokenAuthenticationFailureTokenExpired  apiTokenAuthenticationFailure = "token_expired"
	apiTokenAuthenticationFailureRepository    apiTokenAuthenticationFailure = "token_repository_failure"
	apiTokenAuthenticationFailureOwner         apiTokenAuthenticationFailure = "token_owner_resolution_failure"
	apiTokenAuthenticationFailureUnexpected    apiTokenAuthenticationFailure = "unexpected_internal_failure"
)

func withPATAuthentication(tokens *service.APITokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				log.Printf("MCP API token authentication failed: reason=missing_or_malformed_bearer")
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			authenticated, err := tokens.AuthenticateAPIToken(r.Context(), token)
			if err != nil {
				failure := classifyAPITokenAuthenticationFailure(err)
				log.Printf("MCP API token authentication failed: reason=%s", failure)
				switch failure {
				case apiTokenAuthenticationFailureInvalidFormat,
					apiTokenAuthenticationFailureTokenNotFound,
					apiTokenAuthenticationFailureTokenRevoked,
					apiTokenAuthenticationFailureTokenExpired:
					http.Error(w, "invalid api token", http.StatusUnauthorized)
				case apiTokenAuthenticationFailureRepository,
					apiTokenAuthenticationFailureOwner:
					http.Error(w, "authentication service unavailable", http.StatusServiceUnavailable)
				default:
					http.Error(w, "authentication failed", http.StatusInternalServerError)
				}
				return
			}

			principal := authorization.Principal{
				User: authenticated.User,
				APIToken: &authorization.APITokenIdentity{
					ID:     authenticated.Token.ID,
					Scopes: domain.CloneAPITokenScopes(authenticated.Token.Scopes),
				},
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
		})
	}
}

func classifyAPITokenAuthenticationFailure(err error) apiTokenAuthenticationFailure {
	switch {
	case errors.Is(err, service.ErrAPITokenInvalidFormat):
		return apiTokenAuthenticationFailureInvalidFormat
	case errors.Is(err, service.ErrAPITokenNotFound):
		return apiTokenAuthenticationFailureTokenNotFound
	case errors.Is(err, service.ErrAPITokenRevoked):
		return apiTokenAuthenticationFailureTokenRevoked
	case errors.Is(err, service.ErrAPITokenExpired):
		return apiTokenAuthenticationFailureTokenExpired
	case errors.Is(err, service.ErrAPITokenRepositoryFailure):
		return apiTokenAuthenticationFailureRepository
	case errors.Is(err, service.ErrAPITokenOwnerResolutionFailure):
		return apiTokenAuthenticationFailureOwner
	default:
		return apiTokenAuthenticationFailureUnexpected
	}
}

func withDisabledAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, authorization.Principal{})))
	})
}

func currentPrincipal(ctx context.Context) (authorization.Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(authorization.Principal)
	return principal, ok
}

func bearerToken(r *http.Request) (string, bool) {
	authorizationHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorizationHeader == "" {
		return "", false
	}
	parts := strings.Fields(authorizationHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], service.APITokenPrefix) {
		return "", false
	}
	return parts[1], true
}
