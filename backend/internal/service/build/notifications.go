package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/observability"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	platformslack "github.com/radiation/coyote-ci/backend/internal/platform/slack"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrEmailNotificationsDisabled = errors.New("email notifications are disabled")
var ErrEmailNotificationRecipientsNotConfigured = errors.New("email notification recipients are not configured")

type BuildNotificationService struct {
	enabled           bool
	defaultRecipients []string
	sender            platformemail.Sender
	slackSender       SlackWebhookSender
	slackClient       slackDirectMessageClient
	buildRepo         notificationBuildRepository
	artifactRepo      notificationArtifactRepository
	jobRepo           notificationJobRepository
	projectRepo       notificationProjectRepository
	deliveryRepo      notificationDeliveryRepository
	subscriptionRepo  notificationSubscriptionRepository
	userRepo          notificationUserRepository
	preferenceRepo    notificationPreferenceRepository
	identityRepo      notificationSlackIdentityRepository
	workspaceRepo     notificationSlackWorkspaceRepository
	publicBaseURL     string
	claimOwner        string
	claimDuration     time.Duration
	deliveryMetrics   observability.NotificationDeliveryMetrics
	now               func() time.Time
	retryPolicy       notificationRetryPolicy
}

type notificationBuildRepository interface {
	GetByID(ctx context.Context, id string) (domain.Build, error)
	GetStepsByBuildID(ctx context.Context, buildID string) ([]domain.BuildStep, error)
}

type notificationArtifactRepository interface {
	ListByBuildID(ctx context.Context, buildID string) ([]domain.BuildArtifact, error)
}

type notificationJobRepository interface {
	GetByID(ctx context.Context, id string) (domain.Job, error)
}

type notificationProjectRepository interface {
	GetByID(ctx context.Context, id string) (domain.Project, error)
}

type notificationDeliveryRepository interface {
	AcquireForDelivery(ctx context.Context, input repository.NotificationDeliveryClaimInput) (repository.NotificationDeliveryClaimResult, error)
	ListRecoverable(ctx context.Context, input repository.NotificationDeliveryRecoverableScanInput) ([]domain.NotificationDelivery, error)
	MarkSent(ctx context.Context, input repository.NotificationDeliveryMarkSentInput) (repository.NotificationDeliveryUpdateResult, error)
	RecordRetryableFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error)
	RecordPermanentFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error)
	RecordExhaustedFailure(ctx context.Context, input repository.NotificationDeliveryRecordFailureInput) (repository.NotificationDeliveryUpdateResult, error)
}

type notificationSubscriptionRepository interface {
	ListEnabledMatchesForBuildEvent(ctx context.Context, build domain.Build, eventType domain.NotificationEventType) ([]domain.NotificationSubscriptionMatch, error)
	GetOwnedEmailTargetByUserID(ctx context.Context, userID string) (domain.NotificationTarget, error)
	EnsureConfigEmailTarget(ctx context.Context, input repository.EnsureConfigNotificationEmailTargetInput) (domain.NotificationTarget, error)
	GetTargetByID(ctx context.Context, id string) (domain.NotificationTarget, error)
}

type notificationUserRepository interface {
	GetByEmail(ctx context.Context, email string) (domain.User, error)
}

type notificationPreferenceRepository interface {
	GetByUserID(ctx context.Context, userID string) (domain.UserNotificationPreference, error)
}

type notificationSlackIdentityRepository interface {
	GetByUserID(ctx context.Context, userID string) (domain.UserSlackIdentity, error)
}

type notificationSlackWorkspaceRepository interface {
	Get(ctx context.Context) (domain.SlackWorkspaceIntegration, error)
}

type BuildNotificationConfig struct {
	Enabled          bool
	Recipients       string
	Sender           platformemail.Sender
	SlackSender      SlackWebhookSender
	SlackClient      slackDirectMessageClient
	BuildRepo        repository.BuildRepository
	ArtifactRepo     notificationArtifactRepository
	JobRepo          repository.JobRepository
	ProjectRepo      repository.ProjectRepository
	DeliveryRepo     repository.NotificationDeliveryRepository
	SubscriptionRepo repository.NotificationSubscriptionRepository
	UserRepo         repository.UserRepository
	PreferenceRepo   repository.UserNotificationPreferenceRepository
	IdentityRepo     repository.UserSlackIdentityRepository
	WorkspaceRepo    repository.SlackWorkspaceIntegrationRepository
	PublicBaseURL    string
	ClaimOwner       string
	ClaimDuration    time.Duration
	DeliveryMetrics  observability.NotificationDeliveryMetrics
}

type notificationDestination struct {
	transport                   domain.NotificationTransport
	destinationKind             domain.NotificationDestinationKind
	destinationKey              string
	notificationTargetID        *string
	recipientUserID             *string
	slackWorkspaceIntegrationID *string
	recipient                   string
	emailRecipient              string
	webhookURL                  string
	slackUserID                 string
	slackBotToken               string
}

type slackDirectMessageClient interface {
	PostDirectMessage(ctx context.Context, token string, slackUserID string, message platformslack.Message) (platformslack.PostMessageResult, error)
}

