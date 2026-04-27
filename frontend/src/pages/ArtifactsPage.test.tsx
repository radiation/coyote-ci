import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { ArtifactsPage } from "./ArtifactsPage";
import { createJobVersionTags, listArtifacts } from "../api";

vi.mock("../api", () => ({
  createJobVersionTags: vi.fn(),
  listArtifacts: vi.fn(),
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
      <MemoryRouter initialEntries={["/artifacts"]}>
        <Routes>
          <Route path="/artifacts" element={<ArtifactsPage />} />
          <Route path="/builds/:id" element={<div>build detail</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ArtifactsPage", () => {
  const mockedListArtifacts = vi.mocked(listArtifacts);
  const mockedCreateJobVersionTags = vi.mocked(createJobVersionTags);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedCreateJobVersionTags.mockResolvedValue([]);
    mockedListArtifacts.mockImplementation(async (input) => {
      const type = input?.type ?? "";
      const query = input?.q ?? "";

      if (type === "docker_image") {
        return [
          {
            key: "job-1::images/backend-image.tar",
            path: "images/backend-image.tar",
            project_id: "project-1",
            job_id: "job-1",
            artifact_type: "docker_image",
            latest_created_at: "2026-04-25T10:00:00Z",
            versions: [
              {
                artifact_id: "artifact-docker-1",
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
            path: "packages/pkg-a.tgz",
            project_id: "project-1",
            job_id: "job-1",
            artifact_type: "npm_package",
            latest_created_at: "2026-04-25T09:00:00Z",
            versions: [
              {
                artifact_id: "artifact-pkg-1",
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

      return [
        {
          key: "job-1::packages/pkg-a.tgz",
          path: "packages/pkg-a.tgz",
          project_id: "project-1",
          job_id: "job-1",
          artifact_type: "npm_package",
          latest_created_at: "2026-04-25T09:00:00Z",
          versions: [
            {
              artifact_id: "artifact-pkg-1",
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
          path: "images/backend-image.tar",
          project_id: "project-1",
          job_id: "job-1",
          artifact_type: "docker_image",
          latest_created_at: "2026-04-25T10:00:00Z",
          versions: [
            {
              artifact_id: "artifact-docker-1",
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

  it("renders the artifact list and expands a selected artifact", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("packages/pkg-a.tgz")).toBeTruthy();
      expect(screen.getByText("images/backend-image.tar")).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: /packages\/pkg-a.tgz/i }),
    );

    await waitFor(() => {
      expect(screen.getByText("Build #41")).toBeTruthy();
      expect(screen.getByText(/Step 1: Publish package/)).toBeTruthy();
      expect(screen.getByText("v1.2.3")).toBeTruthy();
    });
  });

  it("applies search and type filters through the browse query", async () => {
    renderPage();

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenCalledWith({ q: "", type: "" });
    });

    fireEvent.change(screen.getByLabelText("Search artifacts"), {
      target: { value: "pkg-a" },
    });

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "pkg-a",
        type: "",
      });
    });

    fireEvent.change(screen.getByLabelText("Type"), {
      target: { value: "docker_image" },
    });

    await waitFor(() => {
      expect(mockedListArtifacts).toHaveBeenLastCalledWith({
        q: "pkg-a",
        type: "docker_image",
      });
    });
  });

  it("preserves tag assignment actions inside the expanded version view", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("packages/pkg-a.tgz")).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: /packages\/pkg-a.tgz/i }),
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
