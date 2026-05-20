import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { DashboardPage } from "./DashboardPage";
import { listBuilds, listProjects, listQueue } from "../api";

vi.mock("../api", () => ({
  listProjects: vi.fn(),
  listQueue: vi.fn(),
  listBuilds: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("DashboardPage", () => {
  const mockedListProjects = vi.mocked(listProjects);
  const mockedListQueue = vi.mocked(listQueue);
  const mockedListBuilds = vi.mocked(listBuilds);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedListProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Platform",
        slug: "platform",
        description: "Core platform pipelines",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:00:00Z",
      },
    ]);
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-1",
        build_number: 12,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-1",
        job_name: "main",
        priority: 5,
        status: "running",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: "2026-05-01T00:00:10Z",
        started_at: "2026-05-01T00:00:15Z",
      },
    ]);
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-2",
        build_number: 11,
        priority: 5,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        status: "failed",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: "2026-05-01T00:00:05Z",
        started_at: "2026-05-01T00:00:07Z",
        finished_at: "2026-05-01T00:01:00Z",
        current_step_index: 2,
        error_message: "boom",
        trigger_ref: "main",
      },
    ]);
  });

  it("renders accessible projects and recent activity", async () => {
    renderPage();

    expect(screen.getByText("Dashboard")).toBeTruthy();

    await waitFor(() => {
      const projectLinks = screen.getAllByRole("link", { name: "Platform" });

      expect(projectLinks.length).toBeGreaterThan(0);
      expect(
        projectLinks.some(
          (link) => link.getAttribute("href") === "/projects/project-1",
        ),
      ).toBe(true);
      expect(screen.getByRole("link", { name: "Build #12" })).toHaveAttribute(
        "href",
        "/builds/build-1",
      );
      expect(screen.getByRole("link", { name: "Build #11" })).toHaveAttribute(
        "href",
        "/builds/build-2",
      );
      expect(screen.getByRole("link", { name: "main" })).toHaveAttribute(
        "href",
        "/jobs/job-1",
      );
      expect(screen.getByText("Failures")).toBeTruthy();
      expect(screen.getByText("Core platform pipelines")).toBeTruthy();
    });
  });

  it("shows empty states when only project data is available", async () => {
    mockedListQueue.mockResolvedValue([]);
    mockedListBuilds.mockResolvedValue([]);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
      expect(screen.getByText("No builds in queue.")).toBeTruthy();
      expect(screen.getByText("No recent build activity.")).toBeTruthy();
    });
  });

  it("shows project error state when projects cannot be loaded", async () => {
    mockedListProjects.mockRejectedValue(new Error("no access"));
    mockedListQueue.mockResolvedValue([]);
    mockedListBuilds.mockResolvedValue([]);

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText("Failed to load projects: Error: no access"),
      ).toBeTruthy();
    });
  });

  it("shows the empty projects state and the non-failure summary copy", async () => {
    mockedListProjects.mockResolvedValue([]);
    mockedListQueue.mockResolvedValue([]);
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-3",
        build_number: 9,
        priority: 1,
        project_id: "project-2",
        status: "success",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: null,
        started_at: null,
        finished_at: null,
        current_step_index: 1,
        error_message: null,
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("No projects available yet.")).toBeTruthy();
      expect(screen.getByText("No recent failures.")).toBeTruthy();
    });
  });

  it("shows queue and build error states independently", async () => {
    mockedListQueue.mockRejectedValue(new Error("queue unavailable"));
    mockedListBuilds.mockRejectedValue(new Error("builds unavailable"));

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText("Failed to load queue: Error: queue unavailable"),
      ).toBeTruthy();
      expect(
        screen.getByText("Failed to load builds: Error: builds unavailable"),
      ).toBeTruthy();
      expect(
        screen.getByText("Unable to load queue: Error: queue unavailable"),
      ).toBeTruthy();
      expect(
        screen.getByText(
          "Unable to load recent builds: Error: builds unavailable",
        ),
      ).toBeTruthy();
    });
  });

  it("shows loading states while queries are pending", () => {
    mockedListProjects.mockImplementation(
      () => new Promise(() => undefined) as Promise<never>,
    );
    mockedListQueue.mockImplementation(
      () => new Promise(() => undefined) as Promise<never>,
    );
    mockedListBuilds.mockImplementation(
      () => new Promise(() => undefined) as Promise<never>,
    );

    renderPage();

    expect(screen.getAllByText("Loading…")).toHaveLength(3);
    expect(screen.getByText("Loading projects…")).toBeTruthy();
    expect(screen.getByText("Loading queue…")).toBeTruthy();
    expect(screen.getByText("Loading recent builds…")).toBeTruthy();
  });

  it("sorts recent builds by newest available lifecycle timestamp", async () => {
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-old",
        build_number: 1,
        priority: 1,
        project_id: "project-1",
        project_name: "Platform",
        status: "success",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: "2026-05-01T00:00:05Z",
        started_at: null,
        finished_at: null,
        current_step_index: 1,
        error_message: null,
      },
      {
        id: "build-new",
        build_number: 2,
        priority: 1,
        project_id: "project-1",
        project_name: "Platform",
        status: "success",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: "2026-05-01T00:00:05Z",
        started_at: "2026-05-01T00:02:00Z",
        finished_at: null,
        current_step_index: 1,
        error_message: null,
      },
    ]);

    renderPage();

    await waitFor(() => {
      const recentBuildSection = screen
        .getByRole("heading", { name: "Recent builds" })
        .closest("section");

      expect(recentBuildSection).toBeTruthy();
      const links = within(recentBuildSection as HTMLElement).getAllByRole(
        "link",
        { name: /Build #/ },
      );

      expect(links[0]).toHaveAttribute("href", "/builds/build-new");
      expect(links[1]).toHaveAttribute("href", "/builds/build-old");
    });
  });
});
