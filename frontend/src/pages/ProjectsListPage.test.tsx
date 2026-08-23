import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { ProjectsListPage } from "./ProjectsListPage";
import {
  createProject,
  deleteProject,
  listProjects,
  listPublicProjects,
} from "../api";
import { AuthContext } from "../auth-context";

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
  listPublicProjects: vi.fn(),
}));

function renderPage(
  authStatus: "authenticated" | "unauthenticated" = "authenticated",
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
        <MemoryRouter>
          <ProjectsListPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
}

describe("ProjectsListPage", () => {
  const mockedListProjects = vi.mocked(listProjects);
  const mockedCreateProject = vi.mocked(createProject);
  const mockedDeleteProject = vi.mocked(deleteProject);
  const mockedListPublicProjects = vi.mocked(listPublicProjects);

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
    mockedListPublicProjects.mockResolvedValue([]);
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

  it("uses the public endpoint and hides mutations for anonymous visitors", async () => {
    mockedListPublicProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Platform",
        slug: "platform",
        description: "Core platform pipelines",
      },
    ]);

    renderPage("unauthenticated");

    await waitFor(() => {
      expect(screen.getByText("Public Projects")).toBeTruthy();
    });
    expect(mockedListPublicProjects).toHaveBeenCalledOnce();
    expect(mockedListProjects).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "Create Project" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
  });
});
