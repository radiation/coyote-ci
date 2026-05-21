import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { JobDetailPage } from "./JobDetailPage";
import {
  getProject,
  getJob,
  listArtifactCatalog,
  listBuildsByJob,
  listSourceCredentials,
  runJob,
  updateJob,
} from "../api";

const navigateMock = vi.fn();

vi.mock("react-router-dom", async () => {
  const actual =
    await vi.importActual<typeof import("react-router-dom")>(
      "react-router-dom",
    );
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

vi.mock("../api", () => ({
  getProject: vi.fn(),
  getJob: vi.fn(),
  listArtifactCatalog: vi.fn(),
  updateJob: vi.fn(),
  runJob: vi.fn(),
  listBuildsByJob: vi.fn(),
  listSourceCredentials: vi.fn(),
}));

function renderPage(seed?: (queryClient: QueryClient) => void) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  seed?.(queryClient);

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/jobs/job-1"]}>
        <Routes>
          <Route path="/jobs/:id" element={<JobDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("JobDetailPage", () => {
  const mockedGetProject = vi.mocked(getProject);
  const mockedGetJob = vi.mocked(getJob);
  const mockedListArtifactCatalog = vi.mocked(listArtifactCatalog);
  const mockedUpdateJob = vi.mocked(updateJob);
  const mockedRunJob = vi.mocked(runJob);
  const mockedListBuildsByJob = vi.mocked(listBuildsByJob);
  const mockedListSourceCredentials = vi.mocked(listSourceCredentials);

  beforeEach(() => {
    vi.clearAllMocks();

    mockedListBuildsByJob.mockResolvedValue([
      {
        id: "build-queued-1",
        build_number: 21,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-1",
        priority: 5,
        status: "queued",
        created_at: "2026-03-30T00:00:00Z",
        queued_at: "2026-03-30T00:00:01Z",
        started_at: null,
        finished_at: null,
        current_step_index: 0,
        error_message: null,
      },
      {
        id: "build-recent-1",
        build_number: 20,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-1",
        priority: 5,
        status: "success",
        created_at: "2026-03-30T00:00:00Z",
        queued_at: "2026-03-30T00:00:01Z",
        started_at: "2026-03-30T00:00:10Z",
        finished_at: "2026-03-30T00:01:10Z",
        current_step_index: 0,
        error_message: null,
        trigger_ref: "main",
      },
    ]);
    mockedListArtifactCatalog.mockResolvedValue([
      {
        id: "artifact-1",
        name: "backend-binary",
        path: "dist/backend",
        artifact_type: "generic",
        build_id: "build-recent-1",
        build_number: 20,
        build_status: "success",
        project_id: "project-1",
        project_name: "Platform",
        project_slug: "platform",
        job_id: "job-1",
        job_name: "backend-ci",
        step_id: null,
        step_index: null,
        step_name: null,
        size_bytes: 1024,
        content_type: "application/octet-stream",
        checksum_sha256: null,
        storage_provider: "filesystem",
        download_url_path: "/artifacts/download/artifact-1",
        created_at: "2026-03-30T00:01:10Z",
      },
    ]);
    mockedGetProject.mockResolvedValue({
      id: "project-1",
      name: "Platform",
      slug: "platform",
      description: "Core platform pipelines",
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });
    mockedListSourceCredentials.mockResolvedValue([
      {
        id: "cred-1",
        name: "github-bot",
        kind: "https_token",
        username: "x-access-token",
        secret_ref: "COYOTE_TOKEN",
        created_at: "2026-03-30T00:00:00Z",
        updated_at: "2026-03-30T00:00:00Z",
      },
    ]);

    mockedGetJob.mockResolvedValue({
      id: "job-1",
      project_id: "project-1",
      name: "backend-ci",
      priority: 5,
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: true,
      push_branch: "main",
      pipeline_yaml:
        "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
      managed_image: {
        enabled: true,
        managed_image_name: "go",
        pipeline_path: ".coyote/pipeline.yml",
        write_credential_id: "cred-1",
        bot_branch_prefix: "coyote/managed-image-refresh",
        commit_author_name: "Coyote CI Bot",
        commit_author_email: "bot@coyote-ci.local",
        created_at: "2026-03-30T00:00:00Z",
        updated_at: "2026-03-30T00:00:00Z",
      },
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });

    mockedUpdateJob.mockResolvedValue({
      id: "job-1",
      project_id: "project-1",
      name: "backend-ci-updated",
      priority: 5,
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: true,
      push_branch: "main",
      pipeline_yaml:
        "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
      managed_image: {
        enabled: true,
        managed_image_name: "go-1-24",
        pipeline_path: ".coyote/pipeline.yml",
        write_credential_id: "cred-1",
        bot_branch_prefix: "coyote/managed-image-refresh",
        commit_author_name: "Coyote CI Bot",
        commit_author_email: "bot@coyote-ci.local",
        created_at: "2026-03-30T00:00:00Z",
        updated_at: "2026-03-30T00:00:01Z",
      },
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:01Z",
    });

    mockedRunJob.mockResolvedValue({
      id: "build-123",
      priority: 5,
      project_id: "project-1",
      status: "queued",
      created_at: "2026-03-30T00:00:00Z",
      queued_at: "2026-03-30T00:00:01Z",
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });
  });

  it("loads job and saves edits", async () => {
    renderPage();

    await screen.findByDisplayValue("backend-ci");
    await waitFor(() => {
      expect(mockedListBuildsByJob).toHaveBeenCalledWith("job-1");
    });

    const platformLinks = screen.getAllByRole("link", { name: "Platform" });
    const buildLinks = screen.getAllByRole("link", { name: /Build\s*#21/i });

    expect(platformLinks).toHaveLength(1);
    expect(platformLinks[0]).toHaveAttribute("href", "/projects/project-1");
    expect(buildLinks.length).toBeGreaterThan(0);
    expect(
      screen.getByRole("link", { name: "View Project Builds" }),
    ).toHaveAttribute("href", "/builds?project_id=project-1");
    expect(
      screen.getByRole("link", { name: "Browse Job Artifacts" }),
    ).toHaveAttribute("href", "/artifacts?project_id=project-1&job_id=job-1");
    expect(
      screen.getByRole("link", { name: "backend-binary" }),
    ).toHaveAttribute("href", "/artifacts/artifact-1");
    expect(screen.queryByRole("link", { name: /^Job\s/i })).toBeNull();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "backend-ci-updated" },
    });
    fireEvent.change(screen.getByLabelText("Managed Image Name"), {
      target: { value: "go-1-24" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(mockedUpdateJob).toHaveBeenCalledWith("job-1", {
        name: "backend-ci-updated",
        priority: 5,
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: true,
        push_branch: "main",
        pipeline_yaml:
          "version: 1\nsteps:\n  - name: test\n    run: go test ./...",
        pipeline_path: "",
        managed_image: {
          enabled: true,
          managed_image_name: "go-1-24",
          pipeline_path: ".coyote/pipeline.yml",
          write_credential_id: "cred-1",
          bot_branch_prefix: "coyote/managed-image-refresh",
          commit_author_name: "Coyote CI Bot",
          commit_author_email: "bot@coyote-ci.local",
        },
        enabled: true,
      });
      expect(screen.getByText("Job saved.")).toBeTruthy();
    });
  });

  it("shows the job loading state", () => {
    mockedGetJob.mockImplementationOnce(
      () => new Promise(() => {}) as Promise<never>,
    );

    renderPage();

    expect(screen.getByText("Loading job…")).toBeTruthy();
  });

  it("shows the job error state", async () => {
    mockedGetJob.mockRejectedValueOnce(new Error("backend unavailable"));

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText("Failed to load job: Error: backend unavailable"),
      ).toBeTruthy();
    });
  });

  it("shows the job not found state", async () => {
    mockedGetJob.mockResolvedValueOnce(null as never);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Job not found.")).toBeTruthy();
    });
  });

  it("shows inline pipeline and disabled job detail fallbacks", async () => {
    mockedListArtifactCatalog.mockResolvedValueOnce([]);
    mockedGetJob.mockResolvedValueOnce({
      id: "job-1",
      project_id: "project-1",
      name: "backend-ci",
      priority: 5,
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: false,
      push_branch: null,
      pipeline_yaml: "version: 1\n",
      pipeline_path: null,
      managed_image: null,
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Platform" })).toHaveAttribute(
        "href",
        "/projects/project-1",
      );
      expect(
        screen.getAllByText("Push Branch")[0].parentElement,
      ).toHaveTextContent("—");
      expect(
        screen.getAllByText("Pipeline Source")[0].parentElement,
      ).toHaveTextContent("Inline YAML");
      expect(
        screen.getAllByText("Managed Build Image")[0].parentElement,
      ).toHaveTextContent("Disabled");
      expect(screen.getByText("No artifacts yet for this job.")).toBeTruthy();
    });
  });

  it("shows repository pipeline and any-branch fallback details", async () => {
    mockedGetJob.mockResolvedValueOnce({
      id: "job-1",
      project_id: "project-1",
      name: "backend-ci",
      priority: 5,
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: true,
      push_branch: "",
      pipeline_yaml: "",
      pipeline_path: ".coyote/repo-pipeline.yml",
      managed_image: {
        enabled: true,
        managed_image_name: "go",
        pipeline_path: ".coyote/repo-pipeline.yml",
        write_credential_id: "cred-1",
        bot_branch_prefix: "coyote/managed-image-refresh",
        commit_author_name: "Coyote CI Bot",
        commit_author_email: "bot@coyote-ci.local",
        created_at: "2026-03-30T00:00:00Z",
        updated_at: "2026-03-30T00:00:00Z",
      },
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText("Push Branch", { selector: "strong" }).parentElement,
      ).toHaveTextContent("Any branch");
      expect(
        screen.getByText("Pipeline Source", { selector: "strong" })
          .parentElement,
      ).toHaveTextContent("Repository file");
      expect(
        screen.getByText("Pipeline Path", { selector: "strong" }).parentElement,
      ).toHaveTextContent(".coyote/repo-pipeline.yml");
    });
  });

  it("shows last-loaded fallback when query timestamp is unavailable", async () => {
    mockedGetJob.mockImplementationOnce(
      () => new Promise(() => {}) as Promise<never>,
    );

    renderPage((queryClient) => {
      queryClient.setQueryData(
        ["job", "job-1"],
        {
          id: "job-1",
          project_id: "project-1",
          name: "backend-ci",
          priority: 5,
          repository_url: "https://github.com/example/backend.git",
          default_ref: "main",
          push_enabled: true,
          push_branch: "main",
          pipeline_yaml: "steps:\n  - run: echo hi",
          pipeline_path: null,
          managed_image: null,
          enabled: true,
          created_at: "2026-03-30T00:00:00Z",
          updated_at: "2026-03-30T00:00:00Z",
        },
        { updatedAt: 0 },
      );
    });

    await waitFor(() => {
      expect(
        screen.getByText("Last Loaded", { selector: "strong" }).parentElement,
      ).toHaveTextContent("—");
    });
  });

  it("starts in repo mode and saves empty push branch when push trigger is disabled", async () => {
    mockedGetJob.mockResolvedValueOnce({
      id: "job-1",
      project_id: "project-1",
      name: "backend-ci",
      priority: 5,
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: true,
      push_branch: "main",
      pipeline_yaml: "",
      pipeline_path: ".coyote/repo-pipeline.yml",
      managed_image: null,
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });

    renderPage();

    await screen.findByDisplayValue("backend-ci");
    expect(
      (
        screen.getByRole("radio", {
          name: "File in repository",
        }) as HTMLInputElement
      ).checked,
    ).toBe(true);

    fireEvent.click(screen.getByLabelText("Enable push trigger"));
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(mockedUpdateJob).toHaveBeenCalledWith("job-1", {
        name: "backend-ci",
        priority: 5,
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: false,
        push_branch: "",
        pipeline_yaml: "",
        pipeline_path: ".coyote/repo-pipeline.yml",
        managed_image: null,
        enabled: true,
      });
    });
  });

  it("shows managed image required field validation when enabled and incomplete", async () => {
    mockedGetJob.mockResolvedValueOnce({
      id: "job-1",
      project_id: "project-1",
      name: "backend-ci",
      priority: 5,
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: true,
      push_branch: "main",
      pipeline_yaml: "",
      pipeline_path: ".coyote/repo-pipeline.yml",
      managed_image: {
        enabled: true,
        managed_image_name: "",
        pipeline_path: "",
        write_credential_id: "",
        bot_branch_prefix: "coyote/managed-image-refresh",
        commit_author_name: "Coyote CI Bot",
        commit_author_email: "bot@coyote-ci.local",
        created_at: "2026-03-30T00:00:00Z",
        updated_at: "2026-03-30T00:00:00Z",
      },
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });

    renderPage();

    await screen.findByDisplayValue("backend-ci");
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(
        screen.getByText(
          "Managed build image name, pipeline path, and write credential are required when automation is enabled.",
        ),
      ).toBeTruthy();
    });
  });

  it("covers job form validation, repo saves, and run-now fallback", async () => {
    const { container } = renderPage();

    await screen.findByDisplayValue("backend-ci");

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(
        screen.getByText("Name, repository URL, and default ref are required."),
      ).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "backend-ci" },
    });
    fireEvent.change(screen.getByLabelText("Priority"), {
      target: { value: "0" },
    });
    fireEvent.submit(container.querySelector(".job-form") as HTMLFormElement);

    await waitFor(() => {
      expect(
        screen.getByText("Priority must be a number from 1 to 10."),
      ).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Priority"), {
      target: { value: "5" },
    });
    fireEvent.change(screen.getByLabelText("Pipeline YAML"), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(screen.getByText("Pipeline YAML is required.")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("radio", { name: "File in repository" }));
    await waitFor(() => {
      expect(
        (
          screen.getByRole("radio", {
            name: "File in repository",
          }) as HTMLInputElement
        ).checked,
      ).toBe(true);
    });
    fireEvent.change(screen.getByLabelText("Pipeline File Path"), {
      target: { value: "" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(screen.getByText("Pipeline file path is required.")).toBeTruthy();
    });

    fireEvent.change(screen.getByLabelText("Pipeline File Path"), {
      target: { value: ".coyote/pipeline.yml" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(mockedUpdateJob).toHaveBeenCalledWith("job-1", {
        name: "backend-ci",
        priority: 5,
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: true,
        push_branch: "main",
        pipeline_yaml: "",
        pipeline_path: ".coyote/pipeline.yml",
        managed_image: {
          enabled: true,
          managed_image_name: "go",
          pipeline_path: ".coyote/pipeline.yml",
          write_credential_id: "cred-1",
          bot_branch_prefix: "coyote/managed-image-refresh",
          commit_author_name: "Coyote CI Bot",
          commit_author_email: "bot@coyote-ci.local",
        },
        enabled: true,
      });
    });

    mockedRunJob.mockResolvedValueOnce({
      id: "",
      priority: 5,
      project_id: "project-1",
      status: "queued",
      created_at: "2026-03-30T00:00:00Z",
      queued_at: "2026-03-30T00:00:01Z",
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
    });

    fireEvent.click(screen.getByRole("button", { name: "Run Now" }));

    await waitFor(() => {
      expect(screen.getByText("Job run started.")).toBeTruthy();
    });

    mockedUpdateJob.mockRejectedValueOnce(new Error("save failed"));
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to save job/)).toBeTruthy();
      expect(screen.getByText(/save failed/)).toBeTruthy();
    });
  });
  it("renders the responsive two-column activity rail layout", async () => {
    const { container } = renderPage();

    await screen.findByDisplayValue("backend-ci");

    expect(container.querySelector(".detail-page-with-rail")).toBeTruthy();
  });

  it("job-scoped activity rows show trigger ref and no redundant job link", async () => {
    renderPage();

    await screen.findByDisplayValue("backend-ci");

    await waitFor(() => {
      expect(
        screen.getAllByRole("link", { name: /Build\s*#20/i }).length,
      ).toBeGreaterThan(0);
      // trigger_ref "main" appears as context in job-scoped recent build row
      expect(screen.getAllByText("main").length).toBeGreaterThan(0);
      expect(screen.queryByRole("link", { name: /^Job\s/i })).toBeNull();
    });
  });

  it("sends managed_image null when automation is disabled", async () => {
    renderPage();

    await screen.findByDisplayValue("backend-ci");

    fireEvent.click(
      screen.getByLabelText("Enable managed build image automation"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(mockedUpdateJob).toHaveBeenCalledWith("job-1", {
        name: "backend-ci",
        priority: 5,
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: true,
        push_branch: "main",
        pipeline_yaml:
          "version: 1\nsteps:\n  - name: test\n    run: go test ./...",
        pipeline_path: "",
        managed_image: null,
        enabled: true,
      });
    });
  });

  it("runs now and navigates to build detail", async () => {
    renderPage();

    await screen.findByDisplayValue("backend-ci");

    fireEvent.click(screen.getByRole("button", { name: "Run Now" }));

    await waitFor(() => {
      expect(mockedRunJob).toHaveBeenCalledWith("job-1");
      expect(navigateMock).toHaveBeenCalledWith("/builds/build-123");
    });
  });

  it("surfaces run-now error message", async () => {
    mockedRunJob.mockRejectedValueOnce(new Error("API 409: job is disabled"));

    renderPage();

    await screen.findByDisplayValue("backend-ci");

    fireEvent.click(screen.getByRole("button", { name: "Run Now" }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to run job/)).toBeTruthy();
      expect(screen.getByText(/job is disabled/)).toBeTruthy();
    });
  });

  it("shows error states for builds and artifacts queries", async () => {
    mockedListBuildsByJob.mockRejectedValue(new Error("builds failed"));
    mockedListArtifactCatalog.mockRejectedValueOnce(
      new Error("artifacts failed"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText("Failed to load latest builds: Error: builds failed"),
      ).toBeTruthy();
      expect(
        screen.getByText(
          "Failed to load latest artifacts: Error: artifacts failed",
        ),
      ).toBeTruthy();
    });
  });

  it("shows latest success link when latest build is not yet successful", async () => {
    mockedListBuildsByJob.mockResolvedValueOnce([
      {
        id: "build-running-1",
        build_number: 22,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-1",
        priority: 5,
        status: "running",
        created_at: "2026-03-30T00:00:00Z",
        queued_at: "2026-03-30T00:00:01Z",
        started_at: "2026-03-30T00:01:00Z",
        finished_at: "2026-03-30T00:02:00Z",
        current_step_index: 0,
        error_message: null,
      },
      {
        id: "build-success-1",
        build_number: 21,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-1",
        priority: 5,
        status: "success",
        created_at: "2026-03-30T00:00:00Z",
        queued_at: "2026-03-30T00:00:01Z",
        started_at: "2026-03-30T00:00:10Z",
        finished_at: "2026-03-30T00:01:10Z",
        current_step_index: 0,
        error_message: null,
      },
    ]);

    renderPage();

    await waitFor(() => {
      // latestBuild = running #22, latestSuccessfulBuild = success #21 (different id)
      expect(
        screen.getByText("Latest Success:", { selector: "strong" }),
      ).toBeTruthy();
    });
  });

  it("shows build id slice as fallback when build has no build number", async () => {
    mockedListBuildsByJob.mockResolvedValueOnce([
      {
        id: "build-no-num-1",
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-1",
        priority: 5,
        status: "queued",
        created_at: "2026-03-30T00:00:00Z",
        queued_at: "2026-03-30T00:00:01Z",
        started_at: null,
        finished_at: null,
        current_step_index: 0,
        error_message: null,
      },
    ]);

    renderPage();

    await waitFor(() => {
      // build_number is undefined → shows first 8 chars of id
      expect(screen.getByRole("link", { name: "build-no" })).toHaveAttribute(
        "href",
        "/builds/build-no-num-1",
      );
    });
  });
});
