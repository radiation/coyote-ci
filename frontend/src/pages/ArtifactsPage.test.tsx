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

function baseArtifact() {
  return {
    id: "artifact-1",
    name: "coyote-ci/package-a",
    path: "packages/pkg-a.tgz",
    artifact_type: "npm_package" as const,
    build_id: "build-1",
    build_number: 41,
    build_status: "success" as const,
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
    created_at: "2026-04-25T09:00:00Z",
  };
}

function buildArtifact(
  overrides: Partial<ReturnType<typeof baseArtifact>> = {},
) {
  return {
    ...baseArtifact(),
    ...overrides,
  };
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
      buildArtifact(),
      buildArtifact({
        id: "artifact-2",
        name: "coyote-ci/backend",
        path: "images/backend-image.tar",
        artifact_type: "docker_image",
        build_id: "build-2",
        build_number: 42,
        build_status: "failed",
        step_id: "step-2",
        step_index: 2,
        step_name: "Publish image",
        size_bytes: 4096,
        content_type: "application/x-tar",
        checksum_sha256: "docker-sha",
        download_url_path: "/builds/build-2/artifacts/artifact-2/download",
        created_at: "2026-04-25T10:00:00Z",
      }),
    ]);
  });

  it("renders artifact cards with metadata and navigation links", async () => {
    renderPage();

    expect(
      await screen.findByRole("link", { name: "Open logical browser" }),
    ).toHaveAttribute("href", "/artifacts/logical");

    const artifactLink = await screen.findByRole("link", {
      name: "coyote-ci/package-a",
    });
    const card = artifactLink.closest("article") as HTMLElement;

    expect(artifactLink).toHaveAttribute("href", "/artifacts/artifact-1");
    expect(within(card).getByText("npm package")).toBeTruthy();
    expect(within(card).getByText("Project Platform")).toBeTruthy();
    expect(
      within(card)
        .getAllByRole("link", { name: "Build #41" })
        .every((link) => link.getAttribute("href") === "/builds/build-1"),
    ).toBe(true);
    expect(
      within(card).getByRole("link", { name: "backend-ci" }),
    ).toHaveAttribute("href", "/jobs/job-1");
    expect(
      within(card).getByRole("link", { name: "Open artifact" }),
    ).toHaveAttribute("href", "/artifacts/artifact-1");
    expect(
      within(card).getByRole("link", { name: "View build" }),
    ).toHaveAttribute("href", "/builds/build-1");
    expect(
      within(card).getByRole("link", { name: "Download" }),
    ).toHaveAttribute(
      "href",
      "/api/builds/build-1/artifacts/artifact-1/download",
    );
    expect(within(card).getByText("0123456789ab…89abcdef")).toHaveAttribute(
      "title",
      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    );
  });

  it("shows an empty listing state with a concise repository message", async () => {
    mockedListArtifactCatalog.mockResolvedValueOnce([]);

    renderPage();

    expect(
      await screen.findByText("No artifacts have been published yet."),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Published build outputs will appear here with lineage back to their producing builds.",
      ),
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

  it("renders fallback labels when optional metadata is sparse", async () => {
    mockedListArtifactCatalog.mockResolvedValueOnce([
      buildArtifact({
        name: "   ",
        build_number: 0,
        job_name: "   ",
        step_name: "   ",
        step_index: 3,
        checksum_sha256: null,
        content_type: null,
      }),
    ]);

    renderPage();

    const artifactLink = await screen.findByRole("link", {
      name: "packages/pkg-a.tgz",
    });
    const card = artifactLink.closest("article") as HTMLElement;

    expect(
      within(card)
        .getAllByRole("link", { name: "Build build-1…" })
        .every((link) => link.getAttribute("href") === "/builds/build-1"),
    ).toBe(true);
    expect(within(card).getByRole("link", { name: "job-1…" })).toHaveAttribute(
      "href",
      "/jobs/job-1",
    );
    expect(within(card).getAllByText("Step 3").length).toBeGreaterThan(0);
    expect(within(card).getAllByText("—").length).toBeGreaterThan(0);
  });

  it("canonicalizes filters and clears them", async () => {
    mockedListArtifactCatalog.mockResolvedValue([]);

    renderPage([
      "/artifacts?q=%20pkg%20&project_id=project-1&job_id=job-1&build_id=build-1&page=2&pageSize=50",
    ]);

    expect(await screen.findByText("No artifacts on this page.")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe("");
    });
  });

  it("shows an error state when the catalog request fails", async () => {
    mockedListArtifactCatalog.mockRejectedValueOnce(new Error("boom"));

    renderPage();

    expect(
      await screen.findByText("Failed to load artifacts: Error: boom"),
    ).toBeTruthy();
  });
});
