package postgres

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type NotificationSubscriptionRepository struct {
	db *sql.DB
}

func NewNotificationSubscriptionRepository(db *sql.DB) *NotificationSubscriptionRepository {
	return &NotificationSubscriptionRepository{db: db}
}

const notificationSubscriptionColumns = `id, target_id, project_id::text, job_id::text, event_type, enabled, created_at, updated_at`

const notificationTargetSelectColumns = `id, owner_user_id::text, type, origin, name, recipient, enabled, created_at, updated_at`

const notificationConfigEmailTargetLookupAttempts = 8

const notificationConfigEmailTargetInsertQuery = `
	INSERT INTO notification_targets (id, owner_user_id, type, origin, name, recipient, enabled, created_at, updated_at)
	VALUES ($1, NULL, $2, $3, $4, $5, TRUE, $6, $7)
	ON CONFLICT DO NOTHING
	RETURNING ` + notificationTargetSelectColumns + `
`

const notificationConfigEmailTargetSelectQuery = `
	SELECT ` + notificationTargetSelectColumns + `
	FROM notification_targets
	WHERE type = $1
	  AND origin = $2
	  AND owner_user_id IS NULL
	  AND lower(recipient) = lower($3)
	ORDER BY created_at ASC, id ASC
	LIMIT 1
`

func (r *NotificationSubscriptionRepository) CreateTarget(ctx context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error) {
	normalizedTarget, err := domain.ValidateExplicitNotificationTarget(target)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	target = normalizedTarget
	const query = `
		INSERT INTO notification_targets (id, owner_user_id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING ` + notificationTargetSelectColumns + `
	`

	created, err := scanNotificationTarget(r.db.QueryRowContext(ctx, query,
		target.ID,
		nullableOptionalString(target.OwnerUserID),
		string(target.Type),
		string(target.Origin),
		target.Name,
		target.Recipient,
		target.Enabled,
		target.CreatedAt,
		target.UpdatedAt,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.NotificationTarget{}, repository.ErrNotificationTargetDuplicate
		}
		return domain.NotificationTarget{}, err
	}
	return created, nil
}

