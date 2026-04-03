import { Link, Outlet } from 'react-router-dom';

export function Layout() {
  return (
    <div className="app">
      <header className="header">
        <div className="header-row">
          <Link to="/" className="logo">Coyote CI</Link>
          <nav className="main-nav" aria-label="Primary">
            <Link to="/jobs">Jobs</Link>
            <Link to="/builds">Build Attempts</Link>
          </nav>
        </div>
      </header>
      <main className="main">
        <Outlet />
      </main>
    </div>
  );
}
