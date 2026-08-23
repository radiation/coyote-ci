import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ProjectDetailPage } from "./ProjectDetailPage";
import { AuthContext } from "../auth-context";
import {
  APIError,
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

function renderPage(
  authStatus: "authenticated" | "unauthenticated" = "authenticated",
  initialEntry = "/projects/project-1",
) {
  if (authStatus === "authenticated") {
    mockedGetPublicProject.mockRejectedValueOnce(
      new APIError(404, "not found"),
    );
  }
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
          <Routes>
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

  it("renders project details, members, and jobs", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
      expect(screen.getByText("Project Summary")).toBeTruthy();
      expect(screen.getByText("Project Actions")).toBeTruthy();
      expect(screen.getByText("Jobs in This Project")).toBeTruthy();
      expect(screen.getByText("Core platform pipelines")).toBeTruthy();
      expect(screen.getByText("maintainer@example.com")).toBeTruthy();
      expect(screen.getByText("backend-ci")).toBeTruthy();
      expect(
        screen.getByText("https://github.com/example/backend.git"),
      ).toBeTruthy();
      expect(
        screen
          .getAllByRole("link", { name: "Create Job" })
          .every(
            (link) =>
              link.getAttribute("href") === "/jobs/new?project_id=project-1",
          ),
      ).toBe(true);
      expect(screen.getByRole("link", { name: "View Builds" })).toHaveAttribute(
        "href",
        "/builds?project_id=project-1",
      );
      expect(
        screen.getByRole("link", { name: "Browse Artifacts" }),
      ).toHaveAttribute("href", "/artifacts?project_id=project-1");
      expect(
        screen.getByRole("link", { name: "Open Release View" }),
      ).toHaveAttribute("href", "/artifacts/logical?project_id=project-1");
      expect(screen.getByRole("link", { name: "Open Job" })).toHaveAttribute(
        "href",
        "/jobs/job-1",
      );
      expect(screen.getByRole("link", { name: "Builds" })).toHaveAttribute(
        "href",
        "/builds?project_id=project-1",
      );
      expect(screen.getByRole("link", { name: "Artifacts" })).toHaveAttribute(
        "href",
        "/artifacts?project_id=project-1&job_id=job-1",
      );
      expect(screen.getByRole("link", { name: "release" })).toHaveAttribute(
        "href",
        "/jobs/job-queue-1",
      );
      expect(
        screen.getByRole("link", { name: "release-history" }),
      ).toHaveAttribute("href", "/jobs/job-recent-1");
    });
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

  it("redirects authenticated public project URLs before calling the ID API", async () => {
    mockedGetPublicProject.mockResolvedValueOnce({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "Core platform pipelines",
    });

    renderPage("authenticated", "/projects/platform");

    await waitFor(() => {
      expect(mockedGetProject).toHaveBeenCalledWith("project-1");
      expect(screen.getByText("Project Summary")).toBeTruthy();
    });
    expect(mockedGetPublicProject).toHaveBeenCalledWith("platform");
    expect(mockedGetProject).not.toHaveBeenCalledWith("platform");
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
    mockedGetProject.mockResolvedValueOnce({
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
      expect(screen.getByText("—")).toBeTruthy();
      expect(screen.getByText("No project members yet.")).toBeTruthy();
      expect(screen.getByText("No jobs in this project yet.")).toBeTruthy();
    });
  });

  it("requires user id before adding a project member", async () => {
    renderPage();

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

    await waitFor(() => {
      expect(screen.getByText("Loading members…")).toBeTruthy();
      expect(screen.getByText("Loading jobs…")).toBeTruthy();
    });
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

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("user-raw")).toBeTruthy();
      expect(screen.getByText("—")).toBeTruthy();
      expect(screen.getByLabelText("Role for user-raw")).toBeTruthy();
    });
  });

  it("renders the responsive two-column activity rail layout", async () => {
    const { container } = renderPage();

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
    });

    expect(container.querySelector(".detail-page-with-rail")).toBeTruthy();
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
    renderPage();

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

    renderPage();

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

    renderPage();

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

    renderPage();

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
