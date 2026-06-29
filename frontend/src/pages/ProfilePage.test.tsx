import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import {
  ensureMyEmailNotificationTarget,
  getCommitAuthorFailureNotificationPreference,
  getCommitAuthorSuccessNotificationPreference,
  getMe,
  getMyEmailNotificationTarget,
  setCommitAuthorFailureNotificationPreference,
  setCommitAuthorSuccessNotificationPreference,
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
    getCommitAuthorFailureNotificationPreference: vi.fn(),
    getCommitAuthorSuccessNotificationPreference: vi.fn(),
    setCommitAuthorFailureNotificationPreference: vi.fn(),
    setCommitAuthorSuccessNotificationPreference: vi.fn(),
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
  const mockedGetCommitAuthorFailureNotificationPreference = vi.mocked(
    getCommitAuthorFailureNotificationPreference,
  );
  const mockedGetCommitAuthorSuccessNotificationPreference = vi.mocked(
    getCommitAuthorSuccessNotificationPreference,
  );
  const mockedSetCommitAuthorFailureNotificationPreference = vi.mocked(
    setCommitAuthorFailureNotificationPreference,
  );
  const mockedSetCommitAuthorSuccessNotificationPreference = vi.mocked(
    setCommitAuthorSuccessNotificationPreference,
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
    mockedGetCommitAuthorFailureNotificationPreference.mockResolvedValue({
      enabled: false,
      eligible: false,
      delivery_active: false,
      target: null,
      unavailable_reason: "personal_target_required",
    });
    mockedGetCommitAuthorSuccessNotificationPreference.mockResolvedValue({
      enabled: false,
      eligible: false,
      delivery_active: false,
      target: null,
      unavailable_reason: "personal_target_required",
    });

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

    mockedSetCommitAuthorFailureNotificationPreference.mockResolvedValue({
      enabled: true,
      eligible: true,
      delivery_active: true,
      unavailable_reason: null,
      target: {
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T00:00:00Z",
      },
    });
    mockedSetCommitAuthorSuccessNotificationPreference.mockResolvedValue({
      enabled: true,
      eligible: true,
      delivery_active: true,
      unavailable_reason: null,
      target: {
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T00:00:00Z",
      },
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
    mockedGetCommitAuthorFailureNotificationPreference
      .mockResolvedValueOnce({
        enabled: false,
        eligible: false,
        delivery_active: false,
        target: null,
        unavailable_reason: "personal_target_required",
      })
      .mockResolvedValueOnce({
        enabled: false,
        eligible: true,
        delivery_active: false,
        unavailable_reason: null,
        target: {
          id: "target-1",
          owner_user_id: "user-1",
          type: "email",
          name: "User Example",
          address: "<user@example.com>",
          webhook_configured: false,
          enabled: true,
          created_at: "2026-06-24T00:00:00Z",
          updated_at: "2026-06-24T00:00:00Z",
        },
      });
    mockedGetCommitAuthorSuccessNotificationPreference
      .mockResolvedValueOnce({
        enabled: false,
        eligible: false,
        delivery_active: false,
        target: null,
        unavailable_reason: "personal_target_required",
      })
      .mockResolvedValueOnce({
        enabled: false,
        eligible: true,
        delivery_active: false,
        unavailable_reason: null,
        target: {
          id: "target-1",
          owner_user_id: "user-1",
          type: "email",
          name: "User Example",
          address: "<user@example.com>",
          webhook_configured: false,
          enabled: true,
          created_at: "2026-06-24T00:00:00Z",
          updated_at: "2026-06-24T00:00:00Z",
        },
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
      expect(
        screen.getByRole("checkbox", {
          name: /Notify me when my commits fail/i,
        }),
      ).toBeTruthy();
      expect(
        screen.getByRole("checkbox", {
          name: /Notify me when my commits succeed/i,
        }),
      ).toBeTruthy();
    });

    expect(
      mockedSetCommitAuthorFailureNotificationPreference,
    ).not.toHaveBeenCalled();
    expect(
      mockedSetCommitAuthorSuccessNotificationPreference,
    ).not.toHaveBeenCalled();
  });

  it("shows an enabled initialized preference after creating a personal target when the instance default is enabled", async () => {
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
    mockedGetCommitAuthorFailureNotificationPreference
      .mockResolvedValueOnce({
        enabled: false,
        eligible: false,
        delivery_active: false,
        target: null,
        unavailable_reason: "personal_target_required",
      })
      .mockResolvedValueOnce({
        enabled: true,
        eligible: true,
        delivery_active: true,
        unavailable_reason: null,
        target: {
          id: "target-1",
          owner_user_id: "user-1",
          type: "email",
          name: "User Example",
          address: "<user@example.com>",
          webhook_configured: false,
          enabled: true,
          created_at: "2026-06-24T00:00:00Z",
          updated_at: "2026-06-24T00:00:00Z",
        },
      });
    mockedGetCommitAuthorSuccessNotificationPreference
      .mockResolvedValueOnce({
        enabled: false,
        eligible: false,
        delivery_active: false,
        target: null,
        unavailable_reason: "personal_target_required",
      })
      .mockResolvedValueOnce({
        enabled: false,
        eligible: true,
        delivery_active: false,
        unavailable_reason: null,
        target: {
          id: "target-1",
          owner_user_id: "user-1",
          type: "email",
          name: "User Example",
          address: "<user@example.com>",
          webhook_configured: false,
          enabled: true,
          created_at: "2026-06-24T00:00:00Z",
          updated_at: "2026-06-24T00:00:00Z",
        },
      });

    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Create my email target" }),
    );

    await waitFor(() => {
      expect(
        screen.getByRole("checkbox", {
          name: /Notify me when my commits fail/i,
        }),
      ).toBeChecked();
    });
  });

  it("shows a disabled initialized preference after creating a personal target when the instance default is disabled", async () => {
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
    mockedGetCommitAuthorFailureNotificationPreference
      .mockResolvedValueOnce({
        enabled: false,
        eligible: false,
        delivery_active: false,
        target: null,
        unavailable_reason: "personal_target_required",
      })
      .mockResolvedValueOnce({
        enabled: false,
        eligible: true,
        delivery_active: false,
        unavailable_reason: null,
        target: {
          id: "target-1",
          owner_user_id: "user-1",
          type: "email",
          name: "User Example",
          address: "<user@example.com>",
          webhook_configured: false,
          enabled: true,
          created_at: "2026-06-24T00:00:00Z",
          updated_at: "2026-06-24T00:00:00Z",
        },
      });
    mockedGetCommitAuthorSuccessNotificationPreference
      .mockResolvedValueOnce({
        enabled: false,
        eligible: false,
        delivery_active: false,
        target: null,
        unavailable_reason: "personal_target_required",
      })
      .mockResolvedValueOnce({
        enabled: false,
        eligible: true,
        delivery_active: false,
        unavailable_reason: null,
        target: {
          id: "target-1",
          owner_user_id: "user-1",
          type: "email",
          name: "User Example",
          address: "<user@example.com>",
          webhook_configured: false,
          enabled: true,
          created_at: "2026-06-24T00:00:00Z",
          updated_at: "2026-06-24T00:00:00Z",
        },
      });

    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Create my email target" }),
    );

    await waitFor(() => {
      expect(
        screen.getByRole("checkbox", {
          name: /Notify me when my commits fail/i,
        }),
      ).not.toBeChecked();
    });
  });

  it("toggles commit-author failure notifications when the personal target is enabled", async () => {
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
    mockedGetCommitAuthorFailureNotificationPreference.mockResolvedValue({
      enabled: true,
      eligible: true,
      delivery_active: true,
      unavailable_reason: null,
      target: {
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T00:00:00Z",
      },
    });

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("checkbox", {
          name: /Notify me when my commits fail/i,
        }),
      ).toBeChecked();
    });

    const checkbox = screen.getByRole("checkbox", {
      name: /Notify me when my commits fail/i,
    });

    fireEvent.click(checkbox);

    await waitFor(() => {
      expect(
        mockedSetCommitAuthorFailureNotificationPreference,
      ).toHaveBeenCalled();
    });
    expect(
      mockedSetCommitAuthorFailureNotificationPreference.mock.calls[0]?.[0],
    ).toEqual({
      enabled: false,
    });
  });

  it("toggles commit-author success notifications when the personal target is enabled", async () => {
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
    mockedGetCommitAuthorSuccessNotificationPreference.mockResolvedValue({
      enabled: true,
      eligible: true,
      delivery_active: true,
      unavailable_reason: null,
      target: {
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T00:00:00Z",
      },
    });

    renderPage();

    const checkbox = await screen.findByRole("checkbox", {
      name: /Notify me when my commits succeed/i,
    });
    expect(checkbox).toBeChecked();

    fireEvent.click(checkbox);

    await waitFor(() => {
      expect(
        mockedSetCommitAuthorSuccessNotificationPreference,
      ).toHaveBeenCalled();
    });
    expect(
      mockedSetCommitAuthorSuccessNotificationPreference.mock.calls[0]?.[0],
    ).toEqual({
      enabled: false,
    });
  });

  it("explains that a disabled personal target pauses commit notifications", async () => {
    mockedGetMyEmailNotificationTarget.mockResolvedValue({
      id: "target-1",
      owner_user_id: "user-1",
      type: "email",
      name: "User Example",
      address: "<user@example.com>",
      webhook_configured: false,
      enabled: false,
      created_at: "2026-06-24T00:00:00Z",
      updated_at: "2026-06-24T00:00:00Z",
    });
    mockedGetCommitAuthorFailureNotificationPreference.mockResolvedValue({
      enabled: true,
      eligible: true,
      delivery_active: false,
      unavailable_reason: "personal_target_disabled",
      target: {
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: false,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T00:00:00Z",
      },
    });

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText(
          /Delivery is paused because your personal email target is disabled/i,
        ),
      ).toBeTruthy();
    });

    const checkbox = screen.getByRole("checkbox", {
      name: /Notify me when my commits fail/i,
    });
    expect(checkbox).toBeChecked();
    expect(checkbox).not.toBeDisabled();

    fireEvent.click(checkbox);

    await waitFor(() => {
      expect(
        mockedSetCommitAuthorFailureNotificationPreference.mock.calls[0]?.[0],
      ).toEqual({
        enabled: false,
      });
    });
  });

  it("does not allow enabling commit notifications when the personal target is disabled", async () => {
    mockedGetMyEmailNotificationTarget.mockResolvedValue({
      id: "target-1",
      owner_user_id: "user-1",
      type: "email",
      name: "User Example",
      address: "<user@example.com>",
      webhook_configured: false,
      enabled: false,
      created_at: "2026-06-24T00:00:00Z",
      updated_at: "2026-06-24T00:00:00Z",
    });
    mockedGetCommitAuthorFailureNotificationPreference.mockResolvedValue({
      enabled: false,
      eligible: true,
      delivery_active: false,
      unavailable_reason: "personal_target_disabled",
      target: {
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: false,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T00:00:00Z",
      },
    });

    renderPage();

    const checkbox = await screen.findByRole("checkbox", {
      name: /Notify me when my commits fail/i,
    });
    expect(checkbox).not.toBeChecked();
    expect(checkbox).toBeDisabled();
    expect(
      mockedSetCommitAuthorFailureNotificationPreference,
    ).not.toHaveBeenCalled();
    expect(
      screen.queryByText(
        /Delivery is paused because your personal email target is disabled/i,
      ),
    ).toBeNull();
  });

  it("allows disabling an enabled commit preference even when the personal target is missing", async () => {
    mockedGetCommitAuthorFailureNotificationPreference.mockResolvedValue({
      enabled: true,
      eligible: false,
      delivery_active: false,
      unavailable_reason: "personal_target_required",
      target: null,
    });

    renderPage();

    const checkbox = await screen.findByRole("checkbox", {
      name: /Notify me when my commits fail/i,
    });
    expect(checkbox).toBeChecked();
    expect(checkbox).not.toBeDisabled();

    fireEvent.click(checkbox);

    await waitFor(() => {
      expect(
        mockedSetCommitAuthorFailureNotificationPreference.mock.calls[0]?.[0],
      ).toEqual({
        enabled: false,
      });
    });
  });

  it("surfaces commit preference update failures clearly", async () => {
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
    mockedGetCommitAuthorFailureNotificationPreference.mockResolvedValue({
      enabled: true,
      eligible: true,
      delivery_active: true,
      unavailable_reason: null,
      target: {
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T00:00:00Z",
      },
    });
    mockedSetCommitAuthorFailureNotificationPreference.mockRejectedValue(
      new Error("conflict"),
    );

    renderPage();

    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: /Notify me when my commits fail/i,
      }),
    );

    await waitFor(() => {
      expect(
        screen.getByText(/Failed to update commit notifications/i),
      ).toBeTruthy();
    });
    expect(
      screen.getByRole("checkbox", { name: /Notify me when my commits fail/i }),
    ).toBeChecked();
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

  it("renders trusted header provider label from auth mode fallback", async () => {
    mockedGetMe.mockResolvedValue({
      auth_mode: "header",
      email_verified: null,
      user: {
        id: "user-1",
        email: "user@example.com",
        display_name: "User Example",
        global_role: "user",
      },
    });

    renderPage({ authMode: "header" });

    await waitFor(() => {
      expect(screen.getByText("Trusted header")).toBeTruthy();
    });
  });

  it("renders trusted header provider label from auth method directly", async () => {
    mockedGetMe.mockResolvedValue({
      auth_mode: "oidc",
      auth_method: "header",
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
      expect(screen.getByText("Trusted header")).toBeTruthy();
    });
  });

  it("renders disabled auth mode provider label from auth mode fallback", async () => {
    mockedGetMe.mockResolvedValue({
      auth_mode: "disabled",
      email_verified: null,
      user: {
        id: "user-1",
        email: "user@example.com",
        display_name: "User Example",
        global_role: "user",
      },
    });

    renderPage({ authMode: "disabled" });

    await waitFor(() => {
      expect(screen.getByText("Disabled auth mode")).toBeTruthy();
    });
  });

  it("renders unknown provider label when neither auth method nor auth mode is available", async () => {
    mockedGetMe.mockResolvedValue({
      auth_mode: "oidc",
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
      expect(screen.getByText("Unknown")).toBeTruthy();
    });
  });

  it("renders notification target loading state", () => {
    mockedGetMyEmailNotificationTarget.mockImplementation(
      () => new Promise(() => undefined) as Promise<null>,
    );

    renderPage();

    expect(
      screen.getByText("Loading personal notification target..."),
    ).toBeTruthy();
  });

  it("renders notification target load failure state", async () => {
    mockedGetMyEmailNotificationTarget.mockRejectedValue(
      new Error("target lookup failed"),
    );

    renderPage();

    await waitFor(() => {
      expect(
        screen.getByText(/Failed to load personal notification target/i),
      ).toBeTruthy();
    });
  });

  it("renders nothing when there is no current user", () => {
    const { container } = renderPage({ currentUser: null });
    expect(container).toBeEmptyDOMElement();
  });
});
