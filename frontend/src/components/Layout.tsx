import { NavLink, Outlet } from "react-router-dom";
import { useTheme } from "../theme-context";

export function Layout() {
  const { theme, toggleTheme } = useTheme();

  return (
    <div className="app">
      <header className="header">
        <div className="header-row">
          <NavLink to="/" className="logo">
            Coyote CI
          </NavLink>
          <div className="header-actions">
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
              <NavLink
                to="/settings/credentials"
                className={({ isActive }) =>
                  isActive ? "main-nav-link is-active" : "main-nav-link"
                }
              >
                Credentials
              </NavLink>
            </nav>
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
        <Outlet />
      </main>
    </div>
  );
}
