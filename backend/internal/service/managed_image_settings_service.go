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

var ErrManagedImageSettingsProjectIDRequired = errors.New("project_id is required")
var ErrManagedImageSettingsNameRequired = errors.New("name is required")
var ErrManagedImageSettingsRepositoryURLRequired = errors.New("repository_url is required")
var ErrManagedImageSettingsPipelinePathRequired = errors.New("pipeline_path is required")
var ErrManagedImageSettingsManagedImageNameRequired = errors.New("managed_image_name is required")
var ErrManagedImageSettingsWriteCredentialIDRequired = errors.New("write_credential_id is required")
var ErrManagedImageSettingsSecretRefRequired = errors.New("secret_ref is required")
var ErrManagedImageSettingsCredentialKindInvalid = errors.New("credential kind must be one of https_token, ssh_key")

// ManagedImageSettingsService owns CRUD operations for source credentials and
// repository write-back configuration used by managed image refresh workflows.
type ManagedImageSettingsService struct {
	credentials repository.SourceCredentialRepository
	writebacks  repository.RepoWritebackConfigRepository
	now         func() time.Time
}

func NewManagedImageSettingsService(credentials repository.SourceCredentialRepository, writebacks repository.RepoWritebackConfigRepository) *ManagedImageSettingsService {
	return &ManagedImageSettingsService{
		credentials: credentials,
		writebacks:  writebacks,
		now:         time.Now,
	}
}

type CreateSourceCredentialInput struct {
	ProjectID string
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

type CreateRepoWritebackConfigInput struct {
	ProjectID         string
	RepositoryURL     string
	PipelinePath      string
	ManagedImageName  string
	WriteCredentialID string
	BotBranchPrefix   *string
	CommitAuthorName  *string
	CommitAuthorEmail *string
	Enabled           *bool
}

type UpdateRepoWritebackConfigInput struct {
	RepositoryURL     *string
	PipelinePath      *string
	ManagedImageName  *string
	WriteCredentialID *string
	BotBranchPrefix   *string
	CommitAuthorName  *string
	CommitAuthorEmail *string
	Enabled           *bool
}

func (s *ManagedImageSettingsService) CreateSourceCredential(ctx context.Context, input CreateSourceCredentialInput) (domain.SourceCredential, error) {
	kind, err := normalizeCredentialKind(input.Kind)
	if err != nil {
		return domain.SourceCredential{}, err
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return domain.SourceCredential{}, ErrManagedImageSettingsProjectIDRequired
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.SourceCredential{}, ErrManagedImageSettingsNameRequired
	}
	secretRef := strings.TrimSpace(input.SecretRef)
	if secretRef == "" {
		return domain.SourceCredential{}, ErrManagedImageSettingsSecretRefRequired
	}

	username := normalizeStringPtr(input.Username)
	now := s.now().UTC()

	return s.credentials.Create(ctx, domain.SourceCredential{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		Name:      name,
		Kind:      kind,
		Username:  username,
		SecretRef: secretRef,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *ManagedImageSettingsService) ListSourceCredentials(ctx context.Context, projectID string) ([]domain.SourceCredential, error) {
	trimmed := strings.TrimSpace(projectID)
	if trimmed == "" {
		return nil, ErrManagedImageSettingsProjectIDRequired
	}
	return s.credentials.ListByProjectID(ctx, trimmed)
}

func (s *ManagedImageSettingsService) GetSourceCredential(ctx context.Context, id string) (domain.SourceCredential, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return domain.SourceCredential{}, repository.ErrSourceCredentialNotFound
	}
	return s.credentials.GetByID(ctx, trimmed)
}

func (s *ManagedImageSettingsService) UpdateSourceCredential(ctx context.Context, id string, input UpdateSourceCredentialInput) (domain.SourceCredential, error) {
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
			return domain.SourceCredential{}, ErrManagedImageSettingsNameRequired
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
			return domain.SourceCredential{}, ErrManagedImageSettingsSecretRefRequired
		}
		current.SecretRef = secretRef
	}

	current.UpdatedAt = s.now().UTC()
	return s.credentials.Update(ctx, current)
}

func (s *ManagedImageSettingsService) DeleteSourceCredential(ctx context.Context, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return repository.ErrSourceCredentialNotFound
	}
	return s.credentials.Delete(ctx, trimmed)
}

