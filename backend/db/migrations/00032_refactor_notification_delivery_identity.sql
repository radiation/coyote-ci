-- +goose Up

ALTER TABLE notification_deliveries
    ADD COLUMN IF NOT EXISTS transport TEXT,
    ADD COLUMN IF NOT EXISTS destination_kind TEXT,
    ADD COLUMN IF NOT EXISTS destination_key TEXT,
    ADD COLUMN IF NOT EXISTS notification_target_id UUID REFERENCES notification_targets(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS recipient_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS slack_workspace_integration_id UUID REFERENCES slack_workspace_integrations(id) ON DELETE SET NULL;

WITH distinct_email_recipients AS (
    SELECT DISTINCT ON (d.recipient)
        d.recipient,
        MIN(d.created_at) OVER (PARTITION BY d.recipient) AS created_at,
        MAX(d.updated_at) OVER (PARTITION BY d.recipient) AS updated_at,
        (
            substr(md5('notification-target:email:' || d.recipient), 1, 8) || '-' ||
            substr(md5('notification-target:email:' || d.recipient), 9, 4) || '-' ||
            '4' || substr(md5('notification-target:email:' || d.recipient), 14, 3) || '-' ||
            'a' || substr(md5('notification-target:email:' || d.recipient), 18, 3) || '-' ||
            substr(md5('notification-target:email:' || d.recipient), 21, 12)
        )::uuid AS id
    FROM notification_deliveries d
    WHERE d.recipient NOT LIKE 'slack_webhook:%'
      AND d.recipient NOT LIKE 'slack_dm:%:%'
      AND btrim(d.recipient) <> ''
)
INSERT INTO notification_targets (id, type, name, recipient, enabled, created_at, updated_at)
SELECT e.id, 'email', e.recipient, e.recipient, TRUE, e.created_at, e.updated_at
FROM distinct_email_recipients e
WHERE NOT EXISTS (
    SELECT 1
    FROM notification_targets t
    WHERE t.type = 'email'
      AND t.recipient = e.recipient
)
ON CONFLICT (id) DO NOTHING;

UPDATE notification_deliveries d
SET transport = 'slack_webhook',
    destination_kind = 'shared_target',
    destination_key = 'slack-webhook-target:' || t.id::text,
    notification_target_id = t.id
FROM notification_targets t
WHERE d.transport IS NULL
  AND d.recipient LIKE 'slack_webhook:%'
  AND t.type = 'slack_webhook'
  AND t.id::text = split_part(d.recipient, ':', 2);

WITH parsed_dm AS (
    SELECT
        d.id,
        split_part(d.recipient, ':', 2) AS workspace_integration_id_text,
        split_part(d.recipient, ':', 3) AS slack_user_id
    FROM notification_deliveries d
    WHERE d.transport IS NULL
      AND d.recipient LIKE 'slack_dm:%:%'
      AND split_part(d.recipient, ':', 2) ~ '^[0-9a-fA-F-]{36}$'
      AND btrim(split_part(d.recipient, ':', 3)) <> ''
)
UPDATE notification_deliveries d
SET transport = 'slack_dm',
    destination_kind = 'slack_identity',
    destination_key = 'slack-dm:' || parsed_dm.workspace_integration_id_text || ':' || parsed_dm.slack_user_id,
    recipient_user_id = identity.user_id,
    slack_workspace_integration_id = parsed_dm.workspace_integration_id_text::uuid
FROM parsed_dm
LEFT JOIN user_slack_identities identity
    ON identity.slack_workspace_integration_id::text = parsed_dm.workspace_integration_id_text
   AND identity.slack_user_id = parsed_dm.slack_user_id
WHERE d.id = parsed_dm.id;

UPDATE notification_deliveries d
SET transport = 'email',
    destination_kind = CASE
        WHEN t.owner_user_id IS NOT NULL THEN 'personal_email'
        ELSE 'shared_target'
    END,
    destination_key = CASE
        WHEN t.owner_user_id IS NOT NULL THEN 'email-personal:' || t.id::text
        ELSE 'email-target:' || t.id::text
    END,
    notification_target_id = t.id,
    recipient_user_id = t.owner_user_id
FROM notification_targets t
WHERE d.transport IS NULL
  AND d.recipient NOT LIKE 'slack_webhook:%'
  AND d.recipient NOT LIKE 'slack_dm:%:%'
  AND t.type = 'email'
  AND t.recipient = d.recipient;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM notification_deliveries
        WHERE transport IS NULL
           OR destination_kind IS NULL
           OR destination_key IS NULL
           OR btrim(destination_key) = ''
    ) THEN
        RAISE EXCEPTION 'notification delivery identity backfill failed: unmapped legacy rows remain';
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM notification_deliveries
        GROUP BY build_id, event_type, transport, destination_key
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'notification delivery identity backfill failed: duplicate logical deliveries detected';
    END IF;
END $$;

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_transport_check
        CHECK (transport IN ('email', 'slack_webhook', 'slack_dm')),
    ADD CONSTRAINT notification_deliveries_destination_kind_check
        CHECK (destination_kind IN ('personal_email', 'shared_target', 'slack_identity')),
    ADD CONSTRAINT notification_deliveries_transport_destination_kind_check
        CHECK (
            (transport = 'email' AND destination_kind IN ('personal_email', 'shared_target'))
            OR (transport = 'slack_webhook' AND destination_kind = 'shared_target')
            OR (transport = 'slack_dm' AND destination_kind = 'slack_identity')
        );

ALTER TABLE notification_deliveries
    ALTER COLUMN transport SET NOT NULL,
    ALTER COLUMN destination_kind SET NOT NULL,
    ALTER COLUMN destination_key SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS notification_deliveries_build_event_transport_destination_key_key
    ON notification_deliveries (build_id, event_type, transport, destination_key);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_build_event
    ON notification_deliveries (build_id, event_type);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_transport_status
    ON notification_deliveries (transport, status);

ALTER TABLE notification_deliveries
    DROP CONSTRAINT IF EXISTS notification_deliveries_build_event_recipient_key;

-- +goose Down

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_build_event_recipient_key UNIQUE (build_id, event_type, recipient);

DROP INDEX IF EXISTS idx_notification_deliveries_transport_status;
DROP INDEX IF EXISTS idx_notification_deliveries_build_event;
DROP INDEX IF EXISTS notification_deliveries_build_event_transport_destination_key_key;

ALTER TABLE notification_deliveries
    DROP CONSTRAINT IF EXISTS notification_deliveries_transport_destination_kind_check,
    DROP CONSTRAINT IF EXISTS notification_deliveries_destination_kind_check,
    DROP CONSTRAINT IF EXISTS notification_deliveries_transport_check;

ALTER TABLE notification_deliveries
    DROP COLUMN IF EXISTS slack_workspace_integration_id,
    DROP COLUMN IF EXISTS recipient_user_id,
    DROP COLUMN IF EXISTS notification_target_id,
    DROP COLUMN IF EXISTS destination_key,
    DROP COLUMN IF EXISTS destination_kind,
    DROP COLUMN IF EXISTS transport;