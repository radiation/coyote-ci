import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ArtifactDetailPage } from "./ArtifactDetailPage";
import { getArtifact } from "../api";
import type { ArtifactDetail } from "../types";

vi.mock("../api", () => ({
  getArtifact: vi.fn(),
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function buildArtifactDetail(
  overrides: Partial<ArtifactDetail> = {},
): ArtifactDetail {
  return {
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
    ...overrides,
  };
}

function renderPage(initialEntries = ["/artifacts/artifact-1"]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={initialEntries}>
        <Routes>
          <Route path="/artifacts" element={<div>artifact catalog</div>} />
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
    mockedGetArtifact.mockResolvedValue(buildArtifactDetail());
  });

  it("renders artifact metadata and build, job, and download links", async () => {
    mockedGetArtifact.mockResolvedValueOnce(
      Object.assign(buildArtifactDetail(), {
        storage_key: "build-1/packages/pkg-a.tgz",
      }),
    );

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
    expect(
      screen.getByRole("link", { name: "← Back to artifacts" }),
    ).toHaveAttribute("href", "/artifacts");
    expect(screen.getByText("Platform (platform)")).toBeTruthy();
    expect(screen.getByText("pkg-sha")).toBeTruthy();
    expect(screen.getAllByText("Step 1: Publish package").length).toBe(2);
    expect(screen.queryByText("Storage Key")).toBeNull();
    expect(screen.queryByText("build-1/packages/pkg-a.tgz")).toBeNull();
  });

  it("shows a loading state while artifact detail is in flight", () => {
    mockedGetArtifact.mockImplementationOnce(
      () => new Promise(() => {}) as ReturnType<typeof getArtifact>,
    );

    renderPage();

    expect(screen.getByText("Loading artifact…")).toBeTruthy();
  });

  it("shows an error state when the artifact request fails", async () => {
    mockedGetArtifact.mockRejectedValueOnce(new Error("boom"));

    renderPage();

    expect(
      await screen.findByText("Failed to load artifact: Error: boom"),
    ).toBeTruthy();
  });

  it("shows a not found state when no artifact detail is returned", async () => {
    mockedGetArtifact.mockResolvedValueOnce(null as never);

    renderPage();

    expect(await screen.findByText("Artifact not found.")).toBeTruthy();
  });

  it("renders fallback metadata when optional fields are missing", async () => {
    mockedGetArtifact.mockResolvedValueOnce(
      buildArtifactDetail({
        name: undefined,
        build_number: 0,
        project_name: undefined,
        project_slug: undefined,
        job_id: undefined,
        job_name: undefined,
        step_id: undefined,
        step_index: undefined,
        step_name: undefined,
        content_type: null,
        checksum_sha256: null,
      }),
    );

    renderPage();

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "packages/pkg-a.tgz",
      }),
    ).toBeTruthy();
    expect(screen.getAllByText("Build-level artifact").length).toBe(2);
    expect(screen.getByText("project-1")).toBeTruthy();
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    expect(screen.queryByRole("link", { name: "backend-ci" })).toBeNull();
    expect(
      screen.getAllByRole("link", { name: "Build build-1…" })[0],
    ).toHaveAttribute("href", "/builds/build-1");
  });

  it("renders id and slug fallbacks when names are blank", async () => {
    mockedGetArtifact.mockResolvedValueOnce(
      buildArtifactDetail({
        name: "   ",
        build_number: 0,
        project_name: "   ",
        project_slug: "platform",
        job_name: "   ",
        step_name: "   ",
        step_index: 2,
      }),
    );

    renderPage();

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "packages/pkg-a.tgz",
      }),
    ).toBeTruthy();
    expect(screen.getByText("platform")).toBeTruthy();
    expect(screen.getAllByText("Step 2").length).toBe(2);
    expect(screen.getAllByRole("link", { name: "job-1…" })[0]).toHaveAttribute(
      "href",
      "/jobs/job-1",
    );
    expect(
      screen.getAllByRole("link", { name: "Build build-1…" })[0],
    ).toHaveAttribute("href", "/builds/build-1");
  });
});
