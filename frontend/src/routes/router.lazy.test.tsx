import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { Outlet, RouterProvider, createMemoryRouter } from "react-router-dom";

vi.mock("../components/Layout", () => ({
  Layout() {
    return (
      <div>
        <h1>Test Layout</h1>
        <Outlet />
      </div>
    );
  },
}));

vi.mock("../pages/ProjectDetailPage", () => ({
  ProjectDetailPage() {
    return <h2>Project Detail Mock</h2>;
  },
}));

vi.mock("../pages/BuildsListPage", () => ({
  BuildsListPage() {
    return <h2>Builds Mock</h2>;
  },
}));

vi.mock("../pages/BuildDetailPage", () => ({
  BuildDetailPage() {
    return <h2>Build Detail Mock</h2>;
  },
}));

vi.mock("../pages/WorkersPage", () => ({
  WorkersPage() {
    return <h2>Workers Mock</h2>;
  },
}));

vi.mock("../pages/ArtifactsPage", () => ({
  ArtifactsPage() {
    return <h2>Artifacts Mock</h2>;
  },
}));

vi.mock("../pages/ArtifactLogicalBrowserPage", () => ({
  ArtifactLogicalBrowserPage() {
    return <h2>Artifact Logical Browser Mock</h2>;
  },
}));

vi.mock("../pages/ArtifactDetailPage", () => ({
  ArtifactDetailPage() {
    return <h2>Artifact Detail Mock</h2>;
  },
}));

vi.mock("../pages/JobsListPage", () => ({
  JobsListPage() {
    return <h2>Jobs Mock</h2>;
  },
}));

vi.mock("../pages/JobCreatePage", () => ({
  JobCreatePage() {
    return <h2>Job Create Mock</h2>;
  },
}));

vi.mock("../pages/JobDetailPage", () => ({
  JobDetailPage() {
    return <h2>Job Detail Mock</h2>;
  },
}));

vi.mock("../pages/APITokensPage", () => ({
  APITokensPage() {
    return <h2>My API Tokens Mock</h2>;
  },
}));

vi.mock("../pages/UsersPage", () => ({
  UsersPage() {
    return <h2>Users Mock</h2>;
  },
}));

vi.mock("../pages/CredentialsPage", () => ({
  CredentialsPage() {
    return <h2>Credentials Mock</h2>;
  },
}));

vi.mock("../pages/NotificationsPage", () => ({
  NotificationsPage() {
    return <h2>Notifications Mock</h2>;
  },
}));

vi.mock("../pages/MyNotificationsPage", () => ({
  MyNotificationsPage() {
    return <h2>My Notifications Mock</h2>;
  },
}));

import { appRoutes } from "./router";

function renderRoute(path: string) {
  const router = createMemoryRouter(appRoutes, {
    initialEntries: [path],
  });

  return render(<RouterProvider router={router} />);
}

describe("router lazy route loaders", () => {
  it.each([
    ["/builds", "Builds Mock"],
    ["/builds/build-1", "Build Detail Mock"],
    ["/workers", "Workers Mock"],
    ["/artifacts", "Artifacts Mock"],
    ["/artifacts/logical", "Artifact Logical Browser Mock"],
    ["/artifacts/artifact-1", "Artifact Detail Mock"],
    ["/projects/project-1", "Project Detail Mock"],
    ["/jobs", "Jobs Mock"],
    ["/jobs/new", "Job Create Mock"],
    ["/jobs/job-1", "Job Detail Mock"],
    ["/settings/my-api-tokens", "My API Tokens Mock"],
    ["/settings/my-notifications", "My Notifications Mock"],
    ["/settings/users", "Users Mock"],
    ["/settings/credentials", "Credentials Mock"],
    ["/settings/notifications", "Notifications Mock"],
  ])("loads %s on demand", async (path, heading) => {
    renderRoute(path);

    expect(await screen.findByRole("heading", { name: heading })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Test Layout" })).toBeTruthy();
  });
});
