import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ProjectDetailPage } from "./ProjectDetailPage";
import { getProject, listJobsByProject } from "../api";

vi.mock("../api", () => ({
  getProject: vi.fn(),
  listJobsByProject: vi.fn(),
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
  });

  it("renders project details and jobs", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Platform")).toBeTruthy();
      expect(screen.getByText("Core platform pipelines")).toBeTruthy();
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
});
