import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ArtifactDetailPage } from "./ArtifactDetailPage";
import { getArtifact, getBuild, getBuildArtifacts } from "../api";
import { APIError } from "../api/request";
import type { ArtifactDetail, Build, BuildArtifact } from "../types";

vi.mock("../api", () => ({
  getArtifact: vi.fn(),
  getBuild: vi.fn(),
  getBuildArtifacts: vi.fn(),
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
    checksum_sha256:
      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    storage_provider: "filesystem",
    download_url_path: "/builds/build-1/artifacts/artifact-1/download",
    version_tags: [
      {
        id: "tag-version",
        job_id: "job-1",
        kind: "version",
        version: "1.2.3",
        target_type: "artifact",
        artifact_id: "artifact-1",
        created_at: "2026-04-25T09:15:00Z",
      },
      {
        id: "tag-channel",
        job_id: "job-1",
        kind: "channel",
        version: "stable",
        target_type: "artifact",
        artifact_id: "artifact-1",
        created_at: "2026-04-25T09:16:00Z",
      },
    ],
    created_at: "2026-04-25T09:00:00Z",
    ...overrides,
  };
}

function buildBuild(overrides: Partial<Build> = {}): Build {
  return {
    id: "build-1",
    build_number: 41,
    project_id: "project-1",
    project_name: "Platform",
    project_slug: "platform",
    job_id: "job-1",
    job_name: "backend-ci",
    priority: 4,
    status: "success",
    created_at: "2026-04-25T08:55:00Z",
    queued_at: "2026-04-25T08:56:00Z",
    started_at: "2026-04-25T08:57:00Z",
    finished_at: "2026-04-25T09:00:00Z",
    current_step_index: 1,
    attempt_number: 1,
    error_message: null,
    repository_url: "https://github.com/example/platform",
    source_ref: "refs/heads/main",
    source_commit_sha: "95f09eb123456789",
    trigger_commit_sha: "95f09eb123456789",
    trigger_kind: "webhook",
    ...overrides,
  };
}

function buildRelatedArtifact(
  overrides: Partial<BuildArtifact> = {},
): BuildArtifact {
  return {
    id: "artifact-2",
    build_id: "build-1",
    step_id: null,
    path: "dist/backend-image.tar",
    name: "backend-image",
    artifact_type: "docker_image",
    size_bytes: 2048,
    content_type: "application/x-tar",
    checksum_sha256: "related-sha",
    storage_provider: "filesystem",
    download_url_path: "/builds/build-1/artifacts/artifact-2/download",
    created_at: "2026-04-25T09:00:30Z",
    version_tags: [],
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
          <Route path="/projects/:id" element={<div>project detail</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ArtifactDetailPage", () => {
  const mockedGetArtifact = vi.mocked(getArtifact);
  const mockedGetBuild = vi.mocked(getBuild);
  const mockedGetBuildArtifacts = vi.mocked(getBuildArtifacts);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedGetArtifact.mockResolvedValue(buildArtifactDetail());
    mockedGetBuild.mockResolvedValue(buildBuild());
    mockedGetBuildArtifacts.mockResolvedValue([
      buildArtifactDetail(),
      buildRelatedArtifact(),
    ]);
  });

  it("renders identity metadata, tag state, and download/build links", async () => {
    renderPage();

    await screen.findByRole("heading", {
      level: 2,
      name: "coyote-ci/package-a",
    });

    expect(
      screen.getByRole("link", { name: "← Back to artifacts" }),
    ).toHaveAttribute("href", "/artifacts");
    expect(
      screen.getByRole("link", { name: "View producing build" }),
    ).toHaveAttribute("href", "/builds/build-1");
    expect(screen.getByRole("link", { name: "Open build" })).toHaveAttribute(
      "href",
      "/builds/build-1",
    );
    expect(screen.getByRole("link", { name: "Download" })).toHaveAttribute(
      "href",
      "/api/builds/build-1/artifacts/artifact-1/download",
    );
    expect(screen.getByText("1.2.3")).toBeTruthy();
    expect(screen.getByText("stable")).toBeTruthy();
    expect(screen.getByText("0123456789ab…89abcdef")).toHaveAttribute(
      "title",
      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    );
    expect(screen.queryByLabelText("Artifact label")).toBeNull();
  });

  it("renders produced-by context and links back to the producing build", async () => {
    renderPage();

    const producedBy = (
      await screen.findByRole("heading", { name: "Produced By" })
    ).closest("section") as HTMLElement;

    await waitFor(() => {
      expect(
        within(producedBy).getByRole("link", { name: "Platform" }),
      ).toHaveAttribute("href", "/projects/project-1");
    });
    expect(
      within(producedBy).getByRole("link", { name: "backend-ci" }),
    ).toHaveAttribute("href", "/jobs/job-1");
    expect(
      within(producedBy).getByRole("link", { name: "Build #41" }),
    ).toHaveAttribute("href", "/builds/build-1");
  });

  it("renders provenance fields and source links when build metadata exists", async () => {
    renderPage();

    const provenance = (
      await screen.findByRole("heading", { name: "Source Provenance" })
    ).closest("section") as HTMLElement;

    await waitFor(() => {
      expect(
        within(provenance).getByRole("link", {
          name: "https://github.com/example/platform",
        }),
      ).toHaveAttribute("href", "https://github.com/example/platform");
    });
    expect(
      within(provenance).getByRole("link", { name: "refs/heads/main" }),
    ).toHaveAttribute("href", "https://github.com/example/platform/tree/main");
    expect(
      within(provenance).getByRole("link", { name: "95f09eb" }),
    ).toHaveAttribute(
      "href",
      "https://github.com/example/platform/commit/95f09eb123456789",
    );
  });

  it("renders related artifacts from the same build", async () => {
    renderPage();

    const related = (
      await screen.findByRole("heading", { name: "Related Artifacts" })
    ).closest("section") as HTMLElement;

    await waitFor(() => {
      expect(
        within(related).getByRole("link", { name: "backend-image" }),
      ).toHaveAttribute("href", "/artifacts/artifact-2");
    });
    expect(
      within(related).getByRole("link", { name: "Open artifact" }),
    ).toHaveAttribute("href", "/artifacts/artifact-2");
    expect(
      within(related).getByRole("link", { name: "Download" }),
    ).toHaveAttribute(
      "href",
      "/api/builds/build-1/artifacts/artifact-2/download",
    );
  });

  it("falls back gracefully when optional metadata and provenance are missing", async () => {
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
        version_tags: [],
      }),
    );
    mockedGetBuild.mockResolvedValueOnce(
      buildBuild({
        project_name: undefined,
        project_slug: undefined,
        job_id: undefined,
        job_name: undefined,
        repository_url: null,
        source_ref: null,
        source_commit_sha: null,
        trigger_commit_sha: null,
        trigger_kind: null,
      }),
    );
    mockedGetBuildArtifacts.mockResolvedValueOnce([
      buildArtifactDetail() as BuildArtifact,
    ]);

    renderPage();

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "packages/pkg-a.tgz",
      }),
    ).toBeTruthy();
    expect(screen.getAllByText("Build-level artifact").length).toBeGreaterThan(
      0,
    );
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    expect(screen.queryByRole("link", { name: "backend-ci" })).toBeNull();
    expect(
      await screen.findByText(
        "No other artifacts from this build were recorded.",
      ),
    ).toBeTruthy();
  });

  it("shows an error state when the artifact request fails", async () => {
    mockedGetArtifact.mockRejectedValueOnce(new Error("boom"));

    renderPage();

    expect(
      await screen.findByText("Failed to load artifact: Error: boom"),
    ).toBeTruthy();
  });

  it("shows a not found state when the backend returns artifact_not_found", async () => {
    mockedGetArtifact.mockRejectedValueOnce(
      new APIError(404, "artifact not found", "artifact_not_found"),
    );

    renderPage();

    expect(await screen.findByText("Artifact not found.")).toBeTruthy();
  });
});
