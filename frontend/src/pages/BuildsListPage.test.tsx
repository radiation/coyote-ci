import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { BuildsListPage } from "./BuildsListPage";
import { listBuilds, listProjects } from "../api";

vi.mock("../api", () => ({
  listBuilds: vi.fn(),
  listProjects: vi.fn(),
}));

function BrowserControls() {
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <>
      <output data-testid="url">{location.search}</output>
      <button onClick={() => navigate(-1)}>Back</button>
      <button onClick={() => navigate(1)}>Forward</button>
    </>
  );
}

function renderPage(initialEntries: string[] = ["/builds"]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={initialEntries}>
        <BuildsListPage />
        <BrowserControls />
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

    await screen.findByText(/No visible builds on this page/);
    expect(screen.getByText(/Builds are created by running a/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "job" })).toBeTruthy();
  });

  it("links numbered and legacy builds with returned context and filters without refetching", async () => {
    mockedListBuilds.mockResolvedValue([
      {
        id: "aaaa-bbbb-cccc-dddd",
        build_number: 123,
        job_id: "job-1",
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
        application_version: null,
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
        application_version: null,
        trigger_kind: "manual",
        scm_provider: null,
        trigger_ref: null,
        trigger_commit_sha: null,
        actor: null,
      },
    ]);
    renderPage(["/builds?project_id=project-1&limit=10&offset=20"]);
    const identity = await screen.findByRole("link", { name: "Build #123" });
    expect(new URL((identity as HTMLAnchorElement).href).pathname).toBe(
      "/builds/aaaa-bbbb-cccc-dddd",
    );
    expect(screen.getByRole("link", { name: "Build eeee-fff" })).toBeVisible();
    expect(screen.getByRole("link", { name: "job-1" })).toHaveAttribute(
      "href",
      "/jobs/job-1",
    );
    expect(
      screen.getAllByRole("link", { name: "Platform" })[0],
    ).toHaveAttribute("href", "/projects/project-1");
    expect(screen.getAllByText("Duration 8s")).toHaveLength(2);
    expect(screen.getByText("webhook")).toBeTruthy();
    expect(screen.getByText("github • main • abc1234 • octocat")).toBeTruthy();
    const filter = screen.getByLabelText("Status on this page");
    fireEvent.change(filter, { target: { value: "success" } });
    expect(screen.queryByRole("link", { name: "Build eeee-fff" })).toBeNull();
    expect(
      new URL((identity as HTMLAnchorElement).href).searchParams.get("history"),
    ).toBe("project_id=project-1&status=success&limit=10&offset=20");
    fireEvent.change(filter, { target: { value: "failed" } });
    expect(
      screen.getByText(/No failed builds among the 2 results on this page/),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "Older" })).toHaveAttribute(
      "href",
      "/builds?project_id=project-1&status=failed&limit=10&offset=30",
    );
    expect(mockedListBuilds).toHaveBeenCalledTimes(1);
    expect(mockedListProjects).toHaveBeenCalledTimes(1);
  });

  it("forwards the project filter from the query string", async () => {
    mockedListBuilds.mockResolvedValue([]);

    renderPage(["/builds?project_id=project-1"]);

    await screen.findByText(/No visible builds on this page/);
    expect(mockedListBuilds).toHaveBeenCalledWith({
      project_id: "project-1",
      limit: 50,
      offset: 0,
    });
    expect(screen.getByLabelText("Project")).toHaveValue("project-1");
  });

  it("does not render any creation form", async () => {
    mockedListBuilds.mockResolvedValue([]);
    renderPage();

    await screen.findByText(/No visible builds on this page/);
    expect(screen.queryByLabelText("Template")).toBeNull();
    expect(screen.queryByLabelText("Project ID")).toBeNull();
    expect(screen.queryByRole("button", { name: /Queue|Create/ })).toBeNull();
  });

  it("navigates empty windows, restores URL state, and resets offset only for project changes", async () => {
    mockedListBuilds.mockResolvedValue([]);
    const view = renderPage([
      "/builds?project_id=project-1&status=failed&limit=12&offset=24",
    ]);
    await screen.findByText(/No visible builds on this page/);
    expect(screen.getByLabelText("Status on this page")).toHaveValue("failed");
    fireEvent.click(screen.getByRole("link", { name: "Older" }));
    await waitFor(() =>
      expect(mockedListBuilds).toHaveBeenLastCalledWith({
        project_id: "project-1",
        limit: 12,
        offset: 36,
      }),
    );
    fireEvent.click(screen.getByRole("link", { name: "Newer" }));
    expect(screen.getByTestId("url")).toHaveTextContent("offset=24");
    fireEvent.change(screen.getByLabelText("Project"), {
      target: { value: "" },
    });
    expect(screen.getByTestId("url")).toHaveTextContent(
      "?status=failed&limit=12&offset=0",
    );
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByLabelText("Project")).toHaveValue("project-1");
    fireEvent.click(screen.getByRole("button", { name: "Forward" }));
    const search = screen.getByTestId("url").textContent;
    view.unmount();
    renderPage([`/builds${search}`]);
    await screen.findByText(/No visible builds on this page/);
    expect(screen.getByLabelText("Status on this page")).toHaveValue("failed");
    expect(screen.queryByRole("link", { name: "Newer" })).toBeNull();
  });

  it("uses defaults for malformed parameters and announces request failure", async () => {
    mockedListBuilds.mockRejectedValue(new Error("unavailable"));
    renderPage(["/builds?project_id=//bad&status=invalid&limit=201&offset=-1"]);
    expect(await screen.findByRole("alert")).toHaveTextContent("unavailable");
    expect(mockedListBuilds).toHaveBeenCalledWith({
      project_id: undefined,
      limit: 50,
      offset: 0,
    });
    expect(screen.getByLabelText("Status on this page")).toHaveValue("");
  });
});
