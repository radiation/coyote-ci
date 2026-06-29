import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  ensureMyEmailNotificationTarget,
  formatAPIErrorMessage,
  getCommitAuthorFailureNotificationPreference,
  getCommitAuthorSuccessNotificationPreference,
  getMe,
  getMyEmailNotificationTarget,
  setCommitAuthorFailureNotificationPreference,
  setCommitAuthorSuccessNotificationPreference,
} from "../api";
import { useAuth } from "../auth-context";

function providerLabel(
  authMethod: "header" | "oidc" | "api_token" | undefined,
  authMode: "disabled" | "header" | "oidc" | null,
): string {
  if (authMethod === "oidc") {
    return "OIDC";
  }
  if (authMethod === "header") {
    return "Trusted header";
  }
  if (authMethod === "api_token") {
    return "API token";
  }
  if (authMode === "oidc") {
    return "OIDC";
  }
  if (authMode === "header") {
    return "Trusted header";
  }
  if (authMode === "disabled") {
    return "Disabled auth mode";
  }
  return "Unknown";
}

export function ProfilePage() {
  const queryClient = useQueryClient();
  const { currentUser, authMode } = useAuth();
  const [actionErrorMessage, setActionErrorMessage] = useState<string | null>(
    null,
  );

  const { data: me } = useQuery({
    queryKey: ["me", "auth-provider"],
    queryFn: getMe,
  });

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

  const ensureTargetMutation = useMutation({
    mutationFn: ensureMyEmailNotificationTarget,
    onMutate: () => {
      setActionErrorMessage(null);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["me", "notification-target", "email"],
      });
      await queryClient.invalidateQueries({
        queryKey: ["me", "notification-preferences", "commit-author-failures"],
      });
      await queryClient.invalidateQueries({
        queryKey: ["me", "notification-preferences", "commit-author-successes"],
      });
    },
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

  const effectiveDisplayName =
    currentUser.display_name?.trim() ||
    "Not provided by authentication provider";
  const effectiveProvider = providerLabel(me?.auth_method, authMode);
  const emailVerified = me?.email_verified;
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

  return (
    <>
      <div className="page-header-row">
        <div>
          <h2>Profile</h2>
          <p className="subtle-text">
            Identity details come from the configured authentication provider
            and are read-only in Coyote CI for now.
          </p>
        </div>
      </div>

      <section className="settings-panel" style={{ marginTop: 14 }}>
        <h3>Identity</h3>
        <dl className="profile-details-list">
          <div>
            <dt>User ID</dt>
            <dd>{currentUser.id}</dd>
          </div>
          <div>
            <dt>Display Name</dt>
            <dd>{effectiveDisplayName}</dd>
          </div>
          <div>
            <dt>Email</dt>
            <dd>{currentUser.email}</dd>
          </div>
          <div>
            <dt>Global Role</dt>
            <dd>{currentUser.global_role}</dd>
          </div>
          <div>
            <dt>Authentication Provider</dt>
            <dd>{effectiveProvider}</dd>
          </div>
          {emailVerified === true && (
            <div>
              <dt>Email Verification</dt>
              <dd>Verified by authentication provider</dd>
            </div>
          )}
        </dl>
        {emailVerified !== true && (
          <p className="subtle-text" style={{ marginTop: 10 }}>
            Email verification state is not currently provided by this
            authentication flow.
          </p>
        )}
      </section>

      <section className="settings-panel" style={{ marginTop: 16 }}>
        <h3>My Notification Target</h3>
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
            <p className="subtle-text">
              This is your personal email notification target.
            </p>
            <dl className="profile-details-list" style={{ marginTop: 10 }}>
              <div>
                <dt>Name</dt>
                <dd>{myTarget.name}</dd>
              </div>
              <div>
                <dt>Email</dt>
                <dd>{myTarget.address ?? "-"}</dd>
              </div>
              <div>
                <dt>Enabled</dt>
                <dd>{myTarget.enabled ? "Yes" : "No"}</dd>
              </div>
            </dl>
            <p className="subtle-text" style={{ marginTop: 10 }}>
              Manage additional targets and subscriptions in{" "}
              <Link to="/settings/notifications">Notification settings</Link>.
            </p>
          </>
        )}
        {!myTargetLoading && !myTarget && (
          <>
            <p className="subtle-text">
              Creating your personal target will use this authenticated email:
            </p>
            <p>{currentUser.email}</p>
            <button
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
        <h3>Commit Notifications</h3>
        <p className="subtle-text">
          Receive an email when a commit attributed to your Coyote account
          causes a build or job to fail or succeed. Success notifications may be
          more frequent.
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
            <>
              <label style={{ display: "flex", gap: 10, alignItems: "center" }}>
                <input
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
              {failurePreference.target?.address && (
                <p className="subtle-text" style={{ marginTop: 10 }}>
                  Failure notifications will be sent to{" "}
                  {failurePreference.target.address}.
                </p>
              )}
              {failurePreference.unavailable_reason ===
                "personal_target_required" && (
                <>
                  <p className="subtle-text" style={{ marginTop: 10 }}>
                    Create your personal email target before enabling commit
                    failure notifications.
                  </p>
                </>
              )}
              {failurePreference.enabled &&
                failurePreference.target &&
                !failurePreference.target.enabled && (
                  <p className="subtle-text" style={{ marginTop: 10 }}>
                    Delivery is paused because your personal email target is
                    disabled. Re-enable it in{" "}
                    <Link to="/settings/notifications">
                      Notification settings
                    </Link>
                    .
                  </p>
                )}

              <label
                style={{
                  display: "flex",
                  gap: 10,
                  alignItems: "center",
                  marginTop: 16,
                }}
              >
                <input
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
              {successPreference.target?.address && (
                <p className="subtle-text" style={{ marginTop: 10 }}>
                  Success notifications will be sent to{" "}
                  {successPreference.target.address}.
                </p>
              )}
              {successPreference.unavailable_reason ===
                "personal_target_required" && (
                <p className="subtle-text" style={{ marginTop: 10 }}>
                  Create your personal email target before enabling commit
                  success notifications.
                </p>
              )}
              {successPreference.enabled &&
                successPreference.target &&
                !successPreference.target.enabled && (
                  <p className="subtle-text" style={{ marginTop: 10 }}>
                    Success delivery is paused because your personal email
                    target is disabled. Re-enable it in{" "}
                    <Link to="/settings/notifications">
                      Notification settings
                    </Link>
                    .
                  </p>
                )}
            </>
          )}
      </section>
    </>
  );
}
