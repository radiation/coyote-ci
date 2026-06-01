import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Link, MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { BuildDetailPage } from "./BuildDetailPage";
import {
  buildStepLogStreamURL,
  cancelBuild,
  createJobVersionTags,
  getBuild,
  getBuildArtifacts,
  getJob,
  getStepLogs,
  getBuildSteps,
  rerunBuild,
} from "../api";
import type { Build, BuildArtifact, BuildStep } from "../types";
import { formatCompactTime } from "../utils/time";

vi.mock("../api", () => ({
  buildStepLogStreamURL: vi.fn(),
  cancelBuild: vi.fn(),
  createJobVersionTags: vi.fn(),
  getBuild: vi.fn(),
  getBuildSteps: vi.fn(),
  getBuildArtifacts: vi.fn(),
  getJob: vi.fn(),
  getStepLogs: vi.fn(),
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
    attempt_number: overrides.attempt_number ?? 1,
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
    artifact_type: "generic",
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

function deferredPromise<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function BuildDetailTestLayout() {
  return (
    <>
      <Link to="/builds/build-2">Go to build 2</Link>
      <Outlet />
    </>
  );
}

describe("BuildDetailPage", () => {
  const mockedGetBuild = vi.mocked(getBuild);
  const mockedBuildStepLogStreamURL = vi.mocked(buildStepLogStreamURL);
  const mockedCancelBuild = vi.mocked(cancelBuild);
  const mockedGetJob = vi.mocked(getJob);
  const mockedRerunBuild = vi.mocked(rerunBuild);
  const mockedGetStepLogs = vi.mocked(getStepLogs);
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
    mockedBuildStepLogStreamURL.mockReturnValue(
      "/api/builds/build-1/steps/1/logs/stream?after=1",
    );
    mockedGetJob.mockResolvedValue({
      id: "job-1",
      project_id: "project-1",
      name: "release",
      slug: "release",
      description: null,
      priority: 9,
      repository_url: "https://github.com/example/platform",
      default_ref: "refs/heads/main",
      push_enabled: true,
      push_branch: null,
      pipeline_yaml: "steps: []",
      pipeline_path: "scenarios/success-basic/coyote.yml",
      managed_image: null,
      latest_build: null,
      enabled: true,
      created_at: "2026-03-29T23:59:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });
    mockedGetBuild.mockResolvedValue(makeBuild());
    mockedGetStepLogs.mockResolvedValue({
      build_id: "build-1",
      step_index: 1,
      after: 0,
      chunks: [
        {
          sequence_no: 1,
          build_id: "build-1",
          step_id: "step-2",
          step_index: 1,
          step_name: "deploy",
          stream: "stderr",
          chunk_text: "streamed deploy output",
          created_at: "2026-03-30T00:02:04Z",
        },
      ],
      next_sequence: 1,
    });
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
        stderr: "connect timeout\nssh: handshake failed",
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
    class MockEventSource {
      addEventListener = vi.fn();
      close = vi.fn();
      onerror: ((this: EventSource, ev: Event) => unknown) | null = null;

      constructor() {}
    }

    Object.defineProperty(window, "EventSource", {
      configurable: true,
      writable: true,
      value: MockEventSource,
    });
  });

  it("renders the summary header, links, timestamps, and duration", async () => {
    renderPage();

    await screen.findByRole("heading", { level: 2, name: "release #21" });
    const summaryPanel = screen
      .getByText(/Build #21 · Build ID build-1 · Attempt 1/)
      .closest("section") as HTMLElement;

    expect(
      screen
        .getAllByRole("link", { name: "Platform" })
        .some((link) => link.getAttribute("href") === "/projects/project-1"),
    ).toBe(true);
    expect(
      screen
        .getAllByRole("link", { name: "release" })
        .some((link) => link.getAttribute("href") === "/jobs/job-1"),
    ).toBe(true);
    expect(
      screen.getAllByText("github • refs/heads/main • abc1234 • octocat"),
    ).toHaveLength(1);
    expect(screen.getByText("Operational overview")).toBeTruthy();
    expect(screen.getByText("Duration")).toBeTruthy();
    expect(screen.getByText("1m 5s")).toBeTruthy();
    expect(
      within(summaryPanel).queryByText("Build failed during deploy."),
    ).toBeNull();
    expect(within(summaryPanel).getByText("Priority")).toBeTruthy();
    expect(within(summaryPanel).getByText("9")).toBeTruthy();
    expect(
      within(summaryPanel).getByRole("link", {
        name: "View full provenance details",
      }),
    ).toHaveAttribute("href", "#build-provenance");
    expect(
      within(summaryPanel).getByText(formatCompactTime("2026-03-30T00:01:00Z")),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Back to builds" }),
    ).toHaveAttribute("href", "/builds");
    const headerActions = document.querySelector(
      ".build-detail-header-actions",
    ) as HTMLElement;
    expect(
      Array.from(headerActions.children).map((element) =>
        element.textContent?.trim(),
      ),
    ).toEqual(["Back to builds", "View project", "View job", "Rerun"]);
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
  });

  it("uses the fetched job name when the build payload omits job_name", async () => {
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({
        job_name: null,
        job_id: "job-1",
      }),
    );

    renderPage();

    await screen.findByRole("heading", { level: 2, name: "release #21" });

    expect(mockedGetJob).toHaveBeenCalledWith("job-1");
    expect(
      screen
        .getAllByRole("link", { name: "release" })
        .some((link) => link.getAttribute("href") === "/jobs/job-1"),
    ).toBe(true);
    expect(screen.queryByText("Job job-1")).toBeNull();
  });

  it("renders failed build state with a visible failed step and failure details", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Execution timeline" });
    const executionSummary = screen
      .getByRole("heading", { name: "Execution summary" })
      .closest("section") as HTMLElement;

    expect(
      screen.getByRole("heading", { name: "Execution summary" }),
    ).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Logs" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Artifacts" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Provenance" })).toBeTruthy();
    expect(document.querySelector(".build-steps-summary")?.textContent).toBe(
      "Steps: 1 succeeded · 1 failed · 1 pending",
    );
    expect(screen.getByText("Failed step")).toBeTruthy();
    expect(within(executionSummary).getByText("Step 2 of 3")).toBeTruthy();
    expect(screen.getAllByText("Exit code 1").length).toBe(2);
    expect(screen.getAllByText("remote deploy failed").length).toBe(2);
    expect(
      screen.getByText("Build stopped after this step failed."),
    ).toBeTruthy();
    expect(screen.getByText("Last error output")).toBeTruthy();
    expect(screen.getByText("ssh: handshake failed")).toBeTruthy();
    expect(screen.getByText("compile")).toBeTruthy();
    expect(screen.getAllByText("deploy").length).toBe(2);
    expect(screen.getByText("notify")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "Open logs" })).toHaveLength(
      3,
    );
    expect(
      screen.getByRole("link", { name: /Step 2 · deploy/ }),
    ).toHaveAttribute("href", "#step-1");
    expect(
      screen.getByRole("link", { name: /Failed · Open inline logs/ }),
    ).toBeTruthy();
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

  it("renders artifact lineage links from build provenance and repository routes", async () => {
    mockedGetBuildArtifacts.mockResolvedValueOnce([
      makeArtifact({
        id: "artifact-lineage",
        artifact_type: "npm_package",
        path: "packages/demo-1.2.3.tgz",
        version_tags: [
          {
            id: "tag-version-1",
            job_id: "job-1",
            version: "1.2.3",
            target_type: "artifact",
            artifact_id: "artifact-lineage",
            created_at: "2026-03-30T00:00:04Z",
          },
          {
            id: "tag-channel-1",
            job_id: "job-1",
            kind: "channel",
            version: "stable",
            target_type: "artifact",
            artifact_id: "artifact-lineage",
            created_at: "2026-03-30T00:00:04Z",
          },
        ],
      }),
    ]);

    renderPage();

    await screen.findByRole("link", { name: "Open artifact" });

    expect(screen.getByText("npm package")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open artifact" })).toHaveAttribute(
      "href",
      "/artifacts/artifact-lineage",
    );
    expect(
      screen
        .getAllByRole("link", { name: "Repository view" })
        .some(
          (link) =>
            link.getAttribute("href") ===
            "/artifacts/logical?q=packages%2Fdemo-1.2.3.tgz&job_id=job-1",
        ),
    ).toBe(true);
    expect(
      screen
        .getAllByRole("link", { name: "refs/heads/main" })
        .every(
          (link) =>
            link.getAttribute("href") ===
            "https://github.com/example/platform/tree/main",
        ),
    ).toBe(true);
    expect(
      screen
        .getAllByRole("link", { name: "def9876" })
        .every(
          (link) =>
            link.getAttribute("href") ===
            "https://github.com/example/platform/commit/def9876543210",
        ),
    ).toBe(true);
  });

  it("renders GitHub provenance links when repository metadata is available", async () => {
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({
        repository_url: "https://github.com/example/platform",
        trigger_ref: "main",
        source_commit_sha: "95f09eb123456789",
        trigger_commit_sha: null,
        pipeline_path: "scenarios/multi-step-failure/coyote.yml",
      }),
    );

    renderPage();

    const provenanceSection = (
      await screen.findByRole("heading", {
        name: "Provenance",
      })
    ).closest("section") as HTMLElement;

    expect(
      within(provenanceSection).getByRole("link", { name: "95f09eb" }),
    ).toHaveAttribute(
      "href",
      "https://github.com/example/platform/commit/95f09eb123456789",
    );
    expect(
      within(provenanceSection).getByRole("link", { name: "main" }),
    ).toHaveAttribute("href", "https://github.com/example/platform/tree/main");
    expect(
      within(provenanceSection).getByRole("link", {
        name: "scenarios/multi-step-failure/coyote.yml",
      }),
    ).toHaveAttribute(
      "href",
      "https://github.com/example/platform/blob/95f09eb123456789/scenarios/multi-step-failure/coyote.yml",
    );
  });

  it("renders plain text provenance values when repository metadata is missing", async () => {
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({
        repository_url: null,
        repository_owner: "example",
        repository_name: "platform",
        trigger_ref: "main",
        source_commit_sha: "95f09eb123456789",
        trigger_commit_sha: null,
        pipeline_path: "scenarios/multi-step-failure/coyote.yml",
      }),
    );

    renderPage();

    const provenanceSection = (
      await screen.findByRole("heading", {
        name: "Provenance",
      })
    ).closest("section") as HTMLElement;

    expect(within(provenanceSection).getByText("main")).toBeTruthy();
    expect(within(provenanceSection).getByText("95f09eb")).toBeTruthy();
    expect(
      within(provenanceSection).getByText(
        "scenarios/multi-step-failure/coyote.yml",
      ),
    ).toBeTruthy();
    expect(
      within(provenanceSection).queryByRole("link", { name: "main" }),
    ).toBeNull();
    expect(
      within(provenanceSection).queryByRole("link", { name: "95f09eb" }),
    ).toBeNull();
    expect(
      within(provenanceSection).queryByRole("link", {
        name: "scenarios/multi-step-failure/coyote.yml",
      }),
    ).toBeNull();
  });

  it("renders plain text provenance values for unsupported repository providers", async () => {
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({
        repository_url: "https://gitlab.com/example/platform",
        trigger_ref: "main",
        source_commit_sha: "95f09eb123456789",
        trigger_commit_sha: null,
        pipeline_path: "scenarios/multi-step-failure/coyote.yml",
      }),
    );

    renderPage();

    const provenanceSection = (
      await screen.findByRole("heading", {
        name: "Provenance",
      })
    ).closest("section") as HTMLElement;

    expect(within(provenanceSection).getByText("main")).toBeTruthy();
    expect(within(provenanceSection).getByText("95f09eb")).toBeTruthy();
    expect(
      within(provenanceSection).getByText(
        "scenarios/multi-step-failure/coyote.yml",
      ),
    ).toBeTruthy();
    expect(
      within(provenanceSection).queryByRole("link", { name: "main" }),
    ).toBeNull();
    expect(
      within(provenanceSection).queryByRole("link", { name: "95f09eb" }),
    ).toBeNull();
    expect(
      within(provenanceSection).queryByRole("link", {
        name: "scenarios/multi-step-failure/coyote.yml",
      }),
    ).toBeNull();
  });

  it("opens the matching step logs when a log card is clicked", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Logs" });

    fireEvent.click(screen.getByRole("link", { name: /Step 2 · deploy/ }));

    await waitFor(() => {
      expect(mockedGetStepLogs).toHaveBeenCalledWith("build-1", 1, 0, 500);
    });
    expect(screen.getByRole("button", { name: "Hide logs" })).toBeTruthy();
    expect(document.querySelector("#step-1 .step-log-panel")).toBeTruthy();
  });

  it("resets the open step when navigating to a different build", async () => {
    mockedGetBuild.mockImplementation(async (buildID: string) => {
      return makeBuild({
        id: buildID,
        build_number: buildID === "build-2" ? 22 : 21,
        status: "running",
        finished_at: null,
        error_message: null,
      });
    });
    mockedGetBuildSteps.mockImplementation(async (buildID: string) => {
      if (buildID === "build-2") {
        return [
          makeStep({
            id: "build-2-step-1",
            build_id: "build-2",
            step_index: 0,
            name: "prepare",
            status: "success",
          }),
          makeStep({
            id: "build-2-step-2",
            build_id: "build-2",
            step_index: 1,
            name: "package",
            status: "running",
            started_at: "2026-03-30T00:02:00Z",
            finished_at: null,
            exit_code: null,
            error_message: null,
          }),
        ];
      }

      return [
        makeStep({
          id: "build-1-step-1",
          build_id: "build-1",
          step_index: 0,
          name: "compile",
          status: "success",
        }),
        makeStep({
          id: "build-1-step-2",
          build_id: "build-1",
          step_index: 1,
          name: "deploy",
          status: "running",
          started_at: "2026-03-30T00:01:20Z",
          finished_at: null,
          exit_code: null,
          error_message: null,
        }),
      ];
    });
    mockedGetBuildArtifacts.mockResolvedValue([]);
    mockedBuildStepLogStreamURL.mockImplementation(
      (buildID: string, stepIndex: number, after?: number) =>
        `/api/builds/${buildID}/steps/${stepIndex}/logs/stream?after=${after ?? 0}`,
    );
    mockedGetStepLogs.mockImplementation(
      async (buildID: string, stepIndex) => ({
        build_id: buildID,
        step_index: stepIndex,
        after: 0,
        chunks: [
          {
            sequence_no: 1,
            build_id: buildID,
            step_id: `${buildID}-step-${stepIndex + 1}`,
            step_index: stepIndex,
            step_name: buildID === "build-2" ? "package" : "deploy",
            stream: "stderr",
            chunk_text: `logs for ${buildID}`,
            created_at: "2026-03-30T00:02:04Z",
          },
        ],
        next_sequence: 1,
      }),
    );

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={["/builds/build-1"]}>
          <Routes>
            <Route element={<BuildDetailTestLayout />}>
              <Route path="/builds/:id" element={<BuildDetailPage />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await screen.findByRole("heading", { name: "Logs" });

    fireEvent.click(screen.getByRole("link", { name: /Step 2 · deploy/ }));

    await waitFor(() => {
      expect(mockedGetStepLogs).toHaveBeenCalledWith("build-1", 1, 0, 500);
    });
    expect(screen.getByRole("button", { name: "Hide logs" })).toBeTruthy();

    fireEvent.click(screen.getByRole("link", { name: "Go to build 2" }));

    await waitFor(() => {
      expect(document.querySelector(".page-header-copy h2")?.textContent).toBe(
        "release #22",
      );
    });

    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Hide logs" })).toBeNull();
    });
    expect(document.querySelector(".step-log-panel")).toBeNull();
    expect(mockedGetStepLogs).toHaveBeenCalledTimes(1);
  });

  it("shows rerun lineage with a link to the source build", async () => {
    mockedGetBuild.mockImplementation(async (buildID: string) => {
      if (buildID === "build-0") {
        return makeBuild({
          id: "build-0",
          build_number: 20,
          rerun_of_build_id: null,
        });
      }
      return makeBuild({
        rerun_of_build_id: "build-0",
        rerun_from_step_index: 2,
      });
    });

    renderPage();

    await waitFor(() => {
      expect(mockedGetBuild).toHaveBeenCalledWith("build-0");
    });
    expect(screen.getByText(/Rerun of/)).toBeTruthy();
    expect(document.body.textContent).toContain("Restarted from step 3.");
    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Build #20" })).toHaveAttribute(
        "href",
        "/builds/build-0",
      );
    });
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
    const runningCallout = screen
      .getByText("Currently running")
      .closest("article") as HTMLElement;
    const summaryPanel = screen
      .getByText(/Build #21 · Build ID build-1 · Attempt 1/)
      .closest("section") as HTMLElement;

    expect(
      screen.getByRole("link", { name: /Step 2 · deploy/ }),
    ).toHaveAttribute("href", "#step-1");
    expect(document.querySelector(".build-steps-summary")?.textContent).toBe(
      "Steps: 1 succeeded · 1 running · 1 pending",
    );
    expect(screen.getByText("1 pending step")).toBeTruthy();
    expect(runningCallout.textContent).toContain("Step 2 · deploy");
    expect(screen.getByText("Step 2 · Current step")).toBeTruthy();
    expect(summaryPanel.textContent).toContain("Current stepStep 2 of 3");
    expect(summaryPanel.textContent).not.toContain("Duration—");
    expect(screen.getByText("Pending")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /Running · Open inline logs/ }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "No artifacts were collected for this build. Check packaging or upload steps in the execution timeline, then rerun if you expected published outputs.",
      ),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeTruthy();
  });

  it("clamps the current step summary to the recorded step count on completed builds", async () => {
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({
        status: "success",
        current_step_index: 5,
        error_message: null,
      }),
    );
    mockedGetBuildSteps.mockResolvedValueOnce([
      makeStep({ step_index: 0, status: "success", name: "compile" }),
      makeStep({
        step_index: 1,
        id: "step-2",
        status: "success",
        name: "test",
      }),
      makeStep({
        step_index: 2,
        id: "step-3",
        status: "success",
        name: "package",
      }),
      makeStep({
        step_index: 3,
        id: "step-4",
        status: "success",
        name: "publish",
      }),
      makeStep({
        step_index: 4,
        id: "step-5",
        status: "success",
        name: "notify",
      }),
    ]);
    mockedGetBuildArtifacts.mockResolvedValueOnce([]);

    renderPage();

    await screen.findByText("Completed successfully");
    const summaryPanel = screen
      .getByText(/Build #21 · Build ID build-1 · Attempt 1/)
      .closest("section") as HTMLElement;

    expect(summaryPanel.textContent).toContain("Current stepStep 5 of 5");
    expect(summaryPanel.textContent).not.toContain("Step 6 of 5");
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

  it("disables the cancel control while cancellation is pending", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    const cancelResult = deferredPromise<Build>();
    mockedCancelBuild.mockImplementationOnce(() => cancelResult.promise);
    mockedGetBuild.mockResolvedValueOnce(
      makeBuild({ status: "running", finished_at: null, error_message: null }),
    );
    mockedGetBuildSteps.mockResolvedValueOnce([
      makeStep({ status: "running", finished_at: null, exit_code: null }),
    ]);
    mockedGetBuildArtifacts.mockResolvedValueOnce([]);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));

    const pendingButton = await screen.findByRole("button", {
      name: "Canceling…",
    });
    expect(pendingButton).toBeDisabled();

    cancelResult.resolve(
      makeBuild({ status: "canceled", finished_at: "2026-03-30T00:01:30Z" }),
    );

    await waitFor(() => {
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
    await screen.findByRole("heading", { level: 2, name: "release #22" });

    confirmSpy.mockRestore();
  });

  it("disables the rerun control while the new build is being created", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    const rerunResult = deferredPromise<Build>();
    mockedRerunBuild.mockImplementationOnce(() => rerunResult.promise);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Rerun" }));

    const pendingButton = await screen.findByRole("button", {
      name: "Rerunning…",
    });
    expect(pendingButton).toBeDisabled();

    rerunResult.resolve(
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

    await waitFor(() => {
      expect(mockedRerunBuild).toHaveBeenCalledWith("build-1");
    });

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

      await screen.findByRole("heading", { level: 2, name: "release #21" });
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

      await screen.findByRole("heading", { level: 2, name: "release #21" });
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
    expect(
      screen.getByText(
        "No step logs are available yet. When execution starts, open a step in the timeline to inspect stdout and stderr inline.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "No artifacts were collected for this build. Check packaging or upload steps in the execution timeline, then rerun if you expected published outputs.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "No source metadata is available for this build. Manual or fixture-driven runs may omit repository and trigger context.",
      ),
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

    await screen.findByRole("heading", { level: 2, name: "release #21" });

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

    fireEvent.click(screen.getByRole("button", { name: "Assign version" }));

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
