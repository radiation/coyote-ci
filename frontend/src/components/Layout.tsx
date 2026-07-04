import { Suspense } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { AppShell, type AppShellNavigationItem } from "./AppShell";
import { useAuth } from "../auth-context";
import { useTheme } from "../theme-context";

const primaryNavigation: AppShellNavigationItem[] = [
  { to: "/dashboard", label: "Dashboard" },
  { to: "/projects", label: "Projects" },
  { to: "/jobs", label: "Jobs" },
  { to: "/builds", label: "Builds" },
  { to: "/queue", label: "Queue" },
  { to: "/workers", label: "Workers" },
  { to: "/artifacts", label: "Artifacts" },
];

export function Layout() {
  const { theme, toggleTheme } = useTheme();
  const {
    currentUser,
    authMode,
    authStatus,
    error,
    isGlobalAdmin,
    loginAvailable,
    login,
    logout,
    refreshCurrentUser,
  } = useAuth();
  const showNavigation = authStatus === "authenticated";
  const showUsersLink = authMode === "disabled" || isGlobalAdmin;
  const showTokensLink = authMode !== "disabled";
  const displayName = currentUser?.display_name || currentUser?.email;
  const settingsNavigation: AppShellNavigationItem[] = [
    { to: "/settings/profile", label: "Profile" },
    { to: "/settings/my-notifications", label: "My notifications" },
    { to: "/settings/credentials", label: "Credentials" },
    {
      to: "/settings/notifications",
      label: "Notifications",
      visible: showUsersLink,
    },
    {
      to: "/settings/my-api-tokens",
      label: "My API Tokens",
      visible: showTokensLink,
    },
    { to: "/settings/users", label: "Users", visible: showUsersLink },
  ];

  return (
    <div className="app">
      <header className="header">
        <div className="header-row">
          <NavLink to="/" className="logo">
            Coyote CI
          </NavLink>
          <div className="header-actions">
            {authStatus === "authenticated" && displayName && (
              <div className="identity-chip">
                <span>{displayName}</span>
                {authMode === "oidc" && (
                  <button
                    type="button"
                    className="header-secondary-button"
                    onClick={() => void logout()}
                  >
                    Logout
                  </button>
                )}
              </div>
            )}
            <button
              type="button"
              className="theme-toggle"
              onClick={toggleTheme}
              aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
              title={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
            >
              <span className="theme-toggle-label">Theme</span>
              <span className="theme-toggle-value">{theme}</span>
            </button>
          </div>
        </div>
      </header>
      <main className="main">
        {authStatus === "loading" && (
          <div className="auth-panel-shell">
            <AuthStatePanel
              title="Loading session"
              message="Checking your Coyote CI session."
            />
          </div>
        )}
        {authStatus === "unauthenticated" && (
          <div className="auth-panel-shell">
            <AuthStatePanel
              title={
                authMode === "header"
                  ? "External authentication required"
                  : "Sign in to Coyote CI"
              }
              message={
                authMode === "header"
                  ? "Coyote CI is configured for trusted proxy authentication. Sign in through the configured gateway or proxy, then retry."
                  : "Use your configured identity provider to continue."
              }
              actionLabel={loginAvailable ? "Sign in" : undefined}
              onAction={loginAvailable ? login : undefined}
            />
          </div>
        )}
        {authStatus === "error" && (
          <div className="auth-panel-shell">
            <AuthStatePanel
              title="Unable to load session"
              message={
                error?.message ?? "The current session could not be loaded."
              }
              actionLabel="Retry"
              onAction={() => void refreshCurrentUser()}
            />
          </div>
        )}
        {authStatus === "authenticated" && (
          <AppShell
            primaryNavigation={showNavigation ? primaryNavigation : []}
            settingsNavigation={showNavigation ? settingsNavigation : []}
          >
            <Suspense fallback={<RouteLoadingPanel />}>
              <Outlet />
            </Suspense>
          </AppShell>
        )}
      </main>
    </div>
  );
}

function RouteLoadingPanel() {
  return (
    <section className="panel auth-state-panel">
      <h2>Loading page</h2>
      <p className="subtle-text">Opening the requested Coyote CI view.</p>
    </section>
  );
}

function AuthStatePanel({
  title,
  message,
  actionLabel,
  onAction,
}: {
  title: string;
  message: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <section className="panel auth-state-panel">
      <h2>{title}</h2>
      <p className="subtle-text">{message}</p>
      {actionLabel && onAction && (
        <button type="button" onClick={onAction}>
          {actionLabel}
        </button>
      )}
    </section>
  );
}
