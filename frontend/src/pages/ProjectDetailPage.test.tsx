import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { FAST_POLL_INTERVAL, SLOW_POLL_INTERVAL } from "../utils/build";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { ProjectDetailPage } from "./ProjectDetailPage";
import { AuthContext } from "../auth-context";
import {
  APIError,
  deleteProject,
  listBuildsByJob,
  runJob,
  deleteProjectMember,
  getProject,
  getPublicProject,
  listBuilds,
  listJobsByProject,
  listProjectMembers,
  listPublicBuilds,
  listQueue,
  listUsers,
  updateProjectMember,
  upsertProjectMember,
} from "../api";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    deleteProject: vi.fn(),
    listBuildsByJob: vi.fn(),
    runJob: vi.fn(),
    deleteProjectMember: vi.fn(),
    getProject: vi.fn(),
    getPublicProject: vi.fn(),
    listBuilds: vi.fn(),
    listJobsByProject: vi.fn(),
    listProjectMembers: vi.fn(),
    listPublicBuilds: vi.fn(),
    listQueue: vi.fn(),
    listUsers: vi.fn(),
    updateProjectMember: vi.fn(),
    upsertProjectMember: vi.fn(),
  };
});

const mockedGetPublicProject = vi.mocked(getPublicProject);

function HistoryControls() {
  const navigate = useNavigate();
  return (
    <>
      <button onClick={() => navigate(-1)}>Back</button>
      <button onClick={() => navigate(1)}>Forward</button>
    </>
  );
}

const settingsURL = "/projects/project-1?view=settings";

