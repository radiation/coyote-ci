import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ProjectDetailPage } from "./ProjectDetailPage";
import {
  APIError,
  deleteProjectMember,
  getProject,
  listBuilds,
  listJobsByProject,
  listProjectMembers,
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
    listBuilds: vi.fn(),
    listJobsByProject: vi.fn(),
    listProjectMembers: vi.fn(),
    listQueue: vi.fn(),
    listUsers: vi.fn(),
    updateProjectMember: vi.fn(),
    upsertProjectMember: vi.fn(),
  };
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/projects/project-1"]}>
        <Routes>
          <Route path="/projects/:id" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ProjectDetailPage", () => {
  const mockedGetProject = vi.mocked(getProject);
  const mockedListBuilds = vi.mocked(listBuilds);
  const mockedListJobsByProject = vi.mocked(listJobsByProject);
  const mockedListProjectMembers = vi.mocked(listProjectMembers);
  const mockedListQueue = vi.mocked(listQueue);
  const mockedListUsers = vi.mocked(listUsers);
  const mockedUpsertProjectMember = vi.mocked(upsertProjectMember);
  const mockedUpdateProjectMember = vi.mocked(updateProjectMember);
  const mockedDeleteProjectMember = vi.mocked(deleteProjectMember);

  beforeEach(() => {
    vi.clearAllMocks();
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
  });

  it("renders project details, members, and jobs", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
      expect(screen.getByText("Core platform pipelines")).toBeTruthy();
      expect(screen.getByText("maintainer@example.com")).toBeTruthy();
      expect(screen.getByText("backend-ci")).toBeTruthy();
      expect(
        screen.getByText("https://github.com/example/backend.git"),
      ).toBeTruthy();
      expect(screen.getByRole("link", { name: "Create Job" })).toHaveAttribute(
        "href",
        "/jobs/new?project_id=project-1",
      );
      expect(screen.getByRole("link", { name: "View Builds" })).toHaveAttribute(
        "href",
        "/builds?project_id=project-1",
      );
      expect(
        screen.getByRole("link", { name: "Browse Artifacts" }),
      ).toHaveAttribute("href", "/artifacts?project_id=project-1");
      expect(screen.getByRole("link", { name: "release" })).toHaveAttribute(
        "href",
        "/jobs/job-queue-1",
      );
      expect(
        screen.getByRole("link", { name: "release-history" }),
      ).toHaveAttribute("href", "/jobs/job-recent-1");
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
});
