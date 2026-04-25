import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { BuildDetailPage } from "./BuildDetailPage";
import {
  createJobVersionTags,
  getBuild,
  getBuildArtifacts,
  getBuildSteps,
} from "../api";

vi.mock("../api", () => ({
  createJobVersionTags: vi.fn(),
  getBuild: vi.fn(),
  getBuildSteps: vi.fn(),
  getBuildArtifacts: vi.fn(),
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
      <MemoryRouter initialEntries={["/builds/build-1"]}>
        <Routes>
          <Route path="/builds/:id" element={<BuildDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("BuildDetailPage artifacts", () => {
  const mockedGetBuild = vi.mocked(getBuild);
  const mockedGetBuildSteps = vi.mocked(getBuildSteps);
  const mockedGetBuildArtifacts = vi.mocked(getBuildArtifacts);
  const mockedCreateJobVersionTags = vi.mocked(createJobVersionTags);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedCreateJobVersionTags.mockResolvedValue([]);
    mockedGetBuild.mockResolvedValue({
      id: "build-1",
      project_id: "project-1",
      job_id: "job-1",
      status: "success",
      created_at: "2026-03-30T00:00:00Z",
      queued_at: "2026-03-30T00:00:01Z",
      started_at: "2026-03-30T00:00:02Z",
      finished_at: "2026-03-30T00:00:03Z",
      current_step_index: 1,
      error_message: null,
      pipeline_source: "repo",
      pipeline_path: "scenarios/success-basic/coyote.yml",
      trigger_kind: "webhook",
      scm_provider: "github",
      event_type: "push",
      trigger_ref: "main",
      actor: "octocat",
      trigger_commit_sha: "abc1234567890",
      source_commit_sha: "def9876543210",
      image: {
        source_kind: "managed",
        resolved_ref: "ghcr.io/coyote/go@sha256:123",
        managed_image_version_id: "image-version-1",
        version_tags: [
          {
            id: "tag-image-1",
            job_id: "job-1",
            version: "v1.2.3",
            target_type: "managed_image_version",
            managed_image_version_id: "image-version-1",
            created_at: "2026-03-30T00:00:03Z",
          },
        ],
      },
    });
    mockedGetBuildSteps.mockResolvedValue([]);
    mockedGetBuildArtifacts.mockResolvedValue([
      {
        id: "artifact-1",
        build_id: "build-1",
        step_id: null,
        path: "dist/app",
        size_bytes: 128,
        content_type: null,
        checksum_sha256: null,
        storage_provider: "filesystem",
        download_url_path: "/builds/build-1/artifacts/artifact-1/download",
        version_tags: [
          {
            id: "tag-artifact-1",
            job_id: "job-1",
            version: "2026.04.22",
            target_type: "artifact",
            artifact_id: "artifact-1",
            created_at: "2026-03-30T00:00:04Z",
          },
        ],
        created_at: "2026-03-30T00:00:04Z",
      },
    ]);
  });

  it("shows artifact row and download link", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Artifacts")).toBeTruthy();
      expect(screen.getByText("dist/app")).toBeTruthy();
    });

    const link = screen.getByRole("link", { name: "Download" });
    expect(link.getAttribute("href")).toBe(
      "/api/builds/build-1/artifacts/artifact-1/download",
    );
  });

  it("shows pipeline metadata when present", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Pipeline Source")).toBeTruthy();
      expect(screen.getByText("repo")).toBeTruthy();
      expect(screen.getByText("Pipeline Path")).toBeTruthy();
      expect(
        screen.getByText("scenarios/success-basic/coyote.yml"),
      ).toBeTruthy();
    });
  });

  it("shows trigger metadata and disambiguated commit SHAs", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("webhook")).toBeTruthy();
      expect(
        screen.getByText("github • main • abc1234 • octocat"),
      ).toBeTruthy();
      expect(screen.getByText("Trigger Commit")).toBeTruthy();
      expect(screen.getByText("Source Commit")).toBeTruthy();
      expect(screen.getByText("abc1234")).toBeTruthy();
      expect(screen.getByText("def9876")).toBeTruthy();
    });
  });

  it("shows version tags for artifacts and managed image versions", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Managed Build Image")).toBeTruthy();
      expect(screen.getByText("v1.2.3")).toBeTruthy();
      expect(screen.getByText("2026.04.22")).toBeTruthy();
    });
  });

  it("creates an artifact version tag from the detail page", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Artifacts")).toBeTruthy();
    });

    const input = screen.getByLabelText("artifact-version-artifact-1");
    fireEvent.change(input, {
      target: { value: "release-42" },
    });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    await waitFor(() => {
      expect(mockedCreateJobVersionTags).toHaveBeenCalledWith("job-1", {
        version: "release-42",
        artifact_ids: ["artifact-1"],
        managed_image_version_ids: undefined,
      });
    });
  });
});
