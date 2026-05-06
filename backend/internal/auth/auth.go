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
)

type contextKey struct{}

type HeaderUserResolver interface {
	ResolveHeaderUser(ctx context.Context, email string, displayName *string, bootstrapAdmins map[string]struct{}) (domain.User, error)
}

type MiddlewareConfig struct {
	Mode                 Mode
	BootstrapAdminEmails map[string]struct{}
}

func ParseMode(value string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(value))) {
	case ModeHeader:
		return ModeHeader
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

func Middleware(cfg MiddlewareConfig, resolver HeaderUserResolver) func(http.Handler) http.Handler {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeDisabled
	}
	bootstrapAdmins := cfg.BootstrapAdminEmails
	if bootstrapAdmins == nil {
		bootstrapAdmins = map[string]struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mode == ModeDisabled {
				next.ServeHTTP(w, r)
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

			user, err := resolver.ResolveHeaderUser(r.Context(), email, displayName, bootstrapAdmins)
			if err != nil {
				writeUnauthorized(w, "unable to resolve user")
				return
			}

			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
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

func CanManageProjectMembers(ctx context.Context, lookup ProjectRoleLookup, mode Mode, user domain.User, projectID string) (bool, error) {
	if mode == ModeDisabled {
		return true, nil
	}
	if IsGlobalAdmin(user) {
		return true, nil
	}
	return HasProjectRole(ctx, lookup, user, projectID, domain.ProjectMemberRoleOwner)
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
