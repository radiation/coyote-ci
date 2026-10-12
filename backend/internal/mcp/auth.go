package mcp

import (
	"context"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/authorization"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type principalContextKey struct{}

func withPATAuthentication(tokens *service.APITokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			authenticated, err := tokens.AuthenticateAPIToken(r.Context(), token)
			if err != nil {
				http.Error(w, "invalid api token", http.StatusUnauthorized)
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
