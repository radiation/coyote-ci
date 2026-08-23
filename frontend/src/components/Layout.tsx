import { Suspense } from "react";
import { Navigate, NavLink, Outlet, useLocation } from "react-router-dom";
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

const publicNavigation: AppShellNavigationItem[] = [
  { to: "/projects", label: "Projects" },
];

function isPublicRoute(pathname: string): boolean {
  return (
    pathname === "/projects" ||
    /^\/projects\/[^/]+(?:\/builds\/[^/]+)?$/.test(pathname)
  );
}

export function Layout() {
  const location = useLocation();
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
  const isAnonymous = authStatus === "unauthenticated";
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
            {isAnonymous && loginAvailable && (
              <button
                type="button"
                className="header-secondary-button"
                onClick={login}
              >
                Sign in
              </button>
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
        {isAnonymous && !isPublicRoute(location.pathname) && (
          <Navigate to="/projects" replace />
        )}
        {(authStatus === "authenticated" ||
          (isAnonymous && isPublicRoute(location.pathname))) && (
          <AppShell
            primaryNavigation={
              showNavigation ? primaryNavigation : publicNavigation
            }
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
