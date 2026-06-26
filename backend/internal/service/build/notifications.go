package build

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	platformemail "github.com/radiation/coyote-ci/backend/internal/platform/email"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrEmailNotificationsDisabled = errors.New("email notifications are disabled")
var ErrEmailNotificationRecipientsNotConfigured = errors.New("email notification recipients are not configured")

type BuildNotificationService struct {
	enabled                     bool
	notifyCommitAuthorOnFailure bool
	defaultRecipients           []string
	sender                      platformemail.Sender
	slackSender                 SlackWebhookSender
	jobRepo                     repository.JobRepository
	projectRepo                 repository.ProjectRepository
	deliveryRepo                repository.NotificationDeliveryRepository
	subscriptionRepo            repository.NotificationSubscriptionRepository
	userRepo                    repository.UserRepository
	preferenceRepo              repository.UserNotificationPreferenceRepository
	publicBaseURL               string
}

type BuildNotificationConfig struct {
	Enabled                     bool
	NotifyCommitAuthorOnFailure bool
	Recipients                  string
	Sender                      platformemail.Sender
	SlackSender                 SlackWebhookSender
	JobRepo                     repository.JobRepository
	ProjectRepo                 repository.ProjectRepository
	DeliveryRepo                repository.NotificationDeliveryRepository
	SubscriptionRepo            repository.NotificationSubscriptionRepository
	UserRepo                    repository.UserRepository
	PreferenceRepo              repository.UserNotificationPreferenceRepository
	PublicBaseURL               string
}

type notificationDestination struct {
	targetType        domain.NotificationTargetType
	deliveryRecipient string
	emailRecipient    string
	webhookURL        string
}

type buildNotificationDetails struct {
	statusSummary string
	projectID     string
	projectName   string
	projectLabel  string
	projectURL    string
	jobID         string
	jobName       string
	jobLabel      string
	jobURL        string
	buildID       string
	buildNumber   int64
	buildLabel    string
	durationLabel string
	refLabel      string
	shaFull       string
	shaLabel      string
	commitURL     string
	authorName    string
	authorEmail   string
	authorLabel   string
	buildURL      string
}

var fullNotificationSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func NewBuildNotificationService(cfg BuildNotificationConfig) (*BuildNotificationService, error) {
	recipients := []string(nil)
	if cfg.Enabled {
		var err error
		recipients, err = parseNotificationRecipients(cfg.Recipients)
		if err != nil {
			return nil, err
		}
	}

	return &BuildNotificationService{
		enabled:                     cfg.Enabled,
		notifyCommitAuthorOnFailure: cfg.NotifyCommitAuthorOnFailure,
		defaultRecipients:           recipients,
		sender:                      cfg.Sender,
		slackSender:                 cfg.SlackSender,
		jobRepo:                     cfg.JobRepo,
		projectRepo:                 cfg.ProjectRepo,
		deliveryRepo:                cfg.DeliveryRepo,
		subscriptionRepo:            cfg.SubscriptionRepo,
		userRepo:                    cfg.UserRepo,
		preferenceRepo:              cfg.PreferenceRepo,
		publicBaseURL:               strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
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

	details := s.buildNotificationDetails(ctx, build)
	subject, body := s.formatBuildStatusEmail(build, details)
	slackText := formatBuildStatusSlackText(details)
	log.Printf("build notification sending: build_id=%s status=%s recipients=%d", build.ID, build.Status, len(destinations))
	sendErr := s.sendTerminalNotification(ctx, build.ID, eventType, destinations, subject, body, slackText)
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

func (s *BuildNotificationService) sendTerminalNotification(ctx context.Context, buildID string, eventType domain.NotificationEventType, destinations []notificationDestination, subject string, body string, slackText string) error {
	var sendErrs []error

	for _, destination := range destinations {
		var delivery domain.NotificationDelivery
		shouldSend := true
		var err error
		if s.deliveryRepo != nil {
			delivery, shouldSend, err = s.prepareDelivery(ctx, buildID, eventType, destination.deliveryRecipient)
			if err != nil {
				return err
			}
		}
		if !shouldSend {
			continue
		}

		sendErr := s.sendDestination(ctx, destination, subject, body, slackText)
		attemptedAt := time.Now().UTC()
		if sendErr != nil {
			if s.deliveryRepo != nil {
				if updateErr := s.markDeliveryFailed(ctx, delivery, sendErr, attemptedAt); updateErr != nil {
					sendErrs = append(sendErrs, errors.Join(sendErr, updateErr))
					continue
				}
			}
			sendErrs = append(sendErrs, sendErr)
			continue
		}
		if s.deliveryRepo != nil {
			if _, updateErr := s.deliveryRepo.Update(ctx, domain.NotificationDelivery{
				ID:        delivery.ID,
				BuildID:   delivery.BuildID,
				EventType: delivery.EventType,
				Recipient: delivery.Recipient,
				Status:    domain.NotificationDeliveryStatusSent,
				Attempts:  delivery.Attempts + 1,
				CreatedAt: delivery.CreatedAt,
				UpdatedAt: attemptedAt,
				SentAt:    &attemptedAt,
			}); updateErr != nil {
				persistErr := fmt.Errorf("persist sent delivery state failed: %w", updateErr)
				if markErr := s.markDeliveryFailed(ctx, delivery, persistErr, attemptedAt); markErr != nil {
					sendErrs = append(sendErrs, errors.Join(persistErr, markErr))
					continue
				}
				sendErrs = append(sendErrs, persistErr)
			}
		}
	}

	return errors.Join(sendErrs...)
}

func (s *BuildNotificationService) resolveTerminalDestinations(ctx context.Context, build domain.Build, eventType domain.NotificationEventType) ([]notificationDestination, error) {
	var destinations []notificationDestination
	if s.subscriptionRepo == nil {
		for _, recipient := range s.defaultRecipients {
			destinations = append(destinations, notificationDestination{
				targetType:        domain.NotificationTargetTypeEmail,
				deliveryRecipient: recipient,
				emailRecipient:    recipient,
			})
		}
	} else {
		matches, err := s.subscriptionRepo.ListEnabledMatchesForBuildEvent(ctx, build, eventType)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			for _, recipient := range s.defaultRecipients {
				destinations = append(destinations, notificationDestination{
					targetType:        domain.NotificationTargetTypeEmail,
					deliveryRecipient: recipient,
					emailRecipient:    recipient,
				})
			}
		} else {
			destinations = make([]notificationDestination, 0, len(matches))
			for _, match := range matches {
				target := match.Target
				recipient := strings.TrimSpace(target.Recipient)
				if recipient == "" {
					continue
				}
				if target.Type == domain.NotificationTargetTypeSlackWebhook {
					destinations = append(destinations, notificationDestination{
						targetType:        target.Type,
						deliveryRecipient: notificationTargetDeliveryRecipient(target),
						webhookURL:        recipient,
					})
					continue
				}
				destinations = append(destinations, notificationDestination{
					targetType:        domain.NotificationTargetTypeEmail,
					deliveryRecipient: recipient,
					emailRecipient:    recipient,
				})
			}
		}
	}

	if eventType == domain.NotificationEventTypeBuildFailed {
		destination, ok, err := s.resolveCommitAuthorDestination(ctx, build)
		if err != nil {
			return nil, err
		}
		if ok {
			destinations = append(destinations, destination)
		}
	}

	return dedupeDestinations(destinations), nil
}

func (s *BuildNotificationService) resolveCommitAuthorDestination(ctx context.Context, build domain.Build) (notificationDestination, bool, error) {
	if s.userRepo == nil || s.preferenceRepo == nil || s.subscriptionRepo == nil {
		return notificationDestination{}, false, nil
	}

	authorEmail := normalizeCommitAuthorEmail(build.SourceAuthorEmail)
	if authorEmail == "" {
		return notificationDestination{}, false, nil
	}

	user, err := s.userRepo.GetByEmail(ctx, authorEmail)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			log.Printf("build notification skipped commit author recipient: build_id=%s reason=author_unmatched email=%s", build.ID, authorEmail)
			return notificationDestination{}, false, nil
		}
		return notificationDestination{}, false, err
	}

	preference, err := s.preferenceRepo.GetByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotificationPreferenceNotFound) {
			return notificationDestination{}, false, nil
		}
		return notificationDestination{}, false, err
	}
	if !preference.CommitAuthorFailureEnabled {
		return notificationDestination{}, false, nil
	}

	target, err := s.subscriptionRepo.GetOwnedEmailTargetByUserID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotificationTargetNotFound) {
			log.Printf("build notification skipped commit author recipient: build_id=%s reason=personal_target_missing user_id=%s", build.ID, user.ID)
			return notificationDestination{}, false, nil
		}
		return notificationDestination{}, false, err
	}
	if !target.Enabled {
		return notificationDestination{}, false, nil
	}

	recipient := strings.TrimSpace(target.Recipient)
	if recipient == "" {
		return notificationDestination{}, false, nil
	}

	return notificationDestination{
		targetType:        domain.NotificationTargetTypeEmail,
		deliveryRecipient: recipient,
		emailRecipient:    recipient,
	}, true, nil
}

