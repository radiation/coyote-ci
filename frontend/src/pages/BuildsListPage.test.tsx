import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { BuildsListPage } from "./BuildsListPage";
import { listBuilds, listProjects } from "../api";

vi.mock("../api", () => ({
  listBuilds: vi.fn(),
  listProjects: vi.fn(),
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
        <BuildsListPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("BuildsListPage", () => {
  const mockedListBuilds = vi.mocked(listBuilds);
  const mockedListProjects = vi.mocked(listProjects);

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

  it("shows empty state when no builds exist", async () => {
    mockedListBuilds.mockResolvedValue([]);
    renderPage();

    await screen.findByText("No builds yet.");
    expect(screen.getByText(/Builds are created by running a/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "job" })).toBeTruthy();
  });

  it("renders builds table with data", async () => {
    mockedListBuilds.mockResolvedValue([
      {
        id: "aaaa-bbbb-cccc-dddd",
        attempt_number: 1,
        priority: 5,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        status: "success",
        created_at: "2026-03-24T00:00:00Z",
        queued_at: "2026-03-24T00:00:01Z",
        started_at: "2026-03-24T00:00:02Z",
        finished_at: "2026-03-24T00:00:10Z",
        current_step_index: 2,
        error_message: null,
        trigger_kind: "webhook",
        scm_provider: "github",
        trigger_ref: "main",
        trigger_commit_sha: "abc1234567890",
        actor: "octocat",
      },
      {
        id: "eeee-ffff-gggg-hhhh",
        attempt_number: 1,
        priority: 4,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        status: "canceled",
        created_at: "2026-03-24T00:00:20Z",
        queued_at: "2026-03-24T00:00:21Z",
        started_at: "2026-03-24T00:00:22Z",
        finished_at: "2026-03-24T00:00:30Z",
        current_step_index: 1,
        error_message: "build canceled by operator request",
        trigger_kind: "manual",
        scm_provider: null,
        trigger_ref: null,
        trigger_commit_sha: null,
        actor: null,
      },
    ]);
    renderPage();

    await screen.findByText("aaaa-bbb…");
    expect(screen.getByText("eeee-fff…")).toBeTruthy();
    expect(screen.getByText("Canceled")).toBeTruthy();
    expect(screen.getAllByText("Platform").length).toBe(2);
    expect(screen.getByText("5")).toBeTruthy();
    expect(screen.getByText("webhook")).toBeTruthy();
    expect(screen.getByText("github • main • abc1234 • octocat")).toBeTruthy();
  });

  it("forwards the project filter from the query string", async () => {
    mockedListBuilds.mockResolvedValue([]);

    renderPage(["/builds?project_id=project-1"]);

    await screen.findByText("No builds yet.");
    expect(mockedListBuilds).toHaveBeenCalledWith({ project_id: "project-1" });
    expect(screen.getByLabelText("Project")).toHaveValue("project-1");
  });

  it("does not render any creation form", async () => {
    mockedListBuilds.mockResolvedValue([]);
    renderPage();

    await screen.findByText("No builds yet.");
    expect(screen.queryByLabelText("Template")).toBeNull();
    expect(screen.queryByLabelText("Project ID")).toBeNull();
    expect(screen.queryByRole("button", { name: /Queue|Create/ })).toBeNull();
  });
});
