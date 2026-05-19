import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ArtifactDetailPage } from "./ArtifactDetailPage";
import { getArtifact } from "../api";

vi.mock("../api", () => ({
  getArtifact: vi.fn(),
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/artifacts/artifact-1"]}>
        <Routes>
          <Route path="/artifacts/:id" element={<ArtifactDetailPage />} />
          <Route path="/builds/:id" element={<div>build detail</div>} />
          <Route path="/jobs/:id" element={<div>job detail</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ArtifactDetailPage", () => {
  const mockedGetArtifact = vi.mocked(getArtifact);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedGetArtifact.mockResolvedValue({
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
      storage_key: "build-1/packages/pkg-a.tgz",
      download_url_path: "/builds/build-1/artifacts/artifact-1/download",
      created_at: "2026-04-25T09:00:00Z",
    });
  });

  it("renders artifact metadata and build, job, and download links", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        screen.getAllByRole("link", { name: "Build #41" })[0],
      ).toHaveAttribute("href", "/builds/build-1");
    });

    expect(
      screen.getAllByRole("link", { name: "backend-ci" })[0],
    ).toHaveAttribute("href", "/jobs/job-1");
    expect(screen.getByRole("link", { name: "Download" })).toHaveAttribute(
      "href",
      "/api/builds/build-1/artifacts/artifact-1/download",
    );
    expect(screen.getByText("build-1/packages/pkg-a.tgz")).toBeTruthy();
    expect(screen.getByText("pkg-sha")).toBeTruthy();
    expect(screen.getAllByText("Step 1: Publish package").length).toBe(2);
  });
});