func (s *ManagedImageSettingsService) CreateRepoWritebackConfig(ctx context.Context, input CreateRepoWritebackConfigInput) (domain.RepoWritebackConfig, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return domain.RepoWritebackConfig{}, ErrManagedImageSettingsProjectIDRequired
	}
	repositoryURL := strings.TrimSpace(input.RepositoryURL)
	if repositoryURL == "" {
		return domain.RepoWritebackConfig{}, ErrManagedImageSettingsRepositoryURLRequired
	}
	pipelinePath := strings.TrimSpace(input.PipelinePath)
	if pipelinePath == "" {
		return domain.RepoWritebackConfig{}, ErrManagedImageSettingsPipelinePathRequired
	}
	managedImageName := strings.TrimSpace(input.ManagedImageName)
	if managedImageName == "" {
		return domain.RepoWritebackConfig{}, ErrManagedImageSettingsManagedImageNameRequired
	}
	writeCredentialID := strings.TrimSpace(input.WriteCredentialID)
	if writeCredentialID == "" {
		return domain.RepoWritebackConfig{}, ErrManagedImageSettingsWriteCredentialIDRequired
	}

	if _, err := s.credentials.GetByID(ctx, writeCredentialID); err != nil {
		return domain.RepoWritebackConfig{}, err
	}

	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	now := s.now().UTC()
	cfg := domain.RepoWritebackConfig{
		ID:                uuid.NewString(),
		ProjectID:         projectID,
		RepositoryURL:     repositoryURL,
		PipelinePath:      pipelinePath,
		ManagedImageName:  managedImageName,
		WriteCredentialID: writeCredentialID,
		BotBranchPrefix:   defaultStringValue(input.BotBranchPrefix, "coyote/managed-image-refresh"),
		CommitAuthorName:  defaultStringValue(input.CommitAuthorName, "Coyote CI Bot"),
		CommitAuthorEmail: defaultStringValue(input.CommitAuthorEmail, "bot@coyote-ci.local"),
		Enabled:           enabled,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return s.writebacks.Create(ctx, cfg)
}

func (s *ManagedImageSettingsService) ListRepoWritebackConfigs(ctx context.Context, projectID string) ([]domain.RepoWritebackConfig, error) {
	trimmed := strings.TrimSpace(projectID)
	if trimmed == "" {
		return nil, ErrManagedImageSettingsProjectIDRequired
	}
	return s.writebacks.ListByProjectID(ctx, trimmed)
}

func (s *ManagedImageSettingsService) GetRepoWritebackConfig(ctx context.Context, id string) (domain.RepoWritebackConfig, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
	}
	return s.writebacks.GetByID(ctx, trimmed)
}

func (s *ManagedImageSettingsService) UpdateRepoWritebackConfig(ctx context.Context, id string, input UpdateRepoWritebackConfigInput) (domain.RepoWritebackConfig, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return domain.RepoWritebackConfig{}, repository.ErrRepoWritebackConfigNotFound
	}

	current, err := s.writebacks.GetByID(ctx, trimmedID)
	if err != nil {
		return domain.RepoWritebackConfig{}, err
	}

	if input.RepositoryURL != nil {
		repositoryURL := strings.TrimSpace(*input.RepositoryURL)
		if repositoryURL == "" {
			return domain.RepoWritebackConfig{}, ErrManagedImageSettingsRepositoryURLRequired
		}
		current.RepositoryURL = repositoryURL
	}
	if input.PipelinePath != nil {
		pipelinePath := strings.TrimSpace(*input.PipelinePath)
		if pipelinePath == "" {
			return domain.RepoWritebackConfig{}, ErrManagedImageSettingsPipelinePathRequired
		}
		current.PipelinePath = pipelinePath
	}
	if input.ManagedImageName != nil {
		managedImageName := strings.TrimSpace(*input.ManagedImageName)
		if managedImageName == "" {
			return domain.RepoWritebackConfig{}, ErrManagedImageSettingsManagedImageNameRequired
		}
		current.ManagedImageName = managedImageName
	}
	if input.WriteCredentialID != nil {
		writeCredentialID := strings.TrimSpace(*input.WriteCredentialID)
		if writeCredentialID == "" {
			return domain.RepoWritebackConfig{}, ErrManagedImageSettingsWriteCredentialIDRequired
		}
		if _, credErr := s.credentials.GetByID(ctx, writeCredentialID); credErr != nil {
			return domain.RepoWritebackConfig{}, credErr
		}
		current.WriteCredentialID = writeCredentialID
	}
	if input.BotBranchPrefix != nil {
		current.BotBranchPrefix = strings.TrimSpace(*input.BotBranchPrefix)
	}
	if input.CommitAuthorName != nil {
		current.CommitAuthorName = strings.TrimSpace(*input.CommitAuthorName)
	}
	if input.CommitAuthorEmail != nil {
		current.CommitAuthorEmail = strings.TrimSpace(*input.CommitAuthorEmail)
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}

	current.UpdatedAt = s.now().UTC()
	return s.writebacks.Update(ctx, current)
}

func (s *ManagedImageSettingsService) DeleteRepoWritebackConfig(ctx context.Context, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return repository.ErrRepoWritebackConfigNotFound
	}
	return s.writebacks.Delete(ctx, trimmed)
}

func normalizeCredentialKind(value string) (domain.SourceCredentialKind, error) {
	trimmed := strings.TrimSpace(value)
	switch domain.SourceCredentialKind(trimmed) {
	case domain.SourceCredentialKindHTTPSToken, domain.SourceCredentialKindSSHKey:
		return domain.SourceCredentialKind(trimmed), nil
	default:
		return "", ErrManagedImageSettingsCredentialKindInvalid
	}
}

func defaultStringValue(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func normalizeStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	v := trimmed
	return &v
}
