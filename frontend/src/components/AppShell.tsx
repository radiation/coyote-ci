import { NavLink } from "react-router-dom";
import type { ReactNode } from "react";

declare const __COYOTE_VERSION__: string;

export type AppShellNavigationItem = {
  to: string;
  label: string;
  visible?: boolean;
};

export function AppShell({
  primaryNavigation,
  settingsNavigation,
  children,
}: {
  primaryNavigation: AppShellNavigationItem[];
  settingsNavigation: AppShellNavigationItem[];
  children: ReactNode;
}) {
  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="Application navigation">
        <nav className="sidebar-nav" aria-label="Primary">
          <div className="sidebar-section">
            <p className="sidebar-section-label">Overview</p>
            {primaryNavigation.map((item) => (
              <SidebarLink key={item.to} to={item.to} label={item.label} />
            ))}
          </div>
          <div className="sidebar-section">
            <p className="sidebar-section-label">Settings</p>
            {settingsNavigation
              .filter((item) => item.visible ?? true)
              .map((item) => (
                <SidebarLink key={item.to} to={item.to} label={item.label} />
              ))}
          </div>
        </nav>
        <p className="sidebar-version" aria-label="Application version">
          Coyote CI {__COYOTE_VERSION__}
        </p>
      </aside>
      <section className="content-shell">
        <div className="page-container">{children}</div>
      </section>
    </div>
  );
}

function SidebarLink({ to, label }: { to: string; label: string }) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        isActive ? "sidebar-link is-active" : "sidebar-link"
      }
    >
      {label}
    </NavLink>
  );
}
