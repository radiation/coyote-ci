import { Link, matchPath, useLocation } from "react-router-dom";
import type { ReactNode } from "react";

declare const __COYOTE_VERSION__: string;

type NavigationItem = {
  to: string;
  label: string;
  visible?: boolean;
};

export type AppShellNavigationSection = NavigationItem & {
  children?: NavigationItem[];
};

export function AppShell({
  primaryNavigation,
  children,
}: {
  primaryNavigation: AppShellNavigationSection[];
  children: ReactNode;
}) {
  const { pathname } = useLocation();
  const matches = (item: NavigationItem) =>
    Boolean(matchPath({ path: item.to, end: false }, pathname));
  const active = primaryNavigation.find(
    (section) => matches(section) || section.children?.some(matches),
  );
  const current = (item: NavigationItem) =>
    matchPath(item.to, pathname) ? "page" : "location";
  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="Application navigation">
        <div className="sidebar-nav">
          <nav className="sidebar-section" aria-label="Primary">
            {primaryNavigation.map((item) => (
              <SidebarLink
                key={item.to}
                item={item}
                current={
                  item === active
                    ? item.children
                      ? "location"
                      : current(item)
                    : undefined
                }
              />
            ))}
          </nav>
          {active?.children && (
            <nav
              className="sidebar-section"
              aria-label={`${active.label} views`}
            >
              <p className="sidebar-section-label">{active.label}</p>
              {active.children
                .filter((item) => item.visible !== false)
                .map((item) => (
                  <SidebarLink
                    key={item.to}
                    item={item}
                    current={matches(item) ? current(item) : undefined}
                  />
                ))}
            </nav>
          )}
        </div>
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

function SidebarLink({
  item,
  current,
}: {
  item: NavigationItem;
  current?: "location" | "page";
}) {
  return (
    <Link
      to={item.to}
      aria-current={current}
      className={current ? "sidebar-link is-active" : "sidebar-link"}
    >
      {item.label}
    </Link>
  );
}
