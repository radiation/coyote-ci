import { NavLink, Outlet } from "react-router-dom";
import { useAuth } from "../auth-context";
import { useTheme } from "../theme-context";

export function Layout() {
  const { theme, toggleTheme } = useTheme();
  const {
    currentUser,
    authMode,
    authStatus,
    error,
    isGlobalAdmin,
    login,
    logout,
    refreshCurrentUser,
  } = useAuth();
  const showNavigation = authStatus === "authenticated";
  const showUsersLink = authMode === "disabled" || isGlobalAdmin;
  const displayName = currentUser?.display_name || currentUser?.email;

  return (
    <div className="app">
      <header className="header">
        <div className="header-row">
          <NavLink to="/" className="logo">
            Coyote CI
          </NavLink>
          <div className="header-actions">
            {showNavigation && (
              <nav className="main-nav" aria-label="Primary">
                <NavLink
                  to="/projects"
                  className={({ isActive }) =>
                    isActive ? "main-nav-link is-active" : "main-nav-link"
                  }
                >
                  Projects
                </NavLink>
                <NavLink
                  to="/jobs"
                  className={({ isActive }) =>
                    isActive ? "main-nav-link is-active" : "main-nav-link"
                  }
                >
                  Jobs
                </NavLink>
                <NavLink
                  to="/builds"
                  className={({ isActive }) =>
                    isActive ? "main-nav-link is-active" : "main-nav-link"
                  }
                >
                  Builds
                </NavLink>
                <NavLink
                  to="/queue"
                  className={({ isActive }) =>
                    isActive ? "main-nav-link is-active" : "main-nav-link"
                  }
                >
                  Queue
                </NavLink>
                <NavLink
                  to="/artifacts"
                  className={({ isActive }) =>
                    isActive ? "main-nav-link is-active" : "main-nav-link"
                  }
                >
                  Artifacts
                </NavLink>
                {showUsersLink && (
                  <NavLink
                    to="/settings/users"
                    className={({ isActive }) =>
                      isActive ? "main-nav-link is-active" : "main-nav-link"
                    }
                  >
                    Users
                  </NavLink>
                )}
                <NavLink
                  to="/settings/credentials"
                  className={({ isActive }) =>
                    isActive ? "main-nav-link is-active" : "main-nav-link"
                  }
                >
                  Credentials
                </NavLink>
              </nav>
            )}
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
          <AuthStatePanel
            title="Loading session"
            message="Checking your Coyote CI session."
          />
        )}
        {authStatus === "unauthenticated" && (
          <AuthStatePanel
            title="Sign in to Coyote CI"
            message="Use your configured identity provider to continue."
            actionLabel="Sign in"
            onAction={login}
          />
        )}
        {authStatus === "error" && (
          <AuthStatePanel
            title="Unable to load session"
            message={
              error?.message ?? "The current session could not be loaded."
            }
            actionLabel="Retry"
            onAction={() => void refreshCurrentUser()}
          />
        )}
        {authStatus === "authenticated" && <Outlet />}
      </main>
    </div>
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
