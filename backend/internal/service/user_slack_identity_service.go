package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

const (
	SlackIdentityWorkspaceStatusNotConfigured = "not_configured"
	SlackIdentityWorkspaceStatusDisabled      = "disabled"
	SlackIdentityWorkspaceStatusReady         = "ready"

	SlackIdentityResolutionMethodAuthenticatedEmail = "authenticated_email"
)

var ErrUserSlackIdentityUserIDRequired = errors.New("authenticated user id is required")
var ErrUserSlackIdentityEmailRequired = errors.New("authenticated user email is required")
var ErrUserSlackIdentityWorkspaceNotConfigured = errors.New("slack is not connected for this Coyote instance")
var ErrUserSlackIdentityWorkspaceDisabled = errors.New("the connected Slack workspace is disabled")
var ErrUserSlackIdentityResolutionMethodInvalid = errors.New("resolution_method must be authenticated_email")
var ErrUserSlackIdentitySlackUserIDRequired = errors.New("slack_user_id is required")
var ErrUserSlackIdentityWorkspaceIntegrationIDRequired = errors.New("workspace_integration_id is required")
var ErrUserSlackIdentitySlackWorkspaceIDRequired = errors.New("slack_workspace_id is required")
var ErrUserSlackIdentityNoMatch = errors.New("no active Slack member matched your Coyote email")
var ErrUserSlackIdentityCandidateChanged = errors.New("the Slack account match changed before confirmation; resolve again")
var ErrUserSlackIdentityConflict = errors.New("that Slack account is already linked")
var ErrUserSlackIdentityEnabledRequired = errors.New("enabled is required")
var ErrUserSlackIdentityMemberUnavailable = errors.New("the matched Slack account is unavailable for personal linking")

type slackDirectoryClient interface {
	LookupUserByEmail(ctx context.Context, token string, email string) (platformslack.User, error)
}

type SlackWorkspaceReference struct {
	ID                string
	SlackWorkspaceID  string
	Name              *string
	LastTestSucceeded *bool
}

type UserSlackIdentityState struct {
	WorkspaceStatus string
	Workspace       *SlackWorkspaceReference
	Identity        *domain.UserSlackIdentity
}

type ResolvedUserSlackIdentityCandidate struct {
	ResolutionMethod string
	Workspace        SlackWorkspaceReference
	SlackUserID      string
	DisplayName      *string
	RealName         *string
	Handle           *string
	ProfileImageURL  *string
}

type LinkUserSlackIdentityInput struct {
	ResolutionMethod       string
	WorkspaceIntegrationID string
	SlackWorkspaceID       string
	SlackUserID            string
}

type UserSlackIdentityService struct {
	identities   repository.UserSlackIdentityRepository
	integrations repository.SlackWorkspaceIntegrationRepository
	client       slackDirectoryClient
	now          func() time.Time
}

func NewUserSlackIdentityService(identities repository.UserSlackIdentityRepository, integrations repository.SlackWorkspaceIntegrationRepository, client slackDirectoryClient) *UserSlackIdentityService {
	if client == nil {
		client = platformslack.NewClient(nil)
	}
	return &UserSlackIdentityService{
		identities:   identities,
		integrations: integrations,
		client:       client,
		now:          time.Now,
	}
}

func (s *UserSlackIdentityService) Get(ctx context.Context, user domain.User) (UserSlackIdentityState, error) {
	userID := strings.TrimSpace(user.ID)
	if userID == "" {
		return UserSlackIdentityState{}, ErrUserSlackIdentityUserIDRequired
	}

	state := UserSlackIdentityState{WorkspaceStatus: SlackIdentityWorkspaceStatusNotConfigured}
	integration, err := s.integrations.Get(ctx)
	if err != nil {
		if !errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
			return UserSlackIdentityState{}, err
		}
	} else {
		state.Workspace = &SlackWorkspaceReference{
			ID:                integration.ID,
			SlackWorkspaceID:  integration.WorkspaceID,
			Name:              cloneString(serviceOptionalString(integration.WorkspaceName)),
			LastTestSucceeded: cloneBool(integration.LastTestSucceeded),
		}
		state.WorkspaceStatus = slackIdentityWorkspaceStatus(integration)
	}

	identity, err := s.identities.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
			return UserSlackIdentityState{}, err
		}
		return state, nil
	}
	state.Identity = &identity
	return state, nil
}

func (s *UserSlackIdentityService) ResolveByAuthenticatedEmail(ctx context.Context, user domain.User) (*ResolvedUserSlackIdentityCandidate, bool, error) {
	userID := strings.TrimSpace(user.ID)
	if userID == "" {
		return nil, false, ErrUserSlackIdentityUserIDRequired
	}
	integration, err := s.requireReadyWorkspace(ctx)
	if err != nil {
		return nil, false, err
	}
	member, matched, err := s.lookupAuthenticatedEmailMember(ctx, integration, user.Email)
	if err != nil || !matched {
		return nil, matched, err
	}

	return &ResolvedUserSlackIdentityCandidate{
		ResolutionMethod: SlackIdentityResolutionMethodAuthenticatedEmail,
		Workspace: SlackWorkspaceReference{
			ID:                integration.ID,
			SlackWorkspaceID:  integration.WorkspaceID,
			Name:              cloneString(serviceOptionalString(integration.WorkspaceName)),
			LastTestSucceeded: cloneBool(integration.LastTestSucceeded),
		},
		SlackUserID:     member.ID,
		DisplayName:     cloneString(member.DisplayName),
		RealName:        cloneString(member.RealName),
		Handle:          cloneString(member.Handle),
		ProfileImageURL: cloneString(member.ProfileImageURL),
	}, true, nil
}

