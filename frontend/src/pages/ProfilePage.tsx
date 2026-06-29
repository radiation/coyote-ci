import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  formatAPIErrorMessage,
  getMe,
  getMyEmailNotificationTarget,
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
  const { currentUser, authMode } = useAuth();

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

  if (!currentUser) {
    return null;
  }

  const effectiveDisplayName =
    currentUser.display_name?.trim() ||
    "Not provided by authentication provider";
  const effectiveProvider = providerLabel(me?.auth_method, authMode);
  const emailVerified = me?.email_verified;

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
        <h3>My notifications</h3>
        <p className="subtle-text">
          Manage your personal notification target and commit notifications from
          your dedicated notification settings page.
        </p>
        {myTargetError && (
          <p className="error-text">
            {formatAPIErrorMessage(
              myTargetError,
              "Unable to load your personal notification target.",
              "Failed to load personal notification target",
            )}
          </p>
        )}
        {myTargetLoading && <p>Loading notification summary...</p>}
        {!myTargetLoading && (
          <>
            <dl className="profile-details-list" style={{ marginTop: 10 }}>
              <div>
                <dt>Personal target</dt>
                <dd>{myTarget ? "Configured" : "Not configured"}</dd>
              </div>
              <div>
                <dt>Delivery</dt>
                <dd>
                  {!myTarget
                    ? "Create a target to enable personal delivery"
                    : myTarget.enabled
                      ? "Active"
                      : "Paused"}
                </dd>
              </div>
            </dl>
            <p className="subtle-text" style={{ marginTop: 10 }}>
              <Link to="/settings/my-notifications">
                Manage my notifications
              </Link>
            </p>
          </>
        )}
      </section>
    </>
  );
}
