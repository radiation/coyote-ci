import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import {
  BuildActivityRail,
  QueueActivityPanel,
  RecentBuildsPanel,
} from "./ScopedBuildActivityPanels";
import { listBuilds, listBuildsByJob, listQueue } from "../api";

vi.mock("../api", () => ({
  listBuilds: vi.fn(),
  listBuildsByJob: vi.fn(),
  listQueue: vi.fn(),
}));

function renderWithProviders(ui: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ScopedBuildActivityPanels", () => {
  const mockedListBuilds = vi.mocked(listBuilds);
  const mockedListBuildsByJob = vi.mocked(listBuildsByJob);
  const mockedListQueue = vi.mocked(listQueue);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedListQueue.mockResolvedValue([]);
    mockedListBuilds.mockResolvedValue([]);
    mockedListBuildsByJob.mockResolvedValue([]);
  });

  it("uses project filters for queue and recent builds", async () => {
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-queue-1",
        build_number: 101,
        project_id: "project-1",
        project_name: "Platform",
        priority: 5,
        status: "queued",
        created_at: "2026-05-01T00:00:00Z",
      },
    ]);
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-recent-1",
        build_number: 100,
        project_id: "project-1",
        project_name: "Platform",
        priority: 5,
        status: "failed",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: null,
        started_at: null,
        finished_at: null,
        current_step_index: 0,
        error_message: null,
      },
    ]);

    renderWithProviders(
      <BuildActivityRail scope={{ type: "project", projectId: "project-1" }} />,
    );

    await waitFor(() => {
      expect(mockedListQueue).toHaveBeenCalledWith({ project_id: "project-1" });
      expect(mockedListBuilds).toHaveBeenCalledWith({
        project_id: "project-1",
      });
      expect(screen.getByRole("link", { name: "Build #101" })).toHaveAttribute(
        "href",
        "/builds/build-queue-1",
      );
      expect(screen.getByRole("link", { name: "Build #100" })).toHaveAttribute(
        "href",
        "/builds/build-recent-1",
      );
    });
  });

  it("filters job queue activity from job build history", async () => {
    mockedListBuildsByJob.mockResolvedValue([
      {
        id: "build-queued",
        build_number: 51,
        project_id: "project-1",
        priority: 5,
        status: "queued",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: "2026-05-01T00:00:01Z",
        started_at: null,
        finished_at: null,
        current_step_index: 0,
        error_message: null,
      },
      {
        id: "build-running",
        build_number: 52,
        project_id: "project-1",
        priority: 5,
        status: "running",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: "2026-05-01T00:00:01Z",
        started_at: "2026-05-01T00:00:10Z",
        finished_at: null,
        current_step_index: 0,
        error_message: null,
      },
      {
        id: "build-success",
        build_number: 53,
        project_id: "project-1",
        priority: 5,
        status: "success",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: null,
        started_at: null,
        finished_at: "2026-05-01T00:01:00Z",
        current_step_index: 0,
        error_message: null,
      },
    ]);

    renderWithProviders(
      <QueueActivityPanel scope={{ type: "job", jobId: "job-1" }} />,
    );

    await waitFor(() => {
      expect(mockedListBuildsByJob).toHaveBeenCalledWith("job-1");
      expect(screen.getByRole("link", { name: "Build #51" })).toBeTruthy();
      expect(screen.getByRole("link", { name: "Build #52" })).toBeTruthy();
      expect(screen.queryByRole("link", { name: "Build #53" })).toBeNull();
    });
  });

  it("renders recent builds error state", async () => {
    mockedListBuilds.mockRejectedValue(new Error("backend unavailable"));

    renderWithProviders(<RecentBuildsPanel scope={{ type: "global" }} />);

    await waitFor(() => {
      expect(
        screen.getByText("Failed to load builds: Error: backend unavailable"),
      ).toBeTruthy();
    });
  });
});