func NewBuildNotificationService(cfg BuildNotificationConfig) (*BuildNotificationService, error) {
	recipients := []string(nil)
	if cfg.Enabled {
		var err error
		recipients, err = parseNotificationRecipients(cfg.Recipients)
		if err != nil {
			return nil, err
		}
	}
	claimDuration := notificationClaimDuration(cfg.ClaimDuration)
	if err := validateNotificationClaimDuration(claimDuration); err != nil {
		return nil, err
	}

	return &BuildNotificationService{
		enabled:           cfg.Enabled,
		defaultRecipients: recipients,
		sender:            cfg.Sender,
		slackSender:       cfg.SlackSender,
		slackClient:       cfg.SlackClient,
		buildRepo:         cfg.BuildRepo,
		artifactRepo:      cfg.ArtifactRepo,
		jobRepo:           cfg.JobRepo,
		projectRepo:       cfg.ProjectRepo,
		deliveryRepo:      cfg.DeliveryRepo,
		subscriptionRepo:  cfg.SubscriptionRepo,
		userRepo:          cfg.UserRepo,
		preferenceRepo:    cfg.PreferenceRepo,
		identityRepo:      cfg.IdentityRepo,
		workspaceRepo:     cfg.WorkspaceRepo,
		publicBaseURL:     strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
		claimOwner:        notificationClaimOwner(cfg.ClaimOwner),
		claimDuration:     claimDuration,
		deliveryMetrics:   deliveryMetricsOrNoop(cfg.DeliveryMetrics),
		now:               func() time.Time { return time.Now().UTC() },
		retryPolicy:       defaultNotificationRetryPolicy(),
	}, nil
}

func (s *BuildNotificationService) NotifyTerminalBuild(ctx context.Context, build domain.Build) error {
	if s == nil {
		return nil
	}
	if !s.enabled {
		log.Printf("build notification skipped: build_id=%s status=%s reason=disabled", build.ID, build.Status)
		return nil
	}
	if !shouldNotifyBuildStatus(build.Status) {
		return nil
	}

	eventType, ok := buildStatusNotificationEventType(build.Status)
	if !ok {
		return nil
	}
	destinations, err := s.resolveTerminalDestinations(ctx, build, eventType)
	if err != nil {
		return err
	}
	if len(destinations) == 0 {
		log.Printf("build notification skipped: build_id=%s status=%s reason=no_recipients", build.ID, build.Status)
		return nil
	}

	log.Printf("build notification sending: build_id=%s status=%s recipients=%d", build.ID, build.Status, len(destinations))
	sendErr := s.sendTerminalNotification(ctx, build, eventType, destinations)
	if sendErr != nil {
		log.Printf("build notification send failed: build_id=%s status=%s err=%v", build.ID, build.Status, sendErr)
		return sendErr
	}
	log.Printf("build notification sent: build_id=%s status=%s recipients=%d", build.ID, build.Status, len(destinations))
	return nil
}

func (s *BuildNotificationService) SendSampleBuildFailure(ctx context.Context) ([]string, error) {
	if !s.enabled {
		log.Printf("sample build notification skipped: reason=disabled")
		return nil, ErrEmailNotificationsDisabled
	}
	if len(s.defaultRecipients) == 0 {
		log.Printf("sample build notification skipped: reason=no_recipients")
		return nil, ErrEmailNotificationRecipientsNotConfigured
	}
	if s.sender == nil {
		log.Printf("sample build notification skipped: reason=no_sender")
		return nil, errors.New("email sender is not configured")
	}

	subject := "Coyote CI sample build failure notification"
	body := strings.Join([]string{
		"This is a dev-only sample notification from Coyote CI.",
		"",
		"Build ID: sample-build-failure",
		"Status: failed",
		"Project: Local Dev Project",
		"Job: local-mailpit-check",
		"",
		"This message was generated by POST /api/dev/notifications/sample-build.",
	}, "\n")

	log.Printf("sample build notification sending: recipients=%d", len(s.defaultRecipients))
	if err := s.send(ctx, s.defaultRecipients, subject, body); err != nil {
		log.Printf("sample build notification send failed: err=%v", err)
		return nil, err
	}
	log.Printf("sample build notification sent: recipients=%d", len(s.defaultRecipients))

	return append([]string(nil), s.defaultRecipients...), nil
}

func (s *BuildNotificationService) isActive() bool {
	return s != nil && s.enabled && ((s.sender != nil && len(s.defaultRecipients) > 0) || s.subscriptionRepo != nil)
}

func (s *BuildNotificationService) send(ctx context.Context, recipients []string, subject string, body string) error {
	for _, recipient := range recipients {
		if err := s.sender.SendText(ctx, platformemail.Message{
			To:      recipient,
			Subject: subject,
			Body:    body,
		}); err != nil {
			return err
		}
	}
	return nil
}

func shouldNotifyBuildStatus(status domain.BuildStatus) bool {
	return status == domain.BuildStatusFailed || status == domain.BuildStatusSuccess
}

func buildStatusNotificationSummary(status domain.BuildStatus) string {
	switch status {
	case domain.BuildStatusSuccess:
		return "succeeded"
	case domain.BuildStatusFailed:
		return "failed"
	default:
		return string(status)
	}
}

func buildStatusNotificationEventType(status domain.BuildStatus) (domain.NotificationEventType, bool) {
	switch status {
	case domain.BuildStatusSuccess:
		return domain.NotificationEventTypeBuildSucceeded, true
	case domain.BuildStatusFailed:
		return domain.NotificationEventTypeBuildFailed, true
	default:
		return "", false
	}
}

func parseNotificationRecipients(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	recipients := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		parsed, err := mail.ParseAddress(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid email notification recipient %q: %w", trimmed, err)
		}
		recipients = append(recipients, parsed.String())
	}
	return recipients, nil
}

func parseNotificationRecipient(value *string) (string, bool) {
	if value == nil {
		return "", false
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return "", false
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}

func dedupeRecipients(recipients []string) []string {
	if len(recipients) == 0 {
		return nil
	}
	result := make([]string, 0, len(recipients))
	seen := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		trimmed := strings.TrimSpace(recipient)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
