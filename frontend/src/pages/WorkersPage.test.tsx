import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { WorkersPage } from "./WorkersPage";
import { listWorkers } from "../api";
import type { Worker } from "../types/worker";
import { formatTime } from "../utils/time";

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

function deferredPromise<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("WorkersPage", () => {
  const mockedListWorkers = vi.mocked(listWorkers);

  function worker(overrides: Partial<Worker>): Worker {
    return {
      id: "worker-default",
      name: "worker-default",
      status: "idle",
      last_heartbeat_at: "2026-05-24T12:00:00Z",
      created_at: "2026-05-24T11:00:00Z",
      updated_at: "2026-05-24T12:00:00Z",
      stale_lease: false,
      stale_heartbeat: false,
      ...overrides,
    };
  }

  function hoursAgo(hours: number): string {
    return new Date(Date.now() - hours * 60 * 60 * 1000).toISOString();
  }

  beforeEach(() => {
    vi.restoreAllMocks();
    vi.clearAllMocks();
  });

  it("renders workers and summary counts", async () => {
    mockedListWorkers.mockResolvedValue([
      worker({
        id: "worker-idle",
        name: "idle-worker",
      }),
      worker({
        id: "worker-busy",
        name: "busy-worker",
        status: "busy",
        last_heartbeat_at: "2026-05-24T12:00:05Z",
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
      }),
    ]);

    renderPage();

    await screen.findByText("idle-worker");
    expect(screen.getByText("2")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Build #18" })).toHaveAttribute(
      "href",
      "/builds/build-1",
    );
    expect(screen.getByText(/Step 1 .* compile/)).toBeTruthy();
    expect(screen.getByText(formatTime("2026-05-24T12:00:01Z"))).toBeTruthy();
  });

  it("renders status badges including stale workers", async () => {
    mockedListWorkers.mockResolvedValue([
      worker({
        id: "worker-stale",
        name: "stale-worker",
        status: "stale",
        last_heartbeat_at: "2026-05-24T11:55:00Z",
        updated_at: "2026-05-24T11:55:00Z",
        stale_lease: true,
        stale_heartbeat: true,
      }),
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
      worker({
        id: "worker-busy",
        name: "busy-worker",
        status: "busy",
        last_heartbeat_at: "2026-05-24T12:00:05Z",
        updated_at: "2026-05-24T12:00:05Z",
        current_build_id: "build-1",
        current_build_number: 18,
        current_step_id: "step-1",
        current_step_index: 0,
        current_step_name: "compile",
        project_id: "project-1",
        job_id: "job-1",
      }),
    ]);

    renderPage();

    await screen.findByText("busy-worker");
    expect(screen.getByText("project-1")).toBeTruthy();
    expect(screen.getByText("job-1")).toBeTruthy();
  });

  it("keeps current data visible while manually refreshing", async () => {
    const refreshResult = deferredPromise<Worker[]>();

    mockedListWorkers
      .mockResolvedValueOnce([
        worker({
          id: "worker-idle",
          name: "idle-worker",
        }),
      ])
      .mockImplementationOnce(() => refreshResult.promise);

    renderPage();

    await screen.findByText("idle-worker");
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    expect(screen.getByText("idle-worker")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Refreshing…" })).toBeDisabled();
    expect(screen.getByText("Refreshing worker state…")).toBeTruthy();

    refreshResult.resolve([
      worker({
        id: "worker-idle",
        name: "idle-worker",
        last_heartbeat_at: "2026-05-24T12:00:05Z",
        updated_at: "2026-05-24T12:00:05Z",
      }),
    ]);

    await waitFor(() => {
      expect(mockedListWorkers).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Refresh" })).toBeTruthy();
    });
  });

  it("shows recent stale workers by default", async () => {
    mockedListWorkers.mockResolvedValue([
      worker({
        id: "worker-stale-recent",
        name: "recent-stale-worker",
        status: "stale",
        last_heartbeat_at: hoursAgo(12),
        updated_at: hoursAgo(12),
        stale_heartbeat: true,
      }),
    ]);

    renderPage();

    await screen.findByText("recent-stale-worker");
    expect(screen.queryByText(/older stale workers hidden/i)).not.toBeTruthy();
  });

  it("hides older stale workers by default and shows the hidden count", async () => {
    mockedListWorkers.mockResolvedValue([
      worker({
        id: "worker-idle",
        name: "idle-worker",
      }),
      worker({
        id: "worker-stale-old",
        name: "old-stale-worker",
        status: "stale",
        last_heartbeat_at: hoursAgo(50),
        updated_at: hoursAgo(50),
        stale_heartbeat: true,
      }),
    ]);

    renderPage();

    await screen.findByText("idle-worker");
    expect(screen.queryByText("old-stale-worker")).toBeNull();
    expect(screen.getByText("1 older stale workers hidden")).toBeTruthy();
  });

  it("reveals older stale workers when toggled", async () => {
    mockedListWorkers.mockResolvedValue([
      worker({
        id: "worker-stale-old",
        name: "old-stale-worker",
        status: "stale",
        last_heartbeat_at: hoursAgo(50),
        updated_at: hoursAgo(50),
        stale_heartbeat: true,
      }),
    ]);

    renderPage();

    await screen.findByText("1 older stale workers hidden");
    fireEvent.click(
      screen.getByRole("button", { name: "Show hidden stale workers" }),
    );

    expect(await screen.findByText("old-stale-worker")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Hide older stale workers" }),
    ).toBeTruthy();
  });

  it("keeps stale workers with expired leases visible", async () => {
    mockedListWorkers.mockResolvedValue([
      worker({
        id: "worker-expired-lease",
        name: "expired-lease-worker",
        status: "stale",
        last_heartbeat_at: hoursAgo(50),
        updated_at: hoursAgo(50),
        lease_expires_at: hoursAgo(49),
        stale_lease: true,
        stale_heartbeat: true,
      }),
    ]);

    renderPage();

    await screen.findByText("expired-lease-worker");
    expect(screen.queryByText(/older stale workers hidden/i)).not.toBeTruthy();
  });

  it("keeps stale workers with current claim context visible", async () => {
    mockedListWorkers.mockResolvedValue([
      worker({
        id: "worker-claimed",
        name: "claimed-stale-worker",
        status: "stale",
        last_heartbeat_at: hoursAgo(50),
        updated_at: hoursAgo(50),
        current_build_id: "build-2",
        current_build_number: 22,
        current_step_name: "publish",
        claimed_at: hoursAgo(49),
        stale_heartbeat: true,
      }),
    ]);

    renderPage();

    await screen.findByText("claimed-stale-worker");
    expect(screen.getByRole("link", { name: "Build #22" })).toHaveAttribute(
      "href",
      "/builds/build-2",
    );
    expect(screen.queryByText(/older stale workers hidden/i)).not.toBeTruthy();
  });
});
