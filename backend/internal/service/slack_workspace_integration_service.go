package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrSlackWorkspaceBotTokenRequired = errors.New("slack bot token is required")
var ErrSlackWorkspaceReplaceRequired = errors.New("a different slack workspace is already connected; set replace_existing to true to replace it")
var ErrSlackWorkspaceEnabledRequired = errors.New("enabled is required")

var ErrSlackWorkspaceInvalidAuth = errors.New("slack authentication failed")
var ErrSlackWorkspaceTokenRevoked = errors.New("slack token is revoked")
var ErrSlackWorkspaceAccountInactive = errors.New("slack account or workspace is inactive")
var ErrSlackWorkspaceRateLimited = errors.New("slack request was rate limited")
var ErrSlackWorkspaceUpstream = errors.New("slack upstream failure")
var ErrSlackWorkspaceMalformedResponse = errors.New("slack auth response was malformed")

type slackAuthClient interface {
	TestAuthentication(ctx context.Context, token string) (platformslack.AuthTestResult, error)
}

type SlackWorkspaceIntegrationService struct {
	repo   repository.SlackWorkspaceIntegrationRepository
	client slackAuthClient
	now    func() time.Time
}

type ConnectSlackWorkspaceIntegrationInput struct {
	BotToken        string
	ReplaceExisting bool
}

func NewSlackWorkspaceIntegrationService(repo repository.SlackWorkspaceIntegrationRepository, client slackAuthClient) *SlackWorkspaceIntegrationService {
	if client == nil {
		client = platformslack.NewClient(nil)
	}
	return &SlackWorkspaceIntegrationService{repo: repo, client: client, now: time.Now}
}

func (s *SlackWorkspaceIntegrationService) Get(ctx context.Context) (domain.SlackWorkspaceIntegration, error) {
	return s.repo.Get(ctx)
}

func (s *SlackWorkspaceIntegrationService) Connect(ctx context.Context, input ConnectSlackWorkspaceIntegrationInput) (domain.SlackWorkspaceIntegration, error) {
	token := strings.TrimSpace(input.BotToken)
	if token == "" {
		return domain.SlackWorkspaceIntegration{}, ErrSlackWorkspaceBotTokenRequired
	}

	authResult, err := s.client.TestAuthentication(ctx, token)
	if err != nil {
		return domain.SlackWorkspaceIntegration{}, mapSlackClientError(err)
	}

	now := s.now().UTC()
	integration := domain.SlackWorkspaceIntegration{
		ID:                uuid.NewString(),
		WorkspaceID:       authResult.WorkspaceID,
		WorkspaceName:     authResult.WorkspaceName,
		WorkspaceURL:      authResult.WorkspaceURL,
		BotUserID:         authResult.BotUserID,
		AuthedUserID:      authResult.AuthedUserID,
		AppID:             authResult.AppID,
		BotTokenSecret:    token,
		Enabled:           true,
		ConnectedAt:       now,
		LastTestedAt:      &now,
		LastTestSucceeded: boolValuePtr(true),
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	updated, connectErr := s.repo.ConnectOrReplace(ctx, integration, input.ReplaceExisting)
	if connectErr != nil {
		if errors.Is(connectErr, repository.ErrSlackWorkspaceIntegrationReplaceRequired) {
			return domain.SlackWorkspaceIntegration{}, ErrSlackWorkspaceReplaceRequired
		}
		return domain.SlackWorkspaceIntegration{}, connectErr
	}

	log.Printf("slack workspace integration updated: workspace_id=%s enabled=%t", updated.WorkspaceID, updated.Enabled)
	return updated, nil
}

func (s *SlackWorkspaceIntegrationService) SetEnabled(ctx context.Context, enabled *bool) (domain.SlackWorkspaceIntegration, error) {
	if enabled == nil {
		return domain.SlackWorkspaceIntegration{}, ErrSlackWorkspaceEnabledRequired
	}
	integration, err := s.repo.SetEnabled(ctx, *enabled, s.now().UTC())
	if err != nil {
		return domain.SlackWorkspaceIntegration{}, err
	}
	log.Printf("slack workspace integration toggled: workspace_id=%s enabled=%t", integration.WorkspaceID, integration.Enabled)
	return integration, nil
}

func (s *SlackWorkspaceIntegrationService) TestConnection(ctx context.Context) (domain.SlackWorkspaceIntegration, error) {
	integration, err := s.repo.Get(ctx)
	if err != nil {
		return domain.SlackWorkspaceIntegration{}, err
	}

	now := s.now().UTC()
	result, testErr := s.client.TestAuthentication(ctx, integration.BotTokenSecret)
	if testErr != nil {
		if _, persistErr := s.repo.UpdateLastTestResult(ctx, now, false); persistErr != nil {
			log.Printf("slack workspace integration failed test status persistence: workspace_id=%s succeeded=false err=%v", integration.WorkspaceID, persistErr)
		}
		return domain.SlackWorkspaceIntegration{}, mapSlackClientError(testErr)
	}
	if strings.TrimSpace(result.WorkspaceID) != integration.WorkspaceID {
		if _, persistErr := s.repo.UpdateLastTestResult(ctx, now, false); persistErr != nil {
			log.Printf("slack workspace integration failed test status persistence: workspace_id=%s succeeded=false err=%v", integration.WorkspaceID, persistErr)
		}
		return domain.SlackWorkspaceIntegration{}, ErrSlackWorkspaceInvalidAuth
	}

	_, updateErr := s.repo.UpdateLastTestResult(ctx, now, true)
	if updateErr != nil {
		return domain.SlackWorkspaceIntegration{}, updateErr
	}

	integration, err = s.repo.Get(ctx)
	if err != nil {
		return domain.SlackWorkspaceIntegration{}, err
	}
	log.Printf("slack workspace integration test succeeded: workspace_id=%s", integration.WorkspaceID)
	return integration, nil
}

func (s *SlackWorkspaceIntegrationService) Disconnect(ctx context.Context) error {
	err := s.repo.Delete(ctx)
	if err != nil {
		return err
	}
	log.Printf("slack workspace integration disconnected")
	return nil
}

func mapSlackClientError(err error) error {
	switch {
	case errors.Is(err, platformslack.ErrInvalidAuth):
		return ErrSlackWorkspaceInvalidAuth
	case errors.Is(err, platformslack.ErrTokenRevoked):
		return ErrSlackWorkspaceTokenRevoked
	case errors.Is(err, platformslack.ErrAccountInactive):
		return ErrSlackWorkspaceAccountInactive
	case errors.Is(err, platformslack.ErrRateLimited):
		return ErrSlackWorkspaceRateLimited
	case errors.Is(err, platformslack.ErrMalformedResponse):
		return ErrSlackWorkspaceMalformedResponse
	case errors.Is(err, platformslack.ErrUpstreamFailure):
		return ErrSlackWorkspaceUpstream
	case errors.Is(err, platformslack.ErrAuthTestFailed):
		return ErrSlackWorkspaceInvalidAuth
	default:
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return ErrSlackWorkspaceUpstream
	}
}

func boolValuePtr(value bool) *bool {
	v := value
	return &v
}
