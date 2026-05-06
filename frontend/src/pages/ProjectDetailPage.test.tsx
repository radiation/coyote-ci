import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ProjectDetailPage } from "./ProjectDetailPage";
import {
  deleteProjectMember,
  getProject,
  listJobsByProject,
  listProjectMembers,
  listUsers,
  updateProjectMember,
  upsertProjectMember,
} from "../api";

vi.mock("../api", () => ({
  deleteProjectMember: vi.fn(),
  getProject: vi.fn(),
  listJobsByProject: vi.fn(),
  listProjectMembers: vi.fn(),
  listUsers: vi.fn(),
  updateProjectMember: vi.fn(),
  upsertProjectMember: vi.fn(),
}));

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
  const mockedListJobsByProject = vi.mocked(listJobsByProject);
  const mockedListProjectMembers = vi.mocked(listProjectMembers);
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
});