func (s *BuildNotificationService) sendDestination(ctx context.Context, destination notificationDestination, subject string, body string, slackText string) error {
	if destination.targetType == domain.NotificationTargetTypeSlackWebhook {
		if s.slackSender == nil {
			return errors.New("slack sender is not configured")
		}
		return s.slackSender.Send(ctx, destination.webhookURL, SlackWebhookMessage{Text: slackText})
	}
	if s.sender == nil {
		return errors.New("email sender is not configured")
	}
	return s.sender.SendText(ctx, platformemail.Message{
		To:      destination.emailRecipient,
		Subject: subject,
		Body:    body,
	})
}

func (s *BuildNotificationService) prepareDelivery(ctx context.Context, buildID string, eventType domain.NotificationEventType, recipient string) (domain.NotificationDelivery, bool, error) {
	delivery, err := s.deliveryRepo.Create(ctx, domain.NotificationDelivery{
		BuildID:   buildID,
		EventType: eventType,
		Recipient: recipient,
		Status:    domain.NotificationDeliveryStatusPending,
	})
	if err == nil {
		return delivery, true, nil
	}
	if !errors.Is(err, repository.ErrNotificationDeliveryDuplicate) {
		return domain.NotificationDelivery{}, false, err
	}

	existing, getErr := s.deliveryRepo.GetByBuildEventRecipient(ctx, buildID, eventType, recipient)
	if getErr != nil {
		return domain.NotificationDelivery{}, false, getErr
	}
	if existing.Status == domain.NotificationDeliveryStatusSent {
		log.Printf("build notification skipped: build_id=%s event_type=%s recipient=%s reason=already_sent", buildID, eventType, recipient)
	} else {
		log.Printf("build notification skipped: build_id=%s event_type=%s recipient=%s reason=already_recorded status=%s", buildID, eventType, recipient, existing.Status)
	}
	return existing, false, nil
}

func (s *BuildNotificationService) markDeliveryFailed(ctx context.Context, delivery domain.NotificationDelivery, sendErr error, attemptedAt time.Time) error {
	message := strings.TrimSpace(sendErr.Error())
	_, err := s.deliveryRepo.Update(ctx, domain.NotificationDelivery{
		ID:        delivery.ID,
		BuildID:   delivery.BuildID,
		EventType: delivery.EventType,
		Recipient: delivery.Recipient,
		Status:    domain.NotificationDeliveryStatusFailed,
		Attempts:  delivery.Attempts + 1,
		LastError: &message,
		CreatedAt: delivery.CreatedAt,
		UpdatedAt: attemptedAt,
	})
	return err
}