func (r *NotificationSubscriptionRepository) ListTargets(ctx context.Context) (targets []domain.NotificationTarget, err error) {
	const query = `
		SELECT ` + notificationTargetSelectColumns + `
		FROM notification_targets
		ORDER BY created_at ASC, id ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	targets = make([]domain.NotificationTarget, 0)
	for rows.Next() {
		target, scanErr := scanNotificationTarget(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		targets = append(targets, target)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return targets, nil
}

func (r *NotificationSubscriptionRepository) GetTargetByID(ctx context.Context, id string) (domain.NotificationTarget, error) {
	const query = `
		SELECT ` + notificationTargetSelectColumns + `
		FROM notification_targets
		WHERE id = $1
	`

	target, err := scanNotificationTarget(r.db.QueryRowContext(ctx, query, strings.TrimSpace(id)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
		}
		return domain.NotificationTarget{}, err
	}
	return target, nil
}

func (r *NotificationSubscriptionRepository) GetOwnedEmailTargetByUserID(ctx context.Context, userID string) (domain.NotificationTarget, error) {
	const query = `
		SELECT ` + notificationTargetSelectColumns + `
		FROM notification_targets
		WHERE owner_user_id = $1
		  AND type = $2
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	`

	target, err := scanNotificationTarget(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(userID),
		string(domain.NotificationTargetTypeEmail),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
		}
		return domain.NotificationTarget{}, err
	}
	return target, nil
}

func (r *NotificationSubscriptionRepository) EnsureConfigEmailTarget(ctx context.Context, input repository.EnsureConfigNotificationEmailTargetInput) (domain.NotificationTarget, error) {
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := input.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = input.Recipient
	}
	configTarget, normalizeErr := domain.NormalizeNotificationTarget(domain.NotificationTarget{
		ID:        notificationConfigEmailTargetID(strings.TrimSpace(input.ID), input.Recipient),
		Type:      domain.NotificationTargetTypeEmail,
		Origin:    domain.NotificationTargetOriginConfigDefault,
		Name:      name,
		Recipient: input.Recipient,
		Enabled:   true,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	})
	if normalizeErr != nil {
		return domain.NotificationTarget{}, normalizeErr
	}
	created, createErr := scanNotificationTarget(r.db.QueryRowContext(ctx, notificationConfigEmailTargetInsertQuery,
		configTarget.ID,
		string(domain.NotificationTargetTypeEmail),
		string(domain.NotificationTargetOriginConfigDefault),
		configTarget.Name,
		configTarget.Recipient,
		configTarget.CreatedAt,
		configTarget.UpdatedAt,
	))
	if createErr == nil {
		return created, nil
	}
	if !errors.Is(createErr, sql.ErrNoRows) {
		return domain.NotificationTarget{}, createErr
	}

	for attempt := 0; attempt < notificationConfigEmailTargetLookupAttempts; attempt++ {
		target, err := scanNotificationTarget(r.db.QueryRowContext(
			ctx,
			notificationConfigEmailTargetSelectQuery,
			string(domain.NotificationTargetTypeEmail),
			string(domain.NotificationTargetOriginConfigDefault),
			configTarget.Recipient,
		))
		if err == nil {
			return target, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationTarget{}, err
		}
		if attempt == notificationConfigEmailTargetLookupAttempts-1 {
			break
		}
		if waitErr := waitForNotificationDeliveryAcquireRetry(ctx, notificationDeliveryAcquireLookupDelay); waitErr != nil {
			return domain.NotificationTarget{}, waitErr
		}
	}

	return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
}

func notificationConfigEmailTargetID(explicitID string, recipient string) string {
	trimmedID := strings.TrimSpace(explicitID)
	if trimmedID != "" {
		return trimmedID
	}

	normalizedRecipient := strings.ToLower(strings.TrimSpace(recipient))
	hash := md5.Sum([]byte("notification-target:config-default-email:" + normalizedRecipient))
	hexValue := hex.EncodeToString(hash[:])

	return hexValue[0:8] + "-" + hexValue[8:12] + "-4" + hexValue[13:16] + "-a" + hexValue[17:20] + "-" + hexValue[20:32]
}

func (r *NotificationSubscriptionRepository) SetOwnedEmailTargetEnabled(ctx context.Context, ownerUserID string, enabled bool, updatedAt time.Time) (domain.NotificationTarget, error) {
	const query = `
		UPDATE notification_targets
		SET enabled = $2,
			updated_at = $3
		WHERE owner_user_id = $1
		  AND type = $4
		RETURNING ` + notificationTargetSelectColumns + `
	`

	target, err := scanNotificationTarget(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(ownerUserID),
		enabled,
		updatedAt,
		string(domain.NotificationTargetTypeEmail),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
		}
		return domain.NotificationTarget{}, err
	}
	return target, nil
}

func (r *NotificationSubscriptionRepository) EnsureOwnedEmailTarget(ctx context.Context, input repository.EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, error) {
	ownerUserID := strings.TrimSpace(input.OwnerUserID)
	for attempts := 0; attempts < 3; attempts++ {
		tx, beginErr := r.db.BeginTx(ctx, nil)
		if beginErr != nil {
			return domain.NotificationTarget{}, beginErr
		}

		ensured, _, ensureErr := r.ensureOwnedEmailTargetTx(ctx, tx, input, ownerUserID)
		if ensureErr != nil {
			_ = tx.Rollback()
			if errors.Is(ensureErr, repository.ErrNotificationTargetDuplicate) {
				continue
			}
			return domain.NotificationTarget{}, ensureErr
		}

		if commitErr := tx.Commit(); commitErr != nil {
			if isUniqueViolation(commitErr) {
				continue
			}
			return domain.NotificationTarget{}, commitErr
		}
		return ensured, nil
	}

	return domain.NotificationTarget{}, repository.ErrNotificationTargetDuplicate
}

func (r *NotificationSubscriptionRepository) EnsureOwnedEmailTargetInitialized(ctx context.Context, input repository.EnsureOwnedNotificationEmailTargetInput) (domain.NotificationTarget, error) {
	ownerUserID := strings.TrimSpace(input.OwnerUserID)
	for attempts := 0; attempts < 3; attempts++ {
		tx, beginErr := r.db.BeginTx(ctx, nil)
		if beginErr != nil {
			return domain.NotificationTarget{}, beginErr
		}

		ensured, newlyEligible, ensureErr := r.ensureOwnedEmailTargetTx(ctx, tx, input, ownerUserID)
		if ensureErr != nil {
			_ = tx.Rollback()
			if errors.Is(ensureErr, repository.ErrNotificationTargetDuplicate) {
				continue
			}
			return domain.NotificationTarget{}, ensureErr
		}

		if newlyEligible {
			initErr := r.initializeCommitAuthorPreferencesTx(ctx, tx, ownerUserID, input.CreatedAt, input.UpdatedAt)
			if initErr != nil {
				_ = tx.Rollback()
				return domain.NotificationTarget{}, initErr
			}
		}

		if commitErr := tx.Commit(); commitErr != nil {
			if isUniqueViolation(commitErr) {
				continue
			}
			return domain.NotificationTarget{}, commitErr
		}
		return ensured, nil
	}

	return domain.NotificationTarget{}, repository.ErrNotificationTargetDuplicate
}

func (r *NotificationSubscriptionRepository) ensureOwnedEmailTargetTx(ctx context.Context, tx *sql.Tx, input repository.EnsureOwnedNotificationEmailTargetInput, ownerUserID string) (domain.NotificationTarget, bool, error) {
	const findOwnedQuery = `
		SELECT ` + notificationTargetSelectColumns + `
		FROM notification_targets
		WHERE owner_user_id = $1
		  AND type = $2
		ORDER BY created_at ASC, id ASC
		LIMIT 1
		FOR UPDATE
	`

	target, err := scanNotificationTarget(tx.QueryRowContext(ctx, findOwnedQuery, ownerUserID, string(domain.NotificationTargetTypeEmail)))
	if err == nil {
		return target, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.NotificationTarget{}, false, err
	}
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := input.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	normalizedTarget, normalizeErr := domain.NormalizeNotificationTarget(domain.NotificationTarget{
		ID:          input.ID,
		OwnerUserID: &ownerUserID,
		Type:        domain.NotificationTargetTypeEmail,
		Origin:      domain.NotificationTargetOriginManual,
		Name:        input.Name,
		Recipient:   input.Recipient,
		Enabled:     true,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	})
	if normalizeErr != nil {
		return domain.NotificationTarget{}, false, normalizeErr
	}

	const insertQuery = `
		INSERT INTO notification_targets (id, owner_user_id, type, origin, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING ` + notificationTargetSelectColumns + `
	`

	created, createErr := scanNotificationTarget(tx.QueryRowContext(ctx, insertQuery,
		normalizedTarget.ID,
		ownerUserID,
		string(domain.NotificationTargetTypeEmail),
		string(domain.NotificationTargetOriginManual),
		normalizedTarget.Name,
		normalizedTarget.Recipient,
		true,
		normalizedTarget.CreatedAt,
		normalizedTarget.UpdatedAt,
	))
	if createErr != nil {
		if isUniqueViolation(createErr) {
			return domain.NotificationTarget{}, false, repository.ErrNotificationTargetDuplicate
		}
		return domain.NotificationTarget{}, false, createErr
	}
	return created, true, nil
}

func (r *NotificationSubscriptionRepository) initializeCommitAuthorPreferencesTx(ctx context.Context, tx *sql.Tx, ownerUserID string, createdAt time.Time, updatedAt time.Time) error {
	failureEnabled, successEnabled, err := getNotificationDefaultsTx(ctx, tx)
	if err != nil {
		return err
	}

	const query = `
		INSERT INTO user_notification_preferences (
			user_id,
			commit_author_failure_email_enabled,
			commit_author_failure_slack_enabled,
			commit_author_failure_email_source,
			commit_author_success_email_enabled,
			commit_author_success_slack_enabled,
			commit_author_success_email_source,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id)
		DO UPDATE SET
			commit_author_success_email_enabled = CASE
				WHEN user_notification_preferences.commit_author_success_email_source IS NULL THEN EXCLUDED.commit_author_success_email_enabled
				ELSE user_notification_preferences.commit_author_success_email_enabled
			END,
			commit_author_success_email_source = CASE
				WHEN user_notification_preferences.commit_author_success_email_source IS NULL THEN EXCLUDED.commit_author_success_email_source
				ELSE user_notification_preferences.commit_author_success_email_source
			END,
			updated_at = CASE
				WHEN user_notification_preferences.commit_author_success_email_source IS NULL THEN EXCLUDED.updated_at
				ELSE user_notification_preferences.updated_at
			END
	`

	_, execErr := tx.ExecContext(ctx, query,
		ownerUserID,
		failureEnabled,
		false,
		domain.UserNotificationPreferenceSourceInstanceDefault,
		successEnabled,
		false,
		domain.UserNotificationPreferenceSourceInstanceDefault,
		createdAt,
		updatedAt,
	)
	return execErr
}

func getNotificationDefaultsTx(ctx context.Context, tx *sql.Tx) (bool, bool, error) {
	const query = `
		SELECT default_commit_author_failure_email_enabled, default_commit_author_success_email_enabled
		FROM notification_instance_settings
		WHERE singleton = TRUE
	`

	var failureEnabled bool
	var successEnabled bool
	err := tx.QueryRowContext(ctx, query).Scan(&failureEnabled, &successEnabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, false, nil
		}
		return false, false, err
	}
	return failureEnabled, successEnabled, nil
}

func (r *NotificationSubscriptionRepository) UpdateTarget(ctx context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error) {
	normalizedTarget, err := domain.ValidateExplicitNotificationTarget(target)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	target = normalizedTarget
	const query = `
		UPDATE notification_targets
		SET owner_user_id = $2,
			type = $3,
			origin = $4,
			name = $5,
			recipient = $6,
			enabled = $7,
			updated_at = $8
		WHERE id = $1
		RETURNING ` + notificationTargetSelectColumns + `
	`

	updated, err := scanNotificationTarget(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(target.ID),
		nullableOptionalString(target.OwnerUserID),
		string(target.Type),
		string(target.Origin),
		target.Name,
		target.Recipient,
		target.Enabled,
		target.UpdatedAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationTarget{}, repository.ErrNotificationTargetNotFound
		}
		if isUniqueViolation(err) {
			return domain.NotificationTarget{}, repository.ErrNotificationTargetDuplicate
		}
		return domain.NotificationTarget{}, err
	}
	return updated, nil
}

func (r *NotificationSubscriptionRepository) DeleteTarget(ctx context.Context, id string) error {
	const query = `DELETE FROM notification_targets WHERE id = $1`

	res, err := r.db.ExecContext(ctx, query, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrNotificationTargetNotFound
	}
	return nil
}

func (r *NotificationSubscriptionRepository) CreateSubscription(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error) {
	const query = `
		INSERT INTO notification_subscriptions (id, target_id, project_id, job_id, event_type, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + notificationSubscriptionColumns + `
	`

	created, err := scanNotificationSubscription(r.db.QueryRowContext(ctx, query,
		subscription.ID,
		strings.TrimSpace(subscription.TargetID),
		nullableOptionalString(subscription.ProjectID),
		nullableOptionalString(subscription.JobID),
		strings.TrimSpace(string(subscription.EventType)),
		subscription.Enabled,
		subscription.CreatedAt,
		subscription.UpdatedAt,
	))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionDuplicate
		}
		return domain.NotificationSubscription{}, err
	}
	return created, nil
}

