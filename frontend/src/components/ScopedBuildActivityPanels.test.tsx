import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import {
  BuildActivityRail,
  QueueActivityPanel,
  RecentBuildsPanel,
} from "./ScopedBuildActivityPanels";
import {
  listBuilds,
  listBuildsByJob,
  listJobsByProject,
  listQueue,
} from "../api";

vi.mock("../api", () => ({
  listBuilds: vi.fn(),
  listBuildsByJob: vi.fn(),
  listJobsByProject: vi.fn(),
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
  const mockedListJobsByProject = vi.mocked(listJobsByProject);
  const mockedListQueue = vi.mocked(listQueue);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedListQueue.mockResolvedValue([]);
    mockedListBuilds.mockResolvedValue([]);
    mockedListBuildsByJob.mockResolvedValue([]);
    mockedListJobsByProject.mockResolvedValue([]);
  });

  it("uses project filters for queue and recent builds", async () => {
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-queue-1",
        build_number: 101,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-queue-1",
        job_name: "release",
        priority: 5,
        status: "queued",
        created_at: "2026-05-01T00:00:00Z",
      },
    ]);
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-recent-1",
        build_number: 100,
        attempt_number: 1,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-recent-1",
        job_name: "release-history",
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
        limit: 6,
      });
      expect(mockedListJobsByProject).toHaveBeenCalledWith("project-1");
      expect(screen.getByRole("link", { name: "Build #101" })).toHaveAttribute(
        "href",
        "/builds/build-queue-1",
      );
      expect(screen.getByRole("link", { name: "Build #100" })).toHaveAttribute(
        "href",
        "/builds/build-recent-1",
      );
      expect(screen.getByRole("link", { name: "release" })).toHaveAttribute(
        "href",
        "/jobs/job-queue-1",
      );
      expect(
        screen.getByRole("link", { name: "release-history" }),
      ).toHaveAttribute("href", "/jobs/job-recent-1");
      expect(screen.queryByRole("link", { name: "Platform" })).toBeNull();
    });
  });

  it("hydrates missing project-scoped job names from project jobs", async () => {
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-queue-2",
        build_number: 102,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-release",
        priority: 5,
        status: "queued",
        created_at: "2026-05-01T00:00:00Z",
      },
    ]);
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-recent-2",
        build_number: 99,
        attempt_number: 1,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-release",
        priority: 5,
        status: "success",
        created_at: "2026-05-01T00:00:00Z",
        queued_at: null,
        started_at: null,
        finished_at: null,
        current_step_index: 0,
        error_message: null,
      },
    ]);
    mockedListJobsByProject.mockResolvedValue([
      {
        id: "job-release",
        project_id: "project-1",
        name: "release",
        priority: 5,
        repository_url: "https://github.com/example/repo.git",
        default_ref: "main",
        push_enabled: true,
        pull_request_enabled: false,
        push_branch: "main",
        pipeline_yaml: "version: 1",
        managed_image: null,
        enabled: true,
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:00:00Z",
      },
    ]);

    renderWithProviders(
      <BuildActivityRail scope={{ type: "project", projectId: "project-1" }} />,
    );

    await waitFor(() => {
      expect(screen.getAllByRole("link", { name: "release" })).toHaveLength(2);
      expect(screen.queryByRole("link", { name: /job-release/i })).toBeNull();
    });
  });

  it("shows project and job context at global scope", async () => {
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-queue-global",
        build_number: 201,
        project_id: "project-2",
        project_name: "Payments",
        job_id: "job-global-1",
        job_name: "nightly",
        priority: 5,
        status: "running",
        created_at: "2026-05-01T00:00:00Z",
      },
    ]);

    renderWithProviders(<QueueActivityPanel scope={{ type: "global" }} />);

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Payments" })).toHaveAttribute(
        "href",
        "/projects/project-2",
      );
      expect(screen.getByRole("link", { name: "nightly" })).toHaveAttribute(
        "href",
        "/jobs/job-global-1",
      );
    });
  });

  it("hydrates missing global recent-build job names from project jobs", async () => {
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-global-recent-1",
        build_number: 202,
        attempt_number: 1,
        project_id: "project-2",
        project_name: "Payments",
        job_id: "job-global-release",
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
    mockedListJobsByProject.mockResolvedValue([
      {
        id: "job-global-release",
        project_id: "project-2",
        name: "release",
        priority: 5,
        repository_url: "https://github.com/example/repo.git",
        default_ref: "main",
        push_enabled: true,
        pull_request_enabled: false,
        push_branch: "main",
        pipeline_yaml: "version: 1",
        managed_image: null,
        enabled: true,
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:00:00Z",
      },
    ]);

    renderWithProviders(<RecentBuildsPanel scope={{ type: "global" }} />);

    await waitFor(() => {
      expect(mockedListJobsByProject).toHaveBeenCalledWith("project-2");
      expect(screen.getByRole("link", { name: "Payments" })).toHaveAttribute(
        "href",
        "/projects/project-2",
      );
      expect(screen.getByRole("link", { name: "release" })).toHaveAttribute(
        "href",
        "/jobs/job-global-release",
      );
      expect(screen.queryByText(/Job job-glob/i)).toBeNull();
    });
  });

  it("filters job queue activity from job build history", async () => {
    mockedListBuildsByJob.mockResolvedValue([
      {
        id: "build-queued",
        build_number: 51,
        attempt_number: 1,
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
        attempt_number: 1,
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
        attempt_number: 1,
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
      expect(screen.queryByRole("link", { name: /project-/i })).toBeNull();
      expect(screen.queryByText(/^Job /i)).toBeNull();
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

  it("supports custom poll interval override", async () => {
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-queue-fast",
        build_number: 301,
        project_id: "project-1",
        project_name: "Platform",
        priority: 5,
        status: "running",
        created_at: "2026-05-01T00:00:00Z",
      },
    ]);

    renderWithProviders(
      <QueueActivityPanel scope={{ type: "global" }} pollInterval={20} />,
    );

    await waitFor(() => {
      expect(mockedListQueue).toHaveBeenCalledTimes(1);
    });

    await waitFor(
      () => {
        expect(mockedListQueue.mock.calls.length).toBeGreaterThan(1);
      },
      { timeout: 1200 },
    );
  });
});
