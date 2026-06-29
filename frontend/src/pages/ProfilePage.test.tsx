import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { getMe, getMyEmailNotificationTarget } from "../api";
import { AuthContext, type AuthContextValue } from "../auth-context";
import { ProfilePage } from "./ProfilePage";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getMe: vi.fn(),
    getMyEmailNotificationTarget: vi.fn(),
  };
});

function renderPage(overrides?: Partial<AuthContextValue>) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
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

  it("shows a configured notification summary and management link", async () => {
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
      expect(screen.getByText("Configured")).toBeTruthy();
      expect(screen.getByText("Active")).toBeTruthy();
      expect(
        screen.getByRole("link", { name: /Manage my notifications/i }),
      ).toHaveAttribute("href", "/settings/my-notifications");
    });
  });

  it("shows an unconfigured notification summary when no target exists", async () => {
    renderPage();

    await waitFor(() => {
      expect(screen.getByText("Not configured")).toBeTruthy();
      expect(
        screen.getByText(/Create a target to enable personal delivery/i),
      ).toBeTruthy();
    });
  });

  it("renders verified email state and display-name fallback", async () => {
    mockedGetMe.mockResolvedValue({
      auth_mode: "oidc",
      auth_method: "oidc",
      email_verified: true,
      user: {
        id: "user-1",
        email: "user@example.com",
        display_name: null,
        global_role: "user",
      },
    });

    renderPage({
      currentUser: {
        id: "user-1",
        email: "user@example.com",
        display_name: "   ",
        global_role: "user",
      },
    });

    await waitFor(() => {
      expect(
        screen.getByText("Not provided by authentication provider"),
      ).toBeTruthy();
      expect(
        screen.getByText("Verified by authentication provider"),
      ).toBeTruthy();
    });

    expect(
      screen.queryByText(/verification state is not currently provided/i),
    ).toBeNull();
  });

  it("renders api token provider label", async () => {
    mockedGetMe.mockResolvedValue({
      auth_mode: "oidc",
      auth_method: "api_token",
      email_verified: null,
      user: {
        id: "user-1",
        email: "user@example.com",
        display_name: "User Example",
        global_role: "user",
      },
    });

    renderPage({ authMode: null });

    await waitFor(() => {
      expect(screen.getByText("API token")).toBeTruthy();
    });
  });
});
