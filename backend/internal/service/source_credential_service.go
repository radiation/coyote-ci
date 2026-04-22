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

var ErrSourceCredentialNameRequired = errors.New("name is required")
var ErrSourceCredentialSecretRefRequired = errors.New("secret_ref is required")
var ErrSourceCredentialKindInvalid = errors.New("credential kind must be one of https_token, ssh_key")

type SourceCredentialService struct {
	credentials repository.SourceCredentialRepository
	now         func() time.Time
}

func NewSourceCredentialService(credentials repository.SourceCredentialRepository) *SourceCredentialService {
	return &SourceCredentialService{
		credentials: credentials,
		now:         time.Now,
	}
}

type CreateSourceCredentialInput struct {
	Name      string
	Kind      string
	Username  *string
	SecretRef string
}

type UpdateSourceCredentialInput struct {
	Name      *string
	Kind      *string
	Username  OptionalStringPatch
	SecretRef *string
}

// OptionalStringPatch models a tri-state string patch at service boundaries.
// - Set=false: field omitted (no update)
// - Set=true, Value=nil: clear field
// - Set=true, Value!=nil: set field value
type OptionalStringPatch struct {
	Set   bool
	Value *string
}

func (s *SourceCredentialService) CreateSourceCredential(ctx context.Context, input CreateSourceCredentialInput) (domain.SourceCredential, error) {
	kind, err := normalizeCredentialKind(input.Kind)
	if err != nil {
		return domain.SourceCredential{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.SourceCredential{}, ErrSourceCredentialNameRequired
	}
	secretRef := strings.TrimSpace(input.SecretRef)
	if secretRef == "" {
		return domain.SourceCredential{}, ErrSourceCredentialSecretRefRequired
	}

	now := s.now().UTC()
	return s.credentials.Create(ctx, domain.SourceCredential{
		ID:        uuid.NewString(),
		Name:      name,
		Kind:      kind,
		Username:  normalizeStringPtr(input.Username),
		SecretRef: secretRef,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *SourceCredentialService) ListSourceCredentials(ctx context.Context) ([]domain.SourceCredential, error) {
	return s.credentials.List(ctx)
}

func (s *SourceCredentialService) GetSourceCredential(ctx context.Context, id string) (domain.SourceCredential, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return domain.SourceCredential{}, repository.ErrSourceCredentialNotFound
	}
	return s.credentials.GetByID(ctx, trimmed)
}

func (s *SourceCredentialService) UpdateSourceCredential(ctx context.Context, id string, input UpdateSourceCredentialInput) (domain.SourceCredential, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return domain.SourceCredential{}, repository.ErrSourceCredentialNotFound
	}

	current, err := s.credentials.GetByID(ctx, trimmedID)
	if err != nil {
		return domain.SourceCredential{}, err
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return domain.SourceCredential{}, ErrSourceCredentialNameRequired
		}
		current.Name = name
	}
	if input.Kind != nil {
		kind, kindErr := normalizeCredentialKind(*input.Kind)
		if kindErr != nil {
			return domain.SourceCredential{}, kindErr
		}
		current.Kind = kind
	}
	if input.Username.Set {
		current.Username = normalizeStringPtr(input.Username.Value)
	}
	if input.SecretRef != nil {
		secretRef := strings.TrimSpace(*input.SecretRef)
		if secretRef == "" {
			return domain.SourceCredential{}, ErrSourceCredentialSecretRefRequired
		}
		current.SecretRef = secretRef
	}

	current.UpdatedAt = s.now().UTC()
	return s.credentials.Update(ctx, current)
}

func (s *SourceCredentialService) DeleteSourceCredential(ctx context.Context, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return repository.ErrSourceCredentialNotFound
	}
	return s.credentials.Delete(ctx, trimmed)
}

func normalizeCredentialKind(value string) (domain.SourceCredentialKind, error) {
	trimmed := strings.TrimSpace(value)
	switch domain.SourceCredentialKind(trimmed) {
	case domain.SourceCredentialKindHTTPSToken, domain.SourceCredentialKindSSHKey:
		return domain.SourceCredentialKind(trimmed), nil
	default:
		return "", ErrSourceCredentialKindInvalid
	}
}

func normalizeStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
