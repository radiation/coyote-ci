import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { CredentialsPage } from "./CredentialsPage";
import {
  createSourceCredential,
  deleteSourceCredential,
  listSourceCredentials,
} from "../api";

vi.mock("../api", () => ({
  listSourceCredentials: vi.fn(),
  createSourceCredential: vi.fn(),
  deleteSourceCredential: vi.fn(),
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
        <CredentialsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("CredentialsPage", () => {
  const mockedListSourceCredentials = vi.mocked(listSourceCredentials);
  const mockedCreateSourceCredential = vi.mocked(createSourceCredential);
  const mockedDeleteSourceCredential = vi.mocked(deleteSourceCredential);

  beforeEach(() => {
    vi.clearAllMocks();

    mockedListSourceCredentials.mockResolvedValue([
      {
        id: "cred-1",
        name: "github-token",
        kind: "https_token",
        username: "x-access-token",
        secret_ref: "GITHUB_WRITE_TOKEN",
        created_at: "2026-04-20T00:00:00Z",
        updated_at: "2026-04-20T00:00:00Z",
      },
    ]);

    mockedCreateSourceCredential.mockResolvedValue({
      id: "cred-2",
      name: "new-token",
      kind: "https_token",
      username: "x-access-token",
      secret_ref: "NEW_TOKEN",
      created_at: "2026-04-20T00:00:00Z",
      updated_at: "2026-04-20T00:00:00Z",
    });

    mockedDeleteSourceCredential.mockResolvedValue();
  });

  it("loads, creates, and deletes credentials", async () => {
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
      expect(mockedCreateSourceCredential.mock.calls[0]?.[0]).toEqual({
        name: "new-token",
        kind: "https_token",
        username: "x-access-token",
        secret_ref: "NEW_TOKEN",
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    await waitFor(() => {
      expect(mockedDeleteSourceCredential).toHaveBeenCalled();
      expect(mockedDeleteSourceCredential.mock.calls[0]?.[0]).toBe("cred-1");
    });
  });
});
