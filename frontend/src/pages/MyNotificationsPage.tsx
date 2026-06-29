import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  ensureMyEmailNotificationTarget,
  formatAPIErrorMessage,
  getCommitAuthorFailureNotificationPreference,
  getCommitAuthorSuccessNotificationPreference,
  getMyEmailNotificationTarget,
  setCommitAuthorFailureNotificationPreference,
  setCommitAuthorSuccessNotificationPreference,
  setMyEmailNotificationTargetEnabled,
} from "../api";
import { useAuth } from "../auth-context";

function formatNotificationEmail(value: string | undefined, fallback: string) {
  const trimmed = value?.trim() ?? "";
  if (trimmed.startsWith("<") && trimmed.endsWith(">")) {
    return trimmed.slice(1, -1);
  }
  return trimmed || fallback;
}

export function MyNotificationsPage() {
  const queryClient = useQueryClient();
  const { currentUser, authMode, isGlobalAdmin } = useAuth();
  const [actionErrorMessage, setActionErrorMessage] = useState<string | null>(
    null,
  );

  const {
    data: myTarget,
    isLoading: myTargetLoading,
    error: myTargetError,
  } = useQuery({
    queryKey: ["me", "notification-target", "email"],
    queryFn: getMyEmailNotificationTarget,
  });

  const {
    data: failurePreference,
    isLoading: failurePreferenceLoading,
    error: failurePreferenceError,
  } = useQuery({
    queryKey: ["me", "notification-preferences", "commit-author-failures"],
    queryFn: getCommitAuthorFailureNotificationPreference,
  });

  const {
    data: successPreference,
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

  if (!currentUser) {
    return null;
  }

  const showAdminLink = authMode === "disabled" || isGlobalAdmin;
  const failurePreferenceCanEnable =
    !!failurePreference?.target && failurePreference.target.enabled;
  const failurePreferenceControlDisabled =
    updateCommitPreferenceMutation.isPending ||
    !failurePreference ||
    failurePreferenceLoading ||
    (!failurePreference.enabled && !failurePreferenceCanEnable);
  const successPreferenceCanEnable =
    !!successPreference?.target && successPreference.target.enabled;
  const successPreferenceControlDisabled =
    updateCommitSuccessPreferenceMutation.isPending ||
    !successPreference ||
    successPreferenceLoading ||
    (!successPreference.enabled && !successPreferenceCanEnable);
  const personalEmail = formatNotificationEmail(
    myTarget?.address,
    currentUser.email,
  );

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>My notifications</h2>
          <p className="subtle-text">
            Manage the personal email address Coyote uses for your notifications
            and choose which build results get emailed to you.
          </p>
        </div>
      </div>

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
          Choose which build results you want emailed to you. Notifications are
          paused whenever your personal email target is disabled. Success
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
                <label
                  className="checkbox-label"
                  htmlFor="my-notifications-commit-failures"
                >
                  <input
                    id="my-notifications-commit-failures"
                    type="checkbox"
                    checked={failurePreference.enabled}
                    disabled={failurePreferenceControlDisabled}
                    onChange={(event) =>
                      updateCommitPreferenceMutation.mutate({
                        enabled: event.currentTarget.checked,
                      })
                    }
                  />
                  <span>Notify me when my commits fail</span>
                </label>
                <p className="subtle-text my-notifications-preference-description">
                  Receive an email when a commit attributed to your account
                  results in a failed build.
                </p>
                <p className="subtle-text my-notifications-preference-meta">
                  Sends to{" "}
                  {formatNotificationEmail(
                    failurePreference.target?.address,
                    currentUser.email,
                  )}
                  .
                </p>
                {failurePreference.unavailable_reason ===
                  "personal_target_required" && (
                  <p className="subtle-text my-notifications-preference-meta">
                    Create your personal email target to turn this on.
                  </p>
                )}
                {failurePreference.enabled &&
                  failurePreference.target &&
                  !failurePreference.target.enabled && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Paused while your personal email target is disabled.
                    </p>
                  )}
                {!failurePreference.enabled &&
                  failurePreference.target &&
                  !failurePreference.target.enabled && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Re-enable your personal email target before turning this
                      on.
                    </p>
                  )}
              </div>

              <div className="my-notifications-preference">
                <label
                  className="checkbox-label"
                  htmlFor="my-notifications-commit-successes"
                >
                  <input
                    id="my-notifications-commit-successes"
                    type="checkbox"
                    checked={successPreference.enabled}
                    disabled={successPreferenceControlDisabled}
                    onChange={(event) =>
                      updateCommitSuccessPreferenceMutation.mutate({
                        enabled: event.currentTarget.checked,
                      })
                    }
                  />
                  <span>Notify me when my commits succeed</span>
                </label>
                <p className="subtle-text my-notifications-preference-description">
                  Receive an email when a commit attributed to your account
                  results in a successful build.
                </p>
                <p className="subtle-text my-notifications-preference-meta">
                  Sends to{" "}
                  {formatNotificationEmail(
                    successPreference.target?.address,
                    currentUser.email,
                  )}
                  .
                </p>
                {successPreference.unavailable_reason ===
                  "personal_target_required" && (
                  <p className="subtle-text my-notifications-preference-meta">
                    Create your personal email target to turn this on.
                  </p>
                )}
                {successPreference.enabled &&
                  successPreference.target &&
                  !successPreference.target.enabled && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Paused while your personal email target is disabled.
                    </p>
                  )}
                {!successPreference.enabled &&
                  successPreference.target &&
                  !successPreference.target.enabled && (
                    <p className="subtle-text my-notifications-preference-meta">
                      Re-enable your personal email target before turning this
                      on.
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
