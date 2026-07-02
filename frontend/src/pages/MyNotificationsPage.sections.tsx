import type {
  ResolvedSlackIdentityCandidate,
  SlackIdentityWorkspace,
  UserSlackIdentity,
} from "../types/notification";

interface MyNotificationsSlackSectionProps {
  actionErrorMessage: string | null;
  loading: boolean;
  errorMessage: string | null;
  workspaceStatus: string | undefined;
  workspace: SlackIdentityWorkspace | null;
  linkedIdentity: UserSlackIdentity | null;
  currentUserEmail: string;
  candidate: ResolvedSlackIdentityCandidate | null;
  noMatchMessage: string | null;
  noMatchWorkspaceID: string | null;
  resolvePending: boolean;
  createPending: boolean;
  patchPending: boolean;
  deletePending: boolean;
  onResolve: () => void;
  onConfirmCandidate: (candidate: ResolvedSlackIdentityCandidate) => void;
  onCancelCandidate: () => void;
  onToggleIdentity: (identity: UserSlackIdentity) => void;
  onUnlinkIdentity: () => void;
}

export function MyNotificationsSlackSection({
  actionErrorMessage,
  loading,
  errorMessage,
  workspaceStatus,
  workspace,
  linkedIdentity,
  currentUserEmail,
  candidate,
  noMatchMessage,
  noMatchWorkspaceID,
  resolvePending,
  createPending,
  patchPending,
  deletePending,
  onResolve,
  onConfirmCandidate,
  onCancelCandidate,
  onToggleIdentity,
  onUnlinkIdentity,
}: MyNotificationsSlackSectionProps) {
  const workspaceName =
    workspace?.name?.trim() || workspace?.slack_workspace_id || "Slack";
  const showWorkspaceHealthWarning = workspace?.last_test_succeeded === false;
  const visibleCandidate =
    workspaceStatus === "ready" &&
    !linkedIdentity &&
    candidate &&
    candidate.workspace.id === workspace?.id
      ? candidate
      : null;
  const visibleNoMatchMessage =
    workspaceStatus === "ready" &&
    !linkedIdentity &&
    noMatchWorkspaceID === (workspace?.id ?? null)
      ? noMatchMessage
      : null;

  return (
    <section className="settings-panel" style={{ marginTop: 14 }}>
      <h3>Personal Slack</h3>
      <p className="subtle-text">
        Link your account to a stable Slack member identity in the connected
        workspace. This does not enable Slack delivery by itself.
      </p>
      {actionErrorMessage && <p className="error-text">{actionErrorMessage}</p>}
      {renderSlackContent({
        loading,
        errorMessage,
        workspaceStatus,
        workspaceName,
        linkedIdentity,
        currentUserEmail,
        visibleCandidate,
        visibleNoMatchMessage,
        showWorkspaceHealthWarning,
        resolvePending,
        createPending,
        patchPending,
        deletePending,
        onResolve,
        onConfirmCandidate,
        onCancelCandidate,
        onToggleIdentity,
        onUnlinkIdentity,
      })}
    </section>
  );
}

