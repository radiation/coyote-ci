import { act, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { listBuilds, listQueue, listWorkers } from "../api";
import { APIError } from "../api/request";
import type { Build, QueueEntry } from "../types/build";
import type { Worker } from "../types/worker";
import { DashboardPage } from "./DashboardPage";

vi.mock("../api", async () => ({
  APIError: (await vi.importActual<typeof import("../api")>("../api")).APIError,
  listBuilds: vi.fn(),
  listQueue: vi.fn(),
  listWorkers: vi.fn(),
}));

const timestamp = "2026-05-01T01:00:00Z";
function build(
  number: number,
  status: Build["status"],
  overrides: Partial<Build> = {},
): Build {
  return {
    id: `execution-${number}`,
    build_number: number,
    project_id: "platform-id",
    project_name: "Platform",
    job_id: "compile-id",
    job_name: "Compile",
    created_at: timestamp,
    started_at: null,
    queued_at: null,
    finished_at: null,
    priority: 1,
    current_step_index: 0,
    attempt_number: 1,
    error_message: null,
    application_version: null,
    status,
    ...overrides,
  };
}
function entry(number: number, status: QueueEntry["status"]): QueueEntry {
  return {
    ...build(number, status),
    build_id: `execution-${number}`,
    build_number: number,
    status,
  };
}
function worker(
  id: string,
  status: Worker["status"],
  extra: Partial<Worker> = {},
): Worker {
  return {
    id,
    name: id,
    status,
    created_at: timestamp,
    updated_at: timestamp,
    stale_heartbeat: false,
    stale_lease: false,
    ...extra,
  };
}
function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retryDelay: 1 } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}
const region = (name: string) => within(screen.getByRole("region", { name }));

