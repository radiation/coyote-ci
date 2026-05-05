import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { QueuePage } from "./QueuePage";
import { listProjects, listQueue } from "../api";

vi.mock("../api", () => ({
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
  });

  it("shows empty state when queue is empty", async () => {
    mockedListQueue.mockResolvedValue([]);

    renderPage(["/queue"]);

    await screen.findByText("No queued or running builds.");
    expect(screen.getByLabelText("Project")).toHaveValue("");
    expect(screen.getByLabelText("Status")).toHaveValue("");
  });

  it("renders queue entries and forwards filters", async () => {
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
        worker_id: null,
        lease_expires_at: null,
        repository_url: "https://github.com/example/backend.git",
        trigger_ref: "main",
        source_commit_sha: "abc1234567890",
        trigger_commit_sha: null,
      },
    ]);

    renderPage(["/queue?project_id=project-1&status=queued"]);

    await screen.findByRole("link", { name: "#12" });
    expect(mockedListQueue).toHaveBeenCalledWith({
      project_id: "project-1",
      status: "queued",
    });
    expect(screen.getByText("backend-ci")).toBeTruthy();
    expect(screen.getByText("9")).toBeTruthy();
    expect(screen.getByText("main • abc1234")).toBeTruthy();
  });
});
