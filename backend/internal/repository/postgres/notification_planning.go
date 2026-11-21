package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

func (r *NotificationDeliveryRepository) ListUnplannedBuildIDs(ctx context.Context, after string, limit int) (ids []string, err error) {
	if limit <= 0 {
		return nil, errors.New("planning batch limit must be positive")
	}
	query := `SELECT id FROM builds WHERE NOT notification_plan_complete AND status IN ('success','failed')`
	args := []any{limit}
	if after != "" {
		query += ` AND id > $2`
		args = append(args, after)
	}
	rows, err := r.db.QueryContext(ctx, query+` ORDER BY id LIMIT $1`, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, scanErr
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *NotificationDeliveryRepository) CommitNotificationPlan(ctx context.Context, buildID string, status domain.BuildStatus, deliveries []domain.NotificationDelivery) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var complete bool
	err = tx.QueryRowContext(ctx, `SELECT notification_plan_complete FROM builds WHERE id=$1 AND status=$2 AND status IN ('success','failed') FOR UPDATE`, buildID, status).Scan(&complete)
	if errors.Is(err, sql.ErrNoRows) || complete {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, delivery := range deliveries {
		if validationErr := repository.ValidatePlannedNotification(buildID, status, delivery); validationErr != nil {
			return false, validationErr
		}
		_, insertErr := tx.ExecContext(ctx, `INSERT INTO notification_deliveries (
			id, build_id, event_type, transport, destination_kind, destination_key,
			notification_target_id, recipient_user_id, slack_workspace_integration_id,
			recipient, status, attempts, max_attempts, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',0,$11,now(),now())
		ON CONFLICT (build_id,event_type,transport,destination_key) DO NOTHING`,
			uuid.NewString(), buildID, delivery.EventType, delivery.Transport, delivery.DestinationKind, delivery.DestinationKey,
			delivery.NotificationTargetID, delivery.RecipientUserID, delivery.SlackWorkspaceIntegrationID, delivery.Recipient, delivery.MaxAttempts)
		if insertErr != nil {
			return false, insertErr
		}
	}
	if _, updateErr := tx.ExecContext(ctx, `UPDATE builds SET notification_plan_complete=true WHERE id=$1`, buildID); updateErr != nil {
		return false, updateErr
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
