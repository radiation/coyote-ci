import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import type { QueueEntry } from "../types/build";
import { formatTime } from "../utils/time";
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

function HistoryControls() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output data-testid="url">
        {location.pathname}
        {location.search}
        {location.hash}
      </output>
      <button onClick={() => navigate(-1)}>Back</button>
      <button onClick={() => navigate(1)}>Forward</button>
    </>
  );
}

function entry(
  number: number,
  status: QueueEntry["status"] = "queued",
): QueueEntry {
  return {
    build_id: `long-build-id-${number}`,
    build_number: number,
    status,
    project_id: "project-1",
    priority: 0,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function renderPage(initialEntries: string[] = ["/"]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  const view = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={initialEntries}>
        <QueuePage />
        <HistoryControls />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...view, queryClient };
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
      application_version: null,
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
        application_version: null,
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
        application_version: null,
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
        application_version: null,
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
        application_version: null,
      },
    ]);

    renderPage(["/queue?project_id=project-1"]);

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
      .getByRole("heading", { name: "Other recent completed builds" })
      .closest("section");
    expect(terminalSection).toBeTruthy();
    expect(
      within(terminalSection as HTMLElement)
        .getAllByRole("link", { name: /^Build #/ })
        .map((link) => link.textContent),
    ).toEqual(["Build #14", "Build #10"]);
    expect(screen.queryByRole("link", { name: "Build #9" })).toBeNull();
    expect(screen.getByText(/From up to 24 recent builds/)).toBeVisible();
    expect(
      screen.getByRole("link", { name: "View build history" }),
    ).toHaveAttribute("href", "/builds?project_id=project-1");
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
    confirmSpy.mockReturnValueOnce(false);
    fireEvent.click(cancelButton);
    expect(mockedCancelBuild).not.toHaveBeenCalled();
    expect(cancelButton).toHaveFocus();
    mockedListQueue.mockResolvedValue([]);
    fireEvent.click(cancelButton);
    fireEvent.click(cancelButton);

    await waitFor(() => {
      expect(confirmSpy).toHaveBeenCalledWith("Cancel Build #12?");
      expect(mockedCancelBuild).toHaveBeenCalledWith("build-1");
    });

    await screen.findByText("Build #12 canceled.");
    expect(screen.getByRole("heading", { name: "Active work" })).toHaveFocus();
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
    expect(mockedCancelBuild).toHaveBeenCalledTimes(1);
    confirmSpy.mockRestore();
  });

  it("shows concise section errors", async () => {
    mockedListQueue.mockRejectedValue(new Error("queue down"));
    mockedListBuilds.mockRejectedValue(new Error("builds down"));

    renderPage(["/queue"]);

    await screen.findByText(/Unable to load active work/);
    expect(screen.getByText(/Unable to load recent outcomes/)).toHaveAttribute(
      "role",
      "alert",
    );
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
        application_version: null,
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
        pull_request_enabled: false,
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
      expect(mockedListJobsByProject).toHaveBeenCalledTimes(1);
    });
  });

  it("reveals each snapshot independently in returned order and retains depth across refresh", async () => {
    const entries = Array.from({ length: 14 }, (_, index) => entry(18 - index));
    const running = Array.from({ length: 10 }, (_, index) =>
      entry(60 - index, "running"),
    );
    mockedListQueue.mockResolvedValue([...entries, ...running]);
    const { queryClient } = renderPage();
    await screen.findByText("Showing 8 of 14 queued builds");
    expect(screen.getByText("Showing 8 of 10 running builds")).toBeVisible();
    const section = within(
      screen
        .getByRole("heading", { name: "Queued builds" })
        .closest("section")!,
    );
    fireEvent.click(section.getByRole("button", { name: /Show more/ }));
    expect(
      section
        .getAllByRole("link", { name: /^Build #/ })
        .map((link) => link.textContent),
    ).toEqual(entries.map((item) => `Build #${item.build_number}`));
    fireEvent.click(screen.getByRole("button", { name: /Show more running/ }));
    expect(screen.getByText("Showing 10 of 10 running builds")).toBeVisible();
    expect(mockedListQueue).toHaveBeenCalledTimes(1);
    mockedListQueue.mockRejectedValue(new Error("offline"));
    await queryClient.invalidateQueries({ queryKey: ["queue"] });
    await screen.findByText(/Showing last known results/);
    expect(section.getByText("Showing 14 of 14 queued builds")).toBeVisible();
    expect(screen.getByText("No recent failures.")).toBeVisible();
    expect(screen.getAllByText(/Last updated/)).toHaveLength(2);
    fireEvent.change(screen.getByLabelText("Active work status"), {
      target: { value: "queued" },
    });
    fireEvent.click(screen.getByText("Back"));
    await screen.findByText("Showing 8 of 14 queued builds");
  });

  it("round-trips URL filters through history and reload without refetching recent builds on status changes", async () => {
    mockedListQueue.mockResolvedValue([]);
    const view = renderPage(["/queue?keep=yes&status=invalid#anchor"]);
    await screen.findByText("No running builds.");
    expect(mockedListQueue).toHaveBeenLastCalledWith({
      project_id: undefined,
      status: undefined,
    });
    fireEvent.change(screen.getByLabelText("Active work status"), {
      target: { value: "running" },
    });
    await waitFor(() =>
      expect(mockedListQueue).toHaveBeenLastCalledWith({
        project_id: undefined,
        status: "running",
      }),
    );
    expect(mockedListBuilds).toHaveBeenCalledTimes(1);
    fireEvent.change(screen.getByLabelText("Project"), {
      target: { value: "project-1" },
    });
    const url = "/queue?keep=yes&status=running&project_id=project-1#anchor";
    expect(screen.getByTestId("url")).toHaveTextContent(url);
    fireEvent.click(screen.getByText("Back"));
    expect(screen.getByLabelText("Project")).toHaveValue("");
    fireEvent.click(screen.getByText("Forward"));
    expect(screen.getByTestId("url")).toHaveTextContent(url);
    view.unmount();
    renderPage([url]);
    await screen.findByText("No running builds.");
    expect(screen.getByLabelText("Active work status")).toHaveValue("running");
    expect(mockedListQueue).toHaveBeenLastCalledWith({
      project_id: "project-1",
      status: "running",
    });
  });

  it("keeps active timing and assignment explicit when history is unavailable", async () => {
    const lease = "2026-03-14T10:00:00Z";
    mockedListQueue.mockResolvedValue([
      entry(1),
      {
        ...entry(2, "running"),
        started_at: "2999-01-01T00:00:00Z",
        worker_id: "worker-72",
        lease_expires_at: lease,
      },
    ]);
    mockedListBuilds.mockRejectedValue(new Error("offline"));
    renderPage();
    await screen.findByText("Waiting —");
    expect(screen.getAllByText("Queued —")).toHaveLength(2);
    for (const text of [
      "Worker Unassigned",
      "Worker worker-72",
      "Running —",
      `Lease expires ${formatTime(lease)}`,
      "Lease expires —",
    ])
      expect(screen.getByText(text)).toBeVisible();
    expect(screen.getAllByText("Priority 0")).toHaveLength(2);
    expect(screen.getByText(/Unable to load recent outcomes/)).toBeVisible();
  });

  it("announces a denied cancellation without losing the row or focus", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    mockedListQueue.mockResolvedValue([entry(1)]);
    mockedCancelBuild.mockRejectedValue(new Error("403 Forbidden"));
    renderPage();
    const button = await screen.findByRole("button", { name: "Cancel" });
    button.focus();
    fireEvent.click(button);
    expect(await screen.findByRole("alert")).toHaveTextContent("403 Forbidden");
    expect(button).toHaveFocus();
    expect(screen.getByRole("link", { name: "Build #1" })).toBeVisible();
    confirm.mockRestore();
  });
});
