import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import {
  ensureMyEmailNotificationTarget,
  getCommitAuthorFailureNotificationPreference,
  getCommitAuthorSuccessNotificationPreference,
  getMyEmailNotificationTarget,
  setCommitAuthorFailureNotificationPreference,
  setCommitAuthorSuccessNotificationPreference,
  setMyEmailNotificationTargetEnabled,
} from "../api";
import { AuthContext, type AuthContextValue } from "../auth-context";
import { MyNotificationsPage } from "./MyNotificationsPage";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getMyEmailNotificationTarget: vi.fn(),
    ensureMyEmailNotificationTarget: vi.fn(),
    getCommitAuthorFailureNotificationPreference: vi.fn(),
    getCommitAuthorSuccessNotificationPreference: vi.fn(),
    setMyEmailNotificationTargetEnabled: vi.fn(),
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
          <MyNotificationsPage />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
}

describe("MyNotificationsPage", () => {
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
  const mockedSetMyEmailNotificationTargetEnabled = vi.mocked(
    setMyEmailNotificationTargetEnabled,
  );
  const mockedSetCommitAuthorFailureNotificationPreference = vi.mocked(
    setCommitAuthorFailureNotificationPreference,
  );
  const mockedSetCommitAuthorSuccessNotificationPreference = vi.mocked(
    setCommitAuthorSuccessNotificationPreference,
  );

  beforeEach(() => {
    vi.resetAllMocks();

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
    mockedSetMyEmailNotificationTargetEnabled.mockResolvedValue({
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

  it("renders the no-target state and offers target creation", async () => {
    renderPage();

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "Create my email target" }),
      ).toBeTruthy();
      expect(screen.getByText("user@example.com")).toBeTruthy();
      expect(
        screen.getByText(
          /initial notification preferences will be set from the current instance defaults/i,
        ),
      ).toBeTruthy();
      expect(screen.queryByText("<user@example.com>")).toBeNull();
      expect(
        screen.queryByRole("link", {
          name: /Open Notification administration/i,
        }),
      ).toBeNull();
      expect(screen.queryByText(/Sends to /i)).toBeNull();
      expect(
        screen.getAllByText(
          /Create your personal email target to turn this on/i,
        ).length,
      ).toBe(2);
    });
  });

  it("refreshes target and both preferences after creating a personal target", async () => {
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
      expect(mockedEnsureMyEmailNotificationTarget).toHaveBeenCalledTimes(1);
      expect(
        screen.getByText("Personal to your authenticated account"),
      ).toBeTruthy();
      expect(screen.getByText("user@example.com")).toBeTruthy();
      expect(screen.queryByText("<user@example.com>")).toBeNull();
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
  });

  it("can disable and re-enable the personal target", async () => {
    mockedGetMyEmailNotificationTarget
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
      })
      .mockResolvedValueOnce({
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: false,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T01:00:00Z",
      })
      .mockResolvedValueOnce({
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T02:00:00Z",
      });
    mockedGetCommitAuthorFailureNotificationPreference
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
      })
      .mockResolvedValueOnce({
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
          updated_at: "2026-06-24T01:00:00Z",
        },
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
          updated_at: "2026-06-24T02:00:00Z",
        },
      });
    mockedGetCommitAuthorSuccessNotificationPreference
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
      })
      .mockResolvedValueOnce({
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
          updated_at: "2026-06-24T01:00:00Z",
        },
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
          updated_at: "2026-06-24T02:00:00Z",
        },
      });

    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Disable my email target" }),
    );

    await waitFor(() => {
      expect(
        mockedSetMyEmailNotificationTargetEnabled.mock.calls[0]?.[0],
      ).toEqual({
        enabled: false,
      });
      expect(
        screen.getByRole("button", { name: "Re-enable my email target" }),
      ).toBeTruthy();
      expect(
        screen.getByRole("button", { name: "Re-enable my email target" }),
      ).toHaveClass("secondary-button");
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Re-enable my email target" }),
    );

    await waitFor(() => {
      expect(
        mockedSetMyEmailNotificationTargetEnabled.mock.calls[1]?.[0],
      ).toEqual({
        enabled: true,
      });
      expect(
        screen.getByRole("button", { name: "Disable my email target" }),
      ).toBeTruthy();
      expect(
        screen.getByRole("button", { name: "Disable my email target" }),
      ).toHaveClass("secondary-button", "danger-button");
    });
  });

  it("preserves both checked preferences while target delivery is paused and resumed", async () => {
    mockedGetMyEmailNotificationTarget
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
      })
      .mockResolvedValueOnce({
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: false,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T01:00:00Z",
      })
      .mockResolvedValueOnce({
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T02:00:00Z",
      });
    mockedGetCommitAuthorFailureNotificationPreference
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
      })
      .mockResolvedValueOnce({
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
          updated_at: "2026-06-24T01:00:00Z",
        },
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
          updated_at: "2026-06-24T02:00:00Z",
        },
      });
    mockedGetCommitAuthorSuccessNotificationPreference
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
      })
      .mockResolvedValueOnce({
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
          updated_at: "2026-06-24T01:00:00Z",
        },
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
          updated_at: "2026-06-24T02:00:00Z",
        },
      });

    renderPage();

    const failureCheckbox = await screen.findByRole("checkbox", {
      name: /Notify me when my commits fail/i,
    });
    const successCheckbox = await screen.findByRole("checkbox", {
      name: /Notify me when my commits succeed/i,
    });
    expect(failureCheckbox).toBeChecked();
    expect(successCheckbox).toBeChecked();

    fireEvent.click(
      screen.getByRole("button", { name: "Disable my email target" }),
    );

    await waitFor(() => {
      expect(failureCheckbox).toBeChecked();
      expect(successCheckbox).toBeChecked();
      expect(
        screen.getAllByText(
          /Paused while your personal email target is disabled/i,
        ).length,
      ).toBeGreaterThan(0);
      expect(
        screen.getByRole("button", { name: "Re-enable my email target" }),
      ).toBeTruthy();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Re-enable my email target" }),
    );

    await waitFor(() => {
      expect(failureCheckbox).toBeChecked();
      expect(successCheckbox).toBeChecked();
      expect(
        screen.queryByText(
          /Paused while your personal email target is disabled/i,
        ),
      ).toBeNull();
      expect(
        screen.getByRole("button", { name: "Disable my email target" }),
      ).toBeTruthy();
    });
  });

  it("preserves a mixed failure-success selection while target delivery is paused and resumed", async () => {
    mockedGetMyEmailNotificationTarget
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
      })
      .mockResolvedValueOnce({
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: false,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T01:00:00Z",
      })
      .mockResolvedValueOnce({
        id: "target-1",
        owner_user_id: "user-1",
        type: "email",
        name: "User Example",
        address: "<user@example.com>",
        webhook_configured: false,
        enabled: true,
        created_at: "2026-06-24T00:00:00Z",
        updated_at: "2026-06-24T02:00:00Z",
      });
    mockedGetCommitAuthorFailureNotificationPreference
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
      })
      .mockResolvedValueOnce({
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
          updated_at: "2026-06-24T01:00:00Z",
        },
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
          updated_at: "2026-06-24T02:00:00Z",
        },
      });
    mockedGetCommitAuthorSuccessNotificationPreference
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
      })
      .mockResolvedValueOnce({
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
          updated_at: "2026-06-24T01:00:00Z",
        },
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
          updated_at: "2026-06-24T02:00:00Z",
        },
      });

    renderPage();

    const failureCheckbox = await screen.findByRole("checkbox", {
      name: /Notify me when my commits fail/i,
    });
    const successCheckbox = await screen.findByRole("checkbox", {
      name: /Notify me when my commits succeed/i,
    });
    expect(failureCheckbox).toBeChecked();
    expect(successCheckbox).not.toBeChecked();

    fireEvent.click(
      screen.getByRole("button", { name: "Disable my email target" }),
    );

    await waitFor(() => {
      expect(failureCheckbox).toBeChecked();
      expect(successCheckbox).not.toBeChecked();
      expect(
        screen.getAllByText(
          /Paused while your personal email target is disabled/i,
        ).length,
      ).toBeGreaterThan(0);
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Re-enable my email target" }),
    );

    await waitFor(() => {
      expect(failureCheckbox).toBeChecked();
      expect(successCheckbox).not.toBeChecked();
      expect(
        screen.queryByText(
          /Paused while your personal email target is disabled/i,
        ),
      ).toBeNull();
    });
  });

  it("shows paused delivery and allows disabling an enabled failure preference while the target is disabled", async () => {
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
          /Paused while your personal email target is disabled/i,
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
      ).toEqual({ enabled: false });
    });
  });

  it("does not allow enabling a disabled failure preference while the target is disabled", async () => {
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
      unavailable_reason: "personal_target_disabled",
    });

    renderPage();

    const checkbox = await screen.findByRole("checkbox", {
      name: /Notify me when my commits fail/i,
    });
    expect(checkbox).not.toBeChecked();
    expect(checkbox).toBeDisabled();
    expect(
      screen.getByText(
        /Re-enable your personal email target before turning this on/i,
      ),
    ).toBeTruthy();
    expect(
      mockedSetCommitAuthorFailureNotificationPreference,
    ).not.toHaveBeenCalled();
  });

  it("keeps success notifications independent from failure notifications", async () => {
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

    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: /Notify me when my commits succeed/i,
      }),
    );

    await waitFor(() => {
      expect(
        mockedSetCommitAuthorSuccessNotificationPreference.mock.calls[0]?.[0],
      ).toEqual({ enabled: false });
    });
    expect(
      mockedSetCommitAuthorFailureNotificationPreference,
    ).not.toHaveBeenCalled();
  });

  it("preserves the visible target state when target mutation fails", async () => {
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
    mockedSetMyEmailNotificationTargetEnabled.mockRejectedValue(
      new Error("conflict"),
    );

    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Disable my email target" }),
    );

    await waitFor(() => {
      expect(
        screen.getByText(/Failed to update personal email target/i),
      ).toBeTruthy();
    });
    expect(
      screen.getByRole("button", { name: "Disable my email target" }),
    ).toBeTruthy();
  });

  it("shows the admin link only for authorized users", async () => {
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

    renderPage({ isGlobalAdmin: true });

    await waitFor(() => {
      expect(
        screen.getByRole("link", { name: /Open Notification administration/i }),
      ).toHaveAttribute("href", "/settings/notifications");
    });
  });

  it("does not render raw angle-bracket email formatting when a target exists", async () => {
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
      expect(screen.getAllByText("user@example.com").length).toBeGreaterThan(0);
      expect(screen.queryByText("<user@example.com>")).toBeNull();
    });
  });
});
