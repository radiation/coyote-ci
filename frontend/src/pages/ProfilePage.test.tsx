import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import {
  ensureMyEmailNotificationTarget,
  getMe,
  getMyEmailNotificationTarget,
} from "../api";
import { AuthContext, type AuthContextValue } from "../auth-context";
import { ProfilePage } from "./ProfilePage";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getMe: vi.fn(),
    getMyEmailNotificationTarget: vi.fn(),
    ensureMyEmailNotificationTarget: vi.fn(),
  };
});

function renderPage(overrides?: Partial<AuthContextValue>) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  const authValue: AuthContextValue = {
    currentUser: {
      id: "user-1",
      email: "user@example.com",
      display_name: "User Example",
      global_role: "user",
    },
    authMode: "oidc",
    authStatus: "authenticated",
    error: null,
    isGlobalAdmin: false,
    loginAvailable: true,
    login: vi.fn(),
    logout: vi.fn(async () => undefined),
    refreshCurrentUser: vi.fn(async () => undefined),
    ...overrides,
  };

  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue}>
        <MemoryRouter>
          <ProfilePage />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
}

describe("ProfilePage", () => {
  const mockedGetMe = vi.mocked(getMe);
  const mockedGetMyEmailNotificationTarget = vi.mocked(
    getMyEmailNotificationTarget,
  );
  const mockedEnsureMyEmailNotificationTarget = vi.mocked(
    ensureMyEmailNotificationTarget,
  );

  beforeEach(() => {
    vi.clearAllMocks();

    mockedGetMe.mockResolvedValue({
      auth_mode: "oidc",
      auth_method: "oidc",
      email_verified: null,
      user: {
        id: "user-1",
        email: "user@example.com",
        display_name: "User Example",
        global_role: "user",
      },
    });

    mockedGetMyEmailNotificationTarget.mockResolvedValue(null);

    mockedEnsureMyEmailNotificationTarget.mockResolvedValue({
      id: "target-1",
      owner_user_id: "user-1",
      type: "email",
      name: "User Example",
      address: "<user@example.com>",
      webhook_configured: false,
      enabled: true,
      created_at: "2026-06-24T00:00:00Z",
      updated_at: "2026-06-24T00:00:00Z",
    });
  });

  it("renders profile fields and read-only identity messaging", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Profile" })).toBeTruthy();
      expect(screen.getByText("user-1")).toBeTruthy();
      expect(screen.getByText("User Example")).toBeTruthy();
      expect(screen.getByText("user@example.com")).toBeTruthy();
      expect(screen.getByText("user")).toBeTruthy();
      expect(screen.getByText("OIDC")).toBeTruthy();
    });

    expect(screen.getByText(/read-only in Coyote CI for now/i)).toBeTruthy();
    expect(
      screen.getByText(/verification state is not currently provided/i),
    ).toBeTruthy();
  });

  it("renders an existing personal target and hides create action", async () => {
    mockedGetMyEmailNotificationTarget.mockResolvedValue({
      id: "target-1",
      owner_user_id: "user-1",
      type: "email",
      name: "User Example",
      address: "<user@example.com>",
      webhook_configured: false,
      enabled: true,
      created_at: "2026-06-24T00:00:00Z",
      updated_at: "2026-06-24T00:00:00Z",
    });

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText("This is your personal email notification target."),
      ).toBeTruthy();
      expect(screen.getByText("<user@example.com>")).toBeTruthy();
      expect(
        screen.getByRole("link", { name: /Notification settings/i }),
      ).toBeTruthy();
    });

    expect(
      screen.queryByRole("button", { name: "Create my email target" }),
    ).toBeNull();
  });

  it("shows create action when no personal target exists and refreshes state after success", async () => {
    mockedGetMyEmailNotificationTarget
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T00:00:00Z",
      });

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Create my email target" }),
      ).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Create my email target" }),
    );

    await waitFor(() => {
      expect(mockedEnsureMyEmailNotificationTarget).toHaveBeenCalledTimes(1);
      expect(
        screen.getByText("This is your personal email notification target."),
      ).toBeTruthy();
    });
  });

  it("surfaces mutation failures clearly", async () => {
    mockedEnsureMyEmailNotificationTarget.mockRejectedValue(
      new Error("conflict"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Create my email target" }),
      ).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Create my email target" }),
    );

    await waitFor(() => {
      expect(
        screen.getByText(/Failed to create personal email target/i),
      ).toBeTruthy();
    });
  });
});
