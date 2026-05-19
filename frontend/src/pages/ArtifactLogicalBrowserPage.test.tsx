import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ThemeProvider } from "../theme";
import { ArtifactLogicalBrowserPage } from "./ArtifactLogicalBrowserPage";
import { createJobVersionTags, listArtifacts, listProjects } from "../api";

vi.mock("../api", () => ({
  createJobVersionTags: vi.fn(),
  listArtifacts: vi.fn(),
  listProjects: vi.fn(),
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false },
    },
  });

  return render(
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/artifacts/logical"]}>
          <Routes>
            <Route
              path="/artifacts/logical"
              element={<ArtifactLogicalBrowserPage />}
            />
            <Route path="/artifacts" element={<div>artifact catalog</div>} />
            <Route path="/builds/:id" element={<div>build detail</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    </ThemeProvider>,
  );
}

describe("ArtifactLogicalBrowserPage", () => {
  const mockedCreateJobVersionTags = vi.mocked(createJobVersionTags);
  const mockedListArtifacts = vi.mocked(listArtifacts);
  const mockedListProjects = vi.mocked(listProjects);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedListProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Platform",
        slug: "platform",
        description: "Core platform pipelines",
        created_at: "2026-04-25T08:00:00Z",
        updated_at: "2026-04-25T08:00:00Z",
      },
    ]);
    mockedListArtifacts.mockResolvedValue([
      {
        key: "project-1:packages/pkg-a.tgz",
        name: "coyote-ci/package-a",
        path: "packages/pkg-a.tgz",
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-1",
        job_name: "backend-ci",
        artifact_type: "npm_package",
        latest_created_at: "2026-04-25T09:00:00Z",
        versions: [
          {
            artifact_id: "artifact-1",
            name: "coyote-ci/package-a",
            build_id: "build-1",
            build_number: 41,
            build_status: "success",
            project_id: "project-1",
            project_name: "Platform",
            project_slug: "platform",
            job_id: "job-1",
            job_name: "backend-ci",
            step_id: "step-1",
            step_index: 1,
            step_name: "Publish package",
            path: "packages/pkg-a.tgz",
            size_bytes: 1024,
            content_type: "application/gzip",
            checksum_sha256: "pkg-sha",
            storage_provider: "filesystem",
            download_url_path: "/builds/build-1/artifacts/artifact-1/download",
            version_tags: [],
            created_at: "2026-04-25T09:00:00Z",
          },
        ],
      },
    ]);
    mockedCreateJobVersionTags.mockResolvedValue([
      {
        id: "tag-1",
        job_id: "job-1",
        version: "2026.04.25",
        target_type: "artifact",
        artifact_id: "artifact-1",
        created_at: "2026-04-25T10:00:00Z",
      },
    ]);
  });

  it("renders the grouped browser and preserves artifact version tag assignment", async () => {
    renderPage();

    expect(
      await screen.findByRole("link", { name: "Open persisted catalog" }),
    ).toHaveAttribute("href", "/artifacts");

    const toggle = await screen.findByRole("button", {
      name: /coyote-ci\/package-a/i,
    });
    fireEvent.click(toggle);

    fireEvent.change(
      await screen.findByLabelText("artifact-browser-version-artifact-1"),
      {
        target: { value: "2026.04.25" },
      },
    );
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));

    await waitFor(() => {
      expect(mockedCreateJobVersionTags).toHaveBeenCalledWith("job-1", {
        version: "2026.04.25",
        artifact_ids: ["artifact-1"],
      });
    });
  });
});
