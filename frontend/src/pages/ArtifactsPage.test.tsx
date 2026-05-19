import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { ArtifactsPage } from "./ArtifactsPage";
import { listArtifactCatalog, listProjects } from "../api";
import { ThemeProvider } from "../theme";

vi.mock("../api", () => ({
  listArtifactCatalog: vi.fn(),
  listProjects: vi.fn(),
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function LocationSearchProbe() {
  const location = useLocation();
  return <output data-testid="location-search">{location.search}</output>;
}

function renderPage(initialEntries = ["/artifacts"]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
    },
  });

  return render(
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={initialEntries}>
          <Routes>
            <Route
              path="/artifacts"
              element={
                <>
                  <LocationSearchProbe />
                  <ArtifactsPage />
                </>
              }
            />
            <Route path="/artifacts/:id" element={<div>artifact detail</div>} />
            <Route path="/builds/:id" element={<div>build detail</div>} />
            <Route path="/jobs/:id" element={<div>job detail</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    </ThemeProvider>,
  );
}

describe("ArtifactsPage", () => {
  const mockedListArtifactCatalog = vi.mocked(listArtifactCatalog);
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
    mockedListArtifactCatalog.mockResolvedValue([
      {
        id: "artifact-1",
        name: "coyote-ci/package-a",
        path: "packages/pkg-a.tgz",
        artifact_type: "npm_package",
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
        size_bytes: 1024,
        content_type: "application/gzip",
        checksum_sha256: "pkg-sha",
        storage_provider: "filesystem",
        download_url_path: "/builds/build-1/artifacts/artifact-1/download",
        created_at: "2026-04-25T09:00:00Z",
      },
      {
        id: "artifact-2",
        name: "coyote-ci/backend",
        path: "images/backend-image.tar",
        artifact_type: "docker_image",
        build_id: "build-2",
        build_number: 42,
        build_status: "success",
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-1",
        job_name: "backend-ci",
        step_id: "step-2",
        step_index: 2,
        step_name: "Publish image",
        size_bytes: 4096,
        content_type: "application/x-tar",
        checksum_sha256: "docker-sha",
        storage_provider: "filesystem",
        download_url_path: "/builds/build-2/artifacts/artifact-2/download",
        created_at: "2026-04-25T10:00:00Z",
      },
    ]);
  });

  it("renders artifact rows with build, job, detail, and download links", async () => {
    renderPage();

    const artifactLink = await screen.findByRole("link", {
      name: "coyote-ci/package-a",
    });
    expect(artifactLink.getAttribute("href")).toBe("/artifacts/artifact-1");

    const row = artifactLink.closest("tr");
    expect(row).toBeTruthy();
    const scope = within(row as HTMLTableRowElement);
    expect(scope.getByRole("link", { name: "Build #41" })).toHaveAttribute(
      "href",
      "/builds/build-1",
    );
    expect(scope.getByRole("link", { name: "backend-ci" })).toHaveAttribute(
      "href",
      "/jobs/job-1",
    );
    expect(scope.getByRole("link", { name: "Details" })).toHaveAttribute(
      "href",
      "/artifacts/artifact-1",
    );
    expect(scope.getByRole("link", { name: "Download" })).toHaveAttribute(
      "href",
      "/api/builds/build-1/artifacts/artifact-1/download",
    );
    expect(scope.getByText("pkg-sha")).toBeTruthy();
  });

  it("shows an empty state when the catalog has no artifacts", async () => {
    mockedListArtifactCatalog.mockResolvedValueOnce([]);

    renderPage();

    expect(
      await screen.findByText("No artifacts have been published yet."),
    ).toBeTruthy();
  });

  it("forwards search and filters to the catalog query", async () => {
    renderPage();

    await waitFor(() => {
      expect(mockedListArtifactCatalog).toHaveBeenCalledWith({
        q: "",
        limit: 21,
        offset: 0,
      });
    });

    fireEvent.change(screen.getByLabelText("Search artifacts"), {
      target: { value: "pkg" },
    });
    fireEvent.change(screen.getByLabelText("Project"), {
      target: { value: "project-1" },
    });
    fireEvent.change(screen.getByLabelText("Job ID"), {
      target: { value: "job-1" },
    });
    fireEvent.change(screen.getByLabelText("Build ID"), {
      target: { value: "build-1" },
    });

    await waitFor(() => {
      expect(mockedListArtifactCatalog).toHaveBeenLastCalledWith({
        q: "pkg",
        project_id: "project-1",
        job_id: "job-1",
        build_id: "build-1",
        limit: 21,
        offset: 0,
      });
    });
  });
});
