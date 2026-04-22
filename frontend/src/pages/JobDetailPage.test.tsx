import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { JobDetailPage } from "./JobDetailPage";
import {
  getJob,
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
  getJob: vi.fn(),
  updateJob: vi.fn(),
  runJob: vi.fn(),
  listBuildsByJob: vi.fn(),
  listSourceCredentials: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

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
  const mockedGetJob = vi.mocked(getJob);
  const mockedUpdateJob = vi.mocked(updateJob);
  const mockedRunJob = vi.mocked(runJob);
  const mockedListBuildsByJob = vi.mocked(listBuildsByJob);
  const mockedListSourceCredentials = vi.mocked(listSourceCredentials);

  beforeEach(() => {
    vi.clearAllMocks();

    mockedListBuildsByJob.mockResolvedValue([]);
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
});
