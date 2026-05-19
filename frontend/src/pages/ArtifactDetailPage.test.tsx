import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ArtifactDetailPage } from "./ArtifactDetailPage";
import { createJobVersionTags, getArtifact } from "../api";
import type { ArtifactDetail, VersionTag } from "../types";

vi.mock("../api", () => ({
  getArtifact: vi.fn(),
  createJobVersionTags: vi.fn(),
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function buildVersionTag(overrides: Partial<VersionTag> = {}): VersionTag {
  return {
    id: "tag-1",
    job_id: "job-1",
    version: "1.2.3",
    target_type: "artifact",
    artifact_id: "artifact-1",
    created_at: "2026-04-25T09:15:00Z",
    ...overrides,
  };
}

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
    version_tags: [],
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

function renderPageWithClient(
  queryClient: QueryClient,
  options: {
    initialEntries?: string[];
    routePath?: string;
  } = {},
) {
  const {
    initialEntries = ["/artifacts/artifact-1"],
    routePath = "/artifacts/:id",
  } = options;

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={initialEntries}>
        <Routes>
          <Route path="/artifacts" element={<div>artifact catalog</div>} />
          <Route path={routePath} element={<ArtifactDetailPage />} />
          <Route path="/builds/:id" element={<div>build detail</div>} />
          <Route path="/jobs/:id" element={<div>job detail</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ArtifactDetailPage", () => {
  const mockedCreateJobVersionTags = vi.mocked(createJobVersionTags);
  const mockedGetArtifact = vi.mocked(getArtifact);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedGetArtifact.mockResolvedValue(buildArtifactDetail());
    mockedCreateJobVersionTags.mockResolvedValue([buildVersionTag()]);
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
    expect(screen.getByText("No version / tags yet.")).toBeTruthy();
    expect(screen.queryByText("Storage Key")).toBeNull();
    expect(screen.queryByText("build-1/packages/pkg-a.tgz")).toBeNull();
  });

  it("renders existing version tags", async () => {
    mockedGetArtifact.mockResolvedValueOnce(
      buildArtifactDetail({
        version_tags: [
          buildVersionTag(),
          buildVersionTag({ id: "tag-2", version: "latest" }),
        ],
      }),
    );

    renderPage();

    expect(await screen.findByText("1.2.3")).toBeTruthy();
    expect(screen.getByText("latest")).toBeTruthy();
  });

  it("creates a version tag from the artifact detail page", async () => {
    renderPage();

    const input = await screen.findByLabelText(
      "artifact-detail-version-artifact-1",
    );
    fireEvent.change(input, {
      target: { value: "release-42" },
    });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    await waitFor(() => {
      expect(mockedCreateJobVersionTags).toHaveBeenCalledWith("job-1", {
        version: "release-42",
        artifact_ids: ["artifact-1"],
      });
    });

    expect(await screen.findByText("1.2.3")).toBeTruthy();
  });

  it("shows duplicate or conflict errors when assigning a version tag fails", async () => {
    mockedCreateJobVersionTags.mockRejectedValueOnce(
      new Error("API 409: version tag already exists for target"),
    );

    renderPage();

    const input = await screen.findByLabelText(
      "artifact-detail-version-artifact-1",
    );
    fireEvent.change(input, {
      target: { value: "1.2.3" },
    });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    expect(
      await screen.findByText("version tag already exists for target"),
    ).toBeTruthy();
  });

  it("shows an artifact id required error when the route param is missing", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });
    queryClient.setQueryData(["artifact", undefined], buildArtifactDetail());

    renderPageWithClient(queryClient, {
      initialEntries: ["/artifacts/detail"],
      routePath: "/artifacts/detail",
    });

    const input = await screen.findByLabelText(
      "artifact-detail-version-artifact-1",
    );
    fireEvent.change(input, {
      target: { value: "release-42" },
    });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    expect(await screen.findByText("Artifact ID is required."));
  });

  it("keeps the empty state when a successful response returns tags for a different artifact", async () => {
    mockedCreateJobVersionTags.mockResolvedValueOnce([
      buildVersionTag({
        id: "tag-foreign",
        artifact_id: "artifact-2",
      }),
    ]);

    renderPage();

    const input = await screen.findByLabelText(
      "artifact-detail-version-artifact-1",
    );
    fireEvent.change(input, {
      target: { value: "release-42" },
    });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    expect(await screen.findByText("No version / tags yet.")).toBeTruthy();
    expect(screen.queryByText("release-42")).toBeNull();
  });

  it("does not duplicate an existing tag when the same tag id is returned again", async () => {
    mockedGetArtifact.mockResolvedValueOnce(
      buildArtifactDetail({
        version_tags: [buildVersionTag()],
      }),
    );
    mockedCreateJobVersionTags.mockResolvedValueOnce([buildVersionTag()]);

    renderPage();

    const input = await screen.findByLabelText(
      "artifact-detail-version-artifact-1",
    );
    fireEvent.change(input, {
      target: { value: "1.2.3" },
    });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    await waitFor(() => {
      expect(screen.getAllByText("1.2.3")).toHaveLength(1);
    });
  });

  it("skips cache updates when the artifact query cache entry is removed before success", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });
    mockedCreateJobVersionTags.mockImplementationOnce(async () => {
      queryClient.removeQueries({
        queryKey: ["artifact", "artifact-1"],
        exact: true,
      });
      return [buildVersionTag({ id: "tag-2", version: "release-42" })];
    });

    renderPageWithClient(queryClient);

    const input = await screen.findByRole("heading", {
      level: 2,
      name: "coyote-ci/package-a",
    });
    expect(input).toBeTruthy();

    fireEvent.change(
      screen.getByLabelText("artifact-detail-version-artifact-1"),
      {
        target: { value: "release-42" },
      },
    );
    fireEvent.submit(
      screen
        .getByLabelText("artifact-detail-version-artifact-1")
        .closest("form") as HTMLFormElement,
    );

    await waitFor(() => {
      expect(
        queryClient.getQueryData(["artifact", "artifact-1"]),
      ).toBeUndefined();
    });
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
      screen.getByText(
        "This artifact is not associated with a job, so new version / tags cannot be assigned.",
      ),
    ).toBeTruthy();
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

  it("does not duplicate the header path line for whitespace-only or whitespace-equivalent names", async () => {
    mockedGetArtifact.mockResolvedValueOnce(
      buildArtifactDetail({
        name: "   packages/pkg-a.tgz   ",
      }),
    );

    renderPage();

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "packages/pkg-a.tgz",
      }),
    ).toBeTruthy();

    const subtlePathLines = screen.getAllByText("packages/pkg-a.tgz", {
      selector: "p.subtle-text.artifact-mono, span",
    });
    expect(subtlePathLines).toHaveLength(1);
  });
});
