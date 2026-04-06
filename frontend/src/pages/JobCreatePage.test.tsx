import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { JobCreatePage } from "./JobCreatePage";
import { createJob } from "../api";

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
  createJob: vi.fn(),
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
      <MemoryRouter>
        <JobCreatePage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("JobCreatePage", () => {
  const mockedCreateJob = vi.mocked(createJob);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedCreateJob.mockResolvedValue({
      id: "job-1",
      project_id: "project-1",
      name: "backend-ci",
      repository_url: "https://github.com/example/backend.git",
      default_ref: "main",
      push_enabled: false,
      push_branch: null,
      pipeline_yaml:
        "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
      enabled: true,
      created_at: "2026-03-30T00:00:00Z",
      updated_at: "2026-03-30T00:00:00Z",
    });
  });

  it("submits expected create payload and navigates to detail", async () => {
    renderPage();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: " backend-ci " },
    });
    fireEvent.change(screen.getByLabelText("Repository URL"), {
      target: { value: " https://github.com/example/backend.git " },
    });
    fireEvent.change(screen.getByLabelText("Default Ref"), {
      target: { value: " main " },
    });
    fireEvent.change(screen.getByLabelText("Pipeline YAML"), {
      target: {
        value: "version: 1\nsteps:\n  - name: test\n    run: go test ./...\n",
      },
    });

    fireEvent.click(screen.getByRole("button", { name: "Create Job" }));

    await waitFor(() => {
      expect(mockedCreateJob).toHaveBeenCalledTimes(1);
      expect(mockedCreateJob.mock.calls[0][0]).toEqual({
        project_id: "project-1",
        name: "backend-ci",
        repository_url: "https://github.com/example/backend.git",
        default_ref: "main",
        push_enabled: false,
        push_branch: "",
        pipeline_yaml:
          "version: 1\nsteps:\n  - name: test\n    run: go test ./...",
        enabled: true,
      });
      expect(navigateMock).toHaveBeenCalledWith("/jobs/job-1");
    });
  });
});
