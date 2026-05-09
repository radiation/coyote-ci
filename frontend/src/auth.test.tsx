import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "./auth";
import { useAuth } from "./auth-context";
import { APIError, getAuthConfig, getMe, logoutSession } from "./api";

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    getAuthConfig: vi.fn(),
    getMe: vi.fn(),
    logoutSession: vi.fn(),
  };
});

function AuthConsumer() {
  const {
    authMode,
    authStatus,
    currentUser,
    error,
    isGlobalAdmin,
    login,
    loginAvailable,
    logout,
  } = useAuth();
  return (
    <div>
      <p>Mode: {authMode ?? "none"}</p>
      <p>Status: {authStatus}</p>
      <p>User: {currentUser?.email ?? "none"}</p>
      <p>Error: {error?.message ?? "none"}</p>
      <p>Admin: {isGlobalAdmin ? "yes" : "no"}</p>
      <p>Can login: {loginAvailable ? "yes" : "no"}</p>
      <button type="button" onClick={login}>
        Login
      </button>
      <button type="button" onClick={() => void logout()}>
        Logout
      </button>
    </div>
  );
}

function renderWithAuthProvider(
  ui: React.ReactNode,
  navigate?: (url: string) => void,
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider navigate={navigate}>{ui}</AuthProvider>
    </QueryClientProvider>,
  );
}

describe("AuthProvider", () => {
  const mockedGetAuthConfig = vi.mocked(getAuthConfig);
  const mockedGetMe = vi.mocked(getMe);
  const mockedLogoutSession = vi.mocked(logoutSession);

  beforeEach(() => {
    vi.clearAllMocks();
    mockedGetAuthConfig.mockResolvedValue({
      auth_mode: "oidc",
      login_url: "/auth/login",
    });
    mockedGetMe.mockResolvedValue({
      auth_mode: "oidc",
      user: {
        id: "user-1",
        email: "admin@example.com",
        global_role: "admin",
      },
    });
    mockedLogoutSession.mockResolvedValue();
  });

  it("loads the current user", async () => {
    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Mode: oidc")).toBeTruthy();
      expect(screen.getByText("Status: authenticated")).toBeTruthy();
      expect(screen.getByText("User: admin@example.com")).toBeTruthy();
      expect(screen.getByText("Admin: yes")).toBeTruthy();
    });
  });

  it("sets oidc unauthenticated state when /api/me returns 401", async () => {
    mockedGetMe.mockRejectedValue(new APIError(401, "authentication required"));

    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Mode: oidc")).toBeTruthy();
      expect(screen.getByText("Status: unauthenticated")).toBeTruthy();
      expect(screen.getByText("Can login: yes")).toBeTruthy();
      expect(screen.getByText("User: none")).toBeTruthy();
    });
  });

  it("sets header unauthenticated state without login when /api/me returns 401", async () => {
    mockedGetAuthConfig.mockResolvedValue({
      auth_mode: "header",
      login_url: null,
    });
    mockedGetMe.mockRejectedValue(new APIError(401, "authentication required"));

    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Mode: header")).toBeTruthy();
      expect(screen.getByText("Status: unauthenticated")).toBeTruthy();
      expect(screen.getByText("Can login: no")).toBeTruthy();
      expect(screen.getByText("User: none")).toBeTruthy();
    });

    expect(mockedGetAuthConfig).toHaveBeenCalledTimes(1);
    expect(mockedGetMe).toHaveBeenCalledTimes(1);
  });

  it("surfaces auth config failures without keeping stale user state", async () => {
    mockedGetAuthConfig.mockRejectedValue(
      new Error("failed to load auth config"),
    );

    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Status: error")).toBeTruthy();
      expect(screen.getByText("User: none")).toBeTruthy();
      expect(
        screen.getByText("Error: failed to load auth config"),
      ).toBeTruthy();
    });
  });

  it("navigates to login", async () => {
    const navigate = vi.fn();
    renderWithAuthProvider(<AuthConsumer />, navigate);

    await waitFor(() => {
      expect(screen.getByText("Status: authenticated")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Login" }));

    expect(navigate).toHaveBeenCalledWith("/auth/login");
  });

  it("logs out and clears auth state", async () => {
    mockedGetMe
      .mockResolvedValueOnce({
        auth_mode: "oidc",
        user: {
          id: "user-1",
          email: "admin@example.com",
          global_role: "admin",
        },
      })
      .mockRejectedValueOnce(new APIError(401, "authentication required"));

    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Status: authenticated")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Logout" }));

    await waitFor(() => {
      expect(mockedLogoutSession).toHaveBeenCalled();
      expect(screen.getByText("Status: unauthenticated")).toBeTruthy();
      expect(screen.getByText("User: none")).toBeTruthy();
    });
  });

  it("does not persist auth state in browser storage", async () => {
    const setItemSpy = vi.spyOn(Storage.prototype, "setItem");
    const removeItemSpy = vi.spyOn(Storage.prototype, "removeItem");

    renderWithAuthProvider(<AuthConsumer />);

    await waitFor(() => {
      expect(screen.getByText("Status: authenticated")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Logout" }));

    await waitFor(() => {
      expect(mockedLogoutSession).toHaveBeenCalled();
    });

    expect(setItemSpy).not.toHaveBeenCalled();
    expect(removeItemSpy).not.toHaveBeenCalled();
  });
});