func (r *NotificationSubscriptionRepository) ListSubscriptions(ctx context.Context, filter repository.NotificationSubscriptionListFilter) (subscriptions []domain.NotificationSubscription, err error) {
	const query = `
		SELECT ` + notificationSubscriptionColumns + `
		FROM notification_subscriptions
		WHERE (
			($1::text IS NULL AND $2::text IS NULL)
			OR ($1::text IS NOT NULL AND project_id::text = $1)
			OR ($2::text IS NOT NULL AND job_id::text = $2)
		)
		ORDER BY created_at ASC, id ASC
	`

	rows, err := r.db.QueryContext(ctx, query, nullableOptionalString(filter.ProjectID), nullableOptionalString(filter.JobID))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	subscriptions = make([]domain.NotificationSubscription, 0)
	for rows.Next() {
		subscription, scanErr := scanNotificationSubscription(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		subscriptions = append(subscriptions, subscription)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return subscriptions, nil
}

func (r *NotificationSubscriptionRepository) GetSubscriptionByID(ctx context.Context, id string) (domain.NotificationSubscription, error) {
	const query = `
		SELECT ` + notificationSubscriptionColumns + `
		FROM notification_subscriptions
		WHERE id = $1
	`

	subscription, err := scanNotificationSubscription(r.db.QueryRowContext(ctx, query, strings.TrimSpace(id)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionNotFound
		}
		return domain.NotificationSubscription{}, err
	}
	return subscription, nil
}

func (r *NotificationSubscriptionRepository) UpdateSubscription(ctx context.Context, subscription domain.NotificationSubscription) (domain.NotificationSubscription, error) {
	const query = `
		UPDATE notification_subscriptions
		SET target_id = $2,
			project_id = $3,
			job_id = $4,
			event_type = $5,
			enabled = $6,
			updated_at = $7
		WHERE id = $1
		RETURNING ` + notificationSubscriptionColumns + `
	`

	updated, err := scanNotificationSubscription(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(subscription.ID),
		strings.TrimSpace(subscription.TargetID),
		nullableOptionalString(subscription.ProjectID),
		nullableOptionalString(subscription.JobID),
		strings.TrimSpace(string(subscription.EventType)),
		subscription.Enabled,
		subscription.UpdatedAt,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionNotFound
		}
		if isUniqueViolation(err) {
			return domain.NotificationSubscription{}, repository.ErrNotificationSubscriptionDuplicate
		}
		return domain.NotificationSubscription{}, err
	}
	return updated, nil
}

func (r *NotificationSubscriptionRepository) DeleteSubscription(ctx context.Context, id string) error {
	const query = `DELETE FROM notification_subscriptions WHERE id = $1`

	res, err := r.db.ExecContext(ctx, query, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return repository.ErrNotificationSubscriptionNotFound
	}
	return nil
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
	t.origin,
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

func scanNotificationTarget(scanner rowScanner) (domain.NotificationTarget, error) {
	var target domain.NotificationTarget
	var ownerUserID sql.NullString
	var targetType string
	var origin string

	err := scanner.Scan(
		&target.ID,
		&ownerUserID,
		&targetType,
		&origin,
		&target.Name,
		&target.Recipient,
		&target.Enabled,
		&target.CreatedAt,
		&target.UpdatedAt,
	)
	if err != nil {
		return domain.NotificationTarget{}, err
	}
	if ownerUserID.Valid {
		value := ownerUserID.String
		target.OwnerUserID = &value
	}
	target.Type = domain.NotificationTargetType(targetType)
	target.Origin = domain.NotificationTargetOrigin(origin)
	return target, nil
}

func scanNotificationSubscription(scanner rowScanner) (domain.NotificationSubscription, error) {
	var subscription domain.NotificationSubscription
	var projectID sql.NullString
	var jobID sql.NullString
	var eventType string

	err := scanner.Scan(
		&subscription.ID,
		&subscription.TargetID,
		&projectID,
		&jobID,
		&eventType,
		&subscription.Enabled,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
	)
	if err != nil {
		return domain.NotificationSubscription{}, err
	}
	if projectID.Valid {
		value := projectID.String
		subscription.ProjectID = &value
	}
	if jobID.Valid {
		value := jobID.String
		subscription.JobID = &value
	}
	subscription.EventType = domain.NotificationEventType(eventType)
	return subscription, nil
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
		&match.Target.Origin,
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

func nullableOptionalString(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}
