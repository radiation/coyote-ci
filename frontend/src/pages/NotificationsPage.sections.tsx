import type { FormEvent } from "react";
import type { SlackWorkspaceIntegration } from "../types/notification";
import { formatCompactTime } from "../utils/time";
import {
  slackConnectionStatus,
  slackLinkedIdentitySummary,
  slackTestStatus,
  slackWorkspaceDisplayName,
  slackWorkspaceLink,
} from "./NotificationsPage.helpers";

const SLACK_SETUP_URL = "https://api.slack.com/apps";

interface SlackStatusBadge {
  label: string;
  className: string;
}

interface NotificationsSlackWorkspaceSectionProps {
  canManageAdminSettings: boolean;
  loading: boolean;
  errorMessage: string | null;
  integration: SlackWorkspaceIntegration | null;
  mutationPending: boolean;
  connectPending: boolean;
  patchPending: boolean;
  testPending: boolean;
  disconnectPending: boolean;
  botToken: string;
  replaceTokenMode: boolean;
  replaceExisting: boolean;
  actionNoticeMessage: string | null;
  actionErrorMessage: string | null;
  onConnect: (event: FormEvent<HTMLFormElement>) => void;
  onBotTokenChange: (value: string) => void;
  onOpenReplaceTokenMode: () => void;
  onReplaceExistingChange: (checked: boolean) => void;
  onCancelReplace: () => void;
  onTestConnection: () => void;
  onToggleEnabled: (integration: SlackWorkspaceIntegration) => void;
  onDisconnect: () => void;
}

