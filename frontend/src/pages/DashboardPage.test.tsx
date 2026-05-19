import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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

    expect(screen.getByText("Where should I look right now?")).toBeTruthy();

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
      expect(screen.getByText("Recent failures")).toBeTruthy();
      expect(screen.getByText("Core platform pipelines")).toBeTruthy();
    });
  });

  it("shows empty states when only project data is available", async () => {
    mockedListQueue.mockResolvedValue([]);
    mockedListBuilds.mockResolvedValue([]);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
      expect(
        screen.getByText("No queued or running builds right now."),
      ).toBeTruthy();
      expect(screen.getByText("No recent builds yet.")).toBeTruthy();
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
});
