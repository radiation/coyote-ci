import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  Link,
  MemoryRouter,
  Route,
  Routes,
  useNavigate,
  useLocation,
} from "react-router-dom";
import { JobDetailPage } from "./JobDetailPage";
import {
  getBuildArtifacts,
  getProject,
  getJob,
  listBuildsByJob,
  listSourceCredentials,
  runJob,
  updateJob,
} from "../api";

function HistoryControls() {
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <>
      <output aria-label="Location">
        {location.pathname + location.search + location.hash}
      </output>
      <button onClick={() => navigate(-1)}>Back</button>
      <button onClick={() => navigate(1)}>Forward</button>
      <Link to="/jobs/job-2?view=configuration">Other job</Link>
    </>
  );
}

vi.mock("../api", () => ({
  artifactDownloadURL: (path: string) => `/api${path}`,
  getBuildArtifacts: vi.fn(),
  getProject: vi.fn(),
  getJob: vi.fn(),
  updateJob: vi.fn(),
  runJob: vi.fn(),
  listBuildsByJob: vi.fn(),
  listSourceCredentials: vi.fn(),
}));

function renderPage(
  seed?: (queryClient: QueryClient) => void,
  url = "/jobs/job-1",
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  seed?.(queryClient);

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[url]}>
        <HistoryControls />
        <Routes>
          <Route path="/builds/:id" element={<h2>Build investigation</h2>} />
          <Route path="/jobs/:id" element={<JobDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("JobDetailPage", () => {
  const mockedGetBuildArtifacts = vi.mocked(getBuildArtifacts);
  const mockedGetProject = vi.mocked(getProject);
  const mockedGetJob = vi.mocked(getJob);
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
        attempt_number: 1,
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
        application_version: null,
      },
      {
        id: "build-recent-1",
        build_number: 20,
        attempt_number: 1,
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
        application_version: null,
        trigger_ref: "main",
      },
    ]);
    mockedGetBuildArtifacts.mockResolvedValue([
      {
        id: "artifact-1",
        build_id: "build-recent-1",
        step_id: null,
        name: "backend-binary",
        path: "dist/backend",
        size_bytes: 1024,
        content_type: "application/octet-stream",
        checksum_sha256:
          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        storage_provider: "filesystem",
        download_url_path: "/artifacts/download/artifact-1",
        version_tags: [
          {
            id: "version-1",
            job_id: "job-1",
            version: "2026.03.30",
            target_type: "artifact",
            artifact_id: "artifact-1",
            created_at: "2026-03-30T00:01:11Z",
          },
        ],
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
      pull_request_enabled: false,
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
      pull_request_enabled: true,
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
      attempt_number: 1,
      priority: 5,
      project_id: "project-1",
      status: "queued",
      created_at: "2026-03-30T00:00:00Z",
      queued_at: "2026-03-30T00:00:01Z",
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
      application_version: null,
    });
  });

  it("loads job and saves edits", async () => {
    renderPage();

    await screen.findByRole("link", { name: "backend-binary" });
    await waitFor(() => {
      expect(mockedListBuildsByJob).toHaveBeenCalledWith("job-1");
    });

    const platformLinks = screen.getAllByRole("link", { name: "Platform" });
    const buildLinks = screen.getAllByRole("link", { name: /Build\s*#21/i });

    expect(platformLinks).toHaveLength(1);
    expect(platformLinks[0]).toHaveAttribute("href", "/projects/project-1");
    expect(buildLinks.length).toBeGreaterThan(0);
    expect(
      screen.queryByRole("link", { name: "View Project Builds" }),
    ).toBeNull();
    expect(
      screen
        .getAllByRole("link", { name: "Browse Job Artifacts" })
        .every(
          (link) =>
            link.getAttribute("href") ===
            "/artifacts?project_id=project-1&job_id=job-1",
        ),
    ).toBe(true);
    expect(
      screen.getByRole("link", { name: "Open Release View" }),
    ).toHaveAttribute(
      "href",
      "/artifacts/logical?project_id=project-1&job_id=job-1",
    );
    expect(
      screen.getByRole("link", { name: "View Latest Build" }),
    ).toHaveAttribute("href", "/builds/build-queued-1");
    expect(
      screen.getByRole("link", { name: "backend-binary" }),
    ).toHaveAttribute("href", "/artifacts/artifact-1");
    expect(screen.getByRole("link", { name: "Open" })).toHaveAttribute(
      "href",
      "/artifacts/artifact-1",
    );
    expect(screen.getByRole("link", { name: "Download" })).toHaveAttribute(
      "href",
      "/api/artifacts/download/artifact-1",
    );
    expect(screen.getByText("1 artifact")).toBeTruthy();
    const checksumPreview = screen.getByText("sha256 0123456789ab…89abcdef");
    expect(checksumPreview).toHaveAttribute(
      "title",
      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    );
    expect(screen.queryByRole("link", { name: /^Job\s/i })).toBeNull();

    fireEvent.click(screen.getByRole("link", { name: "Configuration" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "backend-ci-updated" },
    });
    fireEvent.change(screen.getByLabelText("Managed Image Name"), {
      target: { value: "go-1-24" },
    });
    fireEvent.click(screen.getByLabelText("Enable pull request trigger"));
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(mockedUpdateJob).toHaveBeenCalledWith("job-1", {
        name: "backend-ci-updated",
        priority: 5,
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: true,
        pull_request_enabled: true,
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
    mockedGetBuildArtifacts.mockResolvedValueOnce([]);
    mockedGetJob.mockResolvedValueOnce({
      id: "job-1",
      project_id: "project-1",
      name: "backend-ci",
      priority: 5,
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: false,
      pull_request_enabled: false,
      push_branch: null,
      pipeline_yaml: "version: 1\n",
      pipeline_path: null,
      managed_image: null,
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });

    renderPage();

    await screen.findByText(
      "Latest successful build did not publish artifacts.",
    );
    fireEvent.click(screen.getByRole("link", { name: "Configuration" }));
    await waitFor(() => {
      expect(screen.getByRole("link", { name: "Platform" })).toHaveAttribute(
        "href",
        "/projects/project-1",
      );
      expect(
        screen.getByText("Push Branch", { selector: "strong" }).parentElement,
      ).toHaveTextContent("—");
      expect(
        screen.getByText("Pipeline Source", { selector: "strong" })
          .parentElement,
      ).toHaveTextContent("Inline YAML");
      expect(
        screen.getByText("Managed Build Image", { selector: "strong" })
          .parentElement,
      ).toHaveTextContent("Disabled");
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
      pull_request_enabled: false,
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

    fireEvent.click(await screen.findByRole("link", { name: "Configuration" }));
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
          pull_request_enabled: false,
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
    }, "/jobs/job-1?view=configuration");

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
      pull_request_enabled: false,
      push_branch: "main",
      pipeline_yaml: "",
      pipeline_path: ".coyote/repo-pipeline.yml",
      managed_image: null,
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });

    renderPage();

    fireEvent.click(await screen.findByRole("link", { name: "Configuration" }));
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
        pull_request_enabled: false,
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
      pull_request_enabled: false,
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

    fireEvent.click(await screen.findByRole("link", { name: "Configuration" }));
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
    const { container } = renderPage(
      undefined,
      "/jobs/job-1#job-configuration",
    );

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
        pull_request_enabled: false,
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
      attempt_number: 1,
      priority: 5,
      project_id: "project-1",
      status: "queued",
      created_at: "2026-03-30T00:00:00Z",
      queued_at: "2026-03-30T00:00:01Z",
      started_at: null,
      finished_at: null,
      current_step_index: 0,
      error_message: null,
      application_version: null,
    });

    fireEvent.click(screen.getByRole("button", { name: "Run Now" }));

    await waitFor(() => {
      expect(
        screen.getByText("Job run started. No build link was returned."),
      ).toBeTruthy();
    });

    mockedUpdateJob.mockRejectedValueOnce(new Error("save failed"));
    fireEvent.click(screen.getByRole("button", { name: "Save Job" }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to save job/)).toBeTruthy();
      expect(screen.getByText(/save failed/)).toBeTruthy();
    });
  });
  it("defaults to activity without mounting the editor or fetching credentials", async () => {
    renderPage();
    expect(
      await screen.findByRole("link", { name: "Activity" }),
    ).toHaveAttribute("aria-current", "page");
    expect(screen.queryByLabelText("Name")).toBeNull();
    expect(mockedListSourceCredentials).not.toHaveBeenCalled();
  });

  it("preserves URL state and a local draft across Back/Forward but not across jobs", async () => {
    renderPage(undefined, "/jobs/job-1?from=search#context");
    const config = await screen.findByRole("link", { name: "Configuration" });
    expect(config).toHaveAttribute(
      "href",
      "/jobs/job-1?from=search&view=configuration#context",
    );
    fireEvent.click(config);
    expect(config).toHaveAttribute("aria-current", "page");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "local draft" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByLabelText("Location")).toHaveTextContent(
      "/jobs/job-1?from=search#context",
    );
    expect(screen.queryByLabelText("Name")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Forward" }));
    expect(screen.getByLabelText("Name")).toHaveValue("local draft");
    const other = {
      ...(await mockedGetJob("job-1")),
      id: "job-2",
      name: "release",
    };
    mockedGetJob.mockResolvedValueOnce(other);
    fireEvent.click(screen.getByRole("link", { name: "Other job" }));
    expect(await screen.findByLabelText("Name")).toHaveValue("release");
    expect(mockedListBuildsByJob).toHaveBeenCalledWith("job-2");
  });

  it("opens the legacy anchor and removes only that fragment on leaving it", async () => {
    renderPage(undefined, "/jobs/job-1?from=project#job-configuration");
    expect(await screen.findByLabelText("Name")).toBeVisible();
    fireEvent.click(screen.getByRole("link", { name: "Activity" }));
    expect(screen.getByLabelText("Location").textContent).toBe(
      "/jobs/job-1?from=project",
    );
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByRole("link", { name: "Configuration" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });

  it("runs the saved definition only once while pending and does not retry a missing ID", async () => {
    const result = { ...(await mockedRunJob("job-1")), id: " " };
    mockedRunJob.mockClear();
    let finish!: (value: typeof result) => void;
    mockedRunJob.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    renderPage(undefined, "/jobs/job-1?view=configuration");
    fireEvent.change(await screen.findByLabelText("Name"), {
      target: { value: "not saved" },
    });
    const button = screen.getByRole("button", { name: "Run Now" });
    fireEvent.click(button);
    fireEvent.click(button);
    await waitFor(() =>
      expect(button).toHaveAttribute("aria-disabled", "true"),
    );
    expect(mockedRunJob.mock.calls).toEqual([["job-1"]]);
    expect(mockedUpdateJob).not.toHaveBeenCalled();
    finish(result);
    expect(await screen.findByText(/No build link was returned/)).toBeVisible();
    expect(mockedRunJob).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("Name")).toHaveValue("not saved");
  });

  it("job-scoped activity rows show trigger ref and no redundant job link", async () => {
    renderPage();

    await screen.findByRole("heading", { name: "Recent job builds" });

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
    renderPage(undefined, "/jobs/job-1?view=configuration");

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
        pull_request_enabled: false,
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

    await screen.findByRole("button", { name: "Run Now" });

    fireEvent.click(screen.getByRole("button", { name: "Run Now" }));

    await waitFor(() => {
      expect(mockedRunJob).toHaveBeenCalledWith("job-1");
      expect(screen.getByLabelText("Location")).toHaveTextContent(
        "/builds/build-123",
      );
    });
  });

  it("surfaces run-now error message", async () => {
    mockedRunJob.mockRejectedValueOnce(new Error("API 403: forbidden"));

    renderPage();

    const button = await screen.findByRole("button", { name: "Run Now" });
    button.focus();
    fireEvent.click(button);

    await waitFor(() => {
      expect(screen.getByText(/Failed to run job/)).toBeTruthy();
      expect(screen.getByRole("alert")).toHaveTextContent("403: forbidden");
      expect(button).toHaveFocus();
    });
  });

  it("shows error state for latest successful build outputs", async () => {
    mockedGetBuildArtifacts.mockRejectedValueOnce(
      new Error("artifacts failed"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText(
          "Failed to load latest successful build outputs: Error: artifacts failed",
        ),
      ).toBeTruthy();
    });
  });

  it("links the newest failed execution separately from an older success that finished later", async () => {
    mockedListBuildsByJob.mockResolvedValue([
      {
        id: "build-failed-1",
        build_number: 22,
        attempt_number: 1,
        project_id: "project-1",
        project_name: "Platform",
        job_id: "job-1",
        priority: 5,
        status: "failed",
        created_at: "2026-03-30T00:00:05Z",
        queued_at: "2026-03-30T00:00:01Z",
        started_at: "2026-03-30T00:01:00Z",
        finished_at: "2026-03-30T00:01:05Z",
        current_step_index: 0,
        error_message: null,
        application_version: null,
      },
      {
        id: "build-success-1",
        build_number: 21,
        attempt_number: 1,
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
        application_version: null,
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Build", { selector: "strong" })).toBeTruthy();
      expect(
        screen.queryByText("Latest Success:", { selector: "strong" }),
      ).toBeNull();
      expect(screen.getByRole("link", { name: "#22" })).toHaveAttribute(
        "href",
        "/builds/build-failed-1",
      );
      expect(
        screen.getByRole("link", { name: "View Successful Build" }),
      ).toHaveAttribute("href", "/builds/build-success-1");
    });
  });

  it("shows only the first three latest outputs and the overflow message", async () => {
    mockedGetBuildArtifacts.mockResolvedValueOnce([
      {
        id: "artifact-1",
        build_id: "build-recent-1",
        step_id: null,
        name: "artifact-1",
        path: "dist/artifact-1",
        size_bytes: 100,
        content_type: null,
        checksum_sha256: null,
        storage_provider: "filesystem",
        download_url_path: "/artifacts/download/artifact-1",
        created_at: "2026-03-30T00:01:10Z",
      },
      {
        id: "artifact-2",
        build_id: "build-recent-1",
        step_id: null,
        name: "artifact-2",
        path: "dist/artifact-2",
        size_bytes: 200,
        content_type: null,
        checksum_sha256: null,
        storage_provider: "filesystem",
        download_url_path: "/artifacts/download/artifact-2",
        created_at: "2026-03-30T00:01:11Z",
      },
      {
        id: "artifact-3",
        build_id: "build-recent-1",
        step_id: null,
        name: "artifact-3",
        path: "dist/artifact-3",
        size_bytes: 300,
        content_type: null,
        checksum_sha256: null,
        storage_provider: "filesystem",
        download_url_path: "/artifacts/download/artifact-3",
        created_at: "2026-03-30T00:01:12Z",
      },
      {
        id: "artifact-4",
        build_id: "build-recent-1",
        step_id: null,
        name: "artifact-4",
        path: "dist/artifact-4",
        size_bytes: 400,
        content_type: null,
        checksum_sha256: null,
        storage_provider: "filesystem",
        download_url_path: "/artifacts/download/artifact-4",
        created_at: "2026-03-30T00:01:13Z",
      },
    ]);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText("artifact-1")).toBeTruthy();
      expect(screen.getByText("artifact-2")).toBeTruthy();
      expect(screen.getByText("artifact-3")).toBeTruthy();
      expect(screen.queryByText("artifact-4")).toBeNull();
      expect(
        screen.getByText(
          "Showing 3 of 4 artifacts from the latest successful build.",
        ),
      ).toBeTruthy();
    });
  });

  it("shows build id slice as fallback when build has no build number", async () => {
    mockedListBuildsByJob.mockResolvedValueOnce([
      {
        id: "build-no-num-1",
        attempt_number: 1,
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
        application_version: null,
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