export function NotificationsSlackWorkspaceSection({
  canManageAdminSettings,
  loading,
  errorMessage,
  integration,
  mutationPending,
  connectPending,
  patchPending,
  testPending,
  disconnectPending,
  botToken,
  replaceTokenMode,
  replaceExisting,
  actionNoticeMessage,
  actionErrorMessage,
  onConnect,
  onBotTokenChange,
  onOpenReplaceTokenMode,
  onReplaceExistingChange,
  onCancelReplace,
  onTestConnection,
  onToggleEnabled,
  onDisconnect,
}: NotificationsSlackWorkspaceSectionProps) {
  const slackTechnicalDetails = integration
    ? [
        {
          label: "Workspace ID",
          value: integration.workspace_id,
        },
        {
          label: "Linked identities",
          value: slackLinkedIdentitySummary(integration),
        },
        integration.bot_id
          ? { label: "Bot ID", value: integration.bot_id }
          : null,
        integration.authed_user_id
          ? { label: "Authed user ID", value: integration.authed_user_id }
          : null,
        integration.app_id
          ? { label: "App ID", value: integration.app_id }
          : null,
      ].filter(
        (
          detail,
        ): detail is {
          label: string;
          value: string;
        } => detail !== null,
      )
    : [];
  const slackWorkspaceSummaryLink = integration
    ? slackWorkspaceLink(integration.workspace_url)
    : null;
  const slackStateStatus = integration
    ? slackConnectionStatus(integration)
    : null;
  const slackLastTestStatus = integration ? slackTestStatus(integration) : null;

  return (
    <section className="settings-panel" style={{ marginTop: 16 }}>
      <h3>Slack workspace</h3>
      <p className="subtle-text">
        A global administrator can connect one Slack workspace for this Coyote
        instance.
      </p>
      <p className="subtle-text">
        This workspace connection will be used for personal Slack accounts and
        shared notification destinations.
      </p>
      <p className="subtle-text">
        Existing Slack webhook targets remain separate and continue to work
        independently.
      </p>

      {!canManageAdminSettings && (
        <p className="subtle-text">
          Global admin access is required to manage Slack workspace integration.
        </p>
      )}

      {canManageAdminSettings && loading && (
        <p>Loading Slack workspace integration...</p>
      )}

      {canManageAdminSettings && errorMessage && (
        <p className="error-text">{errorMessage}</p>
      )}

      {canManageAdminSettings && !loading && !integration && (
        <form className="job-form" onSubmit={onConnect}>
          <label htmlFor="slack-bot-token">Slack bot token</label>
          <input
            id="slack-bot-token"
            type="password"
            value={botToken}
            onChange={(event) => onBotTokenChange(event.target.value)}
            placeholder="xoxb-..."
            autoComplete="off"
            disabled={mutationPending}
          />
          <p className="subtle-text">
            Connect a Slack app bot token to enable instance-level Slack
            workspace features. Existing Slack webhook targets are not changed
            by this connection.{" "}
            <a href={SLACK_SETUP_URL} target="_blank" rel="noreferrer">
              Open Slack app setup
            </a>
            .
          </p>
          <div className="job-form-actions">
            <button type="submit" disabled={mutationPending}>
              {connectPending ? "Connecting..." : "Connect Slack workspace"}
            </button>
          </div>
        </form>
      )}

      {canManageAdminSettings && integration && (
        <>
          <div className="slack-integration-summary">
            <div className="slack-integration-summary-header">
              <div>
                <div className="slack-integration-summary-title-row">
                  <h4>{slackWorkspaceDisplayName(integration)}</h4>
                  {slackStateStatus && (
                    <StatusBadge status={slackStateStatus} />
                  )}
                </div>
                {slackWorkspaceSummaryLink ? (
                  <a
                    className="slack-integration-link"
                    href={slackWorkspaceSummaryLink.href}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {slackWorkspaceSummaryLink.label}
                  </a>
                ) : null}
              </div>
            </div>
            <p className="slack-integration-test-summary">
              {integration.last_tested_at
                ? `Last tested ${formatCompactTime(integration.last_tested_at)}`
                : "Connection test not run yet"}{" "}
              {slackLastTestStatus && (
                <StatusBadge status={slackLastTestStatus} />
              )}
            </p>
            {!integration.enabled && (
              <p className="subtle-text">
                This workspace connection is paused. Re-enable it to resume
                Slack workspace features without reconnecting.
              </p>
            )}
          </div>

          <details className="slack-integration-details">
            <summary>Integration details</summary>
            <dl className="slack-integration-details-grid">
              {slackTechnicalDetails.map((detail) => (
                <div key={detail.label}>
                  <dt className="subtle-text">{detail.label}</dt>
                  <dd>{detail.value}</dd>
                </div>
              ))}
            </dl>
          </details>

          {integration.linked_identity_count > 0 && (
            <p className="subtle-text" style={{ marginTop: 10 }}>
              This workspace has linked user identities. Unlink them before
              disconnecting or switching workspaces.
            </p>
          )}

          <div className="job-form-actions" style={{ marginTop: 12 }}>
            <button
              className={
                integration.enabled
                  ? "secondary-button danger-button"
                  : "secondary-button"
              }
              type="button"
              onClick={() => onToggleEnabled(integration)}
              disabled={mutationPending}
            >
              {patchPending
                ? "Saving..."
                : integration.enabled
                  ? "Disable integration"
                  : "Enable integration"}
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={onTestConnection}
              disabled={mutationPending}
            >
              {testPending ? "Testing..." : "Test connection"}
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={onOpenReplaceTokenMode}
              disabled={mutationPending}
            >
              Replace bot token
            </button>
            <button
              className="secondary-button danger-button"
              type="button"
              onClick={onDisconnect}
              disabled={mutationPending}
            >
              {disconnectPending ? "Disconnecting..." : "Disconnect"}
            </button>
          </div>

          {replaceTokenMode && (
            <form
              className="job-form slack-replacement-form"
              style={{ marginTop: 16 }}
              onSubmit={onConnect}
            >
              <label htmlFor="slack-bot-token-replace">
                New Slack bot token
              </label>
              <input
                id="slack-bot-token-replace"
                type="password"
                value={botToken}
                onChange={(event) => onBotTokenChange(event.target.value)}
                placeholder="Enter a new xoxb- token"
                autoComplete="off"
                disabled={mutationPending}
              />
              <p className="subtle-text">
                Use the same-workspace token rotation path by default. Only
                enable workspace switching when the new token belongs to a
                different Slack workspace.
              </p>
              <label
                className="checkbox-label"
                htmlFor="slack-replace-existing-connected"
              >
                <input
                  id="slack-replace-existing-connected"
                  type="checkbox"
                  checked={replaceExisting}
                  onChange={(event) =>
                    onReplaceExistingChange(event.target.checked)
                  }
                  disabled={mutationPending}
                />
                Allow this token to switch Coyote to a different Slack
                workspace.
              </label>
              <div className="job-form-actions">
                <button type="submit" disabled={mutationPending}>
                  {connectPending ? "Saving..." : "Save new token"}
                </button>
                <button
                  className="secondary-button"
                  type="button"
                  onClick={onCancelReplace}
                  disabled={mutationPending}
                >
                  Cancel
                </button>
              </div>
            </form>
          )}
        </>
      )}

      {actionNoticeMessage && (
        <p className="subtle-text" style={{ marginTop: 10 }}>
          {actionNoticeMessage}
        </p>
      )}
      {actionErrorMessage && (
        <p className="error-text" style={{ marginTop: 10 }}>
          {actionErrorMessage}
        </p>
      )}
    </section>
  );
}

function StatusBadge({ status }: { status: SlackStatusBadge }) {
  return (
    <span className={`status-badge ${status.className}`}>{status.label}</span>
  );
}
