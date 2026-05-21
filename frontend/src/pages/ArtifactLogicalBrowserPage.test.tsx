import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { ThemeProvider } from "../theme";
import { ArtifactLogicalBrowserPage } from "./ArtifactLogicalBrowserPage";
import { createJobVersionTags, listArtifacts, listProjects } from "../api";

vi.mock("../api", () => ({
  createJobVersionTags: vi.fn(),
  listArtifacts: vi.fn(),
  listProjects: vi.fn(),
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function LocationSearchProbe() {
  const location = useLocation();
  return <output data-testid="location-search">{location.search}</output>;
}

function buildBrowseItem(index: number) {
  return {
    key: `project-1:packages/pkg-${index}.tgz`,
    name: `coyote-ci/package-${index}`,
    path: `packages/pkg-${index}.tgz`,
    project_id: "project-1",
    project_name: "Platform",
    project_slug: "platform",
    job_id: "job-1",
    job_name: "backend-ci",
    artifact_type: "npm_package" as const,
    latest_created_at: "2026-04-25T09:00:00Z",
    versions: [
      {
        artifact_id: `artifact-${index}`,
        name: `coyote-ci/package-${index}`,
        build_id: `build-${index}`,
        build_number: 40 + index,
        build_status: "success" as const,
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-1",
        job_name: "backend-ci",
        step_id: `step-${index}`,
        step_index: 1,
        step_name: "Publish package",
        path: `packages/pkg-${index}.tgz`,
        size_bytes: 1024,
        content_type: "application/gzip",
        checksum_sha256: `sha-${index}`,
        storage_provider: "filesystem",
        download_url_path: `/builds/build-${index}/artifacts/artifact-${index}/download`,
        version_tags: [
          {
            id: `version-${index}`,
            job_id: "job-1",
            kind: "version" as const,
            version: `2026.04.${24 + index}`,
            target_type: "artifact",
            artifact_id: `artifact-${index}`,
            created_at: "2026-04-25T09:00:00Z",
          },
          {
            id: `channel-${index}`,
            job_id: "job-1",
            kind: "channel" as const,
            version: index === 1 ? "latest" : "beta",
            target_type: "artifact",
            artifact_id: `artifact-${index}`,
            created_at: "2026-04-25T09:00:00Z",
          },
        ],
        created_at: "2026-04-25T09:00:00Z",
      },
    ],
  };
}

function renderPage(initialEntries = ["/artifacts/logical"]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { retry: false },
    },
  });

  return render(
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={initialEntries}>
          <Routes>
            <Route
              path="/artifacts/logical"
              element={
                <>
                  <LocationSearchProbe />
                  <ArtifactLogicalBrowserPage />
                </>
              }
            />
            <Route path="/artifacts" element={<div>artifact catalog</div>} />
            <Route path="/artifacts/:id" element={<div>artifact detail</div>} />
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
    mockedListArtifacts.mockResolvedValue([buildBrowseItem(1)]);
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
      name: /coyote-ci\/package-1/i,
    });
    fireEvent.click(toggle);

    expect(
      screen.getByRole("heading", { level: 4, name: "Channels" }),
    ).toBeTruthy();
    expect(screen.getByText("Mutable aliases")).toBeTruthy();
    expect(screen.getByText("Points to 2026.04.25")).toBeTruthy();
    expect(screen.getAllByRole("link", { name: "Open" })[0]).toHaveAttribute(
      "href",
      "/artifacts/artifact-1",
    );
    expect(
      screen.getAllByRole("link", { name: "Download" })[0],
    ).toHaveAttribute(
      "href",
      "/api/builds/build-1/artifacts/artifact-1/download",
    );

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

  it("renders logical browser metadata and sanitizes invalid type params", async () => {
    renderPage(["/artifacts/logical?type=bad-type"]);

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "Logical Artifact Browser",
      }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        /Grouped release view for artifact versions and channels/,
      ),
    ).toBeTruthy();
    expect(screen.getByDisplayValue("All types")).toBeTruthy();

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenCalledWith({
        q: "",
        limit: 21,
        offset: 0,
      });
    });
  });

  it("drops an invalid type param when canonical search params are rewritten", async () => {
    mockedListArtifacts.mockResolvedValue([]);

    renderPage(["/artifacts/logical?q=pkg&type=bad-type&page=2"]);

    expect(await screen.findByText("No artifacts on page 2")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("Search artifacts"), {
      target: { value: "pkg two" },
    });

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe(
        "?q=pkg+two",
      );
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "pkg two",
        limit: 21,
        offset: 0,
      });
    });
  });

  it("forwards filters and pagination to the logical browser query", async () => {
    mockedListArtifacts.mockResolvedValue(
      Array.from({ length: 21 }, (_value, index) => buildBrowseItem(index + 1)),
    );

    renderPage([
      "/artifacts/logical?q=pkg&type=npm_package&project_id=project-1&page=2",
    ]);

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith(
        expect.objectContaining({
          q: "pkg",
          type: "npm_package",
          project_id: "project-1",
          limit: 21,
          offset: 20,
        }),
      );
    });
    expect(screen.getByTestId("location-search").textContent).toContain(
      "page=2",
    );
    expect(screen.getByText("Page 2")).toBeTruthy();
  });

  it("updates logical filters canonically and resets page state", async () => {
    mockedListArtifacts.mockResolvedValue([]);

    renderPage([
      "/artifacts/logical?q=%20pkg%20&type=npm_package&project_id=project-1&page=2",
    ]);

    expect(await screen.findByText("No artifacts on page 2")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("Search artifacts"), {
      target: { value: "   " },
    });

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe(
        "?type=npm_package&project_id=project-1",
      );
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "npm_package",
        project_id: "project-1",
        limit: 21,
        offset: 0,
      });
    });

    fireEvent.change(screen.getByLabelText("Type"), {
      target: { value: "" },
    });

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe(
        "?project_id=project-1",
      );
    });

    fireEvent.change(screen.getByLabelText("Project"), {
      target: { value: "" },
    });

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe("");
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        limit: 21,
        offset: 0,
      });
    });
  });

  it("forwards a job scope filter to the logical browser query", async () => {
    renderPage(["/artifacts/logical?project_id=project-1&job_id=job-1"]);

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        project_id: "project-1",
        job_id: "job-1",
        limit: 21,
        offset: 0,
      });
    });

    expect(screen.getByText("Job scope: job-1")).toBeTruthy();
    expect(screen.queryByLabelText("Job ID")).toBeNull();
  });

  it("clears the job scope chip without disturbing other filters", async () => {
    renderPage([
      "/artifacts/logical?project_id=project-1&job_id=job-1&type=npm_package",
    ]);

    expect(await screen.findByText("Job scope: job-1")).toBeTruthy();

    const clearScopeButton = screen.getByRole("button", {
      name: "Clear scope",
    });

    await waitFor(() => {
      expect(clearScopeButton).not.toBeDisabled();
    });

    fireEvent.click(clearScopeButton);

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe(
        "?type=npm_package&project_id=project-1",
      );
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "npm_package",
        project_id: "project-1",
        limit: 21,
        offset: 0,
      });
    });
  });

  it("navigates between logical artifact pages and clears filters", async () => {
    mockedListArtifacts
      .mockResolvedValueOnce(
        Array.from({ length: 21 }, (_value, index) =>
          buildBrowseItem(index + 1),
        ),
      )
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce(
        Array.from({ length: 21 }, (_value, index) =>
          buildBrowseItem(index + 1),
        ),
      )
      .mockResolvedValue([]);

    renderPage(["/artifacts/logical?q=pkg"]);

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Next" })).not.toBeDisabled();
    });

    fireEvent.click(screen.getByRole("button", { name: "Next" }));

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe(
        "?q=pkg&page=2",
      );
      expect(screen.getByText("No artifacts on page 2")).toBeTruthy();
      expect(screen.getByText("No artifacts on this page.")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Previous" }));

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe("?q=pkg");
      expect(screen.getByText("Showing 1-20; more available")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));

    await waitFor(() => {
      expect(screen.getByTestId("location-search").textContent).toBe("");
      expect(
        screen.getByText("No versioned artifacts or channels yet."),
      ).toBeTruthy();
    });
  });

  it("shows the filtered empty state when no logical artifacts match", async () => {
    mockedListArtifacts.mockResolvedValue([]);

    renderPage(["/artifacts/logical?q=pkg&type=npm_package"]);

    expect(
      await screen.findByText(
        "No release artifacts matched the current filters.",
      ),
    ).toBeTruthy();
    await waitFor(() => {
      expect(screen.queryByLabelText("Loading artifacts")).toBeNull();
    });
    expect(screen.getByText(/Adjust the search or filters/)).toBeTruthy();
  });

  it("shows the base empty state when no versioned artifacts exist", async () => {
    mockedListArtifacts.mockResolvedValue([]);

    renderPage();

    expect(
      await screen.findByText("No versioned artifacts or channels yet."),
    ).toBeTruthy();
    expect(
      screen.getByText(/Publish artifact versions or channels/),
    ).toBeTruthy();
  });

  it("shows an error state when the logical browser request fails", async () => {
    mockedListArtifacts.mockRejectedValueOnce(new Error("boom"));

    renderPage();

    expect(
      await screen.findByText("Failed to load artifacts: Error: boom"),
    ).toBeTruthy();
  });
});
