import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { BuildDetailPage } from "./BuildDetailPage";
import { getBuild, getBuildArtifacts, getBuildSteps } from "../api";

vi.mock("../api", () => ({
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

  beforeEach(() => {
    vi.clearAllMocks();
    mockedGetBuild.mockResolvedValue({
      id: "build-1",
      project_id: "project-1",
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
});
