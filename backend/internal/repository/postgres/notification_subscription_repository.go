package postgres

import (
	"context"
	"database/sql"
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

const notificationTargetSelectColumns = `id, owner_user_id::text, type, name, recipient, enabled, created_at, updated_at`

func (r *NotificationSubscriptionRepository) CreateTarget(ctx context.Context, target domain.NotificationTarget) (domain.NotificationTarget, error) {
	const query = `
		INSERT INTO notification_targets (id, owner_user_id, type, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + notificationTargetSelectColumns + `
	`

	created, err := scanNotificationTarget(r.db.QueryRowContext(ctx, query,
		target.ID,
		nullableOptionalString(target.OwnerUserID),
		string(target.Type),
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

	const findByRecipientQuery = `
		SELECT ` + notificationTargetSelectColumns + `
		FROM notification_targets
		WHERE type = $1
		  AND lower(recipient) = lower($2)
		ORDER BY created_at ASC, id ASC
		LIMIT 1
		FOR UPDATE
	`

	target, err = scanNotificationTarget(tx.QueryRowContext(ctx, findByRecipientQuery,
		string(domain.NotificationTargetTypeEmail),
		strings.TrimSpace(input.Recipient),
	))
	if err == nil {
		if target.OwnerUserID != nil && strings.TrimSpace(*target.OwnerUserID) != ownerUserID {
			return domain.NotificationTarget{}, false, repository.ErrNotificationTargetOwnershipConflict
		}
		if target.OwnerUserID == nil {
			const subscriptionCountQuery = `SELECT COUNT(*) FROM notification_subscriptions WHERE target_id = $1`
			var subscriptionCount int
			countErr := tx.QueryRowContext(ctx, subscriptionCountQuery, target.ID).Scan(&subscriptionCount)
			if countErr != nil {
				return domain.NotificationTarget{}, false, countErr
			}
			if subscriptionCount > 0 {
				return domain.NotificationTarget{}, false, repository.ErrNotificationTargetOwnershipConflict
			}

			const claimQuery = `
				UPDATE notification_targets
				SET owner_user_id = $2,
					updated_at = $3
				WHERE id = $1
				  AND owner_user_id IS NULL
				RETURNING ` + notificationTargetSelectColumns + `
			`
			claimed, claimErr := scanNotificationTarget(tx.QueryRowContext(ctx, claimQuery,
				target.ID,
				ownerUserID,
				input.UpdatedAt,
			))
			if claimErr != nil {
				if errors.Is(claimErr, sql.ErrNoRows) {
					return domain.NotificationTarget{}, false, repository.ErrNotificationTargetDuplicate
				}
				if isUniqueViolation(claimErr) {
					return domain.NotificationTarget{}, false, repository.ErrNotificationTargetDuplicate
				}
				return domain.NotificationTarget{}, false, claimErr
			}
			return claimed, true, nil
		}
		return target, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.NotificationTarget{}, false, err
	}

	const insertQuery = `
		INSERT INTO notification_targets (id, owner_user_id, type, name, recipient, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + notificationTargetSelectColumns + `
	`

	created, createErr := scanNotificationTarget(tx.QueryRowContext(ctx, insertQuery,
		input.ID,
		ownerUserID,
		string(domain.NotificationTargetTypeEmail),
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.Recipient),
		true,
		input.CreatedAt,
		input.UpdatedAt,
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
			commit_author_failure_enabled,
			source,
			commit_author_success_enabled,
			commit_author_success_source,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id)
		DO UPDATE SET
			commit_author_success_enabled = CASE
				WHEN user_notification_preferences.commit_author_success_source IS NULL THEN EXCLUDED.commit_author_success_enabled
				ELSE user_notification_preferences.commit_author_success_enabled
			END,
			commit_author_success_source = CASE
				WHEN user_notification_preferences.commit_author_success_source IS NULL THEN EXCLUDED.commit_author_success_source
				ELSE user_notification_preferences.commit_author_success_source
			END,
			updated_at = CASE
				WHEN user_notification_preferences.commit_author_success_source IS NULL THEN EXCLUDED.updated_at
				ELSE user_notification_preferences.updated_at
			END
	`

	_, execErr := tx.ExecContext(ctx, query,
		ownerUserID,
		failureEnabled,
		domain.UserNotificationPreferenceSourceInstanceDefault,
		successEnabled,
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
	const query = `
		UPDATE notification_targets
		SET owner_user_id = $2,
			type = $3,
			name = $4,
			recipient = $5,
			enabled = $6,
			updated_at = $7
		WHERE id = $1
		RETURNING ` + notificationTargetSelectColumns + `
	`

	updated, err := scanNotificationTarget(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(target.ID),
		nullableOptionalString(target.OwnerUserID),
		string(target.Type),
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

	err := scanner.Scan(
		&target.ID,
		&ownerUserID,
		&targetType,
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