func (s *UserSlackIdentityService) Link(ctx context.Context, user domain.User, input LinkUserSlackIdentityInput) (domain.UserSlackIdentity, error) {
	userID := strings.TrimSpace(user.ID)
	if userID == "" {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentityUserIDRequired
	}
	if strings.TrimSpace(input.ResolutionMethod) != SlackIdentityResolutionMethodAuthenticatedEmail {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentityResolutionMethodInvalid
	}
	if strings.TrimSpace(input.WorkspaceIntegrationID) == "" {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentityWorkspaceIntegrationIDRequired
	}
	if strings.TrimSpace(input.SlackWorkspaceID) == "" {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentitySlackWorkspaceIDRequired
	}
	if strings.TrimSpace(input.SlackUserID) == "" {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentitySlackUserIDRequired
	}

	integration, err := s.requireReadyWorkspace(ctx)
	if err != nil {
		return domain.UserSlackIdentity{}, err
	}
	if integration.ID != strings.TrimSpace(input.WorkspaceIntegrationID) {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentityCandidateChanged
	}
	if integration.WorkspaceID != strings.TrimSpace(input.SlackWorkspaceID) {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentityCandidateChanged
	}

	member, matched, err := s.lookupAuthenticatedEmailMember(ctx, integration, user.Email)
	if err != nil {
		return domain.UserSlackIdentity{}, err
	}
	if !matched || member.ID != strings.TrimSpace(input.SlackUserID) {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentityCandidateChanged
	}

	now := s.now().UTC()
	identity := domain.UserSlackIdentity{
		ID:                          uuid.NewString(),
		UserID:                      userID,
		SlackWorkspaceIntegrationID: integration.ID,
		SlackUserID:                 member.ID,
		SlackDisplayName:            cloneString(member.DisplayName),
		SlackRealName:               cloneString(member.RealName),
		SlackHandle:                 cloneString(member.Handle),
		SlackEmail:                  cloneString(member.Email),
		ProfileImageURL:             cloneString(member.ProfileImageURL),
		Enabled:                     true,
		LinkedAt:                    now,
		LastVerifiedAt:              &now,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}

	stored, err := s.identities.Upsert(ctx, identity)
	if err != nil {
		if errors.Is(err, repository.ErrUserSlackIdentityConflict) {
			return domain.UserSlackIdentity{}, ErrUserSlackIdentityConflict
		}
		return domain.UserSlackIdentity{}, err
	}
	return stored, nil
}

func (s *UserSlackIdentityService) SetEnabled(ctx context.Context, user domain.User, enabled *bool) (domain.UserSlackIdentity, error) {
	userID := strings.TrimSpace(user.ID)
	if userID == "" {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentityUserIDRequired
	}
	if enabled == nil {
		return domain.UserSlackIdentity{}, ErrUserSlackIdentityEnabledRequired
	}
	return s.identities.SetEnabled(ctx, userID, *enabled, s.now().UTC())
}

func (s *UserSlackIdentityService) Unlink(ctx context.Context, user domain.User) error {
	userID := strings.TrimSpace(user.ID)
	if userID == "" {
		return ErrUserSlackIdentityUserIDRequired
	}
	err := s.identities.DeleteByUserID(ctx, userID)
	if errors.Is(err, repository.ErrUserSlackIdentityNotFound) {
		return nil
	}
	return err
}

func (s *UserSlackIdentityService) requireReadyWorkspace(ctx context.Context) (domain.SlackWorkspaceIntegration, error) {
	integration, err := s.integrations.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrSlackWorkspaceIntegrationNotFound) {
			return domain.SlackWorkspaceIntegration{}, ErrUserSlackIdentityWorkspaceNotConfigured
		}
		return domain.SlackWorkspaceIntegration{}, err
	}
	if !integration.Enabled {
		return domain.SlackWorkspaceIntegration{}, ErrUserSlackIdentityWorkspaceDisabled
	}
	return integration, nil
}

func (s *UserSlackIdentityService) lookupAuthenticatedEmailMember(ctx context.Context, integration domain.SlackWorkspaceIntegration, email string) (platformslack.User, bool, error) {
	normalizedEmail := NormalizeEmail(email)
	if normalizedEmail == "" {
		return platformslack.User{}, false, ErrUserSlackIdentityEmailRequired
	}
	member, err := s.client.LookupUserByEmail(ctx, integration.BotTokenSecret, normalizedEmail)
	if err != nil {
		switch {
		case errors.Is(err, platformslack.ErrUsersNotFound):
			return platformslack.User{}, false, nil
		case errors.Is(err, platformslack.ErrMissingScope):
			return platformslack.User{}, false, mapSlackIdentityMissingScope(err)
		case errors.Is(err, platformslack.ErrDeletedUser), errors.Is(err, platformslack.ErrBotUser), errors.Is(err, platformslack.ErrAppUser):
			return platformslack.User{}, false, ErrUserSlackIdentityMemberUnavailable
		default:
			return platformslack.User{}, false, mapSlackClientError(err)
		}
	}
	return member, true, nil
}

func mapSlackIdentityMissingScope(err error) error {
	var missingScopeErr *platformslack.MissingScopeError
	if !errors.As(err, &missingScopeErr) {
		return fmt.Errorf("slack member lookup requires additional app scopes")
	}
	needed := strings.TrimSpace(missingScopeErr.Needed)
	if needed == "" {
		needed = "users:read.email"
	}
	return fmt.Errorf("slack member lookup requires the %s scope. Ask an administrator to add it and reinstall or reauthorize the Slack app", needed)
}

func slackIdentityWorkspaceStatus(integration domain.SlackWorkspaceIntegration) string {
	if !integration.Enabled {
		return SlackIdentityWorkspaceStatusDisabled
	}
	return SlackIdentityWorkspaceStatusReady
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	return &v
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func serviceOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