function renderPage(
  authStatus: "authenticated" | "unauthenticated" = "authenticated",
  initialEntry = "/projects/project-1",
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={{
          currentUser: null,
          authMode: null,
          authStatus,
          error: null,
          isGlobalAdmin: false,
          loginAvailable: false,
          login: vi.fn(),
          logout: vi.fn(),
          refreshCurrentUser: vi.fn(),
        }}
      >
        <MemoryRouter initialEntries={[initialEntry]}>
          <HistoryControls />
          <Routes>
            <Route path="/projects" element={<p>Project directory</p>} />
            <Route path="/builds/:id" element={<p>Build investigation</p>} />
            <Route path="/projects/:id" element={<ProjectDetailPage />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
}

describe("ProjectDetailPage", () => {
  const mockedGetProject = vi.mocked(getProject);
  const mockedListBuilds = vi.mocked(listBuilds);
  const mockedListJobsByProject = vi.mocked(listJobsByProject);
  const mockedListProjectMembers = vi.mocked(listProjectMembers);
  const mockedListPublicBuilds = vi.mocked(listPublicBuilds);
  const mockedListQueue = vi.mocked(listQueue);
  const mockedListUsers = vi.mocked(listUsers);
  const mockedUpsertProjectMember = vi.mocked(upsertProjectMember);
  const mockedUpdateProjectMember = vi.mocked(updateProjectMember);
  const mockedDeleteProjectMember = vi.mocked(deleteProjectMember);

  beforeEach(() => {
    vi.resetAllMocks();
    mockedGetProject.mockResolvedValue({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "Core platform pipelines",
      created_at: "2026-05-01T00:00:00Z",
      updated_at: "2026-05-01T00:00:00Z",
    });
    mockedListJobsByProject.mockResolvedValue([
      {
        id: "job-1",
        project_id: "project-1",
        name: "backend-ci",
        priority: 5,
        repository_url: "https://github.com/example/backend.git",
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
    mockedListProjectMembers.mockResolvedValue([
      {
        project_id: "project-1",
        user_id: "user-1",
        email: "maintainer@example.com",
        display_name: "Maintainer",
        role: "maintainer",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:00:00Z",
      },
    ]);
    mockedListUsers.mockResolvedValue([
      {
        id: "user-2",
        email: "viewer@example.com",
        global_role: "user",
      },
    ]);
    mockedListQueue.mockResolvedValue([
      {
        build_id: "build-queue-1",
        build_number: 42,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-queue-1",
        job_name: "release",
        priority: 5,
        status: "running",
        created_at: "2026-05-01T00:00:00Z",
      },
    ]);
    mockedListBuilds.mockResolvedValue([
      {
        id: "build-recent-1",
        build_number: 41,
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
        application_version: null,
      },
    ]);
    mockedUpsertProjectMember.mockResolvedValue({
      project_id: "project-1",
      user_id: "user-2",
      role: "viewer",
      created_at: "2026-05-01T00:00:00Z",
      updated_at: "2026-05-01T00:00:00Z",
    });
    mockedUpdateProjectMember.mockResolvedValue({
      project_id: "project-1",
      user_id: "user-1",
      role: "owner",
      created_at: "2026-05-01T00:00:00Z",
      updated_at: "2026-05-01T00:00:00Z",
    });
    mockedDeleteProjectMember.mockResolvedValue();
    mockedListPublicBuilds.mockResolvedValue([]);
  });

  it("renders operational overview without administrative requests", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
      expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute(
        "aria-current",
        "page",
      );
      expect(screen.getByText("Recent results")).toBeTruthy();
      expect(screen.getByText("Core platform pipelines")).toBeTruthy();
      expect(mockedListProjectMembers).not.toHaveBeenCalled();
      expect(mockedListUsers).not.toHaveBeenCalled();
      expect(screen.getByText("backend-ci")).toBeTruthy();
      expect(
        screen.getByRole("button", { name: "Run Now: backend-ci" }),
      ).toBeTruthy();
      expect(
        screen
          .getAllByRole("link", { name: "Create Job" })
          .every(
            (link) =>
              link.getAttribute("href") === "/jobs/new?project_id=project-1",
          ),
      ).toBe(true);
      expect(
        screen.getByRole("link", { name: "Browse Artifacts" }),
      ).toHaveAttribute("href", "/artifacts?project_id=project-1");
      expect(
        screen.getByRole("link", { name: "Logical artifact repository" }),
      ).toHaveAttribute("href", "/artifacts/logical?project_id=project-1");
      expect(screen.getByRole("link", { name: "backend-ci" })).toHaveAttribute(
        "href",
        "/jobs/job-1",
      );
      expect(screen.getByRole("link", { name: "Builds" })).toHaveAttribute(
        "href",
        "/projects/project-1?view=builds",
      );
      expect(screen.getByRole("link", { name: "release" })).toHaveAttribute(
        "href",
        "/jobs/job-queue-1",
      );
      expect(
        screen.getByRole("link", { name: "release-history" }),
      ).toHaveAttribute("href", "/jobs/job-recent-1");
    });
    expect(mockedGetProject).toHaveBeenCalledWith("project-1");
    expect(mockedGetPublicProject).not.toHaveBeenCalled();
  });

  it("uses public project and build endpoints for anonymous visitors", async () => {
    mockedGetPublicProject.mockResolvedValueOnce({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "Core platform pipelines",
    });
    mockedListPublicBuilds.mockResolvedValueOnce([
      {
        id: "build-1",
        number: 42,
        status: "success",
        job_name: "release",
        attempt: 1,
        created_at: "2026-05-01T00:00:00Z",
        completed_at: "2026-05-01T00:01:00Z",
      },
    ]);

    renderPage("unauthenticated");

    await waitFor(() => {
      expect(screen.getByText("Build History")).toBeTruthy();
      expect(screen.getByRole("link", { name: "#42" })).toHaveAttribute(
        "href",
        "/projects/platform/builds/build-1",
      );
    });
    expect(mockedGetPublicProject).toHaveBeenCalledWith("project-1");
    expect(mockedListPublicBuilds).toHaveBeenCalledWith("project-1");
    expect(mockedGetProject).not.toHaveBeenCalled();
    expect(mockedListJobsByProject).not.toHaveBeenCalled();
    expect(mockedListProjectMembers).not.toHaveBeenCalled();
    expect(screen.queryByText("Project Members")).toBeNull();
    expect(screen.queryByRole("link", { name: "Create Job" })).toBeNull();
  });

  it("falls back to a public project slug only after the ID lookup returns 404", async () => {
    mockedGetProject.mockRejectedValueOnce(new APIError(404, "not found"));
    mockedGetPublicProject.mockResolvedValueOnce({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "Core platform pipelines",
    });

    renderPage("authenticated", "/projects/platform?from=ci&view=jobs#resume");

    await waitFor(() => {
      expect(mockedGetProject).toHaveBeenCalledWith("project-1");
      expect(screen.getByRole("link", { name: "Jobs" })).toHaveAttribute(
        "href",
        "/projects/project-1?from=ci&view=jobs#resume",
      );
      expect(screen.getByRole("link", { name: "Jobs" })).toHaveAttribute(
        "aria-current",
        "page",
      );
    });
    expect(mockedGetProject).toHaveBeenCalledWith("platform");
    expect(mockedGetPublicProject).toHaveBeenCalledWith("platform");
  });

  it("shows resolver failures instead of calling the ID API with a public slug", async () => {
    mockedGetProject.mockRejectedValueOnce(new APIError(404, "not found"));
    mockedGetPublicProject.mockRejectedValueOnce(new Error("unavailable"));

    renderPage("authenticated", "/projects/platform");

    expect(
      await screen.findByText(
        "Failed to resolve public project: Error: unavailable",
      ),
    ).toBeTruthy();
    expect(mockedGetProject).toHaveBeenCalledWith("platform");
  });

  it("shows public project and build history fallbacks", async () => {
    mockedGetPublicProject.mockRejectedValueOnce(
      new APIError(404, "not found"),
    );
    renderPage("unauthenticated");
    expect(await screen.findByText("Project not found.")).toBeTruthy();

    mockedGetPublicProject.mockResolvedValueOnce({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "",
    });
    mockedListPublicBuilds.mockRejectedValueOnce(new Error("unavailable"));
    renderPage("unauthenticated");
    expect(
      await screen.findByText("Failed to load builds: Error: unavailable"),
    ).toBeTruthy();
  });

  it("uses the public project description fallback", async () => {
    mockedGetPublicProject.mockResolvedValueOnce({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "",
    });
    renderPage("unauthenticated");

    expect(await screen.findByText("No description.")).toBeTruthy();
  });

  it("shows the project loading state", () => {
    mockedGetProject.mockImplementationOnce(
      () => new Promise(() => {}) as Promise<never>,
    );

    renderPage();

    expect(screen.getByText("Loading project…")).toBeTruthy();
  });

  it("shows the project error state", async () => {
    mockedGetProject.mockRejectedValueOnce(new Error("backend unavailable"));

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText("Failed to load project: Error: backend unavailable"),
      ).toBeTruthy();
    });
  });

  it("shows the project not found state", async () => {
    mockedGetProject.mockResolvedValueOnce(null as never);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Project not found.")).toBeTruthy();
    });
  });

  it("shows empty project activity and detail fallbacks", async () => {
    mockedGetProject.mockResolvedValue({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "",
      created_at: "2026-05-01T00:00:00Z",
      updated_at: "2026-05-01T00:00:00Z",
    });
    mockedListProjectMembers.mockResolvedValueOnce([]);
    mockedListJobsByProject.mockResolvedValueOnce([]);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("No jobs in this project yet.")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("link", { name: "Settings" }));
    expect(await screen.findByText("No project members yet.")).toBeTruthy();
  });

  it("requires user id before adding a project member", async () => {
    renderPage("authenticated", settingsURL);

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("User ID"), {
      target: { value: "   " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add Member" }));

    await waitFor(() => {
      expect(screen.getByText("User ID is required.")).toBeTruthy();
    });
    expect(mockedUpsertProjectMember).not.toHaveBeenCalled();
  });

  it("shows members and jobs loading states", async () => {
    mockedListProjectMembers.mockImplementationOnce(
      () => new Promise(() => {}) as Promise<never>,
    );
    mockedListJobsByProject.mockImplementationOnce(
      () => new Promise(() => {}) as Promise<never>,
    );

    renderPage();

    expect(await screen.findByText("Loading jobs…")).toBeTruthy();
    fireEvent.click(screen.getByRole("link", { name: "Settings" }));
    expect(await screen.findByText("Loading members…")).toBeTruthy();
  });

  it("shows jobs error state when jobs query fails", async () => {
    mockedListJobsByProject.mockRejectedValue(new Error("jobs exploded"));

    renderPage();

    await waitFor(() => {
      expect(screen.getByText(/Failed to load jobs:/)).toBeTruthy();
    });
  });

  it("shows user id and display-name fallbacks in member rows", async () => {
    mockedListProjectMembers.mockResolvedValueOnce([
      {
        project_id: "project-1",
        user_id: "user-raw",
        email: "",
        display_name: "",
        role: "viewer",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:00:00Z",
      },
    ]);

    renderPage("authenticated", settingsURL);

    await waitFor(() => {
      expect(screen.getByText("user-raw")).toBeTruthy();
      expect(screen.getByText("—")).toBeTruthy();
      expect(screen.getByLabelText("Role for user-raw")).toBeTruthy();
    });
  });

  it("restores views through history while preserving unrelated parameters and hash", async () => {
    renderPage("authenticated", "/projects/project-1?source=list#anchor");
    fireEvent.click(await screen.findByRole("link", { name: "Jobs" }));
    fireEvent.click(screen.getByRole("link", { name: "Builds" }));
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByRole("link", { name: "Jobs" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    fireEvent.click(screen.getByRole("button", { name: "Forward" }));
    expect(screen.getByRole("link", { name: "Builds" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute(
      "href",
      "/projects/project-1?source=list#anchor",
    );
    expect(mockedListUsers).not.toHaveBeenCalled();
  });

  it("shows job name links in project-scoped activity rows", async () => {
    renderPage();

    await waitFor(() => {
      const queueJobLink = screen.getByRole("link", { name: "release" });
      expect(queueJobLink).toHaveAttribute("href", "/jobs/job-queue-1");

      const recentJobLink = screen.getByRole("link", {
        name: "release-history",
      });
      expect(recentJobLink).toHaveAttribute("href", "/jobs/job-recent-1");
    });
  });

  it("adds, updates, and removes project members", async () => {
    renderPage("authenticated", settingsURL);

    await waitFor(() => {
      expect(screen.getByText("maintainer@example.com")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("User ID"), {
      target: { value: "user-2" },
    });
    fireEvent.change(screen.getByLabelText("Role"), {
      target: { value: "viewer" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add Member" }));

    await waitFor(() => {
      expect(mockedUpsertProjectMember).toHaveBeenCalledWith(
        "project-1",
        "user-2",
        "viewer",
      );
    });

    fireEvent.change(screen.getByLabelText("Role for maintainer@example.com"), {
      target: { value: "owner" },
    });
    await waitFor(() => {
      expect(mockedUpdateProjectMember).toHaveBeenCalledWith(
        "project-1",
        "user-1",
        "owner",
      );
    });

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    await waitFor(() => {
      expect(mockedDeleteProjectMember).toHaveBeenCalledWith(
        "project-1",
        "user-1",
      );
    });
  });

  it("shows a friendly permission message when project members are forbidden", async () => {
    mockedListProjectMembers.mockRejectedValue(
      new APIError(
        403,
        "project membership visibility requires a project membership or global admin",
      ),
    );

    renderPage("authenticated", settingsURL);

    await waitFor(() => {
      expect(
        screen.getByText("You do not have permission to view project members."),
      ).toBeTruthy();
    });
  });

  it("keeps a load prefix for generic member list failures", async () => {
    mockedListProjectMembers.mockRejectedValue(
      new Error("backend unavailable"),
    );

    renderPage("authenticated", settingsURL);

    await waitFor(() => {
      expect(
        screen.getByText("Failed to load members: backend unavailable"),
      ).toBeTruthy();
    });
  });

  it("shows friendly mutation errors for member changes", async () => {
    mockedUpsertProjectMember.mockRejectedValueOnce(
      new APIError(403, "global admin or project owner is required"),
    );
    mockedUpdateProjectMember.mockRejectedValueOnce(
      new APIError(401, "missing user email header"),
    );
    mockedDeleteProjectMember.mockRejectedValueOnce(
      new APIError(403, "global admin or project owner is required"),
    );

    renderPage("authenticated", settingsURL);

    await waitFor(() => {
      expect(screen.getByText("maintainer@example.com")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("User ID"), {
      target: { value: "user-2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add Member" }));

    await waitFor(() => {
      expect(
        screen.getByText(
          "You do not have permission to manage project members.",
        ),
      ).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Role for maintainer@example.com"), {
      target: { value: "owner" },
    });

    await waitFor(() => {
      expect(
        screen.getByText(
          "Coyote is configured for external authentication. Sign in through the configured gateway or proxy, then retry.",
        ),
      ).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    await waitFor(() => {
      expect(
        screen.getByText(
          "You do not have permission to manage project members.",
        ),
      ).toBeTruthy();
    });
  });

  it.each(["/projects/project-1", "/projects/project-1?view=jobs"])(
    "refreshes idle and active job badges on %s",
    async (url) => {
      const [job] = await mockedListJobsByProject("project-1");
      const latest = {
        id: "external",
        build_number: 8,
        created_at: job.created_at,
      };
      mockedListJobsByProject
        .mockClear()
        .mockResolvedValueOnce([job])
        .mockResolvedValueOnce([
          { ...job, latest_build: { ...latest, status: "running" } },
        ])
        .mockResolvedValue([
          { ...job, latest_build: { ...latest, status: "success" } },
        ]);
      vi.useFakeTimers();
      const advance = (ms: number) =>
        act(async () => {
          await vi.advanceTimersByTimeAsync(ms);
        });
      try {
        renderPage("authenticated", url);
        await advance(5);
        expect(screen.getByText("Not run yet")).toBeTruthy();
        await advance(FAST_POLL_INTERVAL);
        expect(mockedListJobsByProject).toHaveBeenCalledTimes(1);
        await advance(SLOW_POLL_INTERVAL - FAST_POLL_INTERVAL);
        expect(
          screen.getByRole("link", {
            name: "Latest build for backend-ci: running",
          }),
        ).toBeTruthy();
        await advance(FAST_POLL_INTERVAL);
        expect(
          screen.getByRole("link", {
            name: "Latest build for backend-ci: success",
          }),
        ).toBeTruthy();
        await advance(FAST_POLL_INTERVAL);
        expect(mockedListJobsByProject).toHaveBeenCalledTimes(3);
      } finally {
        vi.useRealTimers();
      }
    },
  );

  it("fills the four overview slots from enabled jobs only", async () => {
    const [base] = await mockedListJobsByProject("project-1");
    mockedListJobsByProject.mockResolvedValue(
      Array.from({ length: 9 }, (_, index) => ({
        ...base,
        id: `job-${index}`,
        name: `Pipeline ${index}`,
        enabled: index >= 4,
      })),
    );
    renderPage();
    const buttons = await screen.findAllByRole("button", { name: /^Run Now:/ });
    expect(buttons.map((button) => button.getAttribute("aria-label"))).toEqual([
      "Run Now: Pipeline 4",
      "Run Now: Pipeline 5",
      "Run Now: Pipeline 6",
      "Run Now: Pipeline 7",
    ]);
    expect(screen.queryByRole("link", { name: "Pipeline 0" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Pipeline 8" })).toBeNull();
  });

  it("shows a no-runnable-jobs overview and keeps disabled jobs discoverable in Jobs", async () => {
    const [base] = await mockedListJobsByProject("project-1");
    mockedListJobsByProject.mockResolvedValue([{ ...base, enabled: false }]);
    renderPage();
    expect(
      await screen.findByText("No runnable jobs in this project."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("link", { name: "Jobs" }));
    expect(await screen.findByText("Disabled")).toBeTruthy();
    expect(screen.getByRole("link", { name: base.name })).toHaveAttribute(
      "href",
      `/jobs/${base.id}`,
    );
    expect(screen.queryByRole("button", { name: /^Run Now:/ })).toBeNull();
  });

  it("filters project jobs and uses batched status and job-specific run actions", async () => {
    const [base] = await mockedListJobsByProject("project-1");
    const [build] = await mockedListBuilds();
    mockedListBuilds.mockClear();
    mockedListJobsByProject.mockClear().mockResolvedValue([
      {
        ...base,
        latest_build: {
          id: "latest",
          build_number: 7,
          created_at: base.created_at,
          status: "failed",
        },
      },
      { ...base, id: "other-job", name: "deploy" },
      { ...base, project_id: "other", name: "foreign job" },
    ]);
    vi.mocked(runJob)
      .mockRejectedValueOnce(new APIError(403, "Permission denied"))
      .mockResolvedValueOnce(build);
    renderPage("authenticated", "/projects/project-1?view=jobs");
    expect(
      await screen.findByRole("link", {
        name: "Latest build for backend-ci: failed",
      }),
    ).toHaveAttribute("href", "/builds/latest");
    expect(screen.getByRole("link", { name: "deploy" })).toHaveAttribute(
      "href",
      "/jobs/other-job",
    );
    expect(screen.queryByText("foreign job")).toBeNull();
    const search = screen.getByRole("searchbox", {
      name: "Filter by job name",
    });
    fireEvent.change(search, { target: { value: "absent" } });
    expect(screen.getByText("No jobs match your filter.")).toBeTruthy();
    fireEvent.change(search, { target: { value: "BACK" } });
    expect(screen.queryByText("deploy")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Run Now: backend-ci" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Permission denied",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Run Now: backend-ci" }),
    );
    expect(await screen.findByText("Build investigation")).toBeTruthy();
    expect(vi.mocked(runJob).mock.calls.map(([id]) => id)).toEqual([
      "job-1",
      "job-1",
    ]);
    expect(mockedListJobsByProject.mock.calls).toEqual([["project-1"]]);
    expect(mockedListBuilds).not.toHaveBeenCalled();
    expect(listBuildsByJob).not.toHaveBeenCalled();
    expect(mockedListProjectMembers).not.toHaveBeenCalled();
  });

  it("polls quickly for active builds on older pages and discovers new builds while idle", async () => {
    const [base] = await mockedListBuilds();
    const first = Array.from({ length: 20 }, (_, index) => ({
      ...base,
      id: `build-${index}`,
      build_number: index + 1,
    }));
    const older = { ...base, build_number: 21 };
    mockedListBuilds
      .mockClear()
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce([{ ...older, status: "running" }])
      .mockResolvedValueOnce(first)
      .mockResolvedValueOnce([{ ...older, status: "success" }])
      .mockResolvedValueOnce([
        { ...base, id: "new", build_number: 22 },
        ...first.slice(0, -1),
      ])
      .mockResolvedValueOnce([first[19], older]);
    vi.useFakeTimers();
    const advance = (ms: number) =>
      act(async () => {
        await vi.advanceTimersByTimeAsync(ms);
      });
    try {
      renderPage("authenticated", "/projects/project-1?view=builds");
      await advance(5);
      fireEvent.click(screen.getByRole("button", { name: "Load more" }));
      await advance(5);
      expect(screen.getByText("Running")).toBeTruthy();
      await advance(FAST_POLL_INTERVAL);
      expect(screen.getByText("Success")).toBeTruthy();
      await advance(FAST_POLL_INTERVAL);
      expect(mockedListBuilds).toHaveBeenCalledTimes(4);
      await advance(SLOW_POLL_INTERVAL - FAST_POLL_INTERVAL);
      expect(screen.getByRole("link", { name: "Build #22" })).toHaveAttribute(
        "href",
        "/builds/new",
      );
      expect(
        mockedListBuilds.mock.calls.map(([params]) => params?.offset),
      ).toEqual([0, 20, 0, 20, 0, 20]);
    } finally {
      vi.useRealTimers();
    }
  });

  it("retains the first build page across errors and de-duplicates overlapping pages", async () => {
    const [base] = await mockedListBuilds();
    const page = Array.from({ length: 20 }, (_, index) => ({
      ...base,
      id: `b-${index}`,
      build_number: index + 1,
    }));
    mockedListBuilds
      .mockClear()
      .mockResolvedValueOnce(page)
      .mockRejectedValueOnce(new Error("offline"))
      .mockResolvedValueOnce([
        page[19],
        { ...base, build_number: 21, job_id: "job-1", job_name: null },
        { ...base, id: "foreign", project_id: "other", build_number: 22 },
      ]);
    renderPage("authenticated", "/projects/project-1?view=builds");
    fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("offline");
    expect(screen.getByRole("link", { name: "Build #1" })).toHaveAttribute(
      "href",
      "/builds/b-0",
    );
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: "Build #21" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "backend-ci" })).toHaveAttribute(
      "href",
      "/jobs/job-1",
    );
    expect(screen.getAllByRole("link", { name: "Build #20" })).toHaveLength(1);
    expect(screen.queryByText("Build #22")).toBeNull();
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
    expect(mockedListBuilds.mock.calls).toEqual(
      [0, 20, 20].map((offset) => [
        { project_id: "project-1", limit: 20, offset },
      ]),
    );
    expect(mockedListJobsByProject.mock.calls).toEqual([["project-1"]]);
  });

  it("requires confirmation, reports denied deletion, and returns to the directory on success", async () => {
    vi.mocked(deleteProject)
      .mockRejectedValueOnce(new APIError(403, "Forbidden"))
      .mockResolvedValueOnce(undefined);
    renderPage("authenticated", settingsURL);
    fireEvent.click(
      await screen.findByRole("button", { name: "Delete project" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(deleteProject).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Delete project" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm deletion" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "permission to delete",
    );
    fireEvent.click(screen.getByRole("button", { name: "Confirm deletion" }));
    expect(await screen.findByText("Project directory")).toBeTruthy();
    expect(deleteProject).toHaveBeenCalledWith("project-1");
  });

  it("keeps the job row compact when slug and description are provided", async () => {
    mockedListJobsByProject.mockResolvedValueOnce([
      {
        id: "job-1",
        project_id: "project-1",
        name: "backend-ci",
        priority: 5,
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: true,
        pull_request_enabled: false,
        push_branch: "main",
        pipeline_yaml: "version: 1",
        managed_image: null,
        enabled: true,
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:00:00Z",
        slug: "backend-ci",
        description: "Runs backend tests",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "backend-ci" })).toBeTruthy();
      expect(screen.queryByText("Slug: backend-ci")).toBeNull();
      expect(screen.queryByText("Runs backend tests")).toBeNull();
    });
  });
});
