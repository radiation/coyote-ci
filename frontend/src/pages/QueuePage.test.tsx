import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { QueuePage } from "./QueuePage";
import { listBuilds, listProjects, listQueue } from "../api";

vi.mock("../api", () => ({
  listBuilds: vi.fn(),
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
  const mockedListProjects = vi.mocked(listProjects);
  const mockedListQueue = vi.mocked(listQueue);

  beforeEach(() => {
    vi.clearAllMocks();
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
  });

  it("shows clean empty states for each operational section", async () => {
    mockedListQueue.mockResolvedValue([]);

    renderPage(["/queue"]);

    await screen.findByText("No running builds.");
    expect(screen.getByText("Nothing is queued.")).toBeTruthy();
    expect(screen.getByText("No recent failures.")).toBeTruthy();
    expect(screen.getByText("No recent completed builds.")).toBeTruthy();
    expect(screen.getByLabelText("Project")).toHaveValue("");
  });

  it("renders running, queued, failed, and completed sections with links", async () => {
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
        id: "build-success-1",
        build_number: 10,
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
    expect(screen.getByRole("link", { name: "Build #10" })).toHaveAttribute(
      "href",
      "/builds/build-success-1",
    );
    expect(screen.getAllByRole("link", { name: "Platform" }).length).toBe(4);
    expect(screen.getByRole("link", { name: "backend-ci" })).toHaveAttribute(
      "href",
      "/jobs/job-1",
    );
    expect(screen.getByRole("link", { name: "release" })).toHaveAttribute(
      "href",
      "/jobs/job-2",
    );
    expect(screen.getByText("Duration 1m 5s")).toBeTruthy();
    expect(screen.getByText("Duration 30s")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Build #9" })).toBeNull();
  });

  it("shows concise section errors", async () => {
    mockedListQueue.mockRejectedValue(new Error("queue down"));
    mockedListBuilds.mockRejectedValue(new Error("builds down"));

    renderPage(["/queue"]);

    await screen.findAllByText("Unable to load.");
    expect(screen.getAllByText("Unable to load.").length).toBe(4);
  });
});
