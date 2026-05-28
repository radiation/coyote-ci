import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { QueuePage } from "./QueuePage";
import {
  cancelBuild,
  listBuilds,
  listJobsByProject,
  listProjects,
  listQueue,
} from "../api";

vi.mock("../api", () => ({
  cancelBuild: vi.fn(),
  listBuilds: vi.fn(),
  listJobsByProject: vi.fn(),
  listProjects: vi.fn(),
  listQueue: vi.fn(),
}));

function renderPage(initialEntries: string[] = ["/"]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={initialEntries}>
        <QueuePage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("QueuePage", () => {
  const mockedListBuilds = vi.mocked(listBuilds);
  const mockedCancelBuild = vi.mocked(cancelBuild);
  const mockedListJobsByProject = vi.mocked(listJobsByProject);
  const mockedListProjects = vi.mocked(listProjects);
  const mockedListQueue = vi.mocked(listQueue);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedCancelBuild.mockResolvedValue({
      id: "build-1",
      build_number: 12,
      attempt_number: 1,
      project_id: "project-1",
      priority: 9,
      status: "canceled",
      created_at: "2026-03-24T00:00:00Z",
      queued_at: "2026-03-24T00:00:01Z",
      started_at: null,
      finished_at: "2026-03-24T00:00:30Z",
      current_step_index: 0,
      error_message: "build canceled by operator request",
    });
    mockedListProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Platform",
        slug: "platform",
        description: "Core platform pipelines",
        created_at: "2026-03-24T00:00:00Z",
        updated_at: "2026-03-24T00:00:00Z",
      },
    ]);
    mockedListBuilds.mockResolvedValue([]);
    mockedListJobsByProject.mockResolvedValue([]);
  });

  it("shows clean empty states for each operational section", async () => {
    mockedListQueue.mockResolvedValue([]);

    renderPage(["/queue"]);

    await screen.findByText("No running builds.");
    expect(screen.getByText("Nothing is queued.")).toBeTruthy();
    expect(screen.getByText("No recent failures.")).toBeTruthy();
    expect(screen.getByText("No recent terminal builds.")).toBeTruthy();
    expect(screen.getByLabelText("Project")).toHaveValue("");
  });

  it("renders running, queued, failed, and terminal sections with links", async () => {
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-running-1",
        build_number: 13,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-2",
        job_name: "release",
        priority: 8,
        status: "running",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:01Z",
        started_at: "2026-03-24T00:01:00Z",
        worker_id: null,
        lease_expires_at: null,
        repository_url: "https://github.com/example/backend.git",
        trigger_ref: "main",
        source_commit_sha: "def1234567890",
        trigger_commit_sha: null,
      },
      {
        build_id: "build-1",
        build_number: 12,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-1",
        job_name: "backend-ci",
        priority: 9,
        status: "queued",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:01Z",
        started_at: null,
        worker_id: null,
        lease_expires_at: null,
        repository_url: "https://github.com/example/backend.git",
        trigger_ref: "main",
        source_commit_sha: "abc1234567890",
        trigger_commit_sha: null,
      },
    ]);
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-failed-1",
        build_number: 11,
        attempt_number: 1,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-3",
        job_name: "deploy",
        priority: 7,
        status: "failed",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:05Z",
        started_at: "2026-03-24T00:01:00Z",
        finished_at: "2026-03-24T00:02:05Z",
        current_step_index: 0,
        error_message: "boom",
      },
      {
        id: "build-canceled-1",
        build_number: 14,
        attempt_number: 1,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-6",
        job_name: "package",
        priority: 6,
        status: "canceled",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:08Z",
        started_at: "2026-03-24T00:01:15Z",
        finished_at: "2026-03-24T00:03:00Z",
        current_step_index: 0,
        error_message: "build canceled by operator request",
      },
      {
        id: "build-success-1",
        build_number: 10,
        attempt_number: 1,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-4",
        job_name: "test",
        priority: 6,
        status: "success",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:10Z",
        started_at: "2026-03-24T00:01:10Z",
        finished_at: "2026-03-24T00:01:40Z",
        current_step_index: 0,
        error_message: null,
      },
      {
        id: "build-ignored-1",
        build_number: 9,
        attempt_number: 1,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-5",
        job_name: "queued-again",
        priority: 5,
        status: "queued",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:20Z",
        started_at: null,
        finished_at: null,
        current_step_index: 0,
        error_message: null,
      },
    ]);

    renderPage(["/queue?project_id=project-1&status=queued"]);

    await screen.findByRole("link", { name: "Build #13" });
    expect(mockedListQueue).toHaveBeenCalledWith({
      project_id: "project-1",
    });
    expect(mockedListBuilds).toHaveBeenCalledWith({
      project_id: "project-1",
      limit: 24,
    });
    expect(screen.getByRole("link", { name: "Build #13" })).toHaveAttribute(
      "href",
      "/builds/build-running-1",
    );
    expect(screen.getByRole("link", { name: "Build #12" })).toHaveAttribute(
      "href",
      "/builds/build-1",
    );
    expect(screen.getByRole("link", { name: "Build #11" })).toHaveAttribute(
      "href",
      "/builds/build-failed-1",
    );
    expect(screen.getByRole("link", { name: "Build #14" })).toHaveAttribute(
      "href",
      "/builds/build-canceled-1",
    );
    expect(screen.getByRole("link", { name: "Build #10" })).toHaveAttribute(
      "href",
      "/builds/build-success-1",
    );
    expect(screen.getAllByRole("link", { name: "Platform" }).length).toBe(5);
    expect(screen.getByRole("link", { name: "backend-ci" })).toHaveAttribute(
      "href",
      "/jobs/job-1",
    );
    expect(screen.getByRole("link", { name: "release" })).toHaveAttribute(
      "href",
      "/jobs/job-2",
    );
    expect(screen.getAllByRole("button", { name: "Cancel" }).length).toBe(2);
    expect(screen.getByText("Canceled")).toBeTruthy();
    expect(screen.getByText("Duration 1m 5s")).toBeTruthy();
    expect(screen.getByText("Duration 30s")).toBeTruthy();
    const terminalSection = screen
      .getByRole("heading", { name: "Recent terminal" })
      .closest("section");
    expect(terminalSection).toBeTruthy();
    expect(
      within(terminalSection as HTMLElement)
        .getAllByRole("link", { name: /^Build #/ })
        .map((link) => link.textContent),
    ).toEqual(["Build #14", "Build #10"]);
    expect(screen.queryByRole("link", { name: "Build #9" })).toBeNull();
  });

  it("confirms and cancels queued builds", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-1",
        build_number: 12,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-1",
        job_name: "backend-ci",
        priority: 9,
        status: "queued",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:01Z",
        started_at: null,
      },
    ]);

    renderPage(["/queue"]);

    const cancelButton = await screen.findByRole("button", { name: "Cancel" });
    fireEvent.click(cancelButton);

    await waitFor(() => {
      expect(confirmSpy).toHaveBeenCalledWith("Cancel Build #12?");
      expect(mockedCancelBuild).toHaveBeenCalledWith("build-1");
    });

    confirmSpy.mockRestore();
  });

  it("shows concise section errors", async () => {
    mockedListQueue.mockRejectedValue(new Error("queue down"));
    mockedListBuilds.mockRejectedValue(new Error("builds down"));

    renderPage(["/queue"]);

    await screen.findAllByText("Unable to load.");
    expect(screen.getAllByText("Unable to load.").length).toBe(4);
  });

  it("hydrates missing job names from project jobs across queue sections", async () => {
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-running-2",
        build_number: 21,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-release",
        priority: 8,
        status: "running",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:01Z",
        started_at: "2026-03-24T00:01:00Z",
      },
    ]);
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-failed-2",
        build_number: 20,
        attempt_number: 1,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-release",
        priority: 7,
        status: "failed",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:05Z",
        started_at: "2026-03-24T00:01:00Z",
        finished_at: "2026-03-24T00:02:05Z",
        current_step_index: 0,
        error_message: "boom",
      },
    ]);
    mockedListJobsByProject.mockResolvedValue([
      {
        id: "job-release",
        project_id: "project-1",
        name: "release",
        priority: 5,
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: true,
        push_branch: "main",
        pipeline_yaml: "version: 1",
        managed_image: null,
        enabled: true,
        created_at: "2026-03-24T00:00:00Z",
        updated_at: "2026-03-24T00:00:00Z",
      },
    ]);

    renderPage(["/queue"]);

    await screen.findByRole("link", { name: "Build #21" });
    await waitFor(() => {
      expect(mockedListJobsByProject).toHaveBeenCalledWith("project-1");
      expect(screen.getAllByRole("link", { name: "release" }).length).toBe(2);
      expect(screen.queryByRole("link", { name: "job-release" })).toBeNull();
    });
  });
});