func (s *BuildNotificationService) buildNotificationDetails(ctx context.Context, build domain.Build) buildNotificationDetails {
	details := buildNotificationDetails{
		statusSummary: buildStatusNotificationSummary(build.Status),
		projectID:     strings.TrimSpace(build.ProjectID),
		projectName:   strings.TrimSpace(build.ProjectID),
		projectLabel:  build.ProjectID,
		buildID:       strings.TrimSpace(build.ID),
		buildNumber:   build.BuildNumber,
		buildLabel:    build.ID,
	}
	repositoryRemote := resolveBuildRepositoryRemote(build)
	if s.projectRepo != nil && strings.TrimSpace(build.ProjectID) != "" {
		project, err := s.projectRepo.GetByID(ctx, build.ProjectID)
		if err == nil && strings.TrimSpace(project.Name) != "" {
			details.projectName = strings.TrimSpace(project.Name)
			details.projectLabel = fmt.Sprintf("%s (%s)", project.Name, project.ID)
		}
	}

	jobLabel := ""
	if build.JobID != nil {
		jobID := strings.TrimSpace(*build.JobID)
		if jobID != "" {
			details.jobID = jobID
			jobLabel = jobID
			details.jobName = jobID
			if s.jobRepo != nil {
				job, err := s.jobRepo.GetByID(ctx, jobID)
				if err == nil {
					if strings.TrimSpace(job.RepositoryURL) != "" && repositoryRemote == "" {
						repositoryRemote = strings.TrimSpace(job.RepositoryURL)
					}
					if strings.TrimSpace(job.Name) != "" {
						details.jobName = strings.TrimSpace(job.Name)
						jobLabel = fmt.Sprintf("%s (%s)", job.Name, job.ID)
					}
				}
			}
		}
	}
	details.jobLabel = jobLabel
	if build.BuildNumber > 0 {
		details.buildLabel = fmt.Sprintf("#%d (%s)", build.BuildNumber, build.ID)
	}
	if build.StartedAt != nil && build.FinishedAt != nil && !build.StartedAt.IsZero() && !build.FinishedAt.IsZero() && build.FinishedAt.After(*build.StartedAt) {
		details.durationLabel = build.FinishedAt.Sub(*build.StartedAt).Round(time.Second).String()
	}
	if build.SourceRef != nil && strings.TrimSpace(*build.SourceRef) != "" {
		details.refLabel = strings.TrimSpace(*build.SourceRef)
	} else if build.Ref != nil && strings.TrimSpace(*build.Ref) != "" {
		details.refLabel = strings.TrimSpace(*build.Ref)
	}
	if build.SourceSHA != nil && strings.TrimSpace(*build.SourceSHA) != "" {
		details.shaFull = strings.TrimSpace(*build.SourceSHA)
		details.shaLabel = shortNotificationSHA(*build.SourceSHA)
	} else if build.CommitSHA != nil && strings.TrimSpace(*build.CommitSHA) != "" {
		details.shaFull = strings.TrimSpace(*build.CommitSHA)
		details.shaLabel = shortNotificationSHA(*build.CommitSHA)
	}
	details.authorName = trimNotificationOptionalString(build.SourceAuthorName)
	details.authorEmail = trimNotificationOptionalString(build.SourceAuthorEmail)
	details.authorLabel = formatNotificationAuthor(build)
	if s.publicBaseURL != "" {
		details.projectURL = buildProjectDetailURL(s.publicBaseURL, details.projectID)
		details.jobURL = buildJobDetailURL(s.publicBaseURL, details.jobID)
		details.buildURL = buildBuildDetailURL(s.publicBaseURL, details.buildID)
	}
	if details.shaFull != "" && repositoryRemote != "" {
		commitURL, ok := buildRepositoryCommitURL(repositoryRemote, details.shaFull)
		if ok {
			details.commitURL = commitURL
		}
	}
	return details
}

func (s *BuildNotificationService) formatBuildStatusEmail(build domain.Build, details buildNotificationDetails) (string, string) {

	subjectParts := []string{"Coyote CI", "build", details.statusSummary, build.ID}
	if details.jobLabel != "" {
		subjectParts = []string{"Coyote CI", details.jobLabel, "build", details.statusSummary, build.ID}
	}
	subject := strings.Join(subjectParts, " ")

	bodyLines := []string{
		fmt.Sprintf("A Coyote CI build %s.", details.statusSummary),
		"",
		fmt.Sprintf("Build ID: %s", build.ID),
		fmt.Sprintf("Status: %s", build.Status),
		fmt.Sprintf("Project: %s", details.projectLabel),
	}
	if details.projectURL != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Project detail: %s", details.projectURL))
	}
	if build.BuildNumber > 0 {
		bodyLines = append(bodyLines, fmt.Sprintf("Build number: %d", build.BuildNumber))
	}
	if details.jobLabel != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Job: %s", details.jobLabel))
		if details.jobURL != "" {
			bodyLines = append(bodyLines, fmt.Sprintf("Job detail: %s", details.jobURL))
		}
	}
	if details.durationLabel != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Duration: %s", details.durationLabel))
	}
	if details.refLabel != "" || details.shaLabel != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Git: %s", joinNotificationGitParts(details.refLabel, details.shaLabel)))
	}
	if details.authorLabel != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Commit author: %s", details.authorLabel))
	}
	if details.buildURL != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Build detail: %s", details.buildURL))
	}
	if build.ErrorMessage != nil && strings.TrimSpace(*build.ErrorMessage) != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Error: %s", strings.TrimSpace(*build.ErrorMessage)))
	}

	return subject, strings.Join(bodyLines, "\n")
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

func normalizeCommitAuthorEmail(value *string) string {
	if value == nil {
		return ""
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return ""
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Address))
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

