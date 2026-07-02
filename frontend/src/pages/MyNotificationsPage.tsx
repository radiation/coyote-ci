import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  createMySlackIdentity,
  deleteMySlackIdentity,
  ensureMyEmailNotificationTarget,
  formatAPIErrorMessage,
  getCommitAuthorFailureNotificationPreference,
  getCommitAuthorSuccessNotificationPreference,
  getMyEmailNotificationTarget,
  getMySlackIdentity,
  patchMySlackIdentity,
  resolveMySlackIdentity,
  setCommitAuthorFailureNotificationPreference,
  setCommitAuthorSuccessNotificationPreference,
  setMyEmailNotificationTargetEnabled,
} from "../api";
import { useAuth } from "../auth-context";
import type {
  CommitAuthorNotificationPreference,
  CommitAuthorNotificationPreferenceChannel,
  NotificationTarget,
  ResolvedSlackIdentityCandidate,
} from "../types";

function formatNotificationEmail(value: string | undefined, fallback: string) {
  const trimmed = value?.trim() ?? "";
  if (trimmed.startsWith("<") && trimmed.endsWith(">")) {
    return trimmed.slice(1, -1);
  }
  return trimmed || fallback;
}

function preferenceChannelCanEnable(channel: {
  enabled: boolean;
  unavailable_reason?: string | null;
}) {
  return channel.enabled || !channel.unavailable_reason;
}

function preferenceChannelDisabled(
  channel: { enabled: boolean; unavailable_reason?: string | null },
  pending: boolean,
) {
  return pending || !preferenceChannelCanEnable(channel);
}