describe("DashboardPage", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(listQueue).mockResolvedValue([]);
    vi.mocked(listBuilds).mockResolvedValue([]);
    vi.mocked(listWorkers).mockResolvedValue([]);
  });

  it("counts the complete active snapshot, caps previews and links to filtered queue and build investigation", async () => {
    vi.mocked(listQueue).mockResolvedValue(
      [1, 2, 3, 4]
        .map((number) => entry(number, "running"))
        .concat(entry(5, "queued")),
    );
    renderPage();
    const active = region("Active work");
    expect(
      await active.findByRole("link", { name: "4 Running" }),
    ).toHaveAttribute("href", "/queue?status=running");
    expect(active.getByRole("link", { name: "1 Queued" })).toHaveAttribute(
      "href",
      "/queue?status=queued",
    );
    expect(active.getAllByRole("listitem")).toHaveLength(3);
    expect(active.getByRole("link", { name: "Build #2" })).toHaveAttribute(
      "href",
      "/builds/execution-2",
    );
    expect(
      screen.getByRole("link", { name: "View all projects" }),
    ).toHaveAttribute("href", "/projects");
    expect(screen.queryByRole("link", { name: "Create job" })).toBeNull();
  });

  it("counts failures within the actual window, orders by event time, and excludes displayed IDs from other activity", async () => {
    vi.mocked(listQueue).mockResolvedValue([entry(1, "running")]);
    vi.mocked(listBuilds).mockResolvedValue([
      build(1, "running"),
      build(2, "failed", { queued_at: "2026-05-02T00:00:00Z" }),
      build(3, "failed", { started_at: "2026-05-03T00:00:00Z" }),
      build(4, "failed", {
        finished_at: "2026-05-04T00:00:00Z",
        build_number: undefined,
      }),
      build(5, "failed"),
      ...[6, 7, 8, 9, 10].map((number) => build(number, "success")),
    ]);
    renderPage();
    const failures = region("Recent failures");
    expect(
      await failures.findByText(/4 failed among 10 returned builds/),
    ).toBeTruthy();
    const rows = failures.getAllByRole("listitem");
    expect(rows).toHaveLength(3);
    expect(
      within(rows[0]).getByRole("link", { name: "Build executio" }),
    ).toHaveAttribute("href", "/builds/execution-4");
    expect(
      within(rows[0]).getByRole("link", { name: "Platform" }),
    ).toHaveAttribute("href", "/projects/platform-id");
    expect(
      within(rows[0]).getByRole("link", { name: "Compile" }),
    ).toHaveAttribute("href", "/jobs/compile-id");
    expect(within(rows[1]).getByText("Build #3")).toBeTruthy();
    expect(within(rows[2]).getByText("Build #2")).toBeTruthy();
    const activity = region("Other recent activity");
    await waitFor(() =>
      expect(activity.getAllByRole("listitem")).toHaveLength(4),
    );
    expect(activity.queryByText("Build #1")).toBeNull();
    expect(activity.getByText("Build #5")).toBeTruthy();
    expect(activity.getByText("Build #6")).toBeTruthy();
    expect(
      activity.getByRole("link", { name: "Browse builds" }),
    ).toHaveAttribute("href", "/builds?limit=24&offset=0");
    expect(
      failures.getByRole("link", { name: "Browse failed builds" }),
    ).toHaveAttribute("href", "/builds?status=failed&limit=24&offset=0");
    expect(listBuilds).toHaveBeenCalledExactlyOnceWith({
      limit: 24,
      offset: 0,
    });
    expect(listQueue).toHaveBeenCalledTimes(1);
    expect(listWorkers).toHaveBeenCalledTimes(1);
  });

  it("uses the default Workers visibility policy without disclosing individual claims", async () => {
    vi.mocked(listWorkers).mockResolvedValue([
      worker("one", "idle"),
      worker("two", "busy"),
      worker("hidden", "stale"),
      worker("retained", "stale", { current_step_index: 0 }),
    ]);
    renderPage();
    expect(
      await screen.findByText(
        "Visible workers: 1 idle · 1 busy · 1 stale. 1 older stale hidden.",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "View workers" })).toHaveAttribute(
      "href",
      "/workers",
    );
    expect(screen.queryByText("retained")).toBeNull();
  });

  it("distinguishes pending and successful empty snapshots from missing data", async () => {
    renderPage();
    expect(screen.getByText("Loading queue…")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "0 Running" })).toBeNull();
    expect(
      await screen.findByText("No active builds in the queue snapshot."),
    ).toBeTruthy();
    expect(screen.getByText(/No visible builds in this window/)).toBeTruthy();
    expect(
      screen.getByText("No other executions in this window."),
    ).toBeTruthy();
    expect(
      screen.getByText("No visible workers in this snapshot."),
    ).toBeTruthy();
  });

  it("keeps queue usable when history fails and does not report zero failures", async () => {
    vi.mocked(listBuilds).mockRejectedValue(new Error("offline"));
    renderPage();
    expect(
      await screen.findByText("Unable to refresh recent builds."),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: "0 Queued" })).toBeTruthy();
    expect(screen.queryByText(/failed among/)).toBeNull();
  });

  it("keeps last known history on transient errors but suppresses it on authorization loss", async () => {
    vi.mocked(listBuilds).mockResolvedValue([build(7, "failed")]);
    const client = renderPage();
    await screen.findByText("Build #7");
    const refresh = () =>
      act(async () => {
        await client.invalidateQueries({
          queryKey: ["queue-page", "recent-builds", ""],
        });
      });
    vi.mocked(listBuilds).mockRejectedValue(new Error("offline"));
    await refresh();
    expect(await screen.findByText(/Last known results/)).toBeTruthy();
    expect(screen.getByText("Build #7")).toBeTruthy();
    vi.mocked(listBuilds).mockRejectedValue(new APIError(403, "forbidden"));
    await refresh();
    expect(
      await screen.findByText("Access to recent builds is unavailable."),
    ).toBeTruthy();
    expect(screen.queryByText("Build #7")).toBeNull();
    expect(screen.getByRole("link", { name: "0 Running" })).toBeTruthy();
  });
});