func dedupeDestinations(destinations []notificationDestination) []notificationDestination {
	if len(destinations) == 0 {
		return nil
	}
	result := make([]notificationDestination, 0, len(destinations))
	seen := make(map[string]struct{}, len(destinations))
	for _, destination := range destinations {
		key := strings.TrimSpace(destination.deliveryRecipient)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, destination)
	}
	return result
}

func notificationTargetDeliveryRecipient(target domain.NotificationTarget) string {
	if target.Type == domain.NotificationTargetTypeSlackWebhook {
		return fmt.Sprintf("%s:%s", target.Type, target.ID)
	}
	return strings.TrimSpace(target.Recipient)
}

func formatBuildStatusSlackText(details buildNotificationDetails) string {
	lines := []string{fmt.Sprintf("%s Build %s", slackStatusIndicator(details.statusSummary), details.statusSummary)}
	if details.projectLabel != "" {
		projectText := slackEscapeMrkdwnLabel(details.projectLabel)
		if details.projectURL != "" {
			label := strings.TrimSpace(details.projectName)
			if label == "" {
				label = details.projectLabel
			}
			projectText = slackMrkdwnLink(details.projectURL, label)
		}
		lines = append(lines, fmt.Sprintf("Project: %s", projectText))
	}
	if details.jobLabel != "" {
		jobText := slackEscapeMrkdwnLabel(details.jobLabel)
		if details.jobURL != "" {
			label := strings.TrimSpace(details.jobName)
			if label == "" {
				label = details.jobLabel
			}
			jobText = slackMrkdwnLink(details.jobURL, label)
		}
		lines = append(lines, fmt.Sprintf("Job: %s", jobText))
	}
	if details.buildLabel != "" {
		buildText := slackEscapeMrkdwnLabel(details.buildLabel)
		if details.buildURL != "" {
			label := details.buildLabel
			if details.buildNumber > 0 {
				label = fmt.Sprintf("#%d", details.buildNumber)
			}
			buildText = slackMrkdwnLink(details.buildURL, label)
		}
		lines = append(lines, fmt.Sprintf("Build: %s", buildText))
	}
	if details.durationLabel != "" {
		lines = append(lines, fmt.Sprintf("Duration: %s", details.durationLabel))
	}
	if details.refLabel != "" || details.shaLabel != "" {
		gitLabel := joinNotificationGitParts(details.refLabel, details.shaLabel)
		gitText := slackEscapeMrkdwnLabel(gitLabel)
		if details.commitURL != "" {
			linkedRefLabel := slackGitRefLabel(details.refLabel)
			linkedGitLabel := joinNotificationGitParts(linkedRefLabel, details.shaLabel)
			if linkedGitLabel == "" {
				linkedGitLabel = gitLabel
			}
			gitText = slackMrkdwnLink(details.commitURL, linkedGitLabel)
		}
		lines = append(lines, fmt.Sprintf("Git: %s", gitText))
	}
	authorText := formatNotificationAuthorSlack(details.authorName, details.authorEmail)
	if authorText != "" {
		lines = append(lines, fmt.Sprintf("Commit author: %s", authorText))
	}
	if details.buildURL != "" {
		lines = append(lines, fmt.Sprintf("Build detail: %s", details.buildURL))
	}
	return strings.Join(lines, "\n")
}

func slackStatusIndicator(statusSummary string) string {
	switch statusSummary {
	case "succeeded":
		return ":white_check_mark:"
	case "failed":
		return ":x:"
	default:
		return ":information_source:"
	}
}

func shortNotificationSHA(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 7 {
		return trimmed
	}
	return trimmed[:7]
}

func formatNotificationAuthor(build domain.Build) string {
	name := trimNotificationOptionalString(build.SourceAuthorName)
	email := trimNotificationOptionalString(build.SourceAuthorEmail)
	if name == "" {
		return email
	}
	if email == "" {
		return name
	}
	return fmt.Sprintf("%s <%s>", name, email)
}

func joinNotificationGitParts(refLabel string, shaLabel string) string {
	if refLabel == "" {
		return shaLabel
	}
	if shaLabel == "" {
		return refLabel
	}
	return refLabel + " @ " + shaLabel
}

func slackGitRefLabel(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if strings.HasPrefix(trimmed, "refs/heads/") {
		return strings.TrimPrefix(trimmed, "refs/heads/")
	}
	if strings.HasPrefix(trimmed, "refs/tags/") {
		return strings.TrimPrefix(trimmed, "refs/tags/")
	}
	return trimmed
}

