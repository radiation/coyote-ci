import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { ManagedImageSettingsPage } from "./ManagedImageSettingsPage";
import {
  createRepoWritebackConfig,
  createSourceCredential,
  listRepoWritebackConfigs,
  listSourceCredentials,
} from "../api";

vi.mock("../api", () => ({
  listSourceCredentials: vi.fn(),
  createSourceCredential: vi.fn(),
  deleteSourceCredential: vi.fn(),
  listRepoWritebackConfigs: vi.fn(),
  createRepoWritebackConfig: vi.fn(),
  updateRepoWritebackConfig: vi.fn(),
  deleteRepoWritebackConfig: vi.fn(),
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
        <ManagedImageSettingsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ManagedImageSettingsPage", () => {
  const mockedListSourceCredentials = vi.mocked(listSourceCredentials);
  const mockedCreateSourceCredential = vi.mocked(createSourceCredential);
  const mockedListRepoWritebackConfigs = vi.mocked(listRepoWritebackConfigs);
  const mockedCreateRepoWritebackConfig = vi.mocked(createRepoWritebackConfig);

  beforeEach(() => {
    vi.clearAllMocks();

    mockedListSourceCredentials.mockResolvedValue([
      {
        id: "cred-1",
        project_id: "project-1",
        name: "github-token",
        kind: "https_token",
        username: "x-access-token",
        secret_ref: "GITHUB_WRITE_TOKEN",
        created_at: "2026-04-20T00:00:00Z",
        updated_at: "2026-04-20T00:00:00Z",
      },
    ]);

    mockedListRepoWritebackConfigs.mockResolvedValue([
      {
        id: "cfg-1",
        project_id: "project-1",
        repository_url: "https://github.com/example/repo.git",
        pipeline_path: ".coyote/pipeline.yml",
        managed_image_name: "go",
        write_credential_id: "cred-1",
        bot_branch_prefix: "coyote/managed-image-refresh",
        commit_author_name: "Coyote CI Bot",
        commit_author_email: "bot@coyote-ci.local",
        enabled: true,
        created_at: "2026-04-20T00:00:00Z",
        updated_at: "2026-04-20T00:00:00Z",
      },
    ]);

    mockedCreateSourceCredential.mockResolvedValue({
      id: "cred-2",
      project_id: "project-1",
      name: "new-token",
      kind: "https_token",
      username: "x-access-token",
      secret_ref: "NEW_TOKEN",
      created_at: "2026-04-20T00:00:00Z",
      updated_at: "2026-04-20T00:00:00Z",
    });

    mockedCreateRepoWritebackConfig.mockResolvedValue({
      id: "cfg-2",
      project_id: "project-1",
      repository_url: "https://github.com/example/new-repo.git",
      pipeline_path: ".coyote/pipeline.yml",
      managed_image_name: "go",
      write_credential_id: "cred-1",
      bot_branch_prefix: "coyote/managed-image-refresh",
      commit_author_name: "Coyote CI Bot",
      commit_author_email: "bot@coyote-ci.local",
      enabled: true,
      created_at: "2026-04-20T00:00:00Z",
      updated_at: "2026-04-20T00:00:00Z",
    });
  });

  it("loads and renders existing credentials and writeback configs", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("github-token")).toBeTruthy();
      expect(
        screen.getByText("https://github.com/example/repo.git"),
      ).toBeTruthy();
    });
  });

  it("submits create credential and writeback config forms", async () => {
    renderPage();

    await screen.findByText("github-token");

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "new-token" },
    });
    fireEvent.change(screen.getByLabelText("Secret Ref"), {
      target: { value: "NEW_TOKEN" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add Credential" }));

    await waitFor(() => {
      expect(mockedCreateSourceCredential).toHaveBeenCalled();
    });

    fireEvent.change(screen.getByLabelText("Repository URL"), {
      target: { value: "https://github.com/example/new-repo.git" },
    });
    fireEvent.change(screen.getByLabelText("Write Credential"), {
      target: { value: "cred-1" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Add Write-Back Config" }),
    );

    await waitFor(() => {
      expect(mockedCreateRepoWritebackConfig).toHaveBeenCalled();
    });
  });
});