function renderPreferenceUnavailableReason(
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
      return `Re-enable your linked Slack account to resume ${deliveryLabel}.`;
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

type LegacyCommitAuthorNotificationPreference = {
  enabled?: boolean;
  delivery_active?: boolean;
  target?: NotificationTarget | null;
  unavailable_reason?: string | null;
};

function normalizeCommitAuthorPreference(
  preference:
    | CommitAuthorNotificationPreference
    | LegacyCommitAuthorNotificationPreference,
): {
  email: CommitAuthorNotificationPreferenceChannel;
  slack: CommitAuthorNotificationPreferenceChannel;
} {
  if (preference?.email && preference?.slack) {
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

export function MyNotificationsPage() {
  const queryClient = useQueryClient();
  const { currentUser, authMode, isGlobalAdmin } = useAuth();
  const [actionErrorMessage, setActionErrorMessage] = useState<string | null>(
    null,
  );
  const [slackActionErrorMessage, setSlackActionErrorMessage] = useState<
    string | null
  >(null);
  const [slackCandidate, setSlackCandidate] =
    useState<ResolvedSlackIdentityCandidate | null>(null);
  const [slackNoMatchMessage, setSlackNoMatchMessage] = useState<string | null>(
    null,
  );
  const [slackNoMatchWorkspaceID, setSlackNoMatchWorkspaceID] = useState<
    string | null
  >(null);

  const {
    data: myTarget,
    isLoading: myTargetLoading,
    error: myTargetError,
  } = useQuery({
    queryKey: ["me", "notification-target", "email"],
    queryFn: getMyEmailNotificationTarget,
  });

  const {
    data: mySlackIdentity,
    isLoading: mySlackIdentityLoading,
    error: mySlackIdentityError,
  } = useQuery({
    queryKey: ["me", "slack-identity"],
    queryFn: getMySlackIdentity,
  });

  const {
    data: failurePreferenceData,
    isLoading: failurePreferenceLoading,
    error: failurePreferenceError,
  } = useQuery({
    queryKey: ["me", "notification-preferences", "commit-author-failures"],
    queryFn: getCommitAuthorFailureNotificationPreference,
  });

  const {
    data: successPreferenceData,
    isLoading: successPreferenceLoading,
    error: successPreferenceError,
  } = useQuery({
    queryKey: ["me", "notification-preferences", "commit-author-successes"],
    queryFn: getCommitAuthorSuccessNotificationPreference,
  });

  const refreshPersonalNotificationState = async () => {
    await queryClient.invalidateQueries({
      queryKey: ["me", "notification-target", "email"],
    });
    await queryClient.invalidateQueries({
      queryKey: ["me", "notification-preferences", "commit-author-failures"],
    });
    await queryClient.invalidateQueries({
      queryKey: ["me", "notification-preferences", "commit-author-successes"],
    });
  };

  const refreshMySlackIdentity = async () => {
    await queryClient.invalidateQueries({
      queryKey: ["me", "slack-identity"],
    });
  };

  const ensureTargetMutation = useMutation({
    mutationFn: ensureMyEmailNotificationTarget,
    onMutate: () => {
      setActionErrorMessage(null);
    },
    onSuccess: refreshPersonalNotificationState,
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to create a personal notification target.",
          "Failed to create personal email target",
        ),
      );
    },
  });

  const updateTargetMutation = useMutation({
    mutationFn: setMyEmailNotificationTargetEnabled,
    onMutate: () => {
      setActionErrorMessage(null);
    },
    onSuccess: refreshPersonalNotificationState,
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to manage your personal notification target.",
          "Failed to update personal email target",
        ),
      );
    },
  });

  const updateCommitPreferenceMutation = useMutation({
    mutationFn: setCommitAuthorFailureNotificationPreference,
    onMutate: () => {
      setActionErrorMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["me", "notification-preferences", "commit-author-failures"],
      });
    },
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to update commit notifications.",
          "Failed to update commit notifications",
        ),
      );
    },
  });

  const updateCommitSuccessPreferenceMutation = useMutation({
    mutationFn: setCommitAuthorSuccessNotificationPreference,
    onMutate: () => {
      setActionErrorMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["me", "notification-preferences", "commit-author-successes"],
      });
    },
    onError: (mutationError) => {
      setActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to update commit notifications.",
          "Failed to update commit notifications",
        ),
      );
    },
  });

  const resolveSlackIdentityMutation = useMutation({
    mutationFn: resolveMySlackIdentity,
    onMutate: () => {
      setSlackActionErrorMessage(null);
      setSlackNoMatchMessage(null);
      setSlackNoMatchWorkspaceID(null);
    },
    onSuccess: (response) => {
      if (response.matched && response.candidate) {
        setSlackCandidate(response.candidate);
        setSlackNoMatchMessage(null);
        setSlackNoMatchWorkspaceID(null);
        return;
      }
      setSlackCandidate(null);
      setSlackNoMatchMessage(
        "No active Slack member matched your Coyote email. Ensure the same email is present in Slack or contact an administrator.",
      );
      setSlackNoMatchWorkspaceID(slackWorkspace?.id ?? null);
    },
    onError: (mutationError) => {
      setSlackCandidate(null);
      setSlackNoMatchWorkspaceID(null);
      setSlackActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to link a personal Slack identity.",
          "Failed to find your Slack account",
        ),
      );
    },
  });

  const createSlackIdentityMutation = useMutation({
    mutationFn: createMySlackIdentity,
    onMutate: () => {
      setSlackActionErrorMessage(null);
    },
    onSuccess: async () => {
      setSlackCandidate(null);
      setSlackNoMatchMessage(null);
      setSlackNoMatchWorkspaceID(null);
      await refreshMySlackIdentity();
    },
    onError: (mutationError) => {
      setSlackActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to link a personal Slack identity.",
          "Failed to link Slack account",
        ),
      );
    },
  });

  const patchSlackIdentityMutation = useMutation({
    mutationFn: patchMySlackIdentity,
    onMutate: () => {
      setSlackActionErrorMessage(null);
    },
    onSuccess: refreshMySlackIdentity,
    onError: (mutationError) => {
      setSlackActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to update your Slack identity.",
          "Failed to update Slack identity",
        ),
      );
    },
  });

  const deleteSlackIdentityMutation = useMutation({
    mutationFn: deleteMySlackIdentity,
    onMutate: () => {
      setSlackActionErrorMessage(null);
    },
    onSuccess: async () => {
      setSlackCandidate(null);
      setSlackNoMatchMessage(null);
      setSlackNoMatchWorkspaceID(null);
      await refreshMySlackIdentity();
    },
    onError: (mutationError) => {
      setSlackActionErrorMessage(
        formatAPIErrorMessage(
          mutationError,
          "You do not have permission to unlink your Slack identity.",
          "Failed to unlink Slack identity",
        ),
      );
    },
  });

  if (!currentUser) {
    return null;
  }

  const showAdminLink = authMode === "disabled" || isGlobalAdmin;
  const personalEmail = formatNotificationEmail(
    myTarget?.address,
    currentUser.email,
  );
  const failurePreference = failurePreferenceData
    ? normalizeCommitAuthorPreference(failurePreferenceData)
    : null;
  const successPreference = successPreferenceData
    ? normalizeCommitAuthorPreference(successPreferenceData)
    : null;
  const slackWorkspace = mySlackIdentity?.workspace ?? null;
  const linkedSlackIdentity = mySlackIdentity?.identity ?? null;
  const slackWorkspaceName =
    slackWorkspace?.name?.trim() ||
    slackWorkspace?.slack_workspace_id ||
    "Slack";
  const visibleSlackCandidate =
    mySlackIdentity?.workspace_status === "ready" &&
    !linkedSlackIdentity &&
    slackCandidate &&
    slackCandidate.workspace.id === slackWorkspace?.id
      ? slackCandidate
      : null;
  const visibleSlackNoMatchMessage =
    mySlackIdentity?.workspace_status === "ready" &&
    !linkedSlackIdentity &&
    slackNoMatchWorkspaceID === (slackWorkspace?.id ?? null)
      ? slackNoMatchMessage
      : null;

  const renderSlackSection = () => {
    if (mySlackIdentityLoading) {
      return <p>Loading Slack identity...</p>;
    }

    if (mySlackIdentityError) {
      return (
        <p className="error-text">
          {formatAPIErrorMessage(
            mySlackIdentityError,
            "Unable to load your Slack identity.",
            "Failed to load Slack identity",
          )}
        </p>
      );
    }

    if (mySlackIdentity?.workspace_status === "not_configured") {
      return (
        <p className="subtle-text">
          Slack is not connected for this Coyote instance. Ask an administrator
          to connect a workspace.
        </p>
      );
    }

    if (mySlackIdentity?.workspace_status === "disabled") {
      return (
        <p className="subtle-text">
          {slackWorkspaceName} is connected, but personal Slack linking is
          currently disabled. Ask an administrator to re-enable the workspace.
        </p>
      );
    }

    const showWorkspaceHealthWarning =
      slackWorkspace?.last_test_succeeded === false;

    if (linkedSlackIdentity) {
      return (
        <>
          <div className="my-notifications-target-summary">
            <div>
              <p className="subtle-text my-notifications-detail-label">
                Workspace
              </p>
              <p className="my-notifications-detail-value">
                {slackWorkspaceName}
              </p>
            </div>
            <div>
              <p className="subtle-text my-notifications-detail-label">
                Slack account
              </p>
              <p className="my-notifications-detail-value">
                {linkedSlackIdentity.display_name ||
                  linkedSlackIdentity.real_name ||
                  linkedSlackIdentity.handle ||
                  linkedSlackIdentity.slack_user_id}
                {linkedSlackIdentity.handle
                  ? ` (@${linkedSlackIdentity.handle.replace(/^@+/, "")})`
                  : ""}
              </p>
            </div>
            <div>
              <p className="subtle-text my-notifications-detail-label">
                Status
              </p>
              <p className="my-notifications-detail-value">
                <span
                  className={
                    linkedSlackIdentity.enabled
                      ? "my-notifications-status is-enabled"
                      : "my-notifications-status is-paused"
                  }
                >
                  {linkedSlackIdentity.enabled ? "Linked" : "Paused"}
                </span>
              </p>
            </div>
          </div>
          {showWorkspaceHealthWarning && (
            <p className="subtle-text" style={{ marginTop: 10 }}>
              The last admin Slack connection test failed. Your linked identity
              is still saved, but an administrator may need to refresh the
              workspace credentials if future lookups fail.
            </p>
          )}
          <p className="subtle-text" style={{ marginTop: 10 }}>
            This link only stores your Slack member identity. It does not send
            Slack notifications yet.
          </p>
          <div className="button-row" style={{ marginTop: 10 }}>
            <button
              className={
                linkedSlackIdentity.enabled
                  ? "secondary-button danger-button"
                  : "secondary-button"
              }
              type="button"
              onClick={() =>
                patchSlackIdentityMutation.mutate({
                  enabled: !linkedSlackIdentity.enabled,
                })
              }
              disabled={patchSlackIdentityMutation.isPending}
            >
              {patchSlackIdentityMutation.isPending
                ? "Saving..."
                : linkedSlackIdentity.enabled
                  ? "Pause Slack link"
                  : "Enable Slack link"}
            </button>
            <button
              className="secondary-button danger-button"
              type="button"
              onClick={() => {
                if (
                  window.confirm(
                    "Unlink your Slack account from Coyote? This does not enable or disable any notifications.",
                  )
                ) {
                  deleteSlackIdentityMutation.mutate();
                }
              }}
              disabled={deleteSlackIdentityMutation.isPending}
            >
              {deleteSlackIdentityMutation.isPending
                ? "Unlinking..."
                : "Unlink Slack account"}
            </button>
          </div>
        </>
      );
    }

    return (
      <>
        <div className="my-notifications-target-summary">
          <div>
            <p className="subtle-text my-notifications-detail-label">
              Workspace
            </p>
            <p className="my-notifications-detail-value">
              {slackWorkspaceName}
            </p>
          </div>
          <div>
            <p className="subtle-text my-notifications-detail-label">
              Matching email
            </p>
            <p className="my-notifications-email">{currentUser.email}</p>
          </div>
        </div>
        <p className="subtle-text" style={{ marginTop: 10 }}>
          Coyote stores Slack’s stable member ID after you confirm the account
          match.
        </p>
        {showWorkspaceHealthWarning && (
          <p className="subtle-text" style={{ marginTop: 10 }}>
            The last admin Slack connection test failed. You can still try a
            live lookup now; the lookup result is the current source of truth.
          </p>
        )}
        {!visibleSlackCandidate && (
          <button
            className="secondary-button"
            type="button"
            onClick={() =>
              resolveSlackIdentityMutation.mutate({
                method: "authenticated_email",
              })
            }
            disabled={resolveSlackIdentityMutation.isPending}
          >
            {resolveSlackIdentityMutation.isPending
              ? "Finding..."
              : "Find my Slack account"}
          </button>
        )}
        {visibleSlackNoMatchMessage && (
          <p className="subtle-text" style={{ marginTop: 10 }}>
            {visibleSlackNoMatchMessage}
          </p>
        )}
        {visibleSlackCandidate && (
          <div style={{ marginTop: 14 }}>
            <h4 style={{ marginBottom: 8 }}>Is this your Slack account?</h4>
            <div className="my-notifications-target-summary">
              <div>
                <p className="subtle-text my-notifications-detail-label">
                  Display name
                </p>
                <p className="my-notifications-detail-value">
                  {visibleSlackCandidate.display_name || "Unavailable"}
                </p>
              </div>
              <div>
                <p className="subtle-text my-notifications-detail-label">
                  Real name
                </p>
                <p className="my-notifications-detail-value">
                  {visibleSlackCandidate.real_name || "Unavailable"}
                </p>
              </div>
              <div>
                <p className="subtle-text my-notifications-detail-label">
                  Handle
                </p>
                <p className="my-notifications-detail-value">
                  {visibleSlackCandidate.handle
                    ? `@${visibleSlackCandidate.handle.replace(/^@+/, "")}`
                    : "Unavailable"}
                </p>
              </div>
            </div>
            <div className="button-row" style={{ marginTop: 10 }}>
              <button
                className="secondary-button"
                type="button"
                onClick={() =>
                  createSlackIdentityMutation.mutate({
                    resolution_method: "authenticated_email",
                    workspace_integration_id:
                      visibleSlackCandidate.workspace.id,
                    slack_workspace_id:
                      visibleSlackCandidate.workspace.slack_workspace_id,
                    slack_user_id: visibleSlackCandidate.slack_user_id,
                  })
                }
                disabled={createSlackIdentityMutation.isPending}
              >
                {createSlackIdentityMutation.isPending
                  ? "Linking..."
                  : "Link this Slack account"}
              </button>
              <button
                className="secondary-button"
                type="button"
                onClick={() => {
                  setSlackCandidate(null);
                  setSlackNoMatchMessage(null);
                  setSlackNoMatchWorkspaceID(null);
                }}
                disabled={createSlackIdentityMutation.isPending}
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </>
    );
  };

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>My notifications</h2>
          <p className="subtle-text">
            Manage the personal email and Slack delivery Coyote uses for your
            commit-author build notifications.
          </p>
        </div>
      </div>

      <section className="settings-panel" style={{ marginTop: 14 }}>
        <h3>Personal Slack</h3>
        <p className="subtle-text">
          Link your account to a stable Slack member identity in the connected
          workspace. This does not enable Slack delivery by itself.
        </p>
        {slackActionErrorMessage && (
          <p className="error-text">{slackActionErrorMessage}</p>
        )}
        {renderSlackSection()}
      </section>

      <section className="settings-panel" style={{ marginTop: 14 }}>
        <h3>Personal email</h3>
        <p className="subtle-text">
          This address comes from your authenticated identity and is read-only
          here.
        </p>
        {actionErrorMessage && (
          <p className="error-text">{actionErrorMessage}</p>
        )}
        {myTargetError && (
          <p className="error-text">
            {formatAPIErrorMessage(
              myTargetError,
              "Unable to load your personal notification target.",
              "Failed to load personal notification target",
            )}
          </p>
        )}
        {myTargetLoading && <p>Loading personal notification target...</p>}
        {!myTargetLoading && myTarget && (
          <>
            <div className="my-notifications-target-summary">
              <div>
                <p className="subtle-text my-notifications-detail-label">
                  Email
                </p>
                <p className="my-notifications-email">{personalEmail}</p>
              </div>
              <div>
                <p className="subtle-text my-notifications-detail-label">
                  Delivery status
                </p>
                <p className="my-notifications-detail-value">
                  <span
                    className={
                      myTarget.enabled
                        ? "my-notifications-status is-enabled"
                        : "my-notifications-status is-paused"
                    }
                  >
                    {myTarget.enabled ? "Enabled" : "Paused"}
                  </span>
                </p>
              </div>
              <div>
                <p className="subtle-text my-notifications-detail-label">
                  Ownership
                </p>
                <p className="my-notifications-detail-value">
                  Personal to your authenticated account
                </p>
              </div>
            </div>
            <p className="subtle-text" style={{ marginTop: 10 }}>
              {myTarget.enabled
                ? "Delivery is active. Disable this target to pause notifications without deleting your saved preferences."
                : "Delivery is paused. Re-enable this target to resume any saved commit-author preferences."}
            </p>
            <button
              className={
                myTarget.enabled
                  ? "secondary-button danger-button"
                  : "secondary-button"
              }
              type="button"
              onClick={() =>
                updateTargetMutation.mutate({ enabled: !myTarget.enabled })
              }
              disabled={updateTargetMutation.isPending}
            >
              {updateTargetMutation.isPending
                ? "Saving..."
                : myTarget.enabled
                  ? "Disable my email target"
                  : "Re-enable my email target"}
            </button>
          </>
        )}
        {!myTargetLoading && !myTarget && (
          <>
            <div className="my-notifications-target-summary">
              <div>
                <p className="subtle-text my-notifications-detail-label">
                  Email
                </p>
                <p className="my-notifications-email">{currentUser.email}</p>
              </div>
              <div>
                <p className="subtle-text my-notifications-detail-label">
                  Delivery status
                </p>
                <p className="my-notifications-detail-value">Not configured</p>
              </div>
            </div>
            <p className="subtle-text">
              Create your personal email target to start using this address for
              notifications. When you create your personal email target, your
              initial notification preferences will be set from the current
              instance defaults.
            </p>
            <button
              className="secondary-button"
              type="button"
              onClick={() => ensureTargetMutation.mutate()}
              disabled={ensureTargetMutation.isPending}
            >
              {ensureTargetMutation.isPending
                ? "Creating..."
                : "Create my email target"}
            </button>
          </>
        )}
      </section>

      <section className="settings-panel" style={{ marginTop: 16 }}>
        <h3>Commit notifications</h3>
        <p className="subtle-text">
          Choose which build results you want delivered to you. Email and Slack
          are configured independently for failures and successes. Success
          notifications can be more frequent.
        </p>
        {(failurePreferenceError || successPreferenceError) && (
          <p className="error-text">
            {formatAPIErrorMessage(
              failurePreferenceError ?? successPreferenceError,
              "Unable to load your commit notification preference.",
              "Failed to load commit notifications",
            )}
          </p>
        )}
        {(failurePreferenceLoading || successPreferenceLoading) && (
          <p>Loading commit notification preference...</p>
        )}
        {!failurePreferenceLoading &&
          !successPreferenceLoading &&
          failurePreference &&
          successPreference && (
            <div className="my-notifications-preference-list">
              <div className="my-notifications-preference">
                <p className="my-notifications-detail-value">
                  Notify me when my commits fail
                </p>
                <p className="subtle-text my-notifications-preference-description">
                  Choose whether failed builds attributed to your account are
                  sent by email, Slack DM, or both.
                </p>
                {failurePreference.email.target && (
                  <p className="subtle-text my-notifications-preference-meta">
                    Sends to{" "}
                    {formatNotificationEmail(
                      failurePreference.email.target.address,
                      currentUser.email,
                    )}
                    .
                  </p>
                )}
                <label
                  className="checkbox-label"
                  htmlFor="my-notifications-commit-failures-email"
                >
                  <input
                    id="my-notifications-commit-failures-email"
                    type="checkbox"
                    aria-label="Notify me when my commits fail"
                    checked={failurePreference.email.enabled}
                    disabled={
                      !failurePreference ||
                      failurePreferenceLoading ||
                      preferenceChannelDisabled(
                        failurePreference.email,
                        updateCommitPreferenceMutation.isPending,
                      )
                    }
                    onChange={(event) =>
                      updateCommitPreferenceMutation.mutate({
                        email_enabled: event.currentTarget.checked,
                        slack_enabled: failurePreference.slack.enabled,
                      })
                    }
                  />
                  <span>Email me when my commits fail</span>
                </label>
                {renderPreferenceUnavailableReason(
                  failurePreference.email,
                  "failure emails",
                ) && (
                  <p className="subtle-text my-notifications-preference-meta">
                    {renderPreferenceUnavailableReason(
                      failurePreference.email,
                      "failure emails",
                    )}
                  </p>
                )}
                {failurePreference.email.enabled &&
                  failurePreference.email.target &&
                  !failurePreference.email.target.enabled && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Paused while your personal email target is disabled.
                    </p>
                  )}
                {!failurePreference.email.enabled &&
                  failurePreference.email.target &&
                  !failurePreference.email.target.enabled && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Re-enable your personal email target before turning this
                      on.
                    </p>
                  )}
                <label
                  className="checkbox-label"
                  htmlFor="my-notifications-commit-failures-slack"
                >
                  <input
                    id="my-notifications-commit-failures-slack"
                    type="checkbox"
                    checked={failurePreference.slack.enabled}
                    disabled={
                      !failurePreference ||
                      failurePreferenceLoading ||
                      preferenceChannelDisabled(
                        failurePreference.slack,
                        updateCommitPreferenceMutation.isPending,
                      )
                    }
                    onChange={(event) =>
                      updateCommitPreferenceMutation.mutate({
                        email_enabled: failurePreference.email.enabled,
                        slack_enabled: event.currentTarget.checked,
                      })
                    }
                  />
                  <span>Send me a Slack DM when my commits fail</span>
                </label>
                {renderPreferenceUnavailableReason(
                  failurePreference.slack,
                  "failure Slack delivery",
                ) && (
                  <p className="subtle-text my-notifications-preference-meta">
                    {renderPreferenceUnavailableReason(
                      failurePreference.slack,
                      "failure Slack delivery",
                    )}
                  </p>
                )}
                {failurePreference.slack.enabled &&
                  !failurePreference.slack.delivery_active && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Slack delivery is paused until your linked Slack identity
                      and workspace are active again.
                    </p>
                  )}
              </div>

              <div className="my-notifications-preference">
                <p className="my-notifications-detail-value">
                  Notify me when my commits succeed
                </p>
                <p className="subtle-text my-notifications-preference-description">
                  Choose whether successful builds attributed to your account
                  are sent by email, Slack DM, or both.
                </p>
                {successPreference.email.target && (
                  <p className="subtle-text my-notifications-preference-meta">
                    Sends to{" "}
                    {formatNotificationEmail(
                      successPreference.email.target.address,
                      currentUser.email,
                    )}
                    .
                  </p>
                )}
                <label
                  className="checkbox-label"
                  htmlFor="my-notifications-commit-successes-email"
                >
                  <input
                    id="my-notifications-commit-successes-email"
                    type="checkbox"
                    aria-label="Notify me when my commits succeed"
                    checked={successPreference.email.enabled}
                    disabled={
                      !successPreference ||
                      successPreferenceLoading ||
                      preferenceChannelDisabled(
                        successPreference.email,
                        updateCommitSuccessPreferenceMutation.isPending,
                      )
                    }
                    onChange={(event) =>
                      updateCommitSuccessPreferenceMutation.mutate({
                        email_enabled: event.currentTarget.checked,
                        slack_enabled: successPreference.slack.enabled,
                      })
                    }
                  />
                  <span>Email me when my commits succeed</span>
                </label>
                {renderPreferenceUnavailableReason(
                  successPreference.email,
                  "success emails",
                ) && (
                  <p className="subtle-text my-notifications-preference-meta">
                    {renderPreferenceUnavailableReason(
                      successPreference.email,
                      "success emails",
                    )}
                  </p>
                )}
                {successPreference.email.enabled &&
                  successPreference.email.target &&
                  !successPreference.email.target.enabled && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Paused while your personal email target is disabled.
                    </p>
                  )}
                {!successPreference.email.enabled &&
                  successPreference.email.target &&
                  !successPreference.email.target.enabled && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Re-enable your personal email target before turning this
                      on.
                    </p>
                  )}
                <label
                  className="checkbox-label"
                  htmlFor="my-notifications-commit-successes-slack"
                >
                  <input
                    id="my-notifications-commit-successes-slack"
                    type="checkbox"
                    checked={successPreference.slack.enabled}
                    disabled={
                      !successPreference ||
                      successPreferenceLoading ||
                      preferenceChannelDisabled(
                        successPreference.slack,
                        updateCommitSuccessPreferenceMutation.isPending,
                      )
                    }
                    onChange={(event) =>
                      updateCommitSuccessPreferenceMutation.mutate({
                        email_enabled: successPreference.email.enabled,
                        slack_enabled: event.currentTarget.checked,
                      })
                    }
                  />
                  <span>Send me a Slack DM when my commits succeed</span>
                </label>
                {renderPreferenceUnavailableReason(
                  successPreference.slack,
                  "success Slack delivery",
                ) && (
                  <p className="subtle-text my-notifications-preference-meta">
                    {renderPreferenceUnavailableReason(
                      successPreference.slack,
                      "success Slack delivery",
                    )}
                  </p>
                )}
                {successPreference.slack.enabled &&
                  !successPreference.slack.delivery_active && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Slack delivery is paused until your linked Slack identity
                      and workspace are active again.
                    </p>
                  )}
              </div>
            </div>
          )}
      </section>

      {showAdminLink && (
        <section className="my-notifications-admin-handoff">
          <p className="subtle-text">
            Need to manage shared targets, project/job subscriptions, or
            instance defaults?{" "}
            <Link to="/settings/notifications">
              Open Notification administration
            </Link>
            .
          </p>
        </section>
      )}
    </>
  );
}
