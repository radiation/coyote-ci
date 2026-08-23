package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type Mode string

const (
	ModeDisabled Mode = "disabled"
	ModeHeader   Mode = "header"
	ModeOIDC     Mode = "oidc"
)

type contextKey struct{}
type authMethodContextKey struct{}
type apiTokenContextKey struct{}

type Method string

const (
	MethodHeader   Method = "header"
	MethodOIDC     Method = "oidc"
	MethodAPIToken Method = "api_token"
)

type UserResolver interface {
	ResolveHeaderUser(ctx context.Context, email string, displayName *string) (domain.User, error)
	GetUser(ctx context.Context, id string) (domain.User, error)
}

type MiddlewareConfig struct {
	Mode      Mode
	Sessions  SessionManager
	APITokens APITokenAuthenticator
}

type APITokenAuthenticator interface {
	AuthenticateAPIToken(ctx context.Context, plaintext string) (service.AuthenticatedAPIToken, error)
}

func ParseMode(value string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(value))) {
	case ModeHeader:
		return ModeHeader
	case ModeOIDC:
		return ModeOIDC
	default:
		return ModeDisabled
	}
}

func ParseBootstrapAdminEmails(value string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(value, ",") {
		email := service.NormalizeEmail(part)
		if email == "" {
			continue
		}
		out[email] = struct{}{}
	}
	return out
}