func buildProjectDetailURL(publicBaseURL string, projectID string) string {
	return buildFrontendEntityURL(publicBaseURL, "/projects/", projectID)
}

func buildJobDetailURL(publicBaseURL string, jobID string) string {
	return buildFrontendEntityURL(publicBaseURL, "/jobs/", jobID)
}

func buildBuildDetailURL(publicBaseURL string, buildID string) string {
	return buildFrontendEntityURL(publicBaseURL, "/builds/", buildID)
}

func buildFrontendEntityURL(publicBaseURL string, pathPrefix string, id string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	trimmedID := strings.TrimSpace(id)
	if base == "" || trimmedID == "" {
		return ""
	}
	return base + pathPrefix + url.PathEscape(trimmedID)
}

func slackEscapeMrkdwnLabel(label string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(label)
}

func slackMrkdwnLink(linkURL string, label string) string {
	trimmedURL := strings.TrimSpace(linkURL)
	if trimmedURL == "" {
		return slackEscapeMrkdwnLabel(label)
	}
	return fmt.Sprintf("<%s|%s>", trimmedURL, slackEscapeMrkdwnLabel(label))
}

func formatNotificationAuthorSlack(name string, email string) string {
	trimmedName := strings.TrimSpace(name)
	trimmedEmail := strings.TrimSpace(email)
	if trimmedName == "" {
		return slackEscapeMrkdwnLabel(trimmedEmail)
	}
	if trimmedEmail == "" {
		return slackEscapeMrkdwnLabel(trimmedName)
	}
	return fmt.Sprintf("%s (%s)", slackEscapeMrkdwnLabel(trimmedName), slackEscapeMrkdwnLabel(trimmedEmail))
}

func resolveBuildRepositoryRemote(build domain.Build) string {
	if build.Source != nil && strings.TrimSpace(build.Source.RepositoryURL) != "" {
		return strings.TrimSpace(build.Source.RepositoryURL)
	}
	if build.RepoURL != nil && strings.TrimSpace(*build.RepoURL) != "" {
		return strings.TrimSpace(*build.RepoURL)
	}
	if build.Trigger.RepositoryURL != nil && strings.TrimSpace(*build.Trigger.RepositoryURL) != "" {
		return strings.TrimSpace(*build.Trigger.RepositoryURL)
	}
	return ""
}

func buildRepositoryCommitURL(repositoryRemote string, fullSHA string) (string, bool) {
	trimmedSHA := strings.TrimSpace(fullSHA)
	if !fullNotificationSHAPattern.MatchString(trimmedSHA) {
		return "", false
	}

	host, owner, repo, ok := parseRepositoryRemote(repositoryRemote)
	if !ok {
		return "", false
	}
	if host != "github.com" {
		return "", false
	}

	return fmt.Sprintf("https://%s/%s/%s/commit/%s", host, owner, repo, trimmedSHA), true
}

func parseRepositoryRemote(repositoryRemote string) (host string, owner string, repo string, ok bool) {
	trimmedRemote := strings.TrimSpace(repositoryRemote)
	if trimmedRemote == "" {
		return "", "", "", false
	}

	if strings.HasPrefix(trimmedRemote, "git@") {
		withoutPrefix := strings.TrimPrefix(trimmedRemote, "git@")
		parts := strings.SplitN(withoutPrefix, ":", 2)
		if len(parts) != 2 {
			return "", "", "", false
		}
		return parseRemoteHostAndPath(parts[0], parts[1])
	}

	parsed, err := url.Parse(trimmedRemote)
	if err != nil {
		return "", "", "", false
	}
	if parsed.Hostname() == "" {
		return "", "", "", false
	}

	return parseRemoteHostAndPath(parsed.Hostname(), parsed.Path)
}

func parseRemoteHostAndPath(hostValue string, pathValue string) (host string, owner string, repo string, ok bool) {
	host = strings.ToLower(strings.TrimSpace(hostValue))
	trimmedPath := strings.Trim(strings.TrimSpace(pathValue), "/")
	segments := strings.Split(trimmedPath, "/")
	if host == "" || len(segments) < 2 {
		return "", "", "", false
	}

	owner = strings.TrimSpace(segments[0])
	repo = strings.TrimSuffix(strings.TrimSpace(segments[1]), ".git")
	if owner == "" || repo == "" {
		return "", "", "", false
	}
	return host, owner, repo, true
}

func trimNotificationOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
