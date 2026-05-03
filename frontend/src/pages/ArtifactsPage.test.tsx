import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { ArtifactsPage } from "./ArtifactsPage";
import { createJobVersionTags, listArtifacts, listProjects } from "../api";
import { ThemeProvider } from "../theme";
import { installMockLocalStorage } from "../test/browserMocks";
import { THEME_STORAGE_KEY } from "../theme-context";

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

function renderPage(initialEntries = ["/artifacts"]) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { gcTime: Infinity },
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
            <Route path="/builds/:id" element={<div>build detail</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    </ThemeProvider>,
  );
}

describe("ArtifactsPage", () => {
  const mockedListArtifacts = vi.mocked(listArtifacts);
  const mockedCreateJobVersionTags = vi.mocked(createJobVersionTags);
  const mockedListProjects = vi.mocked(listProjects);
  const writeText = vi.fn();

  function buildArtifact(index: number) {
    return {
      key: `job-1::packages/pkg-${index}.tgz`,
      name: `coyote-ci/package-${index}`,
      path: `packages/pkg-${index}.tgz`,
      project_id: "project-1",
      job_id: "job-1",
      artifact_type: "npm_package" as const,
      latest_created_at: "2026-04-25T09:00:00Z",
      versions: [
        {
          artifact_id: `artifact-pkg-${index}`,
          name: `coyote-ci/package-${index}`,
          build_id: `build-${index}`,
          build_number: 40 + index,
          build_status: "success" as const,
          project_id: "project-1",
          job_id: "job-1",
          step_id: `step-${index}`,
          step_index: 1,
          step_name: "Publish package",
          path: `packages/pkg-${index}.tgz`,
          size_bytes: 1024,
          content_type: "application/gzip",
          checksum_sha256: `pkg-sha-${index}`,
          storage_provider: "filesystem" as const,
          download_url_path: `/builds/build-${index}/artifacts/artifact-pkg-${index}/download`,
          version_tags: [],
          created_at: "2026-04-25T09:00:00Z",
        },
      ],
    };
  }

  beforeEach(() => {
    vi.clearAllMocks();
    installMockLocalStorage();
    window.localStorage.clear();
    writeText.mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    mockedCreateJobVersionTags.mockResolvedValue([]);
    mockedListProjects.mockResolvedValue([
      {
        id: "project-1",
        name: "Platform",
        slug: "platform",
        description: "Core platform pipelines",
        created_at: "2026-04-25T08:00:00Z",
        updated_at: "2026-04-25T08:00:00Z",
      },
      {
        id: "project-2",
        name: "Release",
        slug: "release",
        description: "Release automation",
        created_at: "2026-04-25T08:00:00Z",
        updated_at: "2026-04-25T08:00:00Z",
      },
    ]);
    mockedListArtifacts.mockImplementation(async (input) => {
      const type = input?.type ?? "";
      const query = input?.q ?? "";
      const projectID = input?.project_id ?? "";
      const limit = input?.limit ?? 0;
      const offset = input?.offset ?? 0;

      if (projectID === "project-2") {
        return [
          {
            key: "job-2::packages/release.tgz",
            name: "release-package",
            path: "packages/release.tgz",
            project_id: "project-2",
            project_name: "Release",
            project_slug: "release",
            job_id: "job-2",
            job_name: "release-ci",
            artifact_type: "npm_package",
            latest_created_at: "2026-04-25T11:00:00Z",
            versions: [
              {
                artifact_id: "artifact-release-1",
                name: "release-package",
                build_id: "build-release-1",
                build_number: 55,
                build_status: "success",
                project_id: "project-2",
                project_name: "Release",
                project_slug: "release",
                job_id: "job-2",
                job_name: "release-ci",
                step_id: "step-release-1",
                step_index: 1,
                step_name: "Publish package",
                path: "packages/release.tgz",
                size_bytes: 2048,
                content_type: "application/gzip",
                checksum_sha256: "release-sha",
                storage_provider: "filesystem",
                download_url_path:
                  "/builds/build-release-1/artifacts/artifact-release-1/download",
                version_tags: [],
                created_at: "2026-04-25T11:00:00Z",
              },
            ],
          },
        ];
      }

      if (type === "docker_image") {
        return [
          {
            key: "job-1::images/backend-image.tar",
            name: "coyote-ci/backend",
            path: "images/backend-image.tar",
            project_id: "project-1",
            job_id: "job-1",
            artifact_type: "docker_image",
            latest_created_at: "2026-04-25T10:00:00Z",
            versions: [
              {
                artifact_id: "artifact-docker-1",
                name: "coyote-ci/backend",
                build_id: "build-2",
                build_number: 42,
                build_status: "success",
                project_id: "project-1",
                job_id: "job-1",
                step_id: "step-2",
                step_index: 2,
                step_name: "Publish image",
                path: "images/backend-image.tar",
                size_bytes: 4096,
                content_type: "application/x-tar",
                checksum_sha256: "docker-sha",
                storage_provider: "filesystem",
                download_url_path:
                  "/builds/build-2/artifacts/artifact-docker-1/download",
                version_tags: [],
                created_at: "2026-04-25T10:00:00Z",
              },
            ],
          },
        ];
      }

      if (query === "pkg-a") {
        return [
          {
            key: "job-1::packages/pkg-a.tgz",
            name: "coyote-ci/package-a",
            path: "packages/pkg-a.tgz",
            project_id: "project-1",
            job_id: "job-1",
            artifact_type: "npm_package",
            latest_created_at: "2026-04-25T09:00:00Z",
            versions: [
              {
                artifact_id: "artifact-pkg-1",
                name: "coyote-ci/package-a",
                build_id: "build-1",
                build_number: 41,
                build_status: "success",
                project_id: "project-1",
                job_id: "job-1",
                step_id: "step-1",
                step_index: 1,
                step_name: "Publish package",
                path: "packages/pkg-a.tgz",
                size_bytes: 1024,
                content_type: "application/gzip",
                checksum_sha256: "pkg-search-sha",
                storage_provider: "filesystem",
                download_url_path:
                  "/builds/build-1/artifacts/artifact-pkg-1/download",
                version_tags: [
                  {
                    id: "tag-search-1",
                    job_id: "job-1",
                    version: "v1.2.3",
                    target_type: "artifact",
                    artifact_id: "artifact-pkg-1",
                    created_at: "2026-04-25T09:05:00Z",
                  },
                ],
                created_at: "2026-04-25T09:00:00Z",
              },
            ],
          },
        ];
      }

      if (query === "missing") {
        return [];
      }

      if (limit === 11) {
        if (offset === 10) {
          return [buildArtifact(11)];
        }
        return [
          {
            key: "job-1::packages/pkg-a.tgz",
            name: "coyote-ci/package-a",
            path: "packages/pkg-a.tgz",
            project_id: "project-1",
            job_id: "job-1",
            artifact_type: "npm_package" as const,
            latest_created_at: "2026-04-25T09:00:00Z",
            versions: [
              {
                artifact_id: "artifact-pkg-1",
                name: "coyote-ci/package-a",
                build_id: "build-1",
                build_number: 41,
                build_status: "success" as const,
                project_id: "project-1",
                job_id: "job-1",
                step_id: "step-1",
                step_index: 1,
                step_name: "Publish package",
                path: "packages/pkg-a.tgz",
                size_bytes: 1024,
                content_type: "application/gzip",
                checksum_sha256: "pkg-sha",
                storage_provider: "filesystem" as const,
                download_url_path:
                  "/builds/build-1/artifacts/artifact-pkg-1/download",
                version_tags: [
                  {
                    id: "tag-1",
                    job_id: "job-1",
                    version: "v1.2.3",
                    target_type: "artifact",
                    artifact_id: "artifact-pkg-1",
                    created_at: "2026-04-25T09:05:00Z",
                  },
                ],
                created_at: "2026-04-25T09:00:00Z",
              },
            ],
          },
          {
            key: "job-1::images/backend-image.tar",
            name: "coyote-ci/backend",
            path: "images/backend-image.tar",
            project_id: "project-1",
            job_id: "job-1",
            artifact_type: "docker_image" as const,
            latest_created_at: "2026-04-25T10:00:00Z",
            versions: [
              {
                artifact_id: "artifact-docker-1",
                name: "coyote-ci/backend",
                build_id: "build-2",
                build_number: 42,
                build_status: "success" as const,
                project_id: "project-1",
                job_id: "job-1",
                step_id: "step-2",
                step_index: 2,
                step_name: "Publish image",
                path: "images/backend-image.tar",
                size_bytes: 4096,
                content_type: "application/x-tar",
                checksum_sha256: "docker-sha",
                storage_provider: "filesystem" as const,
                download_url_path:
                  "/builds/build-2/artifacts/artifact-docker-1/download",
                version_tags: [],
                created_at: "2026-04-25T10:00:00Z",
              },
            ],
          },
          ...Array.from({ length: 9 }, (_value, index) =>
            buildArtifact(index + 3),
          ),
        ];
      }

      return [
        {
          key: "job-1::packages/pkg-a.tgz",
          name: "coyote-ci/package-a",
          path: "packages/pkg-a.tgz",
          project_id: "project-1",
          job_id: "job-1",
          artifact_type: "npm_package",
          latest_created_at: "2026-04-25T09:00:00Z",
          versions: [
            {
              artifact_id: "artifact-pkg-1",
              name: "coyote-ci/package-a",
              build_id: "build-1",
              build_number: 41,
              build_status: "success",
              project_id: "project-1",
              job_id: "job-1",
              step_id: "step-1",
              step_index: 1,
              step_name: "Publish package",
              path: "packages/pkg-a.tgz",
              size_bytes: 1024,
              content_type: "application/gzip",
              checksum_sha256: "pkg-sha",
              storage_provider: "filesystem",
              download_url_path:
                "/builds/build-1/artifacts/artifact-pkg-1/download",
              version_tags: [
                {
                  id: "tag-1",
                  job_id: "job-1",
                  version: "v1.2.3",
                  target_type: "artifact",
                  artifact_id: "artifact-pkg-1",
                  created_at: "2026-04-25T09:05:00Z",
                },
              ],
              created_at: "2026-04-25T09:00:00Z",
            },
          ],
        },
        {
          key: "job-1::images/backend-image.tar",
          name: "coyote-ci/backend",
          path: "images/backend-image.tar",
          project_id: "project-1",
          job_id: "job-1",
          artifact_type: "docker_image",
          latest_created_at: "2026-04-25T10:00:00Z",
          versions: [
            {
              artifact_id: "artifact-docker-1",
              name: "coyote-ci/backend",
              build_id: "build-2",
              build_number: 42,
              build_status: "success",
              project_id: "project-1",
              job_id: "job-1",
              step_id: "step-2",
              step_index: 2,
              step_name: "Publish image",
              path: "images/backend-image.tar",
              size_bytes: 4096,
              content_type: "application/x-tar",
              checksum_sha256: "docker-sha",
              storage_provider: "filesystem",
              download_url_path:
                "/builds/build-2/artifacts/artifact-docker-1/download",
              version_tags: [],
              created_at: "2026-04-25T10:00:00Z",
            },
          ],
        },
      ];
    });
  });

  it("renders artifact results with a persisted dark theme", async () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "dark");

    renderPage();

    await screen.findByText("coyote-ci/package-a");
    expect(document.documentElement).toHaveAttribute("data-theme", "dark");
    fireEvent.click(
      screen.getByRole("button", { name: /coyote-ci\/package-a/i }),
    );
    expect(screen.getAllByText("Build #41").length).toBeGreaterThan(0);
  });

  it("renders the artifact list and expands a selected artifact", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("coyote-ci/package-a")).toBeTruthy();
      expect(screen.getByText("coyote-ci/backend")).toBeTruthy();
    });

    expect(screen.getByText("Build #41")).toBeTruthy();
    expect(screen.getAllByText("1 version").length).toBeGreaterThan(0);
    expect(screen.getByText("v1.2.3")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: /coyote-ci\/package-a/i }),
    );

    await waitFor(() => {
      expect(screen.getAllByText("Build #41").length).toBeGreaterThan(0);
      expect(screen.getByText(/Step 1: Publish package/)).toBeTruthy();
      expect(screen.getAllByText("v1.2.3").length).toBeGreaterThan(0);
      expect(screen.getByText("Most recent first")).toBeTruthy();
    });
  });

  it("applies search and type filters through the browse query", async () => {
    renderPage();

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenCalledWith({
        q: "",
        type: "",
        limit: 11,
        offset: 0,
      });
    });

    fireEvent.change(screen.getByLabelText("Search artifacts"), {
      target: { value: "pkg-a" },
    });

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "pkg-a",
        type: "",
        limit: 11,
        offset: 0,
      });
    });

    fireEvent.change(screen.getByLabelText("Type"), {
      target: { value: "docker_image" },
    });

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "pkg-a",
        type: "docker_image",
        limit: 11,
        offset: 0,
      });
    });
  });

  it("forwards the project filter from the query string", async () => {
    renderPage(["/artifacts?project_id=project-2"]);

    await screen.findByText("release-package");
    expect(screen.getByLabelText("Project")).toHaveValue("project-2");
    expect(mockedListArtifacts).toHaveBeenCalledWith({
      q: "",
      type: "",
      project_id: "project-2",
      limit: 11,
      offset: 0,
    });
  });

  it("moves to the next page and reflects it in the URL", async () => {
    renderPage();

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "",
        limit: 11,
        offset: 0,
      });
    });

    const nextButton = await screen.findByRole("button", { name: "Next" });
    fireEvent.click(nextButton);

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "",
        limit: 11,
        offset: 10,
      });
      expect(screen.getByText("Page 2")).toBeTruthy();
      expect(screen.getByTestId("location-search").textContent).toBe("?page=2");
    });
  });

  it("resets to the first page when filters change", async () => {
    renderPage(["/artifacts?page=2"]);

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "",
        limit: 11,
        offset: 10,
      });
      expect(screen.getByText("Page 2")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Search artifacts"), {
      target: { value: "pkg-a" },
    });

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "pkg-a",
        type: "",
        limit: 11,
        offset: 0,
      });
      expect(screen.getByText("Page 1")).toBeTruthy();
      expect(screen.getByTestId("location-search").textContent).toBe(
        "?q=pkg-a",
      );
    });
  });

  it("clears composed filters and returns to the default artifact view", async () => {
    renderPage(["/artifacts?q=pkg-a&type=docker_image&page=2"]);

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "pkg-a",
        type: "docker_image",
        limit: 11,
        offset: 10,
      });
    });

    const clearFiltersButton = screen.getByRole("button", {
      name: "Clear filters",
    });
    await waitFor(() => {
      expect(clearFiltersButton).not.toHaveAttribute("disabled");
    });
    fireEvent.click(clearFiltersButton);

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "",
        limit: 11,
        offset: 0,
      });
      expect(screen.getByLabelText("Search artifacts")).toHaveValue("");
      expect(screen.getByLabelText("Type")).toHaveValue("");
      expect(screen.getByTestId("location-search").textContent).toBe("");
    });
  });

  it("shows a useful empty state for filtered artifact results", async () => {
    renderPage();

    await screen.findByText("coyote-ci/package-a");

    fireEvent.change(screen.getByLabelText("Search artifacts"), {
      target: { value: "missing" },
    });

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "missing",
        type: "",
        limit: 11,
        offset: 0,
      });
      expect(
        screen.getByText("No artifacts matched the current filters."),
      ).toBeTruthy();
      expect(screen.getByText("No matching artifacts")).toBeTruthy();
    });
  });

  it("updates page size and jumps directly to a requested page", async () => {
    renderPage();

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "",
        limit: 11,
        offset: 0,
      });
    });

    fireEvent.change(screen.getByLabelText("Items per page"), {
      target: { value: "25" },
    });

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "",
        limit: 26,
        offset: 0,
      });
      expect(screen.getByText("Page 1")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Jump to page"), {
      target: { value: "3" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Go" }));

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "",
        type: "",
        limit: 26,
        offset: 50,
      });
      expect(screen.getByText("Page 3")).toBeTruthy();
      expect(screen.getByTestId("location-search").textContent).toBe(
        "?page=3&pageSize=25",
      );
    });
  });

  it("hydrates filters and pagination from the URL", async () => {
    renderPage(["/artifacts?q=pkg-a&type=docker_image&page=3&pageSize=25"]);

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "pkg-a",
        type: "docker_image",
        limit: 26,
        offset: 50,
      });
    });

    expect(screen.getByLabelText("Search artifacts")).toHaveValue("pkg-a");
    expect(screen.getByLabelText("Type")).toHaveValue("docker_image");
    expect(screen.getByLabelText("Items per page")).toHaveValue("25");
    expect(screen.getByLabelText("Jump to page")).toHaveValue(3);
    expect(screen.getByTestId("location-search").textContent).toBe(
      "?q=pkg-a&type=docker_image&page=3&pageSize=25",
    );
  });

  it("copies a shareable URL for the current artifact view", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });

    try {
      renderPage(["/artifacts?q=pkg-a&page=2&pageSize=25"]);

      await waitFor(() => {
        expect(mockedListArtifacts).toHaveBeenLastCalledWith({
          q: "pkg-a",
          type: "",
          limit: 26,
          offset: 25,
        });
      });

      fireEvent.click(screen.getByRole("button", { name: "Copy link" }));

      await waitFor(() => {
        expect(writeText).toHaveBeenCalledWith(
          "http://localhost:3000/artifacts?q=pkg-a&page=2&pageSize=25",
        );
        expect(screen.getByText("Link copied.")).toBeTruthy();
      });

      await act(async () => {
        await vi.advanceTimersByTimeAsync(2100);
      });

      await waitFor(() => {
        expect(screen.queryByText("Link copied.")).toBeNull();
      });
    } finally {
      vi.useRealTimers();
    }
  });

  it("preserves tag assignment actions inside the expanded version view", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("coyote-ci/package-a")).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: /coyote-ci\/package-a/i }),
    );

    const input = await screen.findByLabelText(
      "artifact-browser-version-artifact-pkg-1",
    );
    fireEvent.change(input, { target: { value: "release-42" } });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    await waitFor(() => {
      expect(mockedCreateJobVersionTags).toHaveBeenCalledWith("job-1", {
        version: "release-42",
        artifact_ids: ["artifact-pkg-1"],
      });
    });
  });
});
