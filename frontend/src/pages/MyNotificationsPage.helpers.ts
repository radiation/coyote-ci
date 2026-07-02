import type {
  CommitAuthorNotificationPreference,
  CommitAuthorNotificationPreferenceChannel,
  NotificationTarget,
} from "../types";

type LegacyCommitAuthorNotificationPreference = {
  enabled?: boolean;
  delivery_active?: boolean;
  target?: NotificationTarget | null;
  unavailable_reason?: string | null;
};

export function formatNotificationEmail(
  value: string | undefined,
  fallback: string,
) {
  const trimmed = value?.trim() ?? "";
  if (trimmed.startsWith("<") && trimmed.endsWith(">")) {
    return trimmed.slice(1, -1);
  }
  return trimmed || fallback;
}

export function preferenceChannelCanEnable(channel: {
  enabled: boolean;
  unavailable_reason?: string | null;
}) {
  return channel.enabled || !channel.unavailable_reason;
}

export function preferenceChannelDisabled(
  channel: { enabled: boolean; unavailable_reason?: string | null },
  pending: boolean,
) {
  return pending || !preferenceChannelCanEnable(channel);
}

export function renderPreferenceUnavailableReason(
  channel: { unavailable_reason?: string | null },
  deliveryLabel: string,
) {
  switch (channel.unavailable_reason) {
    case "personal_target_required":
      return "Create your personal email target to turn this on.";
    case "personal_target_disabled":
      return null;
    case "slack_identity_required":
      return `Link your personal Slack account to turn on ${deliveryLabel}.`;
    case "slack_identity_disabled":
      return `Re-enable your linked Slack account to use ${deliveryLabel}.`;
    case "slack_workspace_not_configured":
      return `An administrator must connect Slack before ${deliveryLabel} can be enabled.`;
    case "slack_workspace_disabled":
      return `An administrator must re-enable the Slack workspace before ${deliveryLabel} can be enabled.`;
    case "slack_workspace_mismatch":
      return `Your linked Slack account belongs to a different workspace. Relink it to this workspace to turn on ${deliveryLabel}.`;
    default:
      return null;
  }
}

export function normalizeCommitAuthorPreference(
  preference:
    | CommitAuthorNotificationPreference
    | LegacyCommitAuthorNotificationPreference,
): {
  email: CommitAuthorNotificationPreferenceChannel;
  slack: CommitAuthorNotificationPreferenceChannel;
} {
  if (
    preference &&
    "email" in preference &&
    "slack" in preference &&
    preference.email &&
    preference.slack
  ) {
    return {
      email: preference.email,
      slack: preference.slack,
    };
  }

  return {
    email: {
      enabled: Boolean(preference?.enabled),
      delivery_active: Boolean(preference?.delivery_active),
      target: preference?.target ?? null,
      unavailable_reason: preference?.unavailable_reason ?? null,
    },
    slack: {
      enabled: false,
      delivery_active: false,
      target: null,
      unavailable_reason: null,
    },
  };
}