func Middleware(cfg MiddlewareConfig, resolver UserResolver) func(http.Handler) http.Handler {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeDisabled
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == ModeDisabled {
				next.ServeHTTP(w, r)
				return
			}

			if token, attempted := bearerToken(r); attempted {
				if token == "" {
					writeUnauthorized(w, "invalid api token")
					return
				}
				if cfg.APITokens == nil {
					writeUnauthorized(w, "api token auth is not configured")
					return
				}
				authenticated, err := cfg.APITokens.AuthenticateAPIToken(r.Context(), token)
				if err != nil {
					writeUnauthorized(w, "invalid api token")
					return
				}

				ctx := WithUser(r.Context(), authenticated.User)
				ctx = WithAuthMethod(ctx, MethodAPIToken)
				ctx = WithAuthenticatedAPIToken(ctx, authenticated.Token.ID, authenticated.Token.Scopes)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if mode == ModeOIDC {
				if cfg.Sessions == nil {
					writeUnauthorized(w, "session auth is not configured")
					return
				}

				userID, sessionErr := cfg.Sessions.UserID(r)
				if sessionErr != nil {
					writeUnauthorized(w, "authentication required")
					return
				}

				user, resolveErr := resolver.GetUser(r.Context(), userID)
				if resolveErr != nil {
					writeUnauthorized(w, "unable to resolve session user")
					return
				}

				ctx := WithUser(r.Context(), user)
				ctx = WithAuthMethod(ctx, MethodOIDC)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			email := strings.TrimSpace(r.Header.Get("X-Coyote-User-Email"))
			if email == "" {
				writeUnauthorized(w, "missing user email header")
				return
			}
			var displayName *string
			if name := strings.TrimSpace(r.Header.Get("X-Coyote-User-Name")); name != "" {
				displayName = &name
			}

			user, err := resolver.ResolveHeaderUser(r.Context(), email, displayName)
			if err != nil {
				writeUnauthorized(w, "unable to resolve user")
				return
			}

			ctx := WithUser(r.Context(), user)
			ctx = WithAuthMethod(ctx, MethodHeader)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func WithUser(ctx context.Context, user domain.User) context.Context {
	return context.WithValue(ctx, contextKey{}, user)
}

func CurrentUser(ctx context.Context) (domain.User, bool) {
	user, ok := ctx.Value(contextKey{}).(domain.User)
	return user, ok
}

func WithAuthMethod(ctx context.Context, method Method) context.Context {
	return context.WithValue(ctx, authMethodContextKey{}, method)
}

func CurrentAuthMethod(ctx context.Context) (Method, bool) {
	method, ok := ctx.Value(authMethodContextKey{}).(Method)
	return method, ok
}

type AuthenticatedAPIToken struct {
	ID     string
	Scopes []domain.APITokenScope
}

func WithAuthenticatedAPIToken(ctx context.Context, tokenID string, scopes []domain.APITokenScope) context.Context {
	return context.WithValue(ctx, apiTokenContextKey{}, AuthenticatedAPIToken{ID: tokenID, Scopes: domain.CloneAPITokenScopes(scopes)})
}

func CurrentAuthenticatedAPIToken(ctx context.Context) (AuthenticatedAPIToken, bool) {
	token, ok := ctx.Value(apiTokenContextKey{}).(AuthenticatedAPIToken)
	if !ok {
		return AuthenticatedAPIToken{}, false
	}
	token.Scopes = domain.CloneAPITokenScopes(token.Scopes)
	return token, true
}

func bearerToken(r *http.Request) (string, bool) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "" {
		return "", false
	}
	parts := strings.Fields(authorization)
	if len(parts) == 0 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	if len(parts) != 2 {
		return "", true
	}
	return parts[1], true
}

func DisabledModeUser() domain.User {
	displayName := "Local development"
	return domain.User{
		ID:          "disabled-mode-user",
		Email:       "dev@local.coyote-ci",
		DisplayName: &displayName,
		GlobalRole:  domain.GlobalRoleAdmin,
	}
}

func IsGlobalAdmin(user domain.User) bool {
	return user.GlobalRole == domain.GlobalRoleAdmin
}

func CanManageUsers(mode Mode, user domain.User) bool {
	if mode == ModeDisabled {
		return true
	}
	return IsGlobalAdmin(user)
}

type ProjectRoleLookup interface {
	GetProjectMembership(ctx context.Context, projectID string, userID string) (domain.ProjectMembership, error)
}

func HasProjectRole(ctx context.Context, lookup ProjectRoleLookup, user domain.User, projectID string, minimum domain.ProjectMemberRole) (bool, error) {
	membership, err := lookup.GetProjectMembership(ctx, projectID, user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrProjectMembershipNotFound) {
			return false, nil
		}
		return false, err
	}
	return projectRoleRank(membership.Role) >= projectRoleRank(minimum), nil
}

func CanViewProjectMembers(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	if mode == ModeDisabled {
		return true, nil
	}
	if IsGlobalAdmin(user) {
		return true, nil
	}
	return HasProjectRole(ctx, lookup, user, projectID, domain.ProjectMemberRoleViewer)
}

func CanManageProjectMembers(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	if mode == ModeDisabled {
		return true, nil
	}
	if IsGlobalAdmin(user) {
		return true, nil
	}
	return HasProjectRole(ctx, lookup, user, projectID, domain.ProjectMemberRoleOwner)
}

func CanReadProject(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	return canAccessProject(ctx, lookup, mode, user, projectID, domain.ProjectMemberRoleViewer)
}

func CanUpdateProject(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	return canAccessProject(ctx, lookup, mode, user, projectID, domain.ProjectMemberRoleOwner)
}

func CanManageProjectJobs(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	return canAccessProject(ctx, lookup, mode, user, projectID, domain.ProjectMemberRoleMaintainer)
}

func CanTriggerBuild(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	return canAccessProject(ctx, lookup, mode, user, projectID, domain.ProjectMemberRoleMaintainer)
}

func CanCancelBuild(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	return canAccessProject(ctx, lookup, mode, user, projectID, domain.ProjectMemberRoleMaintainer)
}

func CanReadProjectResources(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	return canAccessProject(ctx, lookup, mode, user, projectID, domain.ProjectMemberRoleViewer)
}

func CanDownloadArtifact(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	return canAccessProject(ctx, lookup, mode, user, projectID, domain.ProjectMemberRoleViewer)
}

func CanManageCredentials(mode Mode, user domain.User) bool {
	if mode == ModeDisabled {
		return true
	}
	return IsGlobalAdmin(user)
}

func canAccessProject(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string, minimum domain.ProjectMemberRole) (bool, error) {
	if mode == ModeDisabled {
		return true, nil
	}
	if IsGlobalAdmin(user) {
		return true, nil
	}
	return HasProjectRole(ctx, lookup, user, projectID, minimum)
}

func projectRoleRank(role domain.ProjectMemberRole) int {
	switch role {
	case domain.ProjectMemberRoleOwner:
		return 3
	case domain.ProjectMemberRoleMaintainer:
		return 2
	case domain.ProjectMemberRoleViewer:
		return 1
	default:
		return 0
	}
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(api.ErrorResponse{
		Error: api.ErrorBody{Code: "unauthorized", Message: message},
	})
}
