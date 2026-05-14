import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APITokensPage } from "./APITokensPage";
import { createAPIToken, listAPITokens, revokeAPIToken } from "../api";
import { AuthContext, type AuthContextValue } from "../auth-context";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    createAPIToken: vi.fn(),
    listAPITokens: vi.fn(),
    revokeAPIToken: vi.fn(),
  };
});

function buildAuthValue(
  overrides: Partial<AuthContextValue> = {},
): AuthContextValue {
  return {
    currentUser: {
      id: "user-1",
      email: "user@example.com",
      global_role: "user",
    },
    authMode: "header",
    authStatus: "authenticated",
    error: null,
    isGlobalAdmin: false,
    loginAvailable: false,
    login: vi.fn(),
    logout: vi.fn().mockResolvedValue(undefined),
    refreshCurrentUser: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

function renderPage(authValue?: Partial<AuthContextValue>) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={buildAuthValue(authValue)}>
        <APITokensPage />
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
}

describe("APITokensPage", () => {
  const mockedCreateAPIToken = vi.mocked(createAPIToken);
  const mockedListAPITokens = vi.mocked(listAPITokens);
  const mockedRevokeAPIToken = vi.mocked(revokeAPIToken);
  const writeText = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    mockedListAPITokens.mockResolvedValue([
      {
        id: "token-1",
        name: "fixture-token",
        token_prefix: "coyote_pat_abcd1234",
        created_at: "2026-05-12T00:00:00Z",
      },
    ]);
    mockedCreateAPIToken.mockResolvedValue({
      id: "token-2",
      name: "fixture populator",
      token_prefix: "coyote_pat_rawtok12",
      token: "coyote_pat_rawtoken",
      created_at: "2026-05-12T01:00:00Z",
    });
    mockedRevokeAPIToken.mockResolvedValue();
    writeText.mockResolvedValue(undefined);
  });

  it("lists, creates, copies, and revokes API tokens", async () => {
    renderPage();

    await screen.findByText("fixture-token");

    fireEvent.click(screen.getByRole("button", { name: "Create Token" }));

    await waitFor(() => {
      expect(mockedCreateAPIToken).toHaveBeenCalled();
      expect(mockedCreateAPIToken.mock.calls[0]?.[0]).toEqual({
        name: "fixture populator",
        expires_at: undefined,
      });
    });

    expect(await screen.findByDisplayValue("coyote_pat_rawtoken")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Copy token" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("coyote_pat_rawtoken");
      expect(screen.getByText("Token copied.")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));

    await waitFor(() => {
      expect(mockedRevokeAPIToken).toHaveBeenCalled();
      expect(mockedRevokeAPIToken.mock.calls[0]?.[0]).toBe("token-1");
    });
  });

  it("requires a token name", async () => {
    renderPage();

    await screen.findByText("fixture-token");

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "   " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Token" }));

    expect(screen.getByText("Token name is required.")).toBeTruthy();
    expect(mockedCreateAPIToken).not.toHaveBeenCalled();
  });

  it("shows a disabled-mode message and skips token loading", () => {
    renderPage({
      authMode: "disabled",
      currentUser: {
        id: "disabled-mode-user",
        email: "dev@local.coyote-ci",
        global_role: "admin",
      },
      isGlobalAdmin: true,
    });

    expect(screen.getByText("Unavailable in disabled auth mode")).toBeTruthy();
    expect(mockedListAPITokens).not.toHaveBeenCalled();
    expect(screen.getByText(/token management is not available/i)).toBeTruthy();
  });

  it("shows revoked tokens as revoked and disables their action", async () => {
    mockedListAPITokens.mockResolvedValue([
      {
        id: "token-2",
        name: "old-token",
        token_prefix: "coyote_pat_revoked",
        created_at: "2026-05-10T00:00:00Z",
        revoked_at: "2026-05-12T00:00:00Z",
      },
    ]);

    renderPage();

    expect(await screen.findByText("old-token")).toBeTruthy();
    expect(screen.getByText("Status")).toBeTruthy();
    expect(screen.getAllByText("Revoked")).toHaveLength(2);

    const revokedButton = screen.getByRole("button", { name: "Revoked" });
    expect(revokedButton).toBeDisabled();

    fireEvent.click(revokedButton);

    expect(mockedRevokeAPIToken).not.toHaveBeenCalled();
  });
});
