import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { WorkersPage } from "./WorkersPage";
import { listWorkers } from "../api";

vi.mock("../api", () => ({
  listWorkers: vi.fn(),
}));

function renderPage(initialEntries: string[] = ["/workers"]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={initialEntries}>
        <WorkersPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("WorkersPage", () => {
  const mockedListWorkers = vi.mocked(listWorkers);

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders workers and summary counts", async () => {
    mockedListWorkers.mockResolvedValue([
      {
        id: "worker-idle",
        name: "idle-worker",
        status: "idle",
        last_heartbeat_at: "2026-05-24T12:00:00Z",
        created_at: "2026-05-24T11:00:00Z",
        updated_at: "2026-05-24T12:00:00Z",
        stale_lease: false,
        stale_heartbeat: false,
      },
      {
        id: "worker-busy",
        name: "busy-worker",
        status: "busy",
        last_heartbeat_at: "2026-05-24T12:00:05Z",
        created_at: "2026-05-24T11:00:00Z",
        updated_at: "2026-05-24T12:00:05Z",
        current_build_id: "build-1",
        current_build_number: 18,
        current_step_id: "step-1",
        current_step_index: 0,
        current_step_name: "compile",
        lease_expires_at: "2026-05-24T12:00:30Z",
        claimed_at: "2026-05-24T12:00:01Z",
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-1",
        job_name: "release",
        stale_lease: false,
        stale_heartbeat: false,
      },
    ]);

    renderPage();

    await screen.findByText("idle-worker");
    expect(screen.getByText("2")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Build #18" })).toHaveAttribute(
      "href",
      "/builds/build-1",
    );
    expect(screen.getByText(/Step 1 .* compile/)).toBeTruthy();
  });

  it("renders status badges including stale workers", async () => {
    mockedListWorkers.mockResolvedValue([
      {
        id: "worker-stale",
        name: "stale-worker",
        status: "stale",
        last_heartbeat_at: "2026-05-24T11:55:00Z",
        created_at: "2026-05-24T11:00:00Z",
        updated_at: "2026-05-24T11:55:00Z",
        stale_lease: true,
        stale_heartbeat: true,
      },
    ]);

    renderPage();

    await screen.findByText("stale-worker");
    expect(screen.getByText("Expired lease")).toBeTruthy();
    expect(screen.getByText("Heartbeat overdue")).toBeTruthy();
    expect(document.querySelector(".workers-row-stale")).toBeTruthy();
  });

  it("shows a clean empty state", async () => {
    mockedListWorkers.mockResolvedValue([]);

    renderPage();

    await screen.findByText("No workers have reported heartbeats yet.");
  });

  it("shows a concise error state", async () => {
    mockedListWorkers.mockRejectedValue(new Error("workers down"));

    renderPage();

    await screen.findByText("Unable to load workers.");
  });

  it("falls back to durable project and job ids when names are missing", async () => {
    mockedListWorkers.mockResolvedValue([
      {
        id: "worker-busy",
        name: "busy-worker",
        status: "busy",
        last_heartbeat_at: "2026-05-24T12:00:05Z",
        created_at: "2026-05-24T11:00:00Z",
        updated_at: "2026-05-24T12:00:05Z",
        current_build_id: "build-1",
        current_build_number: 18,
        current_step_id: "step-1",
        current_step_index: 0,
        current_step_name: "compile",
        project_id: "project-1",
        job_id: "job-1",
        stale_lease: false,
        stale_heartbeat: false,
      },
    ]);

    renderPage();

    await screen.findByText("busy-worker");
    expect(screen.getByText("project-1")).toBeTruthy();
    expect(screen.getByText("job-1")).toBeTruthy();
  });
});