function renderSlackContent({
  loading,
  errorMessage,
  workspaceStatus,
  workspaceName,
  linkedIdentity,
  currentUserEmail,
  visibleCandidate,
  visibleNoMatchMessage,
  showWorkspaceHealthWarning,
  resolvePending,
  createPending,
  patchPending,
  deletePending,
  onResolve,
  onConfirmCandidate,
  onCancelCandidate,
  onToggleIdentity,
  onUnlinkIdentity,
}: {
  loading: boolean;
  errorMessage: string | null;
  workspaceStatus: string | undefined;
  workspaceName: string;
  linkedIdentity: UserSlackIdentity | null;
  currentUserEmail: string;
  visibleCandidate: ResolvedSlackIdentityCandidate | null;
  visibleNoMatchMessage: string | null;
  showWorkspaceHealthWarning: boolean;
  resolvePending: boolean;
  createPending: boolean;
  patchPending: boolean;
  deletePending: boolean;
  onResolve: () => void;
  onConfirmCandidate: (candidate: ResolvedSlackIdentityCandidate) => void;
  onCancelCandidate: () => void;
  onToggleIdentity: (identity: UserSlackIdentity) => void;
  onUnlinkIdentity: () => void;
}) {
  if (loading) {
    return <p>Loading Slack identity...</p>;
  }

  if (errorMessage) {
    return <p className="error-text">{errorMessage}</p>;
  }

  if (workspaceStatus === "not_configured") {
    return (
      <p className="subtle-text">
        Slack is not connected for this Coyote instance. Ask an administrator to
        connect a workspace.
      </p>
    );
  }

  if (workspaceStatus === "disabled") {
    return (
      <p className="subtle-text">
        {workspaceName} is connected, but personal Slack linking is currently
        disabled. Ask an administrator to re-enable the workspace.
      </p>
    );
  }

  if (linkedIdentity) {
    return (
      <>
        <div className="my-notifications-target-summary">
          <div>
            <p className="subtle-text my-notifications-detail-label">
              Workspace
            </p>
            <p className="my-notifications-detail-value">{workspaceName}</p>
          </div>
          <div>
            <p className="subtle-text my-notifications-detail-label">
              Slack account
            </p>
            <p className="my-notifications-detail-value">
              {linkedIdentity.display_name ||
                linkedIdentity.real_name ||
                linkedIdentity.handle ||
                linkedIdentity.slack_user_id}
              {linkedIdentity.handle
                ? ` (@${linkedIdentity.handle.replace(/^@+/, "")})`
                : ""}
            </p>
          </div>
          <div>
            <p className="subtle-text my-notifications-detail-label">Status</p>
            <p className="my-notifications-detail-value">
              <span
                className={
                  linkedIdentity.enabled
                    ? "my-notifications-status is-enabled"
                    : "my-notifications-status is-paused"
                }
              >
                {linkedIdentity.enabled ? "Linked" : "Paused"}
              </span>
            </p>
          </div>
        </div>
        {showWorkspaceHealthWarning && (
          <p className="subtle-text" style={{ marginTop: 10 }}>
            The last admin Slack connection test failed. Your linked identity is
            still saved, but an administrator may need to refresh the workspace
            credentials if future lookups fail.
          </p>
        )}
        <p className="subtle-text" style={{ marginTop: 10 }}>
          This link identifies your Slack account for personal notifications.
          Delivery still depends on your enabled preferences and Slack workspace
          availability.
        </p>
        <div className="button-row" style={{ marginTop: 10 }}>
          <button
            className={
              linkedIdentity.enabled
                ? "secondary-button danger-button"
                : "secondary-button"
            }
            type="button"
            onClick={() => onToggleIdentity(linkedIdentity)}
            disabled={patchPending}
          >
            {patchPending
              ? "Saving..."
              : linkedIdentity.enabled
                ? "Pause Slack link"
                : "Enable Slack link"}
          </button>
          <button
            className="secondary-button danger-button"
            type="button"
            onClick={onUnlinkIdentity}
            disabled={deletePending}
          >
            {deletePending ? "Unlinking..." : "Unlink Slack account"}
          </button>
        </div>
      </>
    );
  }

  return (
    <>
      <div className="my-notifications-target-summary">
        <div>
          <p className="subtle-text my-notifications-detail-label">Workspace</p>
          <p className="my-notifications-detail-value">{workspaceName}</p>
        </div>
        <div>
          <p className="subtle-text my-notifications-detail-label">
            Matching email
          </p>
          <p className="my-notifications-email">{currentUserEmail}</p>
        </div>
      </div>
      <p className="subtle-text" style={{ marginTop: 10 }}>
        Coyote stores Slack’s stable member ID after you confirm the account
        match.
      </p>
      {showWorkspaceHealthWarning && (
        <p className="subtle-text" style={{ marginTop: 10 }}>
          The last admin Slack connection test failed. You can still try a live
          lookup now; the lookup result is the current source of truth.
        </p>
      )}
      {!visibleCandidate && (
        <button
          className="secondary-button"
          type="button"
          onClick={onResolve}
          disabled={resolvePending}
        >
          {resolvePending ? "Finding..." : "Find my Slack account"}
        </button>
      )}
      {visibleNoMatchMessage && (
        <p className="subtle-text" style={{ marginTop: 10 }}>
          {visibleNoMatchMessage}
        </p>
      )}
      {visibleCandidate && (
        <div style={{ marginTop: 14 }}>
          <h4 style={{ marginBottom: 8 }}>Is this your Slack account?</h4>
          <div className="my-notifications-target-summary">
            <div>
              <p className="subtle-text my-notifications-detail-label">
                Display name
              </p>
              <p className="my-notifications-detail-value">
                {visibleCandidate.display_name || "Unavailable"}
              </p>
            </div>
            <div>
              <p className="subtle-text my-notifications-detail-label">
                Real name
              </p>
              <p className="my-notifications-detail-value">
                {visibleCandidate.real_name || "Unavailable"}
              </p>
            </div>
            <div>
              <p className="subtle-text my-notifications-detail-label">
                Handle
              </p>
              <p className="my-notifications-detail-value">
                {visibleCandidate.handle
                  ? `@${visibleCandidate.handle.replace(/^@+/, "")}`
                  : "Unavailable"}
              </p>
            </div>
          </div>
          <div className="button-row" style={{ marginTop: 10 }}>
            <button
              className="secondary-button"
              type="button"
              onClick={() => onConfirmCandidate(visibleCandidate)}
              disabled={createPending}
            >
              {createPending ? "Linking..." : "Link this Slack account"}
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={onCancelCandidate}
              disabled={createPending}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </>
  );
}
