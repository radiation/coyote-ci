import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { ProjectsListPage } from "./ProjectsListPage";
import { createProject, deleteProject, listProjects } from "../api";

const navigateMock = vi.fn();

vi.mock("react-router-dom", async () => {
  const actual =
    await vi.importActual<typeof import("react-router-dom")>(
      "react-router-dom",
    );
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

vi.mock("../api", () => ({
  createProject: vi.fn(),
  deleteProject: vi.fn(),
  listProjects: vi.fn(),
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
      <MemoryRouter>
        <ProjectsListPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ProjectsListPage", () => {
  const mockedListProjects = vi.mocked(listProjects);
  const mockedCreateProject = vi.mocked(createProject);
  const mockedDeleteProject = vi.mocked(deleteProject);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedListProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Platform",
        slug: "platform",
        description: "Core platform pipelines",
        created_at: "2026-05-01T00:00:00Z",
        updated_at: "2026-05-01T00:00:00Z",
      },
    ]);
    mockedCreateProject.mockResolvedValue({
      id: "project-2",
      name: "Release",
      slug: "release",
      description: "Release automation",
      created_at: "2026-05-01T00:00:00Z",
      updated_at: "2026-05-01T00:00:00Z",
    });
    mockedDeleteProject.mockResolvedValue();
  });

  it("renders project list", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
      expect(screen.getByText("platform")).toBeTruthy();
      expect(screen.getByText("Core platform pipelines")).toBeTruthy();
    });
  });

  it("creates a project and navigates to its detail page", async () => {
    renderPage();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Release" },
    });
    fireEvent.change(screen.getByLabelText("Slug"), {
      target: { value: "release" },
    });
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Release automation" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Project" }));

    await waitFor(() => {
      expect(mockedCreateProject.mock.calls[0][0]).toEqual({
        name: "Release",
        slug: "release",
        description: "Release automation",
      });
      expect(navigateMock).toHaveBeenCalledWith("/projects/project-2");
    });
  });
});
