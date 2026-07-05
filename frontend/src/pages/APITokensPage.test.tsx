import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { APITokensPage } from "./APITokensPage";
import {
  APIError,
  createAPIToken,
  listAPITokens,
  revokeAPIToken,
} from "../api";
import { AuthContext, type AuthContextValue } from "../auth-context";
import { formatTime } from "../utils/time";

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
  const confirmSpy = vi.spyOn(window, "confirm");

  beforeEach(() => {
    vi.clearAllMocks();
    confirmSpy.mockReset();
    confirmSpy.mockReturnValue(true);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    mockedListAPITokens.mockResolvedValue([
      {
        id: "token-1",
        name: "fixture-token",
        scopes: ["build:read", "build:logs"],
        token_prefix: "coyote_pat_abcd1234",
        created_at: "2026-05-12T00:00:00Z",
      },
    ]);
    mockedCreateAPIToken.mockResolvedValue({
      id: "token-2",
      name: "coyote cli",
      scopes: ["build:read", "build:logs"],
      token_prefix: "coyote_pat_rawtok12",
      token: "coyote_pat_rawtoken",
      created_at: "2026-05-12T01:00:00Z",
    });
    mockedRevokeAPIToken.mockResolvedValue();
    writeText.mockResolvedValue(undefined);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("lists, creates, copies, dismisses, and revokes API tokens", async () => {
    renderPage();

    await screen.findByText("fixture-token");
    expect(screen.getByText("Never")).toBeTruthy();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "coyote cli" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Token" }));

    await waitFor(() => {
      expect(mockedCreateAPIToken).toHaveBeenCalled();
      expect(mockedCreateAPIToken.mock.calls[0]?.[0]).toEqual({
        name: "coyote cli",
        expires_at: undefined,
        scopes: ["build:read", "build:logs"],
      });
    });

    expect(screen.getByText("build:read, build:logs")).toBeTruthy();

    expect(await screen.findByDisplayValue("coyote_pat_rawtoken")).toBeTruthy();
    expect(
      screen.getByText("Copy this token now. Coyote will not show it again."),
    ).toBeTruthy();
    expect(screen.getByText("coyote auth token set")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Copy token" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("coyote_pat_rawtoken");
      expect(screen.getByText("Token copied.")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    await waitFor(() => {
      expect(screen.queryByDisplayValue("coyote_pat_rawtoken")).toBeNull();
    });

    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));

    await waitFor(() => {
      expect(confirmSpy).toHaveBeenCalledWith(
        "Revoke API token fixture-token (coyote_pat_abcd1234)?",
      );
      expect(mockedRevokeAPIToken).toHaveBeenCalled();
      expect(mockedRevokeAPIToken.mock.calls[0]?.[0]).toBe("token-1");
    });
  });

  it("shows the CLI-focused empty state", async () => {
    mockedListAPITokens.mockResolvedValue([]);

    renderPage();

    expect(
      await screen.findByText(/No personal API tokens yet\./i),
    ).toBeTruthy();
    expect(
      screen.getByText(
        /use the Coyote CLI or another authenticated API client without relying on a browser session/i,
      ),
    ).toBeTruthy();
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

  it("does not recreate the secret after a refresh", async () => {
    const firstRender = renderPage();

    await screen.findByText("fixture-token");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "coyote cli" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Token" }));

    expect(await screen.findByDisplayValue("coyote_pat_rawtoken")).toBeTruthy();

    firstRender.unmount();
    renderPage();

    await screen.findByText("fixture-token");
    expect(screen.queryByDisplayValue("coyote_pat_rawtoken")).toBeNull();
    expect(screen.queryByText("coyote auth token set")).toBeNull();
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

  it("shows create validation errors from the API", async () => {
    mockedCreateAPIToken.mockRejectedValueOnce(
      new APIError(400, "api token name is required"),
    );

    renderPage();

    await screen.findByText("fixture-token");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "bad token" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Token" }));

    expect(
      await screen.findByText(
        /Failed to create API token: API 400: api token name is required/i,
      ),
    ).toBeTruthy();
  });

  it("refreshes the authoritative list when revoke returns not found", async () => {
    mockedRevokeAPIToken.mockRejectedValueOnce(
      new APIError(404, "api token not found"),
    );

    renderPage();

    await screen.findByText("fixture-token");
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));

    await waitFor(() => {
      expect(mockedRevokeAPIToken).toHaveBeenCalled();
      expect(mockedRevokeAPIToken.mock.calls[0]?.[0]).toBe("token-1");
      expect(mockedListAPITokens).toHaveBeenCalledTimes(2);
    });

    expect(screen.queryByText(/Failed to revoke API token/i)).toBeNull();
  });

  it("shows revoked tokens as revoked and disables their action", async () => {
    mockedListAPITokens.mockResolvedValue([
      {
        id: "token-2",
        name: "old-token",
        scopes: [],
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

  it("renders last-used and expiration timestamps when present", async () => {
    mockedListAPITokens.mockResolvedValue([
      {
        id: "token-3",
        name: "dated-token",
        scopes: ["artifact:read"],
        token_prefix: "coyote_pat_dates",
        created_at: "2026-05-10T00:00:00Z",
        last_used_at: "2026-05-11T08:30:00Z",
        expires_at: "2026-06-01T12:00:00Z",
      },
    ]);

    renderPage();

    expect(await screen.findByText("dated-token")).toBeTruthy();
    expect(screen.getByText(formatTime("2026-05-11T08:30:00Z"))).toBeTruthy();
    expect(screen.getByText(formatTime("2026-06-01T12:00:00Z"))).toBeTruthy();
  });

  it("does not revoke when confirmation is canceled", async () => {
    confirmSpy.mockReturnValue(false);

    renderPage();

    await screen.findByText("fixture-token");
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));

    expect(confirmSpy).toHaveBeenCalled();
    expect(mockedRevokeAPIToken).not.toHaveBeenCalled();
  });

  it("shows copy failure feedback when clipboard write fails", async () => {
    writeText.mockRejectedValueOnce(new Error("clipboard unavailable"));

    renderPage();

    await screen.findByText("fixture-token");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "coyote cli" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create Token" }));

    expect(await screen.findByDisplayValue("coyote_pat_rawtoken")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Copy token" }));

    expect(await screen.findByText("Unable to copy token.")).toBeTruthy();
  });

  it("shows copy failure feedback when the clipboard API is unavailable", async () => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: undefined,
    });

    renderPage();

    await screen.findByText("fixture-token");
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "coyote cli" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: /Create Token|Creating\.\.\./ }),
    );

    expect(await screen.findByDisplayValue("coyote_pat_rawtoken")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Copy token" }));

    expect(await screen.findByText("Unable to copy token.")).toBeTruthy();
  });

  it("defaults to least-privilege diagnostic scopes and leaves build:run unselected", async () => {
    renderPage();

    await screen.findByText("fixture-token");
    expect(screen.getByLabelText(/Build status/i)).toBeChecked();
    expect(screen.getByLabelText(/Build logs/i)).toBeChecked();
    expect(screen.getByLabelText(/Rerun builds/i)).not.toBeChecked();
    expect(screen.getByLabelText(/Read artifacts/i)).not.toBeChecked();
  });
});
