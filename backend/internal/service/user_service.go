package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrUserEmailRequired = errors.New("email is required")
var ErrUserGlobalRoleInvalid = errors.New("global_role must be one of admin, user")
var ErrUserNotPreauthorized = errors.New("user is not preauthorized")

type UserService struct {
	users repository.UserRepository
	now   func() time.Time
}

func NewUserService(users repository.UserRepository) *UserService {
	return &UserService{users: users, now: time.Now}
}

type CreateUserInput struct {
	Email       string
	DisplayName *string
	GlobalRole  string
}

type UpdateUserInput struct {
	Email       *string
	DisplayName OptionalStringPatch
	GlobalRole  *string
}

func (s *UserService) CreateUser(ctx context.Context, input CreateUserInput) (domain.User, error) {
	email := NormalizeEmail(input.Email)
	if email == "" {
		return domain.User{}, ErrUserEmailRequired
	}
	role, err := normalizeGlobalRole(input.GlobalRole)
	if err != nil {
		return domain.User{}, err
	}

	now := s.now().UTC()
	return s.users.Create(ctx, domain.User{
		ID:          uuid.NewString(),
		Email:       email,
		DisplayName: normalizeStringPtr(input.DisplayName),
		GlobalRole:  role,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

func (s *UserService) ListUsers(ctx context.Context) ([]domain.User, error) {
	return s.users.List(ctx)
}

func (s *UserService) GetUser(ctx context.Context, id string) (domain.User, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return domain.User{}, repository.ErrUserNotFound
	}
	return s.users.GetByID(ctx, trimmed)
}

func (s *UserService) UpdateUser(ctx context.Context, id string, input UpdateUserInput) (domain.User, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return domain.User{}, repository.ErrUserNotFound
	}

	current, err := s.users.GetByID(ctx, trimmedID)
	if err != nil {
		return domain.User{}, err
	}
	if input.Email != nil {
		email := NormalizeEmail(*input.Email)
		if email == "" {
			return domain.User{}, ErrUserEmailRequired
		}
		current.Email = email
	}
	if input.DisplayName.Set {
		current.DisplayName = normalizeStringPtr(input.DisplayName.Value)
	}
	if input.GlobalRole != nil {
		role, roleErr := normalizeGlobalRole(*input.GlobalRole)
		if roleErr != nil {
			return domain.User{}, roleErr
		}
		current.GlobalRole = role
	}

	current.UpdatedAt = s.now().UTC()
	return s.users.Update(ctx, current)
}

func (s *UserService) DeleteUser(ctx context.Context, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return repository.ErrUserNotFound
	}
	return s.users.Delete(ctx, trimmed)
}

func (s *UserService) ResolveHeaderUser(ctx context.Context, email string, displayName *string) (domain.User, error) {
	return s.resolveExternalUser(ctx, email, displayName, false)
}

func (s *UserService) ResolveOIDCUser(ctx context.Context, email string, displayName *string) (domain.User, error) {
	return s.resolveExternalUser(ctx, email, displayName, true)
}

func (s *UserService) BootstrapAdmins(ctx context.Context, emails map[string]struct{}) error {
	for email := range emails {
		normalizedEmail := NormalizeEmail(email)
		if normalizedEmail == "" {
			continue
		}

		user, err := s.users.GetByEmail(ctx, normalizedEmail)
		if errors.Is(err, repository.ErrUserNotFound) {
			_, createErr := s.CreateUser(ctx, CreateUserInput{
				Email:      normalizedEmail,
				GlobalRole: string(domain.GlobalRoleAdmin),
			})
			if createErr == nil {
				continue
			}
			if !errors.Is(createErr, repository.ErrUserEmailConflict) {
				return createErr
			}
			user, err = s.users.GetByEmail(ctx, normalizedEmail)
		}
		if err != nil {
			return err
		}
		if user.GlobalRole == domain.GlobalRoleAdmin {
			continue
		}

		user.GlobalRole = domain.GlobalRoleAdmin
		user.UpdatedAt = s.now().UTC()
		if _, updateErr := s.users.Update(ctx, user); updateErr != nil {
			return updateErr
		}
	}
	return nil
}

func (s *UserService) resolveExternalUser(ctx context.Context, email string, displayName *string, updateDisplayName bool) (domain.User, error) {
	normalizedEmail := NormalizeEmail(email)
	if normalizedEmail == "" {
		return domain.User{}, ErrUserEmailRequired
	}

	user, err := s.users.GetByEmail(ctx, normalizedEmail)
	if errors.Is(err, repository.ErrUserNotFound) {
		return domain.User{}, ErrUserNotPreauthorized
	}
	if err != nil {
		return domain.User{}, err
	}

	if updateDisplayName && displayName != nil {
		normalizedName := normalizeStringPtr(displayName)
		if normalizedName != nil && (user.DisplayName == nil || *user.DisplayName != *normalizedName) {
			user.DisplayName = normalizedName
			user.UpdatedAt = s.now().UTC()
			return s.users.Update(ctx, user)
		}
	}
	return user, nil
}

func NormalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeGlobalRole(value string) (domain.GlobalRole, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return domain.GlobalRoleUser, nil
	}
	switch domain.GlobalRole(trimmed) {
	case domain.GlobalRoleAdmin, domain.GlobalRoleUser:
		return domain.GlobalRole(trimmed), nil
	default:
		return "", ErrUserGlobalRoleInvalid
	}
}
