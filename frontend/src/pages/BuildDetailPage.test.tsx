import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { BuildDetailPage } from "./BuildDetailPage";
import {
  cancelBuild,
  createJobVersionTags,
  getBuild,
  getBuildArtifacts,
  getBuildSteps,
  rerunBuild,
} from "../api";
import type { Build, BuildArtifact, BuildStep } from "../types";
import { formatCompactTime } from "../utils/time";

vi.mock("../api", () => ({
  cancelBuild: vi.fn(),
  createJobVersionTags: vi.fn(),
  getBuild: vi.fn(),
  getBuildSteps: vi.fn(),
  getBuildArtifacts: vi.fn(),
  rerunBuild: vi.fn(),
  artifactDownloadURL: (path: string) => `/api${path}`,
}));

function makeBuild(overrides: Partial<Build> = {}): Build {
  return {
    id: "build-1",
    build_number: 21,
    project_id: "project-1",
    project_name: "Platform",
    project_slug: "platform",
    job_id: "job-1",
    job_name: "release",
    priority: 9,
    status: "failed",
    created_at: "2026-03-30T00:00:00Z",
    queued_at: "2026-03-30T00:00:05Z",
    started_at: "2026-03-30T00:01:00Z",
    finished_at: "2026-03-30T00:02:05Z",
    current_step_index: 1,
    error_message: "Build failed during deploy.",
    pipeline_source: "repo",
    pipeline_path: "scenarios/success-basic/coyote.yml",
    trigger_kind: "webhook",
    scm_provider: "github",
    event_type: "push",
    repository_owner: "example",
    repository_name: "platform",
    repository_url: "https://github.com/example/platform",
    trigger_ref: "refs/heads/main",
    ref_type: "branch",
    actor: "octocat",
    trigger_commit_sha: "abc1234567890",
    source_commit_sha: "def9876543210",
    image: {
      source_kind: "managed",
      requested_ref: "ghcr.io/coyote/go:latest",
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
    ...overrides,
  };
}

function makeStep(overrides: Partial<BuildStep> = {}): BuildStep {
  return {
    id: "step-1",
    build_id: "build-1",
    step_index: 0,
    name: "compile",
    command: "make compile",
    status: "success",
    worker_id: "worker-a",
    started_at: "2026-03-30T00:01:00Z",
    finished_at: "2026-03-30T00:01:20Z",
    exit_code: 0,
    stdout: null,
    stderr: null,
    error_message: null,
    ...overrides,
  };
}

function makeArtifact(overrides: Partial<BuildArtifact> = {}): BuildArtifact {
  return {
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
    ...overrides,
  };
}

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

describe("BuildDetailPage", () => {
  const mockedGetBuild = vi.mocked(getBuild);
  const mockedCancelBuild = vi.mocked(cancelBuild);
  const mockedRerunBuild = vi.mocked(rerunBuild);
  const mockedGetBuildSteps = vi.mocked(getBuildSteps);
  const mockedGetBuildArtifacts = vi.mocked(getBuildArtifacts);
  const mockedCreateJobVersionTags = vi.mocked(createJobVersionTags);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedCancelBuild.mockResolvedValue(
      makeBuild({ status: "canceled", finished_at: "2026-03-30T00:01:30Z" }),
    );
    mockedRerunBuild.mockResolvedValue(
      makeBuild({
        id: "build-rerun-1",
        build_number: 22,
        status: "queued",
        started_at: null,
        finished_at: null,
        error_message: null,
        rerun_of_build_id: "build-1",
      }),
    );
    mockedCreateJobVersionTags.mockResolvedValue([]);
    mockedGetBuild.mockResolvedValue(makeBuild());
    mockedGetBuildSteps.mockResolvedValue([
      makeStep(),
      makeStep({
        id: "step-2",
        step_index: 1,
        name: "deploy",
        command: "./scripts/deploy.sh",
        status: "failed",
        worker_id: "worker-b",
        started_at: "2026-03-30T00:01:20Z",
        finished_at: "2026-03-30T00:02:05Z",
        exit_code: 1,
        error_message: "remote deploy failed",
      }),
      makeStep({
        id: "step-3",
        step_index: 2,
        name: "notify",
        command: "./scripts/notify.sh",
        status: "pending",
        worker_id: null,
        started_at: null,
        finished_at: null,
        exit_code: null,
      }),
    ]);
    mockedGetBuildArtifacts.mockResolvedValue([makeArtifact()]);
  });

  it("renders the summary header, links, timestamps, and duration", async () => {
    renderPage();

    await screen.findByRole("heading", { level: 2, name: "Build #21" });
    const summaryPanel = screen
      .getByText("Build ID build-1")
      .closest("section") as HTMLElement;

    expect(screen.getByRole("link", { name: "Platform" })).toHaveAttribute(
      "href",
      "/projects/project-1",
    );
    expect(screen.getByRole("link", { name: "release" })).toHaveAttribute(
      "href",
      "/jobs/job-1",
    );
    expect(
      screen.getByText("github • refs/heads/main • abc1234 • octocat"),
    ).toBeTruthy();
    expect(screen.getByText("Duration")).toBeTruthy();
    expect(screen.getByText("1m 5s")).toBeTruthy();
    expect(screen.getByText("Build failed during deploy.")).toBeTruthy();
    expect(
      within(summaryPanel).getByText(formatCompactTime("2026-03-30T00:01:00Z")),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Back to builds" }),
    ).toHaveAttribute("href", "/builds");
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
  });

  it("renders failed build state with a visible failed step and failure details", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Execution timeline" });

    expect(
      screen.getByRole("heading", { name: "Execution summary" }),
    ).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Logs" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Artifacts" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Provenance" })).toBeTruthy();
    expect(screen.getByText("Failed at step 1")).toBeTruthy();
    expect(screen.getAllByText("Exit code 1").length).toBe(2);
    expect(screen.getAllByText("remote deploy failed").length).toBe(2);
    expect(screen.getByText("compile")).toBeTruthy();
    expect(screen.getAllByText("deploy").length).toBe(2);
    expect(screen.getByText("notify")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "Open logs" })).toHaveLength(
      3,
    );
    expect(
      screen.getByRole("link", { name: "Step 1 · deploy" }),
    ).toHaveAttribute("href", "#step-1");
    expect(
      screen.getByRole("link", { name: "example/platform" }),
    ).toHaveAttribute("href", "https://github.com/example/platform");
    expect(screen.getByText("v1.2.3")).toBeTruthy();
    const failedStepCard = screen.getAllByText("deploy")[1]?.closest("article");
    expect(failedStepCard?.className).toContain("is-failed");

    const artifactLink = screen.getByRole("link", { name: "dist/app" });
    expect(artifactLink.getAttribute("href")).toBe("/artifacts/artifact-1");

    const link = screen.getByRole("link", { name: "Download" });
    expect(link.getAttribute("href")).toBe(
      "/api/builds/build-1/artifacts/artifact-1/download",
    );
  });

  it("renders running build state with current and pending steps cleanly", async () => {
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({
        status: "running",
        finished_at: null,
        error_message: null,
      }),
    );
    mockedGetBuildSteps.mockResolvedValueOnce([
      makeStep({
        id: "step-1",
        step_index: 0,
        name: "compile",
        status: "success",
      }),
      makeStep({
        id: "step-2",
        step_index: 1,
        name: "deploy",
        status: "running",
        started_at: "2026-03-30T00:01:20Z",
        finished_at: null,
        exit_code: null,
        error_message: null,
      }),
      makeStep({
        id: "step-3",
        step_index: 2,
        name: "notify",
        status: "pending",
        worker_id: null,
        started_at: null,
        finished_at: null,
        exit_code: null,
      }),
    ]);
    mockedGetBuildArtifacts.mockResolvedValueOnce([]);

    renderPage();

    await screen.findByText("Currently running");
    const summaryPanel = screen
      .getByText("Build ID build-1")
      .closest("section") as HTMLElement;

    expect(
      screen.getByRole("link", { name: "Step 1 · deploy" }),
    ).toHaveAttribute("href", "#step-1");
    expect(screen.getByText("1 pending step")).toBeTruthy();
    expect(screen.getByText("Step 1 · Current step")).toBeTruthy();
    expect(summaryPanel.textContent).toContain("Current step1 of 3");
    expect(summaryPanel.textContent).not.toContain("Duration—");
    expect(screen.getAllByText("Pending").length).toBe(2);
    expect(
      screen.getByText("No artifacts were collected for this build."),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeTruthy();
  });

  it("confirms and cancels cancelable builds", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({ status: "running", finished_at: null, error_message: null }),
    );
    mockedGetBuildSteps.mockResolvedValueOnce([
      makeStep({ status: "running", finished_at: null, exit_code: null }),
    ]);
    mockedGetBuildArtifacts.mockResolvedValueOnce([]);

    renderPage();

    const cancelButton = await screen.findByRole("button", { name: "Cancel" });
    fireEvent.click(cancelButton);

    await waitFor(() => {
      expect(confirmSpy).toHaveBeenCalledWith("Cancel Build #21?");
      expect(mockedCancelBuild).toHaveBeenCalledWith("build-1");
    });

    confirmSpy.mockRestore();
  });

  it("confirms rerun and navigates to the new build", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    mockedGetBuild.mockImplementation(async (buildID: string) => {
      if (buildID === "build-rerun-1") {
        return makeBuild({
          id: "build-rerun-1",
          build_number: 22,
          status: "queued",
          started_at: null,
          finished_at: null,
          error_message: null,
          rerun_of_build_id: "build-1",
        });
      }
      return makeBuild();
    });
    mockedGetBuildSteps.mockImplementation(async (buildID: string) => {
      if (buildID === "build-rerun-1") {
        return [
          makeStep({
            id: "rerun-step-1",
            build_id: "build-rerun-1",
            status: "pending",
            started_at: null,
            finished_at: null,
            exit_code: null,
            error_message: null,
          }),
        ];
      }
      return [makeStep({ status: "success" })];
    });
    mockedGetBuildArtifacts.mockImplementation(async () => []);

    renderPage();

    const rerunButton = await screen.findByRole("button", { name: "Rerun" });
    fireEvent.click(rerunButton);

    await waitFor(() => {
      expect(confirmSpy).toHaveBeenCalledWith("Rerun Build #21?");
      expect(mockedRerunBuild).toHaveBeenCalledWith("build-1");
    });
    await screen.findByRole("heading", { level: 2, name: "Build #22" });

    confirmSpy.mockRestore();
  });

  it.each([
    ["queued", true],
    ["preparing", true],
    ["running", true],
    ["pending", false],
    ["success", false],
    ["failed", false],
    ["canceled", false],
  ] as const)(
    "shows Cancel for %s only when cancelable",
    async (status, shouldShowCancel) => {
      mockedGetBuild.mockResolvedValueOnce(
        makeBuild({
          status,
          finished_at:
            status === "success" || status === "failed" || status === "canceled"
              ? "2026-03-30T00:02:05Z"
              : null,
          error_message: null,
        }),
      );
      mockedGetBuildSteps.mockResolvedValueOnce([
        makeStep({
          status: status === "running" ? "running" : "pending",
          finished_at: null,
          exit_code: null,
        }),
      ]);
      mockedGetBuildArtifacts.mockResolvedValueOnce([]);

      renderPage();

      await screen.findByRole("heading", { level: 2, name: "Build #21" });
      const button = screen.queryByRole("button", { name: "Cancel" });
      if (shouldShowCancel) {
        expect(button).toBeTruthy();
      } else {
        expect(button).toBeNull();
      }
    },
  );

  it.each([
    ["pending", false],
    ["queued", false],
    ["preparing", false],
    ["running", false],
    ["success", true],
    ["failed", true],
    ["canceled", true],
  ] as const)(
    "shows Rerun for %s only when terminal",
    async (status, shouldShowRerun) => {
      mockedGetBuild.mockResolvedValueOnce(
        makeBuild({
          status,
          finished_at:
            status === "success" || status === "failed" || status === "canceled"
              ? "2026-03-30T00:02:05Z"
              : null,
          error_message: null,
        }),
      );
      mockedGetBuildSteps.mockResolvedValueOnce([
        makeStep({
          status: status === "running" ? "running" : "pending",
          finished_at: null,
          exit_code: null,
        }),
      ]);
      mockedGetBuildArtifacts.mockResolvedValueOnce([]);

      renderPage();

      await screen.findByRole("heading", { level: 2, name: "Build #21" });
      const button = screen.queryByRole("button", { name: "Rerun" });
      if (shouldShowRerun) {
        expect(button).toBeTruthy();
      } else {
        expect(button).toBeNull();
      }
    },
  );

  it("renders clean empty states when optional fields, logs, and artifacts are missing", async () => {
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({
        build_number: undefined,
        job_id: null,
        job_name: null,
        error_message: null,
        pipeline_source: null,
        pipeline_path: null,
        scm_provider: null,
        event_type: null,
        repository_owner: null,
        repository_name: null,
        repository_url: null,
        trigger_ref: null,
        ref_type: null,
        actor: null,
        trigger_commit_sha: null,
        source_commit_sha: null,
        image: undefined,
      }),
    );
    mockedGetBuildSteps.mockResolvedValueOnce([]);
    mockedGetBuildArtifacts.mockResolvedValueOnce([]);

    renderPage();

    await screen.findByRole("heading", { level: 2, name: "Build build-1" });

    expect(screen.queryByRole("link", { name: "release" })).toBeNull();
    expect(screen.getByText("No step logs available yet.")).toBeTruthy();
    expect(
      screen.getByText("No artifacts were collected for this build."),
    ).toBeTruthy();
    expect(
      screen.getByText("No source metadata available for this build."),
    ).toBeTruthy();
    expect(screen.queryByText("undefined")).toBeNull();
    expect(screen.queryByText("null")).toBeNull();
  });

  it("renders non-http repository urls as plain text instead of clickable links", async () => {
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({
        repository_owner: null,
        repository_name: null,
        repository_url: "javascript:alert(1)",
      }),
    );

    renderPage();

    await screen.findByRole("heading", { level: 2, name: "Build #21" });

    expect(screen.getByText("javascript:alert(1)")).toBeTruthy();
    expect(
      screen.queryByRole("link", { name: "javascript:alert(1)" }),
    ).toBeNull();
  });

  it("creates an artifact version tag from the detail page", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Artifacts")).toBeTruthy();
    });

    const input = screen.getByLabelText("artifact-version-artifact-1");
    fireEvent.change(input, {
      target: { value: "1.2.4" },
    });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    await waitFor(() => {
      expect(mockedCreateJobVersionTags).toHaveBeenCalledWith("job-1", {
        version: "1.2.4",
        artifact_ids: ["artifact-1"],
        managed_image_version_ids: undefined,
      });
    });
  });
});
