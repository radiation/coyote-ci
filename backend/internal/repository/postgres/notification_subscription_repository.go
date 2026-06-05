package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type NotificationSubscriptionRepository struct {
	db *sql.DB
}

func NewNotificationSubscriptionRepository(db *sql.DB) *NotificationSubscriptionRepository {
	return &NotificationSubscriptionRepository{db: db}
}

const notificationSubscriptionMatchColumns = `
	s.id,
	s.target_id,
	s.project_id::text,
	s.job_id::text,
	s.event_type,
	s.enabled,
	s.created_at,
	s.updated_at,
	t.id,
	t.type,
	t.name,
	t.recipient,
	t.enabled,
	t.created_at,
	t.updated_at
`

func (r *NotificationSubscriptionRepository) ListEnabledMatchesForBuildEvent(ctx context.Context, build domain.Build, eventType domain.NotificationEventType) (matches []domain.NotificationSubscriptionMatch, err error) {
	const query = `
		SELECT ` + notificationSubscriptionMatchColumns + `
		FROM notification_subscriptions s
		JOIN notification_targets t ON t.id = s.target_id
		WHERE s.enabled = TRUE
		  AND t.enabled = TRUE
		  AND s.event_type = $1
		  AND (
				(s.project_id IS NOT NULL AND s.project_id::text = $2)
				OR
				(s.job_id IS NOT NULL AND s.job_id::text = $3)
		  )
		ORDER BY s.created_at ASC, t.created_at ASC
	`

	jobID := ""
	if build.JobID != nil {
		jobID = strings.TrimSpace(*build.JobID)
	}
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(string(eventType)), strings.TrimSpace(build.ProjectID), jobID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	matches = make([]domain.NotificationSubscriptionMatch, 0)
	for rows.Next() {
		match, scanErr := scanNotificationSubscriptionMatch(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		matches = append(matches, match)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return matches, nil
}

func scanNotificationSubscriptionMatch(scanner rowScanner) (domain.NotificationSubscriptionMatch, error) {
	var match domain.NotificationSubscriptionMatch
	var subscriptionProjectID sql.NullString
	var subscriptionJobID sql.NullString
	var eventType string
	var targetType string

	err := scanner.Scan(
		&match.Subscription.ID,
		&match.Subscription.TargetID,
		&subscriptionProjectID,
		&subscriptionJobID,
		&eventType,
		&match.Subscription.Enabled,
		&match.Subscription.CreatedAt,
		&match.Subscription.UpdatedAt,
		&match.Target.ID,
		&targetType,
		&match.Target.Name,
		&match.Target.Recipient,
		&match.Target.Enabled,
		&match.Target.CreatedAt,
		&match.Target.UpdatedAt,
	)
	if err != nil {
		return domain.NotificationSubscriptionMatch{}, err
	}

	match.Subscription.EventType = domain.NotificationEventType(eventType)
	if subscriptionProjectID.Valid {
		value := subscriptionProjectID.String
		match.Subscription.ProjectID = &value
	}
	if subscriptionJobID.Valid {
		value := subscriptionJobID.String
		match.Subscription.JobID = &value
	}
	match.Target.Type = domain.NotificationTargetType(targetType)

	return match, nil
}
